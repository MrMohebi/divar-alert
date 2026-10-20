package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
)

var conversationService *conversation.Service
var chatLocks sync.Map
var activeTurns sync.Map

// All updates that can modify a draft share this lock, including /cancel.
func serialized(handler bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		var chatID int64
		if update.Message != nil {
			chatID = update.Message.Chat.ID
		} else if id, _, ok := callbackMessage(update); ok {
			chatID = id
		}
		if update.Message != nil && update.Message.Text == "/cancel" {
			if cancel, ok := activeTurns.Load(chatID); ok {
				cancel.(context.CancelFunc)()
			}
		}
		value, _ := chatLocks.LoadOrStore(chatID, &sync.Mutex{})
		lock := value.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()
		handler(ctx, b, update)
	}
}

func saveDraft(chatID int64, draft *conversation.AlertDraft) error {
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return database.UpsertProcess(chatID, conversation.ProcessKey, string(data), time.Now().Unix())
}

func loadDraft(chatID int64) (*conversation.AlertDraft, *db.Process, error) {
	process, err := database.GetProcess(chatID)
	if err != nil || process == nil {
		return nil, process, err
	}
	if process.ProcessKey != conversation.ProcessKey {
		return nil, process, nil
	}
	var draft conversation.AlertDraft
	if err = json.Unmarshal([]byte(process.ProcessData), &draft); err != nil {
		return nil, process, err
	}
	return &draft, process, nil
}

func startNaturalAlert(ctx context.Context, b *bot.Bot, chatID int64) {
	if chatID < 0 {
		sendMessage(ctx, b, chatID, "برای ساخت اعلان، در گفت‌وگوی خصوصی پیام بده.", nil)
		return
	}
	// Resume unfinished work instead of silently erasing the user's answers.
	draft, _, err := loadDraft(chatID)
	if err == nil && draft != nil {
		sendDraft(ctx, b, chatID, draft)
		return
	}
	if err != nil {
		sendMessage(ctx, b, chatID, "دریافت گفت‌وگوی قبلی ممکن نشد. کمی بعد دوباره تلاش کن.", nil)
		return
	}
	draft, err = conversation.NewDraft()
	if err == nil {
		err = saveDraft(chatID, draft)
	}
	if err != nil {
		sendMessage(ctx, b, chatID, "شروع گفت‌وگو ممکن نشد. دوباره تلاش کن.", nil)
		return
	}
	sendDraft(ctx, b, chatID, draft)
}

func handleNaturalText(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	draft, _, err := loadDraft(chatID)
	if err != nil {
		sendMessage(ctx, b, chatID, "گفت‌وگو فعلاً در دسترس نیست. کمی بعد دوباره تلاش کن.", nil)
		return
	}
	if draft == nil {
		draft, err = conversation.NewDraft()
		if err != nil {
			return
		}
	}
	if len([]rune(text)) > 4000 {
		sendMessage(ctx, b, chatID, "لطفاً خواسته‌ات را کوتاه‌تر، در یک پیام تا چهار هزار حرف بنویس.", nil)
		return
	}
	if draft.State == "question" && draft.Plan != nil && draft.Plan.Question != nil {
		normalized := strings.TrimSpace(divar.NormalizePersian(text))
		for i, words := range [][]string{{"1", "اول", "اولی", "گزینه اول", "گزینه 1", "مورد اول"}, {"2", "دوم", "دومی", "گزینه دوم", "گزینه 2", "مورد دوم"}, {"3", "سوم", "سومی", "گزینه سوم", "گزینه 3", "مورد سوم"}} {
			for _, word := range words {
				if normalized == word && i < len(draft.Plan.Question.Options) {
					text = draft.Plan.Question.Options[i]
				}
			}
		}
	}
	advanceNatural(ctx, b, chatID, draft, text)
}

func advanceNatural(ctx context.Context, b *bot.Bot, chatID int64, draft *conversation.AlertDraft, text string) {
	_, _ = b.SendChatAction(ctx, &bot.SendChatActionParams{ChatID: chatID, Action: models.ChatActionTyping})
	// Persist incoming text before remote work. A restart can retry the same turn.
	if text != "" {
		if draft.OriginalPrompt == "" {
			draft.OriginalPrompt = text
		}
		// Advance owns appending; keep a durable error-state copy until it finishes.
		backup := *draft
		backup.Revision++
		backup.State = "error"
		backup.Messages = append(append([]ai.Message(nil), draft.Messages...), ai.Message{Role: "user", Content: text})
		if err := saveDraft(chatID, &backup); err != nil {
			sendMessage(ctx, b, chatID, "ذخیرهٔ پاسخ ممکن نشد؛ لطفاً دوباره بفرست.", nil)
			return
		}
	}
	turnCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	activeTurns.Store(chatID, context.CancelFunc(cancel))
	defer func() { activeTurns.Delete(chatID); cancel() }()
	err := conversationService.Advance(turnCtx, draft, text)
	if err != nil {
		sugar.Warnw("Conversation turn failed", "chat_id", chatID, "error", err)
		draft.State = "error"
	}
	if saveErr := saveDraft(chatID, draft); saveErr != nil {
		sugar.Errorw("Failed to persist draft", "error", saveErr)
		sendMessage(ctx, b, chatID, "ذخیرهٔ پاسخ ممکن نشد؛ لطفاً دوباره بفرست.", nil)
		return
	}
	sendDraft(ctx, b, chatID, draft)
}

func draftCallback(d *conversation.AlertDraft, action string) string {
	return fmt.Sprintf("draft:%s:%d:%s", d.ID, d.Revision, action)
}

func draftView(d *conversation.AlertDraft) (string, *models.InlineKeyboardMarkup) {
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
		text = conversationService.Summary(d)
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

func sendDraft(ctx context.Context, b *bot.Bot, chatID int64, d *conversation.AlertDraft) {
	text, keyboard := draftView(d)
	runes := []rune(text)
	for len(runes) > 4096 {
		sendMessage(ctx, b, chatID, string(runes[:4096]), nil)
		runes = runes[4096:]
	}
	sendMessage(ctx, b, chatID, string(runes), keyboard)
}

func handlerCallbackDraft(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, _, ok := callbackMessage(update)
	if !ok {
		return
	}
	draft, process, err := loadDraft(chatID)
	parts := strings.Split(update.CallbackQuery.Data, ":")
	if err != nil || draft == nil || len(parts) != 4 || parts[1] != draft.ID || parts[2] != strconv.Itoa(draft.Revision) {
		answerCallback(ctx, b, update, "این گزینه قدیمی شده؛ از آخرین پیام استفاده کن.", true)
		return
	}
	action := parts[3]
	if action == "confirm" {
		if draft.State != "confirm" || draft.Query == nil || draft.Plan == nil {
			answerCallback(ctx, b, update, "این درخواست هنوز آمادهٔ تأیید نیست.", true)
			return
		}
		query, err := json.Marshal(draft.Query)
		if err != nil {
			return
		}
		state, _ := json.Marshal(WatchState{Boundary: time.Now().UTC()})
		alert := db.Alert{Id: time.Now().UnixNano(), ChatId: chatID, Title: draft.Plan.Title, Interval: draft.Plan.Interval, SearchQuery: string(query), OriginalPrompt: draft.OriginalPrompt, WatchState: string(state), AIEnabled: len(draft.Plan.AdditionalPreferences) > 0, AnalysisPrompt: strings.Join(draft.Plan.AdditionalPreferences, "\n")}
		if err = database.ConfirmDraft(chatID, conversation.ProcessKey, process.ProcessData, alert); err != nil {
			answerCallback(ctx, b, update, "ثبت اعلان ممکن نشد؛ لطفاً دوباره تلاش کن.", true)
			return
		}
		answerCallback(ctx, b, update, "اعلان ثبت شد.", false)
		clearCallbackKeyboard(ctx, b, update)
		sendMessage(ctx, b, chatID, "اعلانت فعال شد. آگهی‌های فعلی را بررسی می‌کنم و بعد، آگهی‌های تازه را خبر می‌دهم.", mainMenuKeyboard())
		return
	}
	answerCallback(ctx, b, update, "", false)
	switch {
	case action == "cancel":
		if err = database.DeleteProcess(chatID); err != nil {
			sendMessage(ctx, b, chatID, "لغو درخواست ممکن نشد. دوباره تلاش کن.", nil)
			return
		}
		clearCallbackKeyboard(ctx, b, update)
		sendMessage(ctx, b, chatID, "باشه، ساخت این اعلان لغو شد.", mainMenuKeyboard())
	case action == "edit" && draft.State == "confirm":
		draft.Revision++
		draft.State = "edit"
		if err = saveDraft(chatID, draft); err != nil {
			return
		}
		clearCallbackKeyboard(ctx, b, update)
		sendDraft(ctx, b, chatID, draft)
	case action == "retry" && draft.State == "error":
		clearCallbackKeyboard(ctx, b, update)
		advanceNatural(ctx, b, chatID, draft, "")
	case strings.HasPrefix(action, "o") && draft.State == "question":
		index, e := strconv.Atoi(strings.TrimPrefix(action, "o"))
		if e != nil || draft.Plan == nil || draft.Plan.Question == nil || index < 0 || index >= len(draft.Plan.Question.Options) {
			return
		}
		clearCallbackKeyboard(ctx, b, update)
		advanceNatural(ctx, b, chatID, draft, draft.Plan.Question.Options[index])
	}
}
