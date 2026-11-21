package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPlannerAcceptsStreamedResponse(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		if stream, ok := raw["stream"]; !ok || stream != false {
			t.Errorf("request must ask for a non-streamed response, got stream=%v", raw["stream"])
		}
		content := `{"title":"خودرو در کرمان","city_ids":["13"],"category_slug":"light","selections":{},"question":null}`
		var events strings.Builder
		events.WriteString(": keep-alive\n\n")
		for _, part := range []string{content[:20], content[20:]} {
			chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": part}}}})
			events.WriteString("data: " + string(chunk) + "\n\n")
		}
		events.WriteString("data: [DONE]\n\n")
		return jsonResponse(events.String()), nil
	})}
	plan, err := planAlertTurn(context.Background(), client, AlertTurnInput{})
	if err != nil || plan.Category != "light" || plan.Title != "خودرو در کرمان" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestCompletionContentReportsUnreadableBodies(t *testing.T) {
	if _, err := completionContent([]byte("deployment not found")); err == nil || !strings.Contains(err.Error(), "deployment not found") {
		t.Fatalf("error should quote the body, got %v", err)
	}
	if _, err := completionContent([]byte(`{"error":{"message":"quota exceeded"}}`)); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("provider error not surfaced, got %v", err)
	}
	if _, err := completionContent([]byte("data: [DONE]\n\n")); err == nil {
		t.Fatal("empty stream accepted")
	}
}
