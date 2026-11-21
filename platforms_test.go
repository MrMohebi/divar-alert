package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/mrmohebi/divar-alert/db"
	"go.uber.org/zap"
)

func TestLoadBotConfig(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		platforms []platformConfig
		legacy    string
		wantErr   bool
	}{
		{
			name: "both platforms",
			env:  map[string]string{"TELEGRAM_BOT_TOKEN": "tg", "BALE_BOT_TOKEN": "bl"},
			platforms: []platformConfig{
				{Name: "telegram", Token: "tg", APIURL: defaultTelegramAPIURL},
				{Name: "bale", Token: "bl", APIURL: defaultBaleAPIURL},
			},
		},
		{
			name:      "previous single Bale setup",
			env:       map[string]string{"TELEGRAM_BOT_TOKEN": "bl", "TELEGRAM_API_URL": "https://tapi.bale.ai"},
			platforms: []platformConfig{{Name: "bale", Token: "bl", APIURL: "https://tapi.bale.ai"}},
			legacy:    "bale",
		},
		{
			name:      "previous single Telegram setup",
			env:       map[string]string{"TELEGRAM_BOT_TOKEN": "tg", "TELEGRAM_API_URL": "https://api.telegram.org"},
			platforms: []platformConfig{{Name: "telegram", Token: "tg", APIURL: "https://api.telegram.org"}},
			legacy:    "telegram",
		},
		{
			name: "explicit existing alerts platform",
			env:  map[string]string{"TELEGRAM_BOT_TOKEN": "tg", "BALE_BOT_TOKEN": "bl", "EXISTING_ALERTS_PLATFORM": "Bale"},
			platforms: []platformConfig{
				{Name: "telegram", Token: "tg", APIURL: defaultTelegramAPIURL},
				{Name: "bale", Token: "bl", APIURL: defaultBaleAPIURL},
			},
			legacy: "bale",
		},
		{name: "no token", env: map[string]string{}, wantErr: true},
		{name: "Bale URL with two Bale tokens", env: map[string]string{"TELEGRAM_BOT_TOKEN": "a", "TELEGRAM_API_URL": "https://tapi.bale.ai", "BALE_BOT_TOKEN": "b"}, wantErr: true},
		{name: "unknown existing alerts platform", env: map[string]string{"BALE_BOT_TOKEN": "b", "EXISTING_ALERTS_PLATFORM": "eitaa"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadBotConfig(func(key string) string { return test.env[key] })
			if test.wantErr {
				if err == nil {
					t.Fatalf("config=%+v, want error", config)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config.Platforms, test.platforms) || config.LegacyPlatform != test.legacy {
				t.Fatalf("config=%+v", config)
			}
		})
	}
}

// Each alert must reach its user through the bot of the platform it was made on.
func TestHandlersAndNotificationsUseTheirOwnPlatform(t *testing.T) {
	oldDB, oldMessengers, oldSugar := database, messengers, sugar
	var err error
	database, err = db.Open(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatal(err)
	}
	sugar = zap.NewNop().Sugar()
	t.Cleanup(func() { database.Close(); database, messengers, sugar = oldDB, oldMessengers, oldSugar })

	var mu sync.Mutex
	sent := map[string][]string{}
	newFakeBot := func(platform string) *bot.Bot {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseMultipartForm(1 << 20)
			mu.Lock()
			sent[platform] = append(sent[platform], r.FormValue("text"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"}}}`)
		}))
		t.Cleanup(server.Close)
		b, err := bot.New("test", bot.WithServerURL(server.URL), bot.WithSkipGetMe())
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	telegramBot, baleBot := newFakeBot("telegram"), newFakeBot("bale")
	messengers = []*messenger{
		{platform: "telegram", bot: telegramBot, db: database.ForPlatform("telegram")},
		{platform: "bale", bot: baleBot, db: database.ForPlatform("bale")},
	}

	if err := dbFor(baleBot).InsertAlert(db.Alert{Id: 1, ChatId: 42, Title: "bale-only", Link: "curl", Interval: 60}); err != nil {
		t.Fatal(err)
	}
	handlerAlertList(context.Background(), telegramBot, textUpdate("/alerts"))
	handlerAlertList(context.Background(), baleBot, textUpdate("/alerts"))
	mu.Lock()
	if len(sent["telegram"]) != 1 || len(sent["bale"]) != 1 || sent["telegram"][0] == sent["bale"][0] {
		t.Fatalf("alert lists: %+v", sent)
	}
	if want := "bale-only"; !strings.Contains(sent["bale"][0], want) || strings.Contains(sent["telegram"][0], want) {
		t.Fatalf("alert lists leaked across platforms: %+v", sent)
	}
	sent = map[string][]string{}
	mu.Unlock()

	if _, err := sendPostNotification(context.Background(), "bale", 42, "", "new post"); err != nil {
		t.Fatal(err)
	}
	if _, err := sendPostNotification(context.Background(), "eitaa", 42, "", "new post"); err == nil {
		t.Fatal("sent through an unconfigured platform")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent["bale"]) != 1 || len(sent["telegram"]) != 0 {
		t.Fatalf("notification routing: %+v", sent)
	}
}
