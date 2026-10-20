package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"go.uber.org/zap"
)

type fakeChat struct {
	mu            sync.Mutex
	texts         []string
	callbackTexts []string
	bot           *bot.Bot
}

func setupChat(t *testing.T) *fakeChat {
	t.Helper()
	oldDB, oldService, oldSugar := database, conversationService, sugar
	var err error
	database, err = db.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	conversationService, err = conversation.NewService()
	if err != nil {
		t.Fatal(err)
	}
	sugar = zap.NewNop().Sugar()
	t.Cleanup(func() { database.Close(); database = oldDB; conversationService = oldService; sugar = oldSugar })
	fake := &fakeChat{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			fake.texts = append(fake.texts, r.FormValue("text"))
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"}}}`)
		} else {
			if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
				fake.callbackTexts = append(fake.callbackTexts, r.FormValue("text"))
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(server.Close)
	fake.bot, err = bot.New("test", bot.WithServerURL(server.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	return fake
}
func textUpdate(text string) *models.Update {
	return &models.Update{Message: &models.Message{Chat: models.Chat{ID: 42, Type: "private"}, Text: text}}
}
func callbackUpdate(data string) *models.Update {
	return &models.Update{CallbackQuery: &models.CallbackQuery{ID: "callback", Data: data, Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 1, Chat: models.Chat{ID: 42, Type: "private"}}}}}
}

func setSimplePlanner() {
	conversationService.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		return ai.AlertPlan{Title: "خودرو در کرمان", CityIDs: []string{"13"}, Category: "light", Interval: 60, AdditionalPreferences: []string{"قیمت مناسبی داشته باشد"}}, nil
	}
	conversationService.GetFilters = func(ctx context.Context, p divar.FiltersParams) (*divar.FiltersResult, error) {
		return &divar.FiltersResult{Category: p.Category, CityIDs: p.CityIDs, SearchData: divar.FilterSearchData{FormData: divar.FilterFormData{Data: divar.FilterValues{"category": json.RawMessage(`{"str":{"value":"light"}}`)}}}}, nil
	}
}

func TestChatIdleTextConfirmationStaleClickAndCancellation(t *testing.T) {
	fake := setupChat(t)
	setSimplePlanner()
	ctx := context.Background()
	serialized(handlerDefault)(ctx, fake.bot, textUpdate("خودروی مناسب در کرمان می‌خواهم"))
	draft, _, err := loadDraft(42)
	if err != nil || draft == nil || draft.State != "confirm" {
		t.Fatalf("conversation failed: %+v, %v", draft, err)
	}
	oldCallback := draftCallback(draft, "confirm")
	serialized(handlerCallbackDraft)(ctx, fake.bot, callbackUpdate(draftCallback(draft, "edit")))
	edited, _, _ := loadDraft(42)
	if edited.State != "edit" {
		t.Fatal("edit not applied")
	}
	serialized(handlerCallbackDraft)(ctx, fake.bot, callbackUpdate(oldCallback))
	alerts, _ := database.GetAllAlerts()
	if len(alerts) != 0 {
		t.Fatal("stale callback created alert")
	}
	serialized(handlerDefault)(ctx, fake.bot, textUpdate("همین شرایط در کرمان باشد"))
	draft, _, _ = loadDraft(42)
	confirm := draftCallback(draft, "confirm")
	serialized(handlerCallbackDraft)(ctx, fake.bot, callbackUpdate(confirm))
	serialized(handlerCallbackDraft)(ctx, fake.bot, callbackUpdate(confirm))
	alerts, _ = database.GetAllAlerts()
	if len(alerts) != 1 || alerts[0].Interval != 60 || alerts[0].SearchQuery == "" || !alerts[0].AIEnabled || alerts[0].AnalysisPrompt == "" {
		t.Fatalf("wrong saved alerts: %+v", alerts)
	}
	if draft, _, _ = loadDraft(42); draft != nil {
		t.Fatal("confirmed draft remains")
	}
	serialized(handlerAlertSet)(ctx, fake.bot, textUpdate("/alert_set"))
	draft, _, _ = loadDraft(42)
	serialized(handlerCallbackDraft)(ctx, fake.bot, callbackUpdate(draftCallback(draft, "cancel")))
	if draft, _, _ = loadDraft(42); draft != nil {
		t.Fatal("cancel did not remove draft")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, text := range fake.texts {
		if !ai.PersianText(text) {
			t.Fatalf("non-Persian bot message %q", text)
		}
	}
	for _, text := range fake.callbackTexts {
		if text != "" && !ai.PersianText(text) {
			t.Fatalf("non-Persian callback %q", text)
		}
	}
}

func TestChatErrorRetryAndResume(t *testing.T) {
	fake := setupChat(t)
	ctx := context.Background()
	conversationService.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		return ai.AlertPlan{}, errors.New("provider unavailable")
	}
	serialized(handlerDefault)(ctx, fake.bot, textUpdate("خودرو در کرمان"))
	draft, _, _ := loadDraft(42)
	if draft == nil || draft.State != "error" || len(draft.Messages) != 1 {
		t.Fatal("failed turn lost input")
	}
	count := len(fake.texts)
	serialized(handlerStart)(ctx, fake.bot, textUpdate("/start"))
	if len(fake.texts) != count+1 {
		t.Fatal("start did not resume")
	}
	setSimplePlanner()
	serialized(handlerCallbackDraft)(ctx, fake.bot, callbackUpdate(draftCallback(draft, "retry")))
	draft, _, _ = loadDraft(42)
	if draft.State != "confirm" || len(draft.Messages) != 1 {
		t.Fatal("retry duplicated text or failed")
	}
	serialized(handlerCancel)(ctx, fake.bot, textUpdate("/cancel"))
	process, _ := database.GetProcess(42)
	if process != nil {
		t.Fatal("cancel failed")
	}
}

func TestDraftButtonsHaveThreeChoicesAndBoundedCallbackData(t *testing.T) {
	setupChat(t)
	draft, _ := conversation.NewDraft()
	draft.State = "question"
	draft.Plan = &ai.AlertPlan{Title: "اعلان", Question: &ai.AlertQuestion{Kind: "filter", Text: "کدام نوع؟", Options: []string{"اول", "دوم", "فرقی ندارد"}}}
	_, keyboard := draftView(draft)
	if len(keyboard.InlineKeyboard) != 4 {
		t.Fatal("expected three options and cancel")
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if len(button.CallbackData) > 64 {
				t.Fatal("callback exceeds messaging limit")
			}
		}
	}
}

func TestCancelInterruptsInFlightConversation(t *testing.T) {
	fake := setupChat(t)
	entered := make(chan struct{})
	conversationService.PlanTurn = func(ctx context.Context, in ai.AlertTurnInput) (ai.AlertPlan, error) {
		close(entered)
		<-ctx.Done()
		return ai.AlertPlan{}, ctx.Err()
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		serialized(handlerDefault)(context.Background(), fake.bot, textUpdate("خودرو در کرمان"))
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("interpretation did not start")
	}
	serialized(handlerCancel)(context.Background(), fake.bot, textUpdate("/cancel"))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not interrupt interpretation")
	}
	process, err := database.GetProcess(42)
	if err != nil || process != nil {
		t.Fatal("canceled interpretation resurrected draft")
	}
}
