package app

import (
	"context"
	"fmt"
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
	}
}

func (n notifier) warn(msg string, keysAndValues ...any) {
	if n.Warn != nil {
		n.Warn(msg, keysAndValues...)
	}
}

// Notify sends the listing, with AI commentary when enabled, then its
// location as a map pin replying to it.
func (n notifier) Notify(ctx context.Context, alert db.Alert, post divar.Listing) error {
	var evaluation *ai.EvaluationResult
	var details divar.PostImportantData
	raw, err := n.GetPost(ctx, post.Token)
	if err == nil {
		details = divar.ExtractImportantData(raw)
		if alert.AIEnabled {
			history, _ := n.History(alert.ID)
			evaluation, err = n.Evaluate(ctx, details, history, analysisGoal(alert))
		}
	}
	if err != nil {
		evaluation = nil
		n.warn("Listing details or commentary unavailable", "alert_id", alert.ID, "error", err)
	}
	messageID, err := n.Send(ctx, alert.Chat, post.ImageURL, listingText(alert, post, evaluation))
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
	goal := "Judge how well this listing matches the owner's request and whether its price is good compared with similar earlier listings for this alert. Point out anything the owner should check before contacting the seller. Write the explanation briefly in Persian."
	if request := strings.TrimSpace(alert.OriginalPrompt); request != "" {
		goal += "\n\nOwner's request:\n" + request
	}
	if preferences := strings.TrimSpace(alert.AnalysisPrompt); preferences != "" {
		goal += "\n\nOwner's extra preferences:\n" + preferences
	}
	return goal
}

func listingText(alert db.Alert, post divar.Listing, evaluation *ai.EvaluationResult) string {
	var text strings.Builder
	fmt.Fprintf(&text, "آگهی تازه برای «%s»\n\n%s\n", alert.Title, post.Title)
	for _, line := range []string{post.TopDescriptionText, post.MiddleDescriptionText, post.BottomDescriptionText} {
		if line != "" {
			text.WriteString(line + "\n")
		}
	}
	if evaluation != nil {
		fmt.Fprintf(&text, "\nتحلیل آگهی\nامتیاز: %d از ۱۰۰\nپیشنهاد: %s\nتوضیح: %s\n", evaluation.Score, evaluation.Decision, evaluation.Explanation)
	} else if alert.AIEnabled {
		text.WriteString("\nتحلیل فعلاً در دسترس نیست؛ این آگهی را بدون امتیاز می‌فرستم.\n")
	}
	fmt.Fprintf(&text, "\nhttps://divar.ir/v/%s", post.Token)
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
// text when there is no image or the photo is rejected.
func (a *App) sendListing(ctx context.Context, chat platform.Chat, imageURL, text string) (int, error) {
	b, err := a.botFor(chat)
	if err != nil {
		return 0, err
	}
	if imageURL != "" {
		message, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:  chat.ID,
			Photo:   &models.InputFileString{Data: imageURL},
			Caption: truncate(text, maxCaptionLength),
		})
		if err == nil {
			return message.ID, nil
		}
	}
	message, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chat.ID, Text: truncate(text, maxMessageLength)})
	if err != nil {
		return 0, err
	}
	return message.ID, nil
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

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}
