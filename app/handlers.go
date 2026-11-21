package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/platform"
)

const welcomeText = `ربات اعلان دیوار

خواسته‌ات را با اسم شهر بنویس تا اعلانش را بسازیم. از «اعلان جدید» هم می‌توانی شروع کنی. برای مدیریت اعلان‌ها و روشن یا خاموش کردن تحلیل هوش مصنوعی، «اعلان‌های من» را انتخاب کنید.

در هر مرحله می‌توانید /cancel را ارسال کنید.`

type command struct {
	names       []string
	description string
	handler     handlerFunc
}

// commands lists the slash commands; the first name of each is published to
// the platform's command menu.
func (a *App) commands() []command {
	return []command{
		{[]string{"start"}, "نمایش راهنما و منوی اصلی", a.handleStart},
		{[]string{"alert_set", "alertSet"}, "ساخت اعلان جدید", a.handleNewAlert},
		{[]string{"alerts", "alertList"}, "مدیریت اعلان‌ها", a.handleAlertList},
		{[]string{"cancel"}, "لغو عملیات فعلی", a.handleCancel},
		{[]string{"help"}, "نمایش راهنما", a.handleStart},
	}
}

type callbackRoute struct {
	prefix  string
	match   bot.MatchType
	handler handlerFunc
}

func (a *App) callbackRoutes() []callbackRoute {
	return []callbackRoute{
		{"draft:", bot.MatchTypePrefix, a.handleDraftButton},
		{"menu_", bot.MatchTypePrefix, a.handleMenu},
		{"toggle_ai-", bot.MatchTypePrefix, a.handleToggleAI},
		{"delete_alert-", bot.MatchTypePrefix, a.handleDeleteAlert},
		{"confirm_delete-", bot.MatchTypePrefix, a.handleConfirmDelete},
		{"cancel_delete", bot.MatchTypeExact, a.handleCancelDelete},
		{"reset_alert-", bot.MatchTypePrefix, a.handleResetAlert},
		{"confirm_reset-", bot.MatchTypePrefix, a.handleConfirmReset},
		{"cancel_reset", bot.MatchTypeExact, a.handleCancelReset},
	}
}

func (a *App) publishCommands(ctx context.Context, b *bot.Bot) {
	var menu []models.BotCommand
	for _, c := range a.commands() {
		menu = append(menu, models.BotCommand{Command: c.names[0], Description: c.description})
	}
	if _, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: menu}); err != nil {
		a.log.Warnw("Failed to publish bot commands", "platform", a.platforms[b], "error", err)
	}
}

func (a *App) handleStart(ctx context.Context, b *bot.Bot, chat platform.Chat, _ *models.Update) {
	if draft, _, err := a.loadDraft(chat); err == nil && draft != nil {
		a.sendDraft(ctx, b, chat, draft)
		return
	}
	a.send(ctx, b, chat, welcomeText, mainMenuKeyboard())
}

func (a *App) handleCancel(ctx context.Context, b *bot.Bot, chat platform.Chat, _ *models.Update) {
	if err := a.store.DeleteDraft(chat); err != nil {
		a.log.Errorw("Failed to cancel draft", "chat", chat, "error", err)
		a.send(ctx, b, chat, "لغو عملیات انجام نشد. دوباره تلاش کنید.", nil)
		return
	}
	a.send(ctx, b, chat, "عملیات فعلی لغو شد.", mainMenuKeyboard())
}

func (a *App) handleNewAlert(ctx context.Context, b *bot.Bot, chat platform.Chat, _ *models.Update) {
	a.startDraft(ctx, b, chat)
}

func (a *App) handleAlertList(ctx context.Context, b *bot.Bot, chat platform.Chat, _ *models.Update) {
	a.sendAlertList(ctx, b, chat)
}

// handleText treats any other private text as part of the alert conversation.
func (a *App) handleText(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}
	if update.Message.Chat.Type != models.ChatTypePrivate {
		a.send(ctx, b, chat, "برای ساخت اعلان، در گفت‌وگوی خصوصی پیام بده.", nil)
		return
	}
	if strings.HasPrefix(update.Message.Text, "/") {
		a.send(ctx, b, chat, "خواسته‌ات را بنویس یا از منو یک گزینه انتخاب کن.", mainMenuKeyboard())
		return
	}
	a.continueDraft(ctx, b, chat, update.Message.Text)
}

func (a *App) handleMenu(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	a.answerCallback(ctx, b, update, "", false)
	switch update.CallbackQuery.Data {
	case "menu_new":
		a.startDraft(ctx, b, chat)
	case "menu_list":
		a.sendAlertList(ctx, b, chat)
	}
}

func (a *App) handleToggleAI(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	alertID, enabled, err := parseAIToggle(update.CallbackQuery.Data)
	if err != nil {
		a.answerCallback(ctx, b, update, "انتخاب نامعتبر است.", true)
		return
	}
	if err := a.store.SetAlertAI(chat, alertID, enabled); err != nil {
		a.log.Errorw("Failed to toggle alert AI", "chat", chat, "alert_id", alertID, "error", err)
		a.answerCallback(ctx, b, update, "تغییر وضعیت ممکن نشد.", true)
		return
	}
	status := "خاموش شد"
	if enabled {
		status = "روشن شد"
	}
	a.answerCallback(ctx, b, update, "تحلیل هوش مصنوعی "+status+".", false)
	a.refreshAlertList(ctx, b, chat, callbackMessageID(update))
}

// findAlert returns one of the chat's active alerts by ID.
func (a *App) findAlert(chat platform.Chat, data, prefix string) (db.Alert, error) {
	alertID, err := strconv.ParseInt(strings.TrimPrefix(data, prefix), 10, 64)
	if err != nil {
		return db.Alert{}, err
	}
	alerts, err := a.store.ChatAlerts(chat)
	if err != nil {
		return db.Alert{}, err
	}
	for _, alert := range alerts {
		if alert.ID == alertID {
			return alert, nil
		}
	}
	return db.Alert{}, errors.New("alert not found")
}

func (a *App) handleDeleteAlert(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	alert, err := a.findAlert(chat, update.CallbackQuery.Data, "delete_alert-")
	if err != nil {
		a.answerCallback(ctx, b, update, "اعلان پیدا نشد.", true)
		return
	}
	a.answerCallback(ctx, b, update, "", false)
	a.send(ctx, b, chat, "اعلان «"+alert.Title+"» حذف شود؟", confirmKeyboard(
		"بله، حذف شود", "confirm_delete-"+strconv.FormatInt(alert.ID, 10), "cancel_delete"))
}

func (a *App) handleConfirmDelete(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	alertID, err := strconv.ParseInt(strings.TrimPrefix(update.CallbackQuery.Data, "confirm_delete-"), 10, 64)
	if err == nil {
		err = a.store.DeleteAlert(chat, alertID)
	}
	if err != nil {
		a.log.Errorw("Failed to delete alert", "chat", chat, "error", err)
		a.answerCallback(ctx, b, update, "حذف اعلان ممکن نشد.", true)
		return
	}
	a.answerCallback(ctx, b, update, "اعلان حذف شد.", false)
	a.refreshAlertList(ctx, b, chat, callbackMessageID(update))
}

func (a *App) handleCancelDelete(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	a.answerCallback(ctx, b, update, "حذف لغو شد.", false)
	a.editText(ctx, b, chat, callbackMessageID(update), "حذف اعلان لغو شد.", nil)
}

func (a *App) handleResetAlert(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	alert, err := a.findAlert(chat, update.CallbackQuery.Data, "reset_alert-")
	if err != nil {
		a.answerCallback(ctx, b, update, "اعلان پیدا نشد.", true)
		return
	}
	a.answerCallback(ctx, b, update, "", false)
	a.send(ctx, b, chat, "سابقه اعلان «"+alert.Title+"» پاک شود؟ آگهی‌های فعلی ممکن است دوباره ارسال شوند.", confirmKeyboard(
		"بله، تازه‌سازی شود", "confirm_reset-"+strconv.FormatInt(alert.ID, 10), "cancel_reset"))
}

func (a *App) handleConfirmReset(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	alertID, err := strconv.ParseInt(strings.TrimPrefix(update.CallbackQuery.Data, "confirm_reset-"), 10, 64)
	if err == nil {
		err = a.store.ResetAlert(chat, alertID)
	}
	if err != nil {
		a.log.Errorw("Failed to reset alert", "chat", chat, "error", err)
		a.answerCallback(ctx, b, update, "تازه‌سازی اعلان ممکن نشد.", true)
		return
	}
	a.answerCallback(ctx, b, update, "سابقه پاک شد؛ اعلان تازه است.", false)
	a.refreshAlertList(ctx, b, chat, callbackMessageID(update))
}

func (a *App) handleCancelReset(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	a.answerCallback(ctx, b, update, "تازه‌سازی لغو شد.", false)
	a.editText(ctx, b, chat, callbackMessageID(update), "تازه‌سازی اعلان لغو شد.", nil)
}

func (a *App) sendAlertList(ctx context.Context, b *bot.Bot, chat platform.Chat) {
	alerts, err := a.store.ChatAlerts(chat)
	if err != nil {
		a.log.Errorw("Failed to list alerts", "chat", chat, "error", err)
		a.send(ctx, b, chat, "دریافت اعلان‌ها ممکن نشد.", nil)
		return
	}
	text, keyboard := renderAlertList(alerts)
	a.send(ctx, b, chat, text, keyboard)
}

func (a *App) refreshAlertList(ctx context.Context, b *bot.Bot, chat platform.Chat, messageID int) {
	alerts, err := a.store.ChatAlerts(chat)
	if err != nil {
		a.log.Errorw("Failed to refresh alerts", "chat", chat, "error", err)
		return
	}
	text, keyboard := renderAlertList(alerts)
	a.editText(ctx, b, chat, messageID, text, keyboard)
}

func renderAlertList(alerts []db.Alert) (string, *models.InlineKeyboardMarkup) {
	if len(alerts) == 0 {
		return "هنوز اعلانی ندارید. از /alert_set برای ساخت اولین اعلان استفاده کنید.", nil
	}
	var text strings.Builder
	text.WriteString("اعلان‌های شما:\n")
	keyboard := make([][]models.InlineKeyboardButton, 0, len(alerts)*2)
	for index, alert := range alerts {
		aiStatus := "خاموش"
		if alert.AIEnabled {
			aiStatus = "روشن"
		}
		fmt.Fprintf(&text, "\n%d. %s\nهر %d ثانیه | تحلیل هوش مصنوعی: %s\n", index+1, alert.Title, alert.Interval, aiStatus)
		id := strconv.FormatInt(alert.ID, 10)
		keyboard = append(keyboard,
			[]models.InlineKeyboardButton{
				{Text: "تحلیل: " + aiStatus, CallbackData: fmt.Sprintf("toggle_ai-%s-%t", id, !alert.AIEnabled)},
				{Text: "تازه‌سازی", CallbackData: "reset_alert-" + id},
			},
			[]models.InlineKeyboardButton{{Text: "حذف " + alert.Title, CallbackData: "delete_alert-" + id}},
		)
	}
	return strings.TrimSpace(text.String()), &models.InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func parseAIToggle(data string) (int64, bool, error) {
	parts := strings.Split(strings.TrimPrefix(data, "toggle_ai-"), "-")
	if len(parts) != 2 {
		return 0, false, errors.New("invalid AI toggle")
	}
	alertID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false, err
	}
	enabled, err := strconv.ParseBool(parts[1])
	return alertID, enabled, err
}

func mainMenuKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: "اعلان جدید", CallbackData: "menu_new"},
		{Text: "اعلان‌های من", CallbackData: "menu_list"},
	}}}
}

func confirmKeyboard(yes, yesData, cancelData string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: yes, CallbackData: yesData},
		{Text: "انصراف", CallbackData: cancelData},
	}}}
}

// send writes a message to the chat; failures are logged.
func (a *App) send(ctx context.Context, b *bot.Bot, chat platform.Chat, text string, markup models.ReplyMarkup) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chat.ID, Text: text, ReplyMarkup: markup}); err != nil {
		a.log.Errorw("Failed to send message", "chat", chat, "error", err)
	}
}

func (a *App) editText(ctx context.Context, b *bot.Bot, chat platform.Chat, messageID int, text string, keyboard *models.InlineKeyboardMarkup) {
	params := &bot.EditMessageTextParams{ChatID: chat.ID, MessageID: messageID, Text: text}
	if keyboard != nil {
		params.ReplyMarkup = keyboard
	}
	if _, err := b.EditMessageText(ctx, params); err != nil {
		a.log.Warnw("Failed to edit message", "chat", chat, "error", err)
	}
}

func (a *App) answerCallback(ctx context.Context, b *bot.Bot, update *models.Update, text string, showAlert bool) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID, Text: text, ShowAlert: showAlert}); err != nil {
		a.log.Errorw("Failed to answer callback query", "error", err)
	}
}

func (a *App) clearKeyboard(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update) {
	_, err := b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:      chat.ID,
		MessageID:   callbackMessageID(update),
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	if err != nil {
		a.log.Warnw("Failed to clear keyboard", "chat", chat, "error", err)
	}
}
