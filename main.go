package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"go.uber.org/zap"
)

var logger *zap.Logger
var sugar *zap.SugaredLogger

var database *db.DB

var alertPollInterval = time.Second

func main() {
	logger, _ = zap.NewProduction()

	sugar = logger.Sugar()
	defer logger.Sync()

	err := godotenv.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		sugar.Warn("Error loading .env file")
	}

	config, err := loadBotConfig(os.Getenv)
	if err != nil {
		sugar.Fatal(err)
	}
	DBPath := os.Getenv("DB_PATH")
	alertPollInterval, err = parsePollInterval(os.Getenv("POLL_INTERVAL_SECONDS"))
	if err != nil {
		sugar.Fatal(err)
	}

	if DBPath == "" {
		sugar.Fatal("DB_PATH must be set in .env file")
	}
	for _, platform := range config.Platforms {
		sugar.Infow("Messaging platform enabled", "platform", platform.Name, "api_url", platform.APIURL)
	}
	sugar.Infof("DB_PATH: %s", DBPath)
	sugar.Infof("POLL_INTERVAL_SECONDS: %g", alertPollInterval.Seconds())

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

	if err := adoptLegacyChats(config.LegacyPlatform); err != nil {
		sugar.Fatal(err)
	}

	conversationService, err = conversation.NewService()
	if err != nil {
		sugar.Fatalw("Failed to load embedded catalogs", "error", err)
	}

	// ------------------ init and config bot -----------------
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	for _, platform := range config.Platforms {
		b, err := newBot(ctx, platform)
		if err != nil {
			sugar.Fatalw("Failed to create bot", "platform", platform.Name, "error", err)
		}
		messengers = append(messengers, &messenger{platform: platform.Name, bot: b, db: database.ForPlatform(platform.Name)})
	}

	pollerDone := make(chan struct{})
	go func() { defer close(pollerDone); checkForNewAlert(ctx) }()

	var bots sync.WaitGroup
	for _, m := range messengers {
		bots.Add(1)
		go func() { defer bots.Done(); m.bot.Start(ctx) }()
	}
	bots.Wait()
	cancel()
	<-pollerDone
	watchJobs.Wait()
}

// adoptLegacyChats assigns alerts and drafts saved before platform support to
// the platform whose bot created them.
func adoptLegacyChats(platform string) error {
	unscoped, err := database.CountUnscoped()
	if err != nil || unscoped == 0 {
		return err
	}
	if platform == "" {
		return fmt.Errorf("found %d alerts or drafts saved before Telegram and Bale ran together; set EXISTING_ALERTS_PLATFORM to the platform (telegram or bale) that created them", unscoped)
	}
	adopted, err := database.AdoptUnscoped(platform)
	if err != nil {
		return fmt.Errorf("assign existing alerts to %s: %w", platform, err)
	}
	sugar.Infow("Assigned existing alerts and drafts to platform", "platform", platform, "rows", adopted)
	return nil
}

func newBot(ctx context.Context, platform platformConfig) (*bot.Bot, error) {
	opts := []bot.Option{
		bot.WithServerURL(platform.APIURL),
		// Check the token below instead, so one unreachable platform does not
		// stop the bot from starting on the other.
		bot.WithSkipGetMe(),
		bot.WithErrorsHandler(func(err error) {
			sugar.Errorw("Bot update error", "platform", platform.Name, "error", err)
		}),
		bot.WithCallbackQueryDataHandler("draft:", bot.MatchTypePrefix, serialized(handlerCallbackDraft)),
		bot.WithDefaultHandler(serialized(handlerDefault)),
		bot.WithCallbackQueryDataHandler("delete_alert-", bot.MatchTypePrefix, serialized(handlerCallbackDeleteAlert)),
		bot.WithCallbackQueryDataHandler("confirm_delete-", bot.MatchTypePrefix, serialized(handlerCallbackConfirmDelete)),
		bot.WithCallbackQueryDataHandler("cancel_delete", bot.MatchTypeExact, serialized(handlerCallbackCancelDelete)),
		bot.WithCallbackQueryDataHandler("reset_alert-", bot.MatchTypePrefix, serialized(handlerCallbackResetAlert)),
		bot.WithCallbackQueryDataHandler("confirm_reset-", bot.MatchTypePrefix, serialized(handlerCallbackConfirmReset)),
		bot.WithCallbackQueryDataHandler("cancel_reset", bot.MatchTypeExact, serialized(handlerCallbackCancelReset)),
		bot.WithCallbackQueryDataHandler("toggle_ai-", bot.MatchTypePrefix, serialized(handlerCallbackToggleAI)),
		bot.WithCallbackQueryDataHandler("set_ai-", bot.MatchTypePrefix, serialized(handlerCallbackSetAI)),
		bot.WithCallbackQueryDataHandler("set_interval-", bot.MatchTypePrefix, serialized(handlerCallbackSetInterval)),
		bot.WithCallbackQueryDataHandler("menu_", bot.MatchTypePrefix, serialized(handlerCallbackMenu)),
	}

	b, err := bot.New(platform.Token, opts...)
	if err != nil {
		return nil, err
	}
	if me, err := b.GetMe(ctx); err != nil {
		sugar.Errorw("Bot token check failed; updates will be retried", "platform", platform.Name, "error", err)
	} else {
		sugar.Infow("Bot connected", "platform", platform.Name, "username", me.Username)
	}
	configureBotCommands(ctx, b)

	for _, command := range []struct {
		text    string
		handler bot.HandlerFunc
	}{
		{"/start", handlerStart},
		{"/help", handlerStart},
		{"/cancel", handlerCancel},
		{"/alert_set", handlerAlertSet},
		{"/alerts", handlerAlertList},
		{"/alertSet", handlerAlertSet},
		{"/alertList", handlerAlertList},
	} {
		b.RegisterHandler(bot.HandlerTypeMessageText, command.text, bot.MatchTypeExact, serialized(command.handler))
	}
	return b, nil
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

			if messengerFor(alert.Platform) == nil {
				warnUnservedPlatform(alert.Platform)
				continue
			}

			if alert.SearchQuery != "" {
				launchStructuredPoll(ctx, alert)
				continue
			}

			sugar.Infow("Checking for new posts for alert", "alert", alert.Title)

			res, err := divar.Search(alert.Link)
			if err != nil {
				sugar.Errorw("Failed to search for alert", "error", err, "alert", alert.Title)
				if updateErr := database.UpdateAlertLastCheckedForGeneration(alert.Id, alert.Generation, time.Now().Unix()); updateErr != nil {
					sugar.Errorw("Failed to update last time checked", "error", updateErr, "alert", alert.Title)
				}
				continue
			}

			// Process posts
			if len(res.ListWidgets) > 0 {
				slices.Reverse(res.ListWidgets)

				for _, post := range res.ListWidgets {
					err := database.WithActiveAlert(alert, func() error {
						// check if post is already in database
						exists, err := database.PostExists(post.Data.Token, alert.Id)
						if err != nil {
							sugar.Errorw("Failed to check post existence", "error", err, "post", post.Data.Title)
							return err
						}

						if !exists {
							var evaluation *ai.EvaluationResult
							var importantData divar.PostImportantData
							postDetail, err := divar.GetPostContext(ctx, post.Data.Token)
							if err != nil {
								sugar.Errorw("Failed to get post details", "error", err, "token", post.Data.Token)
							} else {
								importantData = divar.ExtractImportantData(postDetail)
								if alert.AIEnabled {
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
								text += "🤖 تحلیل آگهی:\n"
								text += fmt.Sprintf("امتیاز: %d از ۱۰۰\n", evaluation.Score)
								if evaluation.PriceScore > 0 {
									text += fmt.Sprintf("امتیاز قیمت: %d از ۱۰۰\n", evaluation.PriceScore)
								}
								if evaluation.LocationScore > 0 {
									text += fmt.Sprintf("امتیاز موقعیت: %d از ۱۰۰\n", evaluation.LocationScore)
								}
								if evaluation.FeaturesScore > 0 {
									text += fmt.Sprintf("امتیاز امکانات: %d از ۱۰۰\n", evaluation.FeaturesScore)
								}
								text += fmt.Sprintf("پیشنهاد: %s\n", evaluation.Decision)
								text += fmt.Sprintf("توضیح: %s\n\n", evaluation.Explanation)
							}

							if alert.AIEnabled && evaluation == nil {
								text += "تحلیل آگهی فعلاً در دسترس نیست؛ آگهی را بدون امتیاز می‌فرستم.\n\n"
							}
							text += fmt.Sprintf("https://divar.ir/v/%s", post.Data.Token)

							// Step 6: Send the notification before recording the post as delivered.
							messageID, err := sendPostNotification(ctx, alert.Platform, alert.ChatId, post.Data.ImageURL, text)
							if err != nil {
								sugar.Errorw("Failed to notify user", "error", err, "alert", alert.Title)
								return err
							}
							if importantData.Location.HasPoint() {
								if err := sendListingLocation(ctx, alert.Platform, alert.ChatId, messageID, importantData.Location); err != nil {
									sugar.Warnw("Unable to send listing location", "error", err, "alert", alert.Title)
								}
							}

							// Step 7: Record successful delivery so failed sends can be retried.
							err = database.InsertPost(post.Data.Token, alert.Id, post, time.Now().Unix())
							if err != nil {
								sugar.Errorw("Failed to save delivered post", "error", err, "post", post.Data.Title)
								return err
							}

							if alert.AIEnabled && importantData.Token != "" {
								if err := database.AppendHistory(alert.Id, importantData); err != nil {
									sugar.Errorw("Failed to save post to history", "error", err, "post", post.Data.Title)
								}
							}
						}
						return nil
					})
					if err != nil {
						sugar.Warnw("Legacy delivery stopped", "alert_id", alert.Id, "error", err)
						if errors.Is(err, sql.ErrNoRows) {
							break
						}
					}
				}
			} else {
				sugar.Infow("No new posts found for alert", "alert", alert.Title)
			}

			// Update last time checked
			err = database.UpdateAlertLastCheckedForGeneration(alert.Id, alert.Generation, time.Now().Unix())
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

var unservedPlatforms sync.Map

// warnUnservedPlatform logs once per platform whose alerts are paused because
// its bot token is not configured.
func warnUnservedPlatform(platform string) {
	if _, warned := unservedPlatforms.LoadOrStore(platform, true); !warned {
		sugar.Warnw("Alerts paused: no bot configured for their platform", "platform", platform)
	}
}

// sendPostNotification sends a listing and returns the sent message's ID.
func sendPostNotification(ctx context.Context, platform string, chatID int64, imageURL, text string) (int, error) {
	m := messengerFor(platform)
	if m == nil {
		return 0, fmt.Errorf("no bot configured for platform %q", platform)
	}
	b := m.bot
	if imageURL != "" {
		message, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:  chatID,
			Photo:   &models.InputFileString{Data: imageURL},
			Caption: truncateText(text, 1024),
		})
		if err == nil {
			return message.ID, nil
		}
	}
	message, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: truncateText(text, 4096)})
	if err != nil {
		return 0, err
	}
	return message.ID, nil
}

// sendListingLocation sends a listing's location as a map pin replying to the
// listing message. Approximate locations carry their radius, which Telegram
// draws as an accuracy circle (up to 1500 m).
func sendListingLocation(ctx context.Context, platform string, chatID int64, replyTo int, location divar.LocationInfo) error {
	m := messengerFor(platform)
	if m == nil {
		return fmt.Errorf("no bot configured for platform %q", platform)
	}
	params := &bot.SendLocationParams{
		ChatID:             chatID,
		Latitude:           location.Latitude,
		Longitude:          location.Longitude,
		HorizontalAccuracy: min(location.RadiusMeters, 1500),
	}
	if replyTo != 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}
	_, err := m.bot.SendLocation(ctx, params)
	return err
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
