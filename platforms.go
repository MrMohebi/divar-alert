package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/mrmohebi/divar-alert/db"
)

const (
	platformTelegram = "telegram"
	platformBale     = "bale"

	defaultTelegramAPIURL = "https://api.telegram.org"
	defaultBaleAPIURL     = "https://tapi.bale.ai"
)

// platformConfig is one messaging platform the bot serves.
type platformConfig struct {
	Name   string
	Token  string
	APIURL string
}

// botConfig is the messaging setup read from the environment.
type botConfig struct {
	Platforms []platformConfig
	// LegacyPlatform owns alerts and drafts saved before platform support.
	// Empty means it cannot be inferred from the configuration.
	LegacyPlatform string
}

// loadBotConfig reads TELEGRAM_* and BALE_* settings. A single-bot setup from
// earlier versions, where TELEGRAM_API_URL points at Bale, still works and is
// treated as the Bale bot.
func loadBotConfig(getenv func(string) string) (botConfig, error) {
	telegramToken := strings.TrimSpace(getenv("TELEGRAM_BOT_TOKEN"))
	telegramURL := strings.TrimSpace(getenv("TELEGRAM_API_URL"))
	baleToken := strings.TrimSpace(getenv("BALE_BOT_TOKEN"))
	baleURL := strings.TrimSpace(getenv("BALE_API_URL"))

	legacyBale := false
	if isBaleURL(telegramURL) {
		if baleToken != "" {
			return botConfig{}, fmt.Errorf("TELEGRAM_API_URL points to Bale while BALE_BOT_TOKEN is also set; set TELEGRAM_API_URL=%s or leave it empty", defaultTelegramAPIURL)
		}
		legacyBale = true
		baleToken, baleURL = telegramToken, telegramURL
		telegramToken, telegramURL = "", ""
	}
	if telegramURL == "" {
		telegramURL = defaultTelegramAPIURL
	}
	if baleURL == "" {
		baleURL = defaultBaleAPIURL
	}

	var config botConfig
	if telegramToken != "" {
		config.Platforms = append(config.Platforms, platformConfig{Name: platformTelegram, Token: telegramToken, APIURL: telegramURL})
	}
	if baleToken != "" {
		config.Platforms = append(config.Platforms, platformConfig{Name: platformBale, Token: baleToken, APIURL: baleURL})
	}
	if len(config.Platforms) == 0 {
		return botConfig{}, fmt.Errorf("set TELEGRAM_BOT_TOKEN, BALE_BOT_TOKEN, or both")
	}

	switch legacy := strings.ToLower(strings.TrimSpace(getenv("EXISTING_ALERTS_PLATFORM"))); {
	case legacy == platformTelegram || legacy == platformBale:
		config.LegacyPlatform = legacy
	case legacy != "":
		return botConfig{}, fmt.Errorf("EXISTING_ALERTS_PLATFORM must be %q or %q", platformTelegram, platformBale)
	case legacyBale:
		config.LegacyPlatform = platformBale
	case len(config.Platforms) == 1:
		config.LegacyPlatform = config.Platforms[0].Name
	}
	return config, nil
}

func isBaleURL(raw string) bool {
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "bale.ai" || strings.HasSuffix(host, ".bale.ai")
}

// messenger is a running bot together with the database view for its chats.
type messenger struct {
	platform string
	bot      *bot.Bot
	db       *db.DB
}

// messengers holds every running bot. It is filled before polling starts and
// is read-only afterwards.
var messengers []*messenger

func messengerFor(platform string) *messenger {
	for _, m := range messengers {
		if m.platform == platform {
			return m
		}
	}
	return nil
}

// dbFor returns the database view for the chats of the bot that received an
// update. Bots that are not registered, as in tests, use the unscoped view.
func dbFor(b *bot.Bot) *db.DB {
	for _, m := range messengers {
		if m.bot == b {
			return m.db
		}
	}
	return database
}
