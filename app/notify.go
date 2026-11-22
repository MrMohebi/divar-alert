package app

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
)

// Bot API limits.
const (
	maxMessageLength = 4096
	maxCaptionLength = 1024
	// maxLocationAccuracy is the largest horizontal_accuracy, in meters.
	maxLocationAccuracy = 1500
)

// notifier sends one listing to its alert's chat. AI commentary and the map
// pin are extras: their failures never stop the listing from being sent.
type notifier struct {
	GetPost       func(context.Context, string) (divar.PostDetailRes, error)
	Evaluate      func(ctx context.Context, post any, history []any, goal string) (*ai.EvaluationResult, error)
	History       func(alertID int64) ([]any, error)
	AppendHistory func(alertID int64, post any) error
	// Send delivers the listing and returns the sent message's ID.
	Send         func(ctx context.Context, chat platform.Chat, imageURL, text string) (int, error)
	SendLocation func(ctx context.Context, chat platform.Chat, replyTo int, location divar.LocationInfo) error
	Warn         func(msg string, keysAndValues ...any)
	// RentInflation is the yearly inflation, in percent, used to price rentals.
	RentInflation float64
}

func (a *App) notifier() notifier {
	return notifier{
		GetPost:       a.getPost,
		Evaluate:      a.evaluate,
		History:       a.store.LoadHistory,
		AppendHistory: a.store.AppendHistory,
		Send:          a.sendListing,
		SendLocation:  a.sendLocation,
		Warn:          a.log.Warnw,
		RentInflation: a.rentInflation,
	}
}

func (n notifier) warn(msg string, keysAndValues ...any) {
	if n.Warn != nil {
		n.Warn(msg, keysAndValues...)
	}
}

// Notify sends the listing, with a rental's real monthly rent and AI
// commentary when enabled, then its location as a map pin replying to it.
func (n notifier) Notify(ctx context.Context, alert db.Alert, post divar.Listing) error {
	var evaluation *ai.EvaluationResult
	var details divar.PostImportantData
	raw, err := n.GetPost(ctx, post.Token)
	if err == nil {
		details = divar.ExtractImportantData(raw)
	}
	quote := quoteRent(details, post, n.RentInflation)
	if quote != nil {
		details.Price.RealMonthlyRent = int64(math.Round(quote.Real))
	}
	if err == nil {
		if alert.AIEnabled {
			history, _ := n.History(alert.ID)
			evaluation, err = n.Evaluate(ctx, details, history, analysisGoal(alert))
		}
	}
	if err != nil {
		evaluation = nil
		n.warn("Listing details or commentary unavailable", "alert_id", alert.ID, "error", err)
	}
	messageID, err := n.Send(ctx, alert.Chat, post.ImageURL, listingText(alert, post, quote, evaluation))
	if err != nil {
		return err
	}
	if details.Location.HasPoint() && n.SendLocation != nil {
		if err := n.SendLocation(ctx, alert.Chat, messageID, details.Location); err != nil {
			n.warn("Unable to send listing location", "alert_id", alert.ID, "error", err)
		}
	}
	if alert.AIEnabled && details.Token != "" {
		if err := n.AppendHistory(alert.ID, details); err != nil {
			n.warn("Unable to save commentary history", "alert_id", alert.ID, "error", err)
		}
	}
	return nil
}

// analysisGoal asks the AI to judge a listing against the owner's request.
func analysisGoal(alert db.Alert) string {
	goal := "Judge how well this listing matches the owner's request and whether its price is good compared with similar earlier listings for this alert. Point out anything the owner should check before contacting the seller. Write everything briefly in Persian."
	if request := strings.TrimSpace(alert.OriginalPrompt); request != "" {
		goal += "\n\nOwner's request:\n" + request
	}
	if preferences := strings.TrimSpace(alert.AnalysisPrompt); preferences != "" {
		goal += "\n\nOwner's extra preferences:\n" + preferences
	}
	return goal
}

// listingText is the listing message. Bold spans are marked, not rendered;
// sendListing renders them for the chat's platform.
func listingText(alert db.Alert, post divar.Listing, quote *rentQuote, evaluation *ai.EvaluationResult) string {
	var text strings.Builder
	fmt.Fprintf(&text, "آگهی تازه برای «%s»\n\n%s\n", plainText(alert.Title), bold(post.Title))
	for _, line := range []string{post.TopDescriptionText, post.MiddleDescriptionText, post.BottomDescriptionText} {
		if line != "" {
			text.WriteString(boldValue(line) + "\n")
		}
	}
	if quote != nil {
		text.WriteString("\n" + quote.text())
	}
	if evaluation != nil {
		text.WriteString("\n" + evaluationText(evaluation))
	} else if alert.AIEnabled {
		text.WriteString("\nتحلیل فعلاً در دسترس نیست؛ این آگهی را بدون امتیاز می‌فرستم.\n")
	}
	fmt.Fprintf(&text, "\nhttps://divar.ir/v/%s", post.Token)
	return text.String()
}

// evaluationText shows the AI analysis: the verdict, then strengths and
// concerns as bullet points, then a short summary.
func evaluationText(e *ai.EvaluationResult) string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s\nامتیاز: %s\nپیشنهاد: %s\n", bold("تحلیل آگهی"), bold(fmt.Sprintf("%d از ۱۰۰", e.Score)), richText(e.Decision))
	for _, list := range []struct {
		title  string
		points []string
	}{{"✅ نقاط قوت", e.Strengths}, {"⚠️ نکات قابل توجه", e.Concerns}} {
		if len(list.points) == 0 {
			continue
		}
		text.WriteString("\n" + bold(list.title) + "\n")
		for _, point := range list.points {
			text.WriteString("• " + richText(point) + "\n")
		}
	}
	if summary := richText(e.Explanation); summary != "" {
		text.WriteString("\n" + summary + "\n")
	}
	return text.String()
}

func (a *App) botFor(chat platform.Chat) (*bot.Bot, error) {
	b, ok := a.bots[chat.Platform]
	if !ok {
		return nil, fmt.Errorf("no bot configured for platform %q", chat.Platform)
	}
	return b, nil
}

// sendListing sends the listing as a photo with caption, falling back to
// text when there is no image or the photo is rejected. Text longer than the
// caption or message limit continues in follow-up messages, so nothing (in
// particular the listing link at the end) is cut off. It returns the ID of
// the first message sent.
func (a *App) sendListing(ctx context.Context, chat platform.Chat, imageURL, text string) (int, error) {
	b, err := a.botFor(chat)
	if err != nil {
		return 0, err
	}
	if imageURL != "" {
		caption := balanceBold(splitText(text, maxCaptionLength))
		body, mode := render(chat.Platform, caption[0])
		message, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:    chat.ID,
			Photo:     &models.InputFileString{Data: imageURL},
			Caption:   body,
			ParseMode: mode,
		})
		if err == nil {
			rest := strings.Join(caption[1:], "\n")
			if rest != "" {
				if _, err := a.sendTexts(ctx, b, chat, rest); err != nil {
					return message.ID, err
				}
			}
			return message.ID, nil
		}
	}
	return a.sendTexts(ctx, b, chat, text)
}

// sendTexts sends marked text as one or more formatted messages within the
// message limit and returns the first message's ID. A part whose formatting
// is rejected is resent as plain text, so formatting never loses a listing.
func (a *App) sendTexts(ctx context.Context, b *bot.Bot, chat platform.Chat, text string) (int, error) {
	firstID := 0
	for _, part := range balanceBold(splitText(text, maxMessageLength)) {
		body, mode := render(chat.Platform, part)
		message, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chat.ID, Text: body, ParseMode: mode})
		if err != nil && mode != "" {
			message, err = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chat.ID, Text: plainText(part)})
		}
		if err != nil {
			return firstID, err
		}
		if firstID == 0 {
			firstID = message.ID
		}
	}
	return firstID, nil
}

// sendLocation sends a map pin replying to the listing. An approximate
// location carries its radius, drawn as an accuracy circle.
func (a *App) sendLocation(ctx context.Context, chat platform.Chat, replyTo int, location divar.LocationInfo) error {
	b, err := a.botFor(chat)
	if err != nil {
		return err
	}
	params := &bot.SendLocationParams{
		ChatID:             chat.ID,
		Latitude:           location.Latitude,
		Longitude:          location.Longitude,
		HorizontalAccuracy: min(location.RadiusMeters, maxLocationAccuracy),
	}
	if replyTo != 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}
	_, err = b.SendLocation(ctx, params)
	return err
}

// splitText cuts text into parts of at most limit characters, preferring to
// break at a line break, then at a space, so words and links stay whole. It
// always returns at least one part.
func splitText(text string, limit int) []string {
	runes := []rune(strings.TrimSpace(text))
	var parts []string
	for len(runes) > limit {
		cut := lastBreak(runes[:limit+1], '\n', limit/2)
		if cut < 0 {
			cut = lastBreak(runes[:limit+1], ' ', limit/2)
		}
		next := cut + 1
		if cut < 0 {
			cut, next = limit, limit
		}
		if part := strings.TrimSpace(string(runes[:cut])); part != "" {
			parts = append(parts, part)
		}
		runes = []rune(strings.TrimLeft(string(runes[next:]), " \n"))
	}
	if len(runes) > 0 || len(parts) == 0 {
		parts = append(parts, string(runes))
	}
	return parts
}

// lastBreak returns the index of the last sep in runes at or after from, or -1.
func lastBreak(runes []rune, sep rune, from int) int {
	for i := len(runes) - 1; i >= from; i-- {
		if runes[i] == sep {
			return i
		}
	}
	return -1
}
