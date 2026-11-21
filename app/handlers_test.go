package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
)

func TestConversationConfirmEditStaleButtonAndCancel(t *testing.T) {
	ta := newTestApp(t)
	ta.useSimplePlanner()
	tg := ta.telegram

	ta.dispatch(tg, ta.handleText, textUpdate("خودروی مناسب در کرمان می‌خواهم"))
	draft := ta.draft(t, telegramChat)
	if draft == nil || draft.State != "confirm" {
		t.Fatalf("conversation failed: %+v", draft)
	}
	oldConfirm := draftCallback(draft, "confirm")
	ta.dispatch(tg, ta.handleDraftButton, callbackUpdate(draftCallback(draft, "edit")))
	if ta.draft(t, telegramChat).State != "edit" {
		t.Fatal("edit not applied")
	}
	ta.dispatch(tg, ta.handleDraftButton, callbackUpdate(oldConfirm))
	if alerts, _ := ta.store.ActiveAlerts(); len(alerts) != 0 {
		t.Fatal("a stale button created an alert")
	}

	ta.dispatch(tg, ta.handleText, textUpdate("همین شرایط در کرمان باشد"))
	confirm := draftCallback(ta.draft(t, telegramChat), "confirm")
	ta.dispatch(tg, ta.handleDraftButton, callbackUpdate(confirm))
	ta.dispatch(tg, ta.handleDraftButton, callbackUpdate(confirm))
	alerts, _ := ta.store.ActiveAlerts()
	if len(alerts) != 1 || alerts[0].Chat != telegramChat || alerts[0].Interval != 60 || alerts[0].SearchQuery == "" ||
		!alerts[0].AIEnabled || alerts[0].AnalysisPrompt == "" || alerts[0].OriginalPrompt == "" {
		t.Fatalf("wrong saved alerts: %+v", alerts)
	}
	if ta.draft(t, telegramChat) != nil {
		t.Fatal("the confirmed draft remains")
	}

	ta.dispatch(tg, ta.handleNewAlert, textUpdate("/alert_set"))
	ta.dispatch(tg, ta.handleDraftButton, callbackUpdate(draftCallback(ta.draft(t, telegramChat), "cancel")))
	if ta.draft(t, telegramChat) != nil {
		t.Fatal("cancel did not remove the draft")
	}

	for _, method := range []string{"sendMessage", "answerCallbackQuery"} {
		for _, text := range tg.texts(method) {
			if text != "" && !ai.PersianText(text) {
				t.Fatalf("non-Persian %s %q", method, text)
			}
		}
	}
	if len(ta.bale.calls) != 0 {
		t.Fatalf("a Telegram conversation reached Bale: %+v", ta.bale.calls)
	}
}

func TestAIIsOnByDefaultWithoutExtraPreferences(t *testing.T) {
	ta := newTestApp(t)
	ta.useSimplePlanner()
	planner := ta.conv.PlanTurn
	ta.conv.PlanTurn = func(ctx context.Context, in ai.AlertTurnInput) (ai.AlertPlan, error) {
		plan, err := planner(ctx, in)
		plan.AdditionalPreferences = nil
		return plan, err
	}
	ta.dispatch(ta.bale, ta.handleText, textUpdate("خودرو در کرمان"))
	ta.dispatch(ta.bale, ta.handleDraftButton, callbackUpdate(draftCallback(ta.draft(t, baleChat), "confirm")))
	alerts, _ := ta.store.ChatAlerts(baleChat)
	if len(alerts) != 1 || !alerts[0].AIEnabled || alerts[0].AnalysisPrompt != "" {
		t.Fatalf("alerts = %+v", alerts)
	}
}

func TestFailedTurnKeepsInputAndRetryResumes(t *testing.T) {
	ta := newTestApp(t)
	tg := ta.telegram
	ta.conv.PlanTurn = func(context.Context, ai.AlertTurnInput) (ai.AlertPlan, error) {
		return ai.AlertPlan{}, errors.New("provider unavailable")
	}
	ta.dispatch(tg, ta.handleText, textUpdate("خودرو در کرمان"))
	draft := ta.draft(t, telegramChat)
	if draft == nil || draft.State != "error" || len(draft.Messages) != 1 {
		t.Fatal("a failed turn lost the input")
	}
	sent := len(tg.texts("sendMessage"))
	ta.dispatch(tg, ta.handleStart, textUpdate("/start"))
	if len(tg.texts("sendMessage")) != sent+1 {
		t.Fatal("/start did not resume the draft")
	}
	ta.useSimplePlanner()
	ta.dispatch(tg, ta.handleDraftButton, callbackUpdate(draftCallback(draft, "retry")))
	if draft = ta.draft(t, telegramChat); draft.State != "confirm" || len(draft.Messages) != 1 {
		t.Fatal("retry duplicated the text or failed")
	}
	ta.dispatch(tg, ta.handleCancel, textUpdate("/cancel"))
	if ta.draft(t, telegramChat) != nil {
		t.Fatal("/cancel failed")
	}
}

func TestCancelInterruptsInFlightTurn(t *testing.T) {
	ta := newTestApp(t)
	entered := make(chan struct{})
	ta.conv.PlanTurn = func(ctx context.Context, in ai.AlertTurnInput) (ai.AlertPlan, error) {
		close(entered)
		<-ctx.Done()
		return ai.AlertPlan{}, ctx.Err()
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ta.dispatch(ta.telegram, ta.handleText, textUpdate("خودرو در کرمان"))
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the turn did not start")
	}
	ta.dispatch(ta.telegram, ta.handleCancel, textUpdate("/cancel"))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("/cancel did not interrupt the turn")
	}
	if ta.draft(t, telegramChat) != nil {
		t.Fatal("the canceled turn resurrected the draft")
	}
}

func TestDraftButtonsFitCallbackLimit(t *testing.T) {
	ta := newTestApp(t)
	draft, _ := conversation.NewDraft()
	draft.State = "question"
	draft.Plan = &ai.AlertPlan{Title: "اعلان", Question: &ai.AlertQuestion{Kind: "filter", Text: "کدام نوع؟", Options: []string{"اول", "دوم", "فرقی ندارد"}}}
	_, keyboard := ta.draftView(draft)
	if len(keyboard.InlineKeyboard) != 4 {
		t.Fatal("expected three options and cancel")
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if len(button.CallbackData) > 64 {
				t.Fatal("callback data exceeds the Bot API limit")
			}
		}
	}
}

// The same chat ID on Telegram and Bale is two different users.
func TestSameChatIDOnBothPlatformsStaysSeparate(t *testing.T) {
	ta := newTestApp(t)
	ta.useSimplePlanner()
	ta.dispatch(ta.bale, ta.handleText, textUpdate("خودرو در کرمان"))
	if ta.draft(t, telegramChat) != nil || ta.draft(t, baleChat) == nil {
		t.Fatal("the Bale draft is not Bale's alone")
	}
	ta.dispatch(ta.bale, ta.handleDraftButton, callbackUpdate(draftCallback(ta.draft(t, baleChat), "confirm")))

	ta.dispatch(ta.telegram, ta.handleAlertList, textUpdate("/alerts"))
	ta.dispatch(ta.bale, ta.handleAlertList, textUpdate("/alerts"))
	telegramList := ta.telegram.texts("sendMessage")
	baleList := ta.bale.texts("sendMessage")
	if !strings.Contains(telegramList[len(telegramList)-1], "هنوز اعلانی ندارید") || !strings.Contains(baleList[len(baleList)-1], "خودرو در کرمان") {
		t.Fatalf("alert lists leaked across platforms:\ntelegram=%q\nbale=%q", telegramList, baleList)
	}

	alerts, _ := ta.store.ChatAlerts(baleChat)
	ta.dispatch(ta.telegram, ta.handleConfirmDelete, callbackUpdate("confirm_delete-"+itoa(alerts[0].ID)))
	if remaining, _ := ta.store.ChatAlerts(baleChat); len(remaining) != 1 {
		t.Fatal("a Telegram user deleted a Bale alert")
	}
}

func TestTogglingAIUpdatesTheAlert(t *testing.T) {
	ta := newTestApp(t)
	alert := db.Alert{ID: 5, Chat: telegramChat, Title: "خانه", SearchQuery: `{"city_ids":["13"]}`, Interval: 60}
	if err := ta.store.InsertAlert(alert); err != nil {
		t.Fatal(err)
	}
	ta.dispatch(ta.telegram, ta.handleToggleAI, callbackUpdate("toggle_ai-5-true"))
	alerts, _ := ta.store.ChatAlerts(telegramChat)
	if len(alerts) != 1 || !alerts[0].AIEnabled {
		t.Fatalf("alerts = %+v", alerts)
	}
	if ta.draft(t, telegramChat) != nil {
		t.Fatal("turning AI on started a conversation")
	}
}

func TestRenderAlertListShowsAIStateAndActions(t *testing.T) {
	text, keyboard := renderAlertList([]db.Alert{
		{ID: 10, Title: "cars", Interval: 300, AIEnabled: true},
		{ID: 20, Title: "jobs", Interval: 900, AIEnabled: false},
	})
	if !strings.Contains(text, "cars") || !strings.Contains(text, "روشن") || !strings.Contains(text, "خاموش") {
		t.Fatalf("rendered text = %q", text)
	}
	if keyboard == nil || len(keyboard.InlineKeyboard) != 4 {
		t.Fatalf("keyboard = %#v", keyboard)
	}
	for _, check := range []struct {
		row, col int
		want     string
	}{{0, 0, "toggle_ai-10-false"}, {0, 1, "reset_alert-10"}, {1, 0, "delete_alert-10"}, {2, 0, "toggle_ai-20-true"}} {
		if got := keyboard.InlineKeyboard[check.row][check.col].CallbackData; got != check.want {
			t.Fatalf("button %d,%d = %q, want %q", check.row, check.col, got, check.want)
		}
	}
}

func TestParseAIToggle(t *testing.T) {
	id, enabled, err := parseAIToggle("toggle_ai-42-true")
	if err != nil || id != 42 || !enabled {
		t.Fatalf("parseAIToggle() = %d, %t, %v", id, enabled, err)
	}
	if _, _, err := parseAIToggle("toggle_ai-invalid"); err == nil {
		t.Fatal("parseAIToggle() accepted invalid data")
	}
}
