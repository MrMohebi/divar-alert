package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"go.uber.org/zap"
)

var logger *zap.Logger
var sugar *zap.SugaredLogger

var database *db.DB

var b *bot.Bot

var alertPollInterval = time.Second

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
	alertPollInterval, err = parsePollInterval(os.Getenv("POLL_INTERVAL_SECONDS"))
	if err != nil {
		sugar.Fatal(err)
	}

	if TelegramApiUrl == "" || TelegramToken == "" || DBPath == "" {
		sugar.Fatal("TELEGRAM_API_URL and TELEGRAM_BOT_TOKEN and DB_PATH must be set in .env file")
	} else {
		sugar.Infof("TELEGRAM_API_URL: %s", TelegramApiUrl)
		sugar.Infof("DB_PATH: %s", DBPath)
		sugar.Infof("POLL_INTERVAL_SECONDS: %g", alertPollInterval.Seconds())
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

	// ------------------ init and config bot -----------------
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts := []bot.Option{
		bot.WithServerURL(TelegramApiUrl),
		bot.WithDefaultHandler(handlerDefault),
		bot.WithCallbackQueryDataHandler("delete_alert-", bot.MatchTypePrefix, handlerCallbackDeleteAlert),
		bot.WithCallbackQueryDataHandler("confirm_delete-", bot.MatchTypePrefix, handlerCallbackConfirmDelete),
		bot.WithCallbackQueryDataHandler("cancel_delete", bot.MatchTypeExact, handlerCallbackCancelDelete),
		bot.WithCallbackQueryDataHandler("reset_alert-", bot.MatchTypePrefix, handlerCallbackResetAlert),
		bot.WithCallbackQueryDataHandler("confirm_reset-", bot.MatchTypePrefix, handlerCallbackConfirmReset),
		bot.WithCallbackQueryDataHandler("cancel_reset", bot.MatchTypeExact, handlerCallbackCancelReset),
		bot.WithCallbackQueryDataHandler("toggle_ai-", bot.MatchTypePrefix, handlerCallbackToggleAI),
		bot.WithCallbackQueryDataHandler("set_ai-", bot.MatchTypePrefix, handlerCallbackSetAI),
		bot.WithCallbackQueryDataHandler("set_interval-", bot.MatchTypePrefix, handlerCallbackSetInterval),
		bot.WithCallbackQueryDataHandler("menu_", bot.MatchTypePrefix, handlerCallbackMenu),
	}

	b, err = bot.New(TelegramToken, opts...)
	if err != nil {
		sugar.Fatal(err)
	}
	configureBotCommands(ctx, b)

	go checkForNewAlert(ctx)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, handlerStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypeExact, handlerStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/cancel", bot.MatchTypeExact, handlerCancel)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/alert_set", bot.MatchTypeExact, handlerAlertSet)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/alerts", bot.MatchTypeExact, handlerAlertList)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertSet", bot.MatchTypeExact, handlerAlertSet)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/alertList", bot.MatchTypeExact, handlerAlertList)

	b.Start(ctx)
}

func checkForNewAlert(ctx context.Context) {
	for ctx.Err() == nil {
		panicked := runAlertPoller(ctx)
		if !panicked || !sleep(ctx, 5*time.Second) {
			return
		}
	}
}

func runAlertPoller(ctx context.Context) (panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			sugar.Errorw("Panic recovered in alert poller", "panic", recovered)
			panicked = true
		}
	}()

	for {
		if ctx.Err() != nil {
			return
		}

		// Read all alerts from the database
		alerts, err := database.GetAllAlerts()
		if err != nil {
			sugar.Errorw("Failed to read alerts from database", "error", err)
			if !sleep(ctx, time.Second) {
				return
			}
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
				if updateErr := database.UpdateAlertLastChecked(alert.Id, time.Now().Unix()); updateErr != nil {
					sugar.Errorw("Failed to update last time checked", "error", updateErr, "alert", alert.Title)
				}
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
						var evaluation *ai.EvaluationResult
						var importantData divar.PostImportantData
						if alert.AIEnabled {
							postDetail, err := divar.GetPost(post.Data.Token)
							if err != nil {
								sugar.Errorw("Failed to get post details", "error", err, "token", post.Data.Token)
								continue
							}

							importantData = divar.ExtractImportantData(postDetail)
							postHistory, historyErr := database.LoadHistory(alert.Id)
							if historyErr != nil {
								sugar.Errorw("Failed to load post history", "error", historyErr, "alert", alert.Title)
								postHistory = []interface{}{}
							}

							evaluation, err = ai.EvaluatePost(ctx, importantData, postHistory, alert.AnalysisPrompt)
							if err != nil {
								sugar.Errorw("Failed to analyze post with AI", "error", err, "post", post.Data.Title)
								evaluation = nil
							}
						}

						// Step 5: Prepare notification message
						text := "پست جدید برای: " + alert.Title + "\n\n"
						text += post.Data.Title + "\n"
						text += post.Data.TopDescriptionText + "\n"
						text += post.Data.BottomDescriptionText + "\n"
						text += post.Data.MiddleDescriptionText + "\n\n"

						// Add AI evaluation if available
						if evaluation != nil {
							text += "━━━━━━━━━━━━━━━━━━\n"
							text += "🤖 AI Analysis:\n"
							text += fmt.Sprintf("Score: %d/100\n", evaluation.Score)
							if evaluation.PriceScore > 0 {
								text += fmt.Sprintf("PriceScore: %d/100\n", evaluation.PriceScore)
							}
							if evaluation.LocationScore > 0 {
								text += fmt.Sprintf("LocationScore: %d/100\n", evaluation.LocationScore)
							}
							if evaluation.FeaturesScore > 0 {
								text += fmt.Sprintf("FeaturesScore: %d/100\n", evaluation.FeaturesScore)
							}
							text += fmt.Sprintf("Decision: %s\n", evaluation.Decision)
							text += fmt.Sprintf("Explanation: %s\n\n", evaluation.Explanation)
						}

						text += fmt.Sprintf("https://divar.ir/v/%s", post.Data.Token)

						// Step 6: Send the notification before recording the post as delivered.
						err = sendPostNotification(ctx, alert.ChatId, post.Data.ImageURL, text)
						if err != nil {
							sugar.Errorw("Failed to notify user", "error", err, "alert", alert.Title)
							continue
						}

						// Step 7: Record successful delivery so failed sends can be retried.
						err = database.InsertPost(post.Data.Token, alert.Id, post, time.Now().Unix())
						if err != nil {
							sugar.Errorw("Failed to save delivered post", "error", err, "post", post.Data.Title)
							continue
						}

						if alert.AIEnabled {
							if err := database.AppendHistory(alert.Id, importantData); err != nil {
								sugar.Errorw("Failed to save post to history", "error", err, "post", post.Data.Title)
							}
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
		if !sleep(ctx, alertPollInterval) {
			return
		}
	}
}

func parsePollInterval(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Second, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 1 || seconds > 3600 {
		return 0, fmt.Errorf("POLL_INTERVAL_SECONDS must be an integer between 1 and 3600")
	}
	return time.Duration(seconds) * time.Second, nil
}

func sendPostNotification(ctx context.Context, chatID int64, imageURL, text string) error {
	if imageURL == "" {
		_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: truncateText(text, 4096)})
		return err
	}

	_, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileString{Data: imageURL},
		Caption: truncateText(text, 1024),
	})
	if err == nil {
		return nil
	}

	_, fallbackErr := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: truncateText(text, 4096)})
	return fallbackErr
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
