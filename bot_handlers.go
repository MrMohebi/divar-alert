package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
)

const welcomeText = `ربات اعلان دیوار

خواسته‌ات را با اسم شهر بنویس تا اعلانش را بسازیم. از «اعلان جدید» هم می‌توانی شروع کنی. برای مدیریت اعلان‌ها و روشن یا خاموش کردن تحلیل هوش مصنوعی، «اعلان‌های من» را انتخاب کنید.

در هر مرحله می‌توانید /cancel را ارسال کنید.`

func configureBotCommands(ctx context.Context, b *bot.Bot) {
	_, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: []models.BotCommand{
		{Command: "start", Description: "نمایش راهنما و منوی اصلی"},
		{Command: "alert_set", Description: "ساخت اعلان جدید"},
		{Command: "alerts", Description: "مدیریت اعلان‌ها"},
		{Command: "cancel", Description: "لغو عملیات فعلی"},
		{Command: "help", Description: "نمایش راهنما"},
	}})
	if err != nil {
		sugar.Warnw("Failed to configure bot commands", "error", err)
	}
}

func handlerStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	if draft, _, err := loadDraft(update.Message.Chat.ID); err == nil && draft != nil {
		sendDraft(ctx, b, update.Message.Chat.ID, draft)
		return
	}
	sendMessage(ctx, b, update.Message.Chat.ID, welcomeText, mainMenuKeyboard())
}

func handlerCancel(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	if err := database.DeleteProcess(update.Message.Chat.ID); err != nil {
		sugar.Errorw("Failed to cancel process", "error", err)
		sendMessage(ctx, b, update.Message.Chat.ID, "لغو عملیات انجام نشد. دوباره تلاش کنید.", nil)
		return
	}
	sendMessage(ctx, b, update.Message.Chat.ID, "عملیات فعلی لغو شد.", mainMenuKeyboard())
}

func handlerAlertSet(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	startAlertProcess(ctx, b, update.Message.Chat.ID)
}

func startAlertProcess(ctx context.Context, b *bot.Bot, chatID int64) {
	startNaturalAlert(ctx, b, chatID)
}

func handlerAlertList(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	sendAlertList(ctx, b, update.Message.Chat.ID)
}

func handlerDefault(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}
	if update.Message.Chat.Type != "private" {
		sendMessage(ctx, b, update.Message.Chat.ID, "برای ساخت اعلان، در گفت‌وگوی خصوصی پیام بده.", nil)
		return
	}
	stored, loadErr := database.GetProcess(update.Message.Chat.ID)
	if loadErr != nil {
		sendMessage(ctx, b, update.Message.Chat.ID, "خواندن گفت‌وگو ممکن نشد؛ کمی بعد دوباره تلاش کن.", nil)
		return
	}
	if stored == nil || stored.ProcessKey == conversation.ProcessKey {
		if strings.HasPrefix(update.Message.Text, "/") {
			sendMessage(ctx, b, update.Message.Chat.ID, "خواسته‌ات را بنویس یا از منو یک گزینه انتخاب کن.", mainMenuKeyboard())
			return
		}
		handleNaturalText(ctx, b, update.Message.Chat.ID, update.Message.Text)
		return
	}
	_, process, err := CurrentProcess(update.Message.Chat.ID, database)
	if err != nil {
		sendMessage(ctx, b, update.Message.Chat.ID, "یک گزینه از منو انتخاب کنید.", mainMenuKeyboard())
		return
	}

	if currentStep(process).Name == "ai_enabled" {
		sendProcessStep(ctx, b, process)
		return
	}
	nextProcess, err := ProcessGoNextStep(process, update.Message.Text, database)
	if err != nil {
		sugar.Infow("Invalid process input", "error", err, "chat_id", update.Message.Chat.ID)
		sendMessage(ctx, b, update.Message.Chat.ID, "ورودی معتبر نیست. لطفا دوباره تلاش کنید.\n\n"+currentStep(process).Message, forceReply(currentStep(process)))
		return
	}
	sendProcessStep(ctx, b, nextProcess)
}

func handlerCallbackMenu(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, _, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	answerCallback(ctx, b, update, "", false)
	switch update.CallbackQuery.Data {
	case "menu_new":
		startAlertProcess(ctx, b, chatID)
	case "menu_list":
		sendAlertList(ctx, b, chatID)
	}
}

func handlerCallbackSetAI(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, _, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	enabled := strings.TrimPrefix(update.CallbackQuery.Data, "set_ai-")
	if enabled != "true" && enabled != "false" {
		answerCallback(ctx, b, update, "انتخاب نامعتبر است.", true)
		return
	}
	_, process, err := CurrentProcess(chatID, database)
	if err != nil || currentStep(process).Name != "ai_enabled" {
		answerCallback(ctx, b, update, "این مرحله قبلا تمام شده است.", true)
		return
	}
	nextProcess, err := ProcessGoNextStep(process, enabled, database)
	if err != nil {
		sugar.Errorw("Failed to set process AI state", "error", err)
		answerCallback(ctx, b, update, "ثبت انتخاب ممکن نشد.", true)
		return
	}
	answerCallback(ctx, b, update, "انتخاب ثبت شد.", false)
	clearCallbackKeyboard(ctx, b, update)
	sendProcessStep(ctx, b, nextProcess)
}

func handlerCallbackSetInterval(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, _, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	interval := strings.TrimPrefix(update.CallbackQuery.Data, "set_interval-")
	_, process, err := CurrentProcess(chatID, database)
	if err != nil || currentStep(process).Name != "interval" {
		answerCallback(ctx, b, update, "این مرحله قبلا تمام شده است.", true)
		return
	}
	nextProcess, err := ProcessGoNextStep(process, interval, database)
	if err != nil {
		sugar.Errorw("Failed to set process interval", "error", err)
		answerCallback(ctx, b, update, "ثبت فاصله ممکن نشد.", true)
		return
	}
	answerCallback(ctx, b, update, "فاصله ثبت شد.", false)
	clearCallbackKeyboard(ctx, b, update)
	sendProcessStep(ctx, b, nextProcess)
}

func handlerCallbackToggleAI(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, messageID, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	alertID, enabled, err := parseAIToggle(update.CallbackQuery.Data)
	if err != nil {
		answerCallback(ctx, b, update, "انتخاب نامعتبر است.", true)
		return
	}
	if enabled {
		alerts, listErr := database.GetAlertsByChatId(chatID)
		if listErr != nil {
			answerCallback(ctx, b, update, "دریافت اعلان ممکن نشد.", true)
			return
		}
		for _, alert := range alerts {
			if alert.Id == alertID && strings.TrimSpace(alert.AnalysisPrompt) == "" {
				process, processErr := ProcessStartAIPrompt(chatID, alertID, database)
				if processErr != nil {
					sugar.Errorw("Failed to start AI prompt process", "error", processErr)
					answerCallback(ctx, b, update, "شروع تنظیم تحلیل ممکن نشد.", true)
					return
				}
				answerCallback(ctx, b, update, "ابتدا شرایط تحلیل را مشخص کنید.", false)
				sendProcessStep(ctx, b, process)
				return
			}
		}
	}
	if err := database.UpdateAlertAIEnabled(chatID, alertID, enabled); err != nil {
		sugar.Errorw("Failed to toggle alert AI", "error", err)
		answerCallback(ctx, b, update, "تغییر وضعیت ممکن نشد.", true)
		return
	}
	status := "خاموش شد"
	if enabled {
		status = "روشن شد"
	}
	answerCallback(ctx, b, update, "تحلیل هوش مصنوعی "+status+".", false)
	refreshAlertList(ctx, b, chatID, messageID)
}

func handlerCallbackDeleteAlert(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, _, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimPrefix(update.CallbackQuery.Data, "delete_alert-"), 10, 64)
	if err != nil {
		answerCallback(ctx, b, update, "اعلان نامعتبر است.", true)
		return
	}
	alerts, err := database.GetAlertsByChatId(chatID)
	if err != nil {
		answerCallback(ctx, b, update, "دریافت اعلان ممکن نشد.", true)
		return
	}
	for _, alert := range alerts {
		if alert.Id == alertID {
			answerCallback(ctx, b, update, "", false)
			sendMessage(ctx, b, chatID, "اعلان «"+alert.Title+"» حذف شود؟", &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
				{Text: "بله، حذف شود", CallbackData: "confirm_delete-" + strconv.FormatInt(alertID, 10)},
				{Text: "انصراف", CallbackData: "cancel_delete"},
			}}})
			return
		}
	}
	answerCallback(ctx, b, update, "اعلان پیدا نشد.", true)
}

func handlerCallbackConfirmDelete(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, messageID, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimPrefix(update.CallbackQuery.Data, "confirm_delete-"), 10, 64)
	if err != nil {
		answerCallback(ctx, b, update, "اعلان نامعتبر است.", true)
		return
	}
	if err := database.DeleteAlert(chatID, alertID); err != nil {
		sugar.Errorw("Failed to delete alert", "error", err)
		answerCallback(ctx, b, update, "حذف اعلان ممکن نشد.", true)
		return
	}
	answerCallback(ctx, b, update, "اعلان حذف شد.", false)
	refreshAlertList(ctx, b, chatID, messageID)
}

func handlerCallbackCancelDelete(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, messageID, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	answerCallback(ctx, b, update, "حذف لغو شد.", false)
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: "حذف اعلان لغو شد."}); err != nil {
		sugar.Warnw("Failed to close delete confirmation", "error", err)
	}
}

func handlerCallbackResetAlert(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, _, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimPrefix(update.CallbackQuery.Data, "reset_alert-"), 10, 64)
	if err != nil {
		answerCallback(ctx, b, update, "اعلان نامعتبر است.", true)
		return
	}
	alerts, err := database.GetAlertsByChatId(chatID)
	if err != nil {
		answerCallback(ctx, b, update, "دریافت اعلان ممکن نشد.", true)
		return
	}
	for _, alert := range alerts {
		if alert.Id != alertID {
			continue
		}
		answerCallback(ctx, b, update, "", false)
		sendMessage(ctx, b, chatID, "سابقه اعلان «"+alert.Title+"» پاک شود؟ آگهی‌های فعلی ممکن است دوباره ارسال شوند.", &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "بله، تازه‌سازی شود", CallbackData: "confirm_reset-" + strconv.FormatInt(alertID, 10)},
			{Text: "انصراف", CallbackData: "cancel_reset"},
		}}})
		return
	}
	answerCallback(ctx, b, update, "اعلان پیدا نشد.", true)
}

func handlerCallbackConfirmReset(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, messageID, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimPrefix(update.CallbackQuery.Data, "confirm_reset-"), 10, 64)
	if err != nil {
		answerCallback(ctx, b, update, "اعلان نامعتبر است.", true)
		return
	}
	if err := database.ResetAlert(chatID, alertID); err != nil {
		sugar.Errorw("Failed to reset alert", "error", err)
		answerCallback(ctx, b, update, "تازه‌سازی اعلان ممکن نشد.", true)
		return
	}
	answerCallback(ctx, b, update, "سابقه پاک شد؛ اعلان تازه است.", false)
	refreshAlertList(ctx, b, chatID, messageID)
}

func handlerCallbackCancelReset(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, messageID, ok := callbackMessage(update)
	if !ok {
		answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
		return
	}
	answerCallback(ctx, b, update, "تازه‌سازی لغو شد.", false)
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: "تازه‌سازی اعلان لغو شد."}); err != nil {
		sugar.Warnw("Failed to close reset confirmation", "error", err)
	}
}

func sendProcessStep(ctx context.Context, b *bot.Bot, process Process) {
	step := currentStep(process)
	if step.Name == "" {
		return
	}
	if step.Name == "end" {
		sendMessage(ctx, b, process.ChatId, step.Message, mainMenuKeyboard())
		return
	}
	if step.Name == "ai_enabled" {
		sendMessage(ctx, b, process.ChatId, step.Message, &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "فعال", CallbackData: "set_ai-true"},
			{Text: "غیرفعال", CallbackData: "set_ai-false"},
		}}})
		return
	}
	if step.Name == "interval" {
		sendMessage(ctx, b, process.ChatId, step.Message, &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "1 دقیقه", CallbackData: "set_interval-60"}, {Text: "5 دقیقه", CallbackData: "set_interval-300"}},
			{{Text: "15 دقیقه", CallbackData: "set_interval-900"}, {Text: "1 ساعت", CallbackData: "set_interval-3600"}},
		}})
		return
	}
	sendMessage(ctx, b, process.ChatId, step.Message, forceReply(step))
}

func forceReply(step Step) *models.ForceReply {
	placeholder := "پاسخ خود را بنویسید"
	switch step.Name {
	case "title":
		placeholder = "مثلا خودروهای مناسب"
	case "link":
		placeholder = "دستور curl جستجوی دیوار"
	case "analysis_prompt":
		placeholder = "شرایط تحلیل آگهی"
	case "interval":
		placeholder = "بین 10 تا 86400 ثانیه"
	}
	return &models.ForceReply{ForceReply: true, InputFieldPlaceholder: placeholder, Selective: true}
}

func currentStep(process Process) Step {
	if process.CurrentStepIndex < 0 || process.CurrentStepIndex >= len(process.Step) {
		return Step{}
	}
	return process.Step[process.CurrentStepIndex]
}

func sendAlertList(ctx context.Context, b *bot.Bot, chatID int64) {
	alerts, err := database.GetAlertsByChatId(chatID)
	if err != nil {
		sugar.Errorw("Failed to list alerts", "error", err)
		sendMessage(ctx, b, chatID, "دریافت اعلان‌ها ممکن نشد.", nil)
		return
	}
	text, keyboard := renderAlertList(alerts)
	sendMessage(ctx, b, chatID, text, keyboard)
}

func refreshAlertList(ctx context.Context, b *bot.Bot, chatID int64, messageID int) {
	alerts, err := database.GetAlertsByChatId(chatID)
	if err != nil {
		sugar.Errorw("Failed to refresh alerts", "error", err)
		return
	}
	text, keyboard := renderAlertList(alerts)
	params := &bot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text}
	if keyboard != nil {
		params.ReplyMarkup = keyboard
	}
	if _, err := b.EditMessageText(ctx, params); err != nil {
		sugar.Errorw("Failed to refresh alert list message", "error", err)
	}
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
		keyboard = append(keyboard, []models.InlineKeyboardButton{
			{Text: "تحلیل: " + aiStatus, CallbackData: fmt.Sprintf("toggle_ai-%d-%t", alert.Id, !alert.AIEnabled)},
			{Text: "تازه‌سازی", CallbackData: "reset_alert-" + strconv.FormatInt(alert.Id, 10)},
		})
		keyboard = append(keyboard, []models.InlineKeyboardButton{
			{Text: "حذف " + alert.Title, CallbackData: "delete_alert-" + strconv.FormatInt(alert.Id, 10)},
		})
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

func callbackMessage(update *models.Update) (int64, int, bool) {
	if update == nil || update.CallbackQuery == nil || update.CallbackQuery.Message.Message == nil {
		return 0, 0, false
	}
	message := update.CallbackQuery.Message.Message
	return message.Chat.ID, message.ID, true
}

func answerCallback(ctx context.Context, b *bot.Bot, update *models.Update, text string, showAlert bool) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID, Text: text, ShowAlert: showAlert}); err != nil {
		sugar.Errorw("Failed to answer callback query", "error", err)
	}
}

func clearCallbackKeyboard(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, messageID, ok := callbackMessage(update)
	if !ok {
		return
	}
	_, err := b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	if err != nil {
		sugar.Warnw("Failed to clear callback keyboard", "error", err)
	}
}

func sendMessage(ctx context.Context, b *bot.Bot, chatID int64, text string, markup models.ReplyMarkup) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: markup}); err != nil {
		sugar.Errorw("Failed to send bot message", "error", err, "chat_id", chatID)
	}
}

func mainMenuKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: "اعلان جدید", CallbackData: "menu_new"},
		{Text: "اعلان‌های من", CallbackData: "menu_list"},
	}}}
}
