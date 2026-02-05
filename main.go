package main

import (
	"context"
	"fmt"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/history"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"time"
)

var logger *zap.Logger
var sugar *zap.SugaredLogger

var database *db.DB

var b *bot.Bot

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
	database, err = db.Open(DBPath)

	if err != nil {
		sugar.Fatal(err)
	}
	defer database.Close()

	//// DEBUG: select * from process
	//processes, err := database.GetAllProcesses()
	//if err != nil {
	//	sugar.Errorw("Failed to query processes", "error", err)
	//} else {
	//	sugar.Info("===== DEBUG: All Processes =====")
	//	for _, p := range processes {
	//		sugar.Infow("Process",
	//			"chat_id", p.ChatId,
	//			"process_key", p.ProcessKey,
	//			"process_data", p.ProcessData,
	//			"last_action_at", p.LastActionAt,
	//		)
	//	}
	//	sugar.Infof("Total processes: %d", len(processes))
	//	sugar.Info("================================")
	//}

	// ------------------ init history directory -----------------
	if err := history.EnsureHistoryDir(); err != nil {
		sugar.Warnw("Failed to create history directory", "error", err)
	}

	// ------------------ init and config bot -----------------
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithServerURL(TelegramApiUrl),
		bot.WithDefaultHandler(handlerDefault),
		bot.WithCallbackQueryDataHandler("delete_alert-", bot.MatchTypePrefix, handlerCallbackDeleteAlert),
	}

	b, err = bot.New(TelegramToken, opts...)
	if err != nil {
		sugar.Fatal(err)
	}

	go checkForNewAlert()

	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertSet", bot.MatchTypeExact, handlerAlertSet)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertList", bot.MatchTypeExact, handlerAlertList)

	b.Start(ctx)
}

func check(args ...any) {
	err := args[len(args)-1]
	if err != nil {
		panic(err)
	}
}

func checkForNewAlert() {
	// Add panic recovery to prevent application crashes
	defer func() {
		if r := recover(); r != nil {
			sugar.Errorw("Panic recovered in checkForNewAlert", "panic", r)
			// Restart the function after a panic
			time.Sleep(5 * time.Second)
			go checkForNewAlert()
		}
	}()

	// Infinite loop instead of recursion to prevent stack overflow
	for {
		// Read all alerts from the database
		alerts, err := database.GetAllAlerts()
		if err != nil {
			sugar.Errorw("Failed to read alerts from database", "error", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Process each alert
		for _, alert := range alerts {
			// check if alert is due for checking
			if alert.LastTimeChecked+int64(alert.Interval) > time.Now().Unix() {
				continue
			}

			sugar.Infow("Checking for new posts for alert", "alert", alert.Title)

			res, err := divar.Search(alert.Link)
			if err != nil {
				sugar.Errorw("Failed to search for alert", "error", err, "alert", alert.Title)
				continue
			}

			// Process posts
			if len(res.ListWidgets) > 0 {
				slices.Reverse(res.ListWidgets)

				for _, post := range res.ListWidgets {
					// check if post is already in database
					exists, err := database.PostExists(post.Data.Token, alert.Id)
					if err != nil {
						sugar.Errorw("Failed to check post existence", "error", err, "post", post.Data.Title)
						continue
					}

					if !exists {
						// post not found, process it with AI evaluation

						// Step 1: Get full post details
						postDetail, err := divar.GetPost(post.Data.Token)
						if err != nil {
							sugar.Errorw("Failed to get post details", "error", err, "token", post.Data.Token)
							continue
						}

						// Step 2: Extract important rental data
						importantData := divar.ExtractRentalImportantData(postDetail)

						// Step 3: Load history of previous posts
						postHistory, err := history.LoadHistory(alert.Id)
						if err != nil {
							sugar.Errorw("Failed to load post history", "error", err, "alert", alert.Title)
							postHistory = []interface{}{} // Continue with empty history
						}

						// Step 4: AI Evaluation
						evaluation, err := ai.EvaluateRentalPost(importantData, postHistory)
						if err != nil {
							sugar.Errorw("Failed to evaluate post with AI", "error", err, "post", post.Data.Title)
							// Continue without AI evaluation
							evaluation = nil
						}

						// Step 5: Save to history
						err = history.AppendToHistory(alert.Id, importantData)
						if err != nil {
							sugar.Errorw("Failed to save post to history", "error", err, "post", post.Data.Title)
						}

						// Step 6: Save to database
						err = database.InsertPost(post.Data.Token, alert.Id, post, time.Now().Unix())
						if err != nil {
							sugar.Errorw("Failed to save post to database", "error", err, "post", post.Data.Title)
							continue
						}

						// Step 7: Prepare notification message
						text := "پست جدید برای: " + alert.Title + "\n\n"
						text += post.Data.Title + "\n"
						text += post.Data.TopDescriptionText + "\n"
						text += post.Data.BottomDescriptionText + "\n"
						text += post.Data.MiddleDescriptionText + "\n\n"

						// Add AI evaluation if available
						if evaluation != nil {
							text += "━━━━━━━━━━━━━━━━━━\n"
							text += "🤖 AI Evaluation:\n"
							text += fmt.Sprintf("Score: %d/100\n", evaluation.Score)
							text += fmt.Sprintf("Decision: %s\n", evaluation.Decision)
							text += fmt.Sprintf("Explanation: %s\n\n", evaluation.Explanation)
						}

						text += fmt.Sprintf("https://divar.ir/v/%s", post.Data.Token)

						// Step 8: Send Telegram notification
						_, err = b.SendPhoto(context.Background(), &bot.SendPhotoParams{
							ChatID:  alert.ChatId,
							Photo:   &models.InputFileString{Data: post.Data.ImageURL},
							Caption: text,
						})
						if err != nil {
							sugar.Errorw("Failed to send photo to user", "error", err, "alert", alert.Title)
						}
					}
				}
			} else {
				sugar.Infow("No new posts found for alert", "alert", alert.Title)
			}

			// Update last time checked
			err = database.UpdateAlertLastChecked(alert.Id, time.Now().Unix())
			if err != nil {
				sugar.Errorw("Failed to update last time checked for alert", "error", err, "alert", alert.Title)
			}
		}

		sugar.Infow("Finished checking for new alerts")
		time.Sleep(1 * time.Second)
	}
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
	err = database.DeleteAlert(update.CallbackQuery.Message.Message.Chat.ID, alertId)
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
	p, err := ProcessStart(ProcessKey.SetAlert, update.Message.Chat.ID, database)
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

	alerts, err := database.GetAlertsByChatId(update.Message.Chat.ID)
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
	key, p, err := CurrentProcess(update.Message.Chat.ID, database)

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "لطفا یکی از دستورات را انتخاب کنید.",
		})
		return
	}

	if key != "" {
		p, err = ProcessGoNextStep(p, update.Message.Text, database)
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
