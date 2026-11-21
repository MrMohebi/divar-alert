package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
)

const testChatID = 42

var (
	telegramChat = platform.Chat{Platform: platform.Telegram, ID: testChatID}
	baleChat     = platform.Chat{Platform: platform.Bale, ID: testChatID}
)

// call is one Bot API request a fake platform received.
type call struct {
	method string
	text   string
}

// fakePlatform is a Bot API server that records requests.
type fakePlatform struct {
	mu    sync.Mutex
	calls []call
	bot   *bot.Bot
}

func (f *fakePlatform) record(method, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{method, text})
}

// texts returns the text of every request to the given method.
func (f *fakePlatform) texts(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var texts []string
	for _, c := range f.calls {
		if c.method == method {
			texts = append(texts, c.text)
		}
	}
	return texts
}

func newFakePlatform(t *testing.T) *fakePlatform {
	t.Helper()
	fake := &fakePlatform{}
	nextID := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		text := r.FormValue("text")
		if text == "" {
			text = r.FormValue("caption")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("reply_markup") == "null" {
			// Telegram rejects a null reply_markup (Bale tolerates it), so a
			// rejected request is not recorded as delivered.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: object expected as reply markup"}`)
			return
		}
		fake.record(method, text)
		switch method {
		case "sendMessage", "sendPhoto", "sendLocation", "editMessageText":
			fake.mu.Lock()
			nextID++
			id := nextID
			fake.mu.Unlock()
			fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":%d,"type":"private"}}}`, id, testChatID)
		default:
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(server.Close)
	b, err := bot.New("test", bot.WithServerURL(server.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	fake.bot = b
	return fake
}

// testApp is an App served by a fake Telegram and a fake Bale bot.
type testApp struct {
	*App
	telegram, bale *fakePlatform
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	conv, err := conversation.NewService()
	if err != nil {
		t.Fatal(err)
	}
	a := New(Options{Store: store, Conversation: conv})
	ta := &testApp{App: a, telegram: newFakePlatform(t), bale: newFakePlatform(t)}
	a.registerBot(platform.Telegram, ta.telegram.bot)
	a.registerBot(platform.Bale, ta.bale.bot)
	return ta
}

// dispatch runs a handler as the bot of the given fake platform would.
func (ta *testApp) dispatch(f *fakePlatform, handler handlerFunc, update *models.Update) {
	ta.serialized(handler)(context.Background(), f.bot, update)
}

func (ta *testApp) draft(t *testing.T, chat platform.Chat) *conversation.AlertDraft {
	t.Helper()
	draft, _, err := ta.loadDraft(chat)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func textUpdate(text string) *models.Update {
	return &models.Update{Message: &models.Message{Chat: models.Chat{ID: testChatID, Type: models.ChatTypePrivate}, Text: text}}
}

func callbackUpdate(data string) *models.Update {
	return &models.Update{CallbackQuery: &models.CallbackQuery{ID: "callback", Data: data, Message: models.MaybeInaccessibleMessage{
		Message: &models.Message{ID: 1, Chat: models.Chat{ID: testChatID, Type: models.ChatTypePrivate}},
	}}}
}

// useSimplePlanner makes every conversation turn produce a ready alert.
func (ta *testApp) useSimplePlanner() {
	ta.conv.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		return ai.AlertPlan{Title: "خودرو در کرمان", CityIDs: []string{"13"}, Category: "light", Interval: 60, AdditionalPreferences: []string{"قیمت مناسبی داشته باشد"}}, nil
	}
	ta.conv.GetFilters = func(ctx context.Context, p divar.FiltersParams) (*divar.FiltersResult, error) {
		return &divar.FiltersResult{Category: p.Category, CityIDs: p.CityIDs, SearchData: divar.FilterSearchData{FormData: divar.FilterFormData{Data: divar.FilterValues{"category": json.RawMessage(`{"str":{"value":"light"}}`)}}}}, nil
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
