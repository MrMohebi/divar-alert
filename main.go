package main

import (
	"context"
	"github.com/dgraph-io/badger/v4"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"os"
	"os/signal"
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
	}

	b, err := bot.New(TelegramToken, opts...)
	if err != nil {
		sugar.Fatal(err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertSet", bot.MatchTypeExact, handlerAlertSet)

	b.Start(ctx)
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
