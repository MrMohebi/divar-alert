package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrmohebi/divar-alert/db"
)

func TestValidateStepInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stepName string
		input    string
		want     string
		wantErr  bool
	}{
		{name: "trims title", stepName: "title", input: "  خانه  ", want: "خانه"},
		{name: "empty title", stepName: "title", input: "  ", wantErr: true},
		{name: "valid link", stepName: "link", input: `curl 'https://api.divar.ir/v8/postlist/w/search'`, want: `curl 'https://api.divar.ir/v8/postlist/w/search'`},
		{name: "invalid link", stepName: "link", input: `curl 'https://example.com/v8/postlist/w/search'`, wantErr: true},
		{name: "analysis prompt", stepName: "analysis_prompt", input: "  only cheap cars  ", want: "only cheap cars"},
		{name: "analysis prompt too long", stepName: "analysis_prompt", input: strings.Repeat("x", 4001), wantErr: true},
		{name: "AI enabled", stepName: "ai_enabled", input: "true", want: "true"},
		{name: "invalid AI option", stepName: "ai_enabled", input: "yes", wantErr: true},
		{name: "valid interval", stepName: "interval", input: " 60 ", want: "60"},
		{name: "non numeric interval", stepName: "interval", input: "soon", wantErr: true},
		{name: "interval too short", stepName: "interval", input: "9", wantErr: true},
		{name: "interval too long", stepName: "interval", input: "86401", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateStepInput(tt.stepName, tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateStepInput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("validateStepInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisabledAISkipsPromptAndPersistsChoice(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "process.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })

	process, err := ProcessStart(ProcessKey.SetAlert, 42, database)
	if err != nil {
		t.Fatalf("ProcessStart() error = %v", err)
	}
	inputs := []string{
		"cars",
		`curl 'https://api.divar.ir/v8/postlist/w/search'`,
		"false",
	}
	for _, input := range inputs {
		process, err = ProcessGoNextStep(process, input, database)
		if err != nil {
			t.Fatalf("ProcessGoNextStep(%q) error = %v", input, err)
		}
	}
	if step := currentStep(process); step.Name != "interval" {
		t.Fatalf("current step after disabling AI = %#v", step)
	}
	process, err = ProcessGoNextStep(process, "300", database)
	if err != nil {
		t.Fatalf("complete process error = %v", err)
	}
	alerts, err := database.GetAlertsByChatId(42)
	if err != nil {
		t.Fatalf("GetAlertsByChatId() error = %v", err)
	}
	if len(alerts) != 1 || alerts[0].AIEnabled || alerts[0].AnalysisPrompt != "" {
		t.Fatalf("saved alerts = %#v", alerts)
	}
}

func TestAIPromptProcessEnablesExistingAlert(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "ai-prompt.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	alert := db.Alert{Id: 7, ChatId: 42, Title: "cars", Link: "curl", Interval: 300}
	if err := database.InsertAlert(alert); err != nil {
		t.Fatalf("InsertAlert() error = %v", err)
	}

	process, err := ProcessStartAIPrompt(alert.ChatId, alert.Id, database)
	if err != nil {
		t.Fatalf("ProcessStartAIPrompt() error = %v", err)
	}
	process, err = ProcessGoNextStep(process, "only accident-free cars", database)
	if err != nil {
		t.Fatalf("ProcessGoNextStep() error = %v", err)
	}
	if step := currentStep(process); step.Name != "end" {
		t.Fatalf("current step = %#v", step)
	}
	alerts, err := database.GetAlertsByChatId(alert.ChatId)
	if err != nil || len(alerts) != 1 || !alerts[0].AIEnabled || alerts[0].AnalysisPrompt != "only accident-free cars" {
		t.Fatalf("alerts = %#v, error = %v", alerts, err)
	}
}

func TestSetAlertProcessIncludesAnalysisPrompt(t *testing.T) {
	t.Parallel()

	process := setAlertEmpty(42)
	if _, err := getStepData(process, "analysis_prompt"); err != nil {
		t.Fatalf("analysis prompt step missing: %v", err)
	}
	if _, err := getStepData(process, "interval"); err != nil {
		t.Fatalf("interval step missing: %v", err)
	}
}

func TestTruncateText(t *testing.T) {
	t.Parallel()

	if got := truncateText("سلام", 5); got != "سلام" {
		t.Fatalf("truncateText() = %q", got)
	}
	if got := truncateText("سلام دنیا", 5); got != "سلام…" {
		t.Fatalf("truncateText() = %q", got)
	}
}

func TestParsePollInterval(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{input: "", want: time.Second},
		{input: "15", want: 15 * time.Second},
		{input: "0", wantErr: true},
		{input: "invalid", wantErr: true},
	} {
		got, err := parsePollInterval(test.input)
		if (err != nil) != test.wantErr || got != test.want {
			t.Fatalf("parsePollInterval(%q) = %v, %v; want %v, wantErr %t", test.input, got, err, test.want, test.wantErr)
		}
	}
}
