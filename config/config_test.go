package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/platform"
)

func TestLoad(t *testing.T) {
	base := map[string]string{"DB_PATH": "./data/bot.db"}
	with := func(extra map[string]string) map[string]string {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}
	tests := []struct {
		name    string
		env     map[string]string
		bots    []Bot
		poll    time.Duration
		rent    float64
		wantErr bool
	}{
		{
			name: "both platforms with defaults",
			env:  with(map[string]string{"TELEGRAM_BOT_TOKEN": "tg", "BALE_BOT_TOKEN": "bl"}),
			bots: []Bot{
				{Platform: platform.Telegram, Token: "tg", APIURL: "https://api.telegram.org"},
				{Platform: platform.Bale, Token: "bl", APIURL: "https://tapi.bale.ai"},
			},
			poll: time.Second,
			rent: 80,
		},
		{
			name: "Bale only with custom URL and poll interval",
			env:  with(map[string]string{"BALE_BOT_TOKEN": "bl", "BALE_API_URL": "http://mock/", "POLL_INTERVAL_SECONDS": "5", "RENT_INFLATION_PERCENT": "45.5"}),
			bots: []Bot{{Platform: platform.Bale, Token: "bl", APIURL: "http://mock"}},
			poll: 5 * time.Second,
			rent: 45.5,
		},
		{name: "no token", env: with(nil), wantErr: true},
		{name: "no database path", env: map[string]string{"TELEGRAM_BOT_TOKEN": "tg"}, wantErr: true},
		{name: "bad poll interval", env: with(map[string]string{"TELEGRAM_BOT_TOKEN": "tg", "POLL_INTERVAL_SECONDS": "0"}), wantErr: true},
		{name: "bad rent inflation", env: with(map[string]string{"TELEGRAM_BOT_TOKEN": "tg", "RENT_INFLATION_PERCENT": "-5"}), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := Load(func(key string) string { return tt.env[key] })
			if tt.wantErr {
				if err == nil {
					t.Fatalf("config=%+v, want error", config)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config.Bots, tt.bots) || config.PollInterval != tt.poll || config.RentInflation != tt.rent || config.DBPath != "./data/bot.db" {
				t.Fatalf("config=%+v", config)
			}
		})
	}
}
