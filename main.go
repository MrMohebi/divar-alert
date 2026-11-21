// Command divar-alert watches Divar searches and notifies chats on Telegram
// and Bale about new listings.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mrmohebi/divar-alert/app"
	"github.com/mrmohebi/divar-alert/config"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"go.uber.org/zap"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()
	log := logger.Sugar()

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warnw("Failed to load .env", "error", err)
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	platforms := make([]string, 0, len(cfg.Bots))
	for _, b := range cfg.Bots {
		platforms = append(platforms, string(b.Platform))
	}
	log.Infow("Starting divar-alert", "version", version, "platforms", platforms, "db_path", cfg.DBPath, "poll_interval", cfg.PollInterval)

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	conv, err := conversation.NewService()
	if err != nil {
		log.Fatalw("Failed to load embedded catalogs", "error", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	bot := app.New(app.Options{Store: store, Conversation: conv, Logger: log, PollInterval: cfg.PollInterval})
	for _, b := range cfg.Bots {
		if err := bot.AddBot(ctx, b); err != nil {
			log.Fatalw("Failed to create bot", "platform", b.Platform, "error", err)
		}
	}
	bot.Run(ctx)
}
