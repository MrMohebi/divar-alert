package main

import (
	"context"
	"encoding/json"
	"github.com/dgraph-io/badger/v4"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"strconv"
	"time"
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

	b.RegisterHandler(bot.HandlerTypeMessageText, "/setAlert", bot.MatchTypeExact, handlerSetAlert)

	b.Start(ctx)
}

func handlerSetAlert(ctx context.Context, b *bot.Bot, update *models.Update) {
	p, err := ProcessStartSetAlert(update.Message.Chat.ID, db)
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

func currentProcess(chatId int64, db *badger.DB) (string, Process, error) {
	var processKey string
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(strconv.FormatInt(chatId, 10) + "-CURRENT_PROCESS"))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			processKey = string(val)
			return nil
		})
	})
	if err != nil {
		return "", Process{}, err
	}

	var p Process
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(strconv.FormatInt(chatId, 10) + "-" + processKey))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			if err := json.Unmarshal(val, &p); err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		return "", Process{}, err
	}

	return processKey, p, nil
}

func handlerDefault(ctx context.Context, b *bot.Bot, update *models.Update) {
	key, p, err := currentProcess(update.Message.Chat.ID, db)
	if err != nil {
		sugar.Errorw("Failed to get current process", "error", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "خطا در دریافت فرآیند جاری.",
		})
		return
	}

	if key != "" {
		if p.CurrentStepIndex < len(p.Step)-1 {
			p.Step[p.CurrentStepIndex].Data = update.Message.Text
			p.CurrentStepIndex++
			p.LastActionAt = time.Now().Unix()
			err = db.Update(func(txn *badger.Txn) error {
				userProcessKey := []byte(strconv.FormatInt(update.Message.Chat.ID, 10) + "-" + key)
				value, err := json.Marshal(p)
				if err != nil {
					return err
				}
				return txn.Set(userProcessKey, value)
			})
			if err != nil {
				sugar.Errorw("Failed to update process", "error", err)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: update.Message.Chat.ID,
					Text:   "خطا در ثبت.",
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
				Text:   "فرآیند تکمیل شد.",
			})
			return
		}
	}
}
