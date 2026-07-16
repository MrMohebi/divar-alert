package main

import (
	"strings"
	"testing"

	"github.com/mrmohebi/divar-alert/db"
)

func TestRenderAlertListShowsAIStateAndActions(t *testing.T) {
	t.Parallel()

	text, keyboard := renderAlertList([]db.Alert{
		{Id: 10, Title: "cars", Interval: 300, AIEnabled: true},
		{Id: 20, Title: "jobs", Interval: 900, AIEnabled: false},
	})
	if !strings.Contains(text, "cars") || !strings.Contains(text, "روشن") || !strings.Contains(text, "خاموش") {
		t.Fatalf("rendered text = %q", text)
	}
	if keyboard == nil || len(keyboard.InlineKeyboard) != 4 {
		t.Fatalf("keyboard = %#v", keyboard)
	}
	if got := keyboard.InlineKeyboard[0][0].CallbackData; got != "toggle_ai-10-false" {
		t.Fatalf("enabled toggle callback = %q", got)
	}
	if got := keyboard.InlineKeyboard[0][1].CallbackData; got != "reset_alert-10" {
		t.Fatalf("reset callback = %q", got)
	}
	if got := keyboard.InlineKeyboard[2][0].CallbackData; got != "toggle_ai-20-true" {
		t.Fatalf("disabled toggle callback = %q", got)
	}
}

func TestParseAIToggle(t *testing.T) {
	t.Parallel()

	id, enabled, err := parseAIToggle("toggle_ai-42-true")
	if err != nil || id != 42 || !enabled {
		t.Fatalf("parseAIToggle() = %d, %t, %v", id, enabled, err)
	}
	if _, _, err := parseAIToggle("toggle_ai-invalid"); err == nil {
		t.Fatal("parseAIToggle() accepted invalid data")
	}
}
