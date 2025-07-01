package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"strconv"
)

var logger *zap.Logger
var sugar *zap.SugaredLogger

var db *badger.DB

func main() {
	logger, _ = zap.NewProduction()

	sugar = logger.Sugar()
	defer logger.Sync()

	err := godotenv.Load()
	if err != nil {
		sugar.Warn("Error loading .env file")
	}

	TelegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	TelegramApiUrl := os.Getenv("TELEGRAM_API_URL")
	DBPath := os.Getenv("DB_PATH")

	if TelegramApiUrl == "" || TelegramToken == "" || DBPath == "" {
		sugar.Fatal("TELEGRAM_API_URL and TELEGRAM_BOT_TOKEN and DB_PATH must be set in .env file")
	} else {
		sugar.Infof("TELEGRAM_API_URL: %s", TelegramApiUrl)
		sugar.Infof("TELEGRAM_BOT_TOKEN: %s", TelegramToken)
		sugar.Infof("DB_PATH: %s", DBPath)
	}

	// ------------------ init db -----------------
	db, err = badger.Open(badger.DefaultOptions(DBPath))
	if err != nil {
		sugar.Fatal(err)
	}
	defer db.Close()

	// ------------------ init and config bot -----------------
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithServerURL(TelegramApiUrl),
		bot.WithDefaultHandler(handlerDefault),
		bot.WithCallbackQueryDataHandler("delete_alert-", bot.MatchTypePrefix, handlerCallbackDeleteAlert),
	}

	b, err := bot.New(TelegramToken, opts...)
	if err != nil {
		sugar.Fatal(err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertSet", bot.MatchTypeExact, handlerAlertSet)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertList", bot.MatchTypeExact, handlerAlertList)

	b.Start(ctx)
}

func checkForNewAlert() {

}

func handlerCallbackDeleteAlert(ctx context.Context, b *bot.Bot, update *models.Update) {
	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		ShowAlert:       false,
	})

	alertIdStr := update.CallbackQuery.Data[len("delete_alert-"):]

	// delete alert from database
	alertId, err := strconv.ParseInt(alertIdStr, 10, 64)
	if err != nil {
		sugar.Errorw("Failed to parse alert ID", "error", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.CallbackQuery.Message.Message.Chat.ID,
			Text:   "خطا در حذف اعلان.",
		})
		return
	}
	err = db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("alert-%d-%d", update.CallbackQuery.Message.Message.Chat.ID, alertId))
		println(string(key))
		return txn.Delete(key)
	})
	if err != nil {
		sugar.Errorw("Failed to delete alert", "error", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.CallbackQuery.Message.Message.Chat.ID,
			Text:   "خطا در حذف اعلان.",
		})
		return
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.CallbackQuery.Message.Message.Chat.ID,
		Text:   "اعلان با موفقیت حذف شد.",
	})

}

func handlerAlertSet(ctx context.Context, b *bot.Bot, update *models.Update) {
	p, err := ProcessStart(ProcessKey.SetAlert, update.Message.Chat.ID, db)
	if err != nil {
		sugar.Errorw("Failed to start alert process", "error", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "خطا در شروع فرآیند تنظیم هشدار.",
		})
		return
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   p.Step[p.CurrentStepIndex].Message,
	})
}

func handlerAlertList(ctx context.Context, b *bot.Bot, update *models.Update) {

	var alerts []Alert
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("alert-" + strconv.FormatInt(update.Message.Chat.ID, 10))
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var alert Alert
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &alert)
			})
			if err != nil {
				return err
			}
			alerts = append(alerts, alert)
		}
		return nil
	})
	if err != nil {
		sugar.Errorw("Failed to list alerts", "error", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "خطا در دریافت اعلان‌ها.",
		})
		return
	}

	if len(alerts) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "هیچ اعلان فعالی وجود ندارد.",
		})
		return
	}

	var response string

	for i, alert := range alerts {
		response += strconv.Itoa(i+1) + ". " + alert.Title + " (هر" + strconv.Itoa(alert.Interval) + " ثانیه)"
		if i != len(alerts)-1 {
			response += "\n"
		}
	}

	var inlineKeyboardButtons [][]models.InlineKeyboardButton
	for _, alert := range alerts {
		inlineKeyboardButtons = append(inlineKeyboardButtons, []models.InlineKeyboardButton{
			{
				Text:         "حذف " + alert.Title,
				CallbackData: "delete_alert-" + strconv.FormatInt(alert.Id, 10),
			},
		})
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        response,
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: inlineKeyboardButtons},
	})
}

func handlerDefault(ctx context.Context, b *bot.Bot, update *models.Update) {
	key, p, err := CurrentProcess(update.Message.Chat.ID, db)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "لطفا یکی از دستورات را انتخاب کنید.",
		})
		return
	}

	if key != "" {
		p, err = ProcessGoNextStep(p, update.Message.Text, db)
		if err != nil {
			sugar.Errorw("Failed to go to next step", "error", err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "خطا در ادامه فرآیند.",
			})
			return
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   p.Step[p.CurrentStepIndex].Message,
		})
		return
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "لطفا یکی از دستورات را انتخاب کنید.",
		})
		return
	}
}
