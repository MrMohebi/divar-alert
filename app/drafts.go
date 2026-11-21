package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
)

// turnTimeout bounds one AI conversation turn.
const turnTimeout = 2 * time.Minute

func (a *App) saveDraft(chat platform.Chat, draft *conversation.AlertDraft) error {
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return a.store.SaveDraft(chat, string(data))
}

// loadDraft returns the chat's draft and its stored JSON, or nil if none.
func (a *App) loadDraft(chat platform.Chat) (*conversation.AlertDraft, string, error) {
	raw, err := a.store.Draft(chat)
	if err != nil || raw == "" {
		return nil, raw, err
	}
	var draft conversation.AlertDraft
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		return nil, raw, err
	}
	return &draft, raw, nil
}

// startDraft begins a new alert, or resumes the unfinished one instead of
// silently erasing the user's answers.
func (a *App) startDraft(ctx context.Context, b *bot.Bot, chat platform.Chat) {
	if chat.ID < 0 {
		a.send(ctx, b, chat, "برای ساخت اعلان، در گفت‌وگوی خصوصی پیام بده.", nil)
		return
	}
	draft, _, err := a.loadDraft(chat)
	if err != nil {
		a.send(ctx, b, chat, "دریافت گفت‌وگوی قبلی ممکن نشد. کمی بعد دوباره تلاش کن.", nil)
		return
	}
	if draft == nil {
		draft, err = conversation.NewDraft()
		if err == nil {
			err = a.saveDraft(chat, draft)
		}
		if err != nil {
			a.send(ctx, b, chat, "شروع گفت‌وگو ممکن نشد. دوباره تلاش کن.", nil)
			return
		}
	}
	a.sendDraft(ctx, b, chat, draft)
}

// optionWords lets users answer a question by its position ("اول", "2", ...).
var optionWords = [][]string{
	{"1", "اول", "اولی", "گزینه اول", "گزینه 1", "مورد اول"},
	{"2", "دوم", "دومی", "گزینه دوم", "گزینه 2", "مورد دوم"},
	{"3", "سوم", "سومی", "گزینه سوم", "گزینه 3", "مورد سوم"},
}

// continueDraft feeds the user's text into the chat's draft, starting one if
// needed.
func (a *App) continueDraft(ctx context.Context, b *bot.Bot, chat platform.Chat, text string) {
	draft, _, err := a.loadDraft(chat)
	if err != nil {
		a.send(ctx, b, chat, "گفت‌وگو فعلاً در دسترس نیست. کمی بعد دوباره تلاش کن.", nil)
		return
	}
	if draft == nil {
		if draft, err = conversation.NewDraft(); err != nil {
			return
		}
	}
	if len([]rune(text)) > 4000 {
		a.send(ctx, b, chat, "لطفاً خواسته‌ات را کوتاه‌تر، در یک پیام تا چهار هزار حرف بنویس.", nil)
		return
	}
	if draft.State == "question" && draft.Plan != nil && draft.Plan.Question != nil {
		normalized := strings.TrimSpace(divar.NormalizePersian(text))
		for i, words := range optionWords {
			for _, word := range words {
				if normalized == word && i < len(draft.Plan.Question.Options) {
					text = draft.Plan.Question.Options[i]
				}
			}
		}
	}
	a.advanceDraft(ctx, b, chat, draft, text)
}

// advanceDraft runs one AI turn. The user's text is saved before the remote
// call, so a failure or restart can retry the same turn.
func (a *App) advanceDraft(ctx context.Context, b *bot.Bot, chat platform.Chat, draft *conversation.AlertDraft, text string) {
	_, _ = b.SendChatAction(ctx, &bot.SendChatActionParams{ChatID: chat.ID, Action: models.ChatActionTyping})
	if text != "" {
		if draft.OriginalPrompt == "" {
			draft.OriginalPrompt = text
		}
		// Advance appends the message itself; keep a durable copy in the
		// error state until it finishes.
		backup := *draft
		backup.Revision++
		backup.State = "error"
		backup.Messages = append(append([]ai.Message(nil), draft.Messages...), ai.Message{Role: "user", Content: text})
		if err := a.saveDraft(chat, &backup); err != nil {
			a.send(ctx, b, chat, "ذخیرهٔ پاسخ ممکن نشد؛ لطفاً دوباره بفرست.", nil)
			return
		}
	}
	turnCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	a.activeTurns.Store(chat, context.CancelFunc(cancel))
	defer func() { a.activeTurns.Delete(chat); cancel() }()
	if err := a.conv.Advance(turnCtx, draft, text); err != nil {
		a.log.Warnw("Conversation turn failed", "chat", chat, "error", err)
		draft.State = "error"
	}
	if err := a.saveDraft(chat, draft); err != nil {
		a.log.Errorw("Failed to save draft", "chat", chat, "error", err)
		a.send(ctx, b, chat, "ذخیرهٔ پاسخ ممکن نشد؛ لطفاً دوباره بفرست.", nil)
		return
	}
	a.sendDraft(ctx, b, chat, draft)
}

func draftCallback(d *conversation.AlertDraft, action string) string {
	return fmt.Sprintf("draft:%s:%d:%s", d.ID, d.Revision, action)
}

func (a *App) draftView(d *conversation.AlertDraft) (string, *models.InlineKeyboardMarkup) {
	rows := [][]models.InlineKeyboardButton{}
	button := func(label, action string) models.InlineKeyboardButton {
		return models.InlineKeyboardButton{Text: label, CallbackData: draftCallback(d, action)}
	}
	var text string
	switch d.State {
	case "question":
		if d.Plan == nil || d.Plan.Question == nil {
			return "خواسته‌ات را دوباره بنویس تا ادامه بدهیم.", nil
		}
		q := d.Plan.Question
		text = q.Text
		for i, label := range q.Options {
			rows = append(rows, []models.InlineKeyboardButton{button(label, "o"+strconv.Itoa(i))})
		}
		text += "\n\nمی‌توانی پاسخت را هم بنویسی."
	case "confirm":
		text = a.conv.Summary(d)
		rows = append(rows, []models.InlineKeyboardButton{button("تأیید و شروع", "confirm"), button("اصلاح", "edit"), button("انصراف", "cancel")})
	case "edit":
		text = "چه چیزی را تغییر بدهم؟ اصلاحاتت را بنویس؛ بقیهٔ خواسته‌ها را نگه می‌دارم."
	case "error":
		text = "الان نتوانستم درخواستت را کامل بررسی کنم. پاسخ‌هایت ذخیره شده؛ دوباره تلاش کنیم؟"
		rows = append(rows, []models.InlineKeyboardButton{button("تلاش دوباره", "retry"), button("انصراف", "cancel")})
	default:
		text = "چه آگهی‌ای می‌خواهی پیدا کنی؟ خواسته‌ات را با اسم شهر بنویس؛ مثلاً یک آپارتمان اجاره‌ای در کرمان با بودجه‌ای که در نظر داری."
	}
	if d.State != "confirm" && d.State != "error" {
		rows = append(rows, []models.InlineKeyboardButton{button("انصراف", "cancel")})
	}
	return text, &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (a *App) sendDraft(ctx context.Context, b *bot.Bot, chat platform.Chat, d *conversation.AlertDraft) {
	text, keyboard := a.draftView(d)
	parts := splitText(text, maxMessageLength)
	for _, part := range parts[:len(parts)-1] {
		a.send(ctx, b, chat, part, nil)
	}
	a.send(ctx, b, chat, parts[len(parts)-1], keyboard)
}

// handleDraftButton handles the buttons under a draft message. Buttons carry
// the draft's revision, so a button from an older message is refused.
func (a *App) handleDraftButton(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	draft, raw, err := a.loadDraft(chat)
	parts := strings.Split(update.CallbackQuery.Data, ":")
	if err != nil || draft == nil || len(parts) != 4 || parts[1] != draft.ID || parts[2] != strconv.Itoa(draft.Revision) {
		a.answerCallback(ctx, b, update, "این گزینه قدیمی شده؛ از آخرین پیام استفاده کن.", true)
		return
	}
	action := parts[3]
	if action == "confirm" {
		a.confirmDraft(ctx, b, chat, update, draft, raw)
		return
	}
	a.answerCallback(ctx, b, update, "", false)
	switch {
	case action == "cancel":
		if err := a.store.DeleteDraft(chat); err != nil {
			a.send(ctx, b, chat, "لغو درخواست ممکن نشد. دوباره تلاش کن.", nil)
			return
		}
		a.clearKeyboard(ctx, b, chat, update)
		a.send(ctx, b, chat, "باشه، ساخت این اعلان لغو شد.", mainMenuKeyboard())
	case action == "edit" && draft.State == "confirm":
		draft.Revision++
		draft.State = "edit"
		if err := a.saveDraft(chat, draft); err != nil {
			return
		}
		a.clearKeyboard(ctx, b, chat, update)
		a.sendDraft(ctx, b, chat, draft)
	case action == "retry" && draft.State == "error":
		a.clearKeyboard(ctx, b, chat, update)
		a.advanceDraft(ctx, b, chat, draft, "")
	case strings.HasPrefix(action, "o") && draft.State == "question":
		index, err := strconv.Atoi(strings.TrimPrefix(action, "o"))
		if err != nil || draft.Plan == nil || draft.Plan.Question == nil || index < 0 || index >= len(draft.Plan.Question.Options) {
			return
		}
		a.clearKeyboard(ctx, b, chat, update)
		a.advanceDraft(ctx, b, chat, draft, draft.Plan.Question.Options[index])
	}
}

// confirmDraft turns the confirmed draft into an alert with AI commentary on.
func (a *App) confirmDraft(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update, draft *conversation.AlertDraft, raw string) {
	if draft.State != "confirm" || draft.Query == nil || draft.Plan == nil {
		a.answerCallback(ctx, b, update, "این درخواست هنوز آمادهٔ تأیید نیست.", true)
		return
	}
	query, err := json.Marshal(draft.Query)
	if err != nil {
		return
	}
	state, _ := json.Marshal(WatchState{Boundary: time.Now().UTC()})
	alert := db.Alert{
		ID:             time.Now().UnixNano(),
		Chat:           chat,
		Title:          draft.Plan.Title,
		SearchQuery:    string(query),
		OriginalPrompt: draft.OriginalPrompt,
		AIEnabled:      true,
		AnalysisPrompt: strings.Join(draft.Plan.AdditionalPreferences, "\n"),
		Interval:       draft.Plan.Interval,
		WatchState:     string(state),
	}
	if err := a.store.ConfirmDraft(chat, raw, alert); err != nil {
		a.log.Warnw("Failed to confirm draft", "chat", chat, "error", err)
		a.answerCallback(ctx, b, update, "ثبت اعلان ممکن نشد؛ لطفاً دوباره تلاش کن.", true)
		return
	}
	a.answerCallback(ctx, b, update, "اعلان ثبت شد.", false)
	a.clearKeyboard(ctx, b, chat, update)
	a.send(ctx, b, chat, "اعلانت فعال شد. آگهی‌های فعلی را بررسی می‌کنم و بعد، آگهی‌های تازه را خبر می‌دهم.", mainMenuKeyboard())
}
