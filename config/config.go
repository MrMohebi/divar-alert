// Package config reads the bot's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrmohebi/divar-alert/platform"
	"github.com/mrmohebi/divar-alert/rent"
)

// Default Bot API endpoints.
var defaultAPIURLs = map[platform.Platform]string{
	platform.Telegram: "https://api.telegram.org",
	platform.Bale:     "https://tapi.bale.ai",
}

// Bot is one messaging platform to serve.
type Bot struct {
	Platform platform.Platform
	Token    string
	APIURL   string
}

// Config holds every setting the bot needs at startup. AI settings are read
// by the ai package itself.
type Config struct {
	Bots         []Bot
	DBPath       string
	PollInterval time.Duration
	// RentInflation is the yearly inflation, in percent, used to show each
	// rental's real monthly rent (RENT_INFLATION_PERCENT).
	RentInflation float64
}

// Load reads the configuration. Each platform is enabled by setting its
// token: TELEGRAM_BOT_TOKEN, BALE_BOT_TOKEN, or both.
func Load(getenv func(string) string) (Config, error) {
	var config Config
	for _, p := range platform.All {
		prefix := strings.ToUpper(string(p))
		token := strings.TrimSpace(getenv(prefix + "_BOT_TOKEN"))
		if token == "" {
			continue
		}
		apiURL := strings.TrimRight(strings.TrimSpace(getenv(prefix+"_API_URL")), "/")
		if apiURL == "" {
			apiURL = defaultAPIURLs[p]
		}
		config.Bots = append(config.Bots, Bot{Platform: p, Token: token, APIURL: apiURL})
	}
	if len(config.Bots) == 0 {
		return Config{}, errors.New("set TELEGRAM_BOT_TOKEN, BALE_BOT_TOKEN, or both")
	}

	config.DBPath = strings.TrimSpace(getenv("DB_PATH"))
	if config.DBPath == "" {
		return Config{}, errors.New("DB_PATH must be set")
	}

	config.PollInterval = time.Second
	if raw := strings.TrimSpace(getenv("POLL_INTERVAL_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 1 || seconds > 3600 {
			return Config{}, fmt.Errorf("POLL_INTERVAL_SECONDS must be an integer between 1 and 3600")
		}
		config.PollInterval = time.Duration(seconds) * time.Second
	}

	config.RentInflation = rent.DefaultInflation
	if raw := strings.TrimSpace(getenv("RENT_INFLATION_PERCENT")); raw != "" {
		percent, err := strconv.ParseFloat(raw, 64)
		if err != nil || percent < 0 || percent > 1000 {
			return Config{}, fmt.Errorf("RENT_INFLATION_PERCENT must be a number between 0 and 1000")
		}
		config.RentInflation = percent
	}
	return config, nil
}
