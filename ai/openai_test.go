package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEvaluatePostUsesCustomAnalysisPrompt(t *testing.T) {
	var received OpenAIRequest
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		return jsonResponse(`{"choices":[{"message":{"content":"{\"score\":91,\"decision\":\"خرید مناسب\",\"explanation\":\"با شرایط درخواست‌شده هماهنگ است.\"}"}}]}`), nil
	})}
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/")
	t.Setenv("OPENAI_MODEL", "test-model")

	history := make([]interface{}, maxHistoryPosts+1)
	for i := range history {
		history[i] = map[string]int{"index": i}
	}
	result, err := evaluatePost(context.Background(), client, map[string]string{"title": "car"}, history, "Recommend only accident-free cars")
	if err != nil {
		t.Fatalf("EvaluatePost() error = %v", err)
	}
	if result.Score != 91 || result.Decision != "خرید مناسب" {
		t.Fatalf("EvaluatePost() = %#v", result)
	}
	if len(received.Messages) != 2 || received.Messages[0].Role != "system" || received.Messages[1].Role != "user" {
		t.Fatalf("messages = %#v", received.Messages)
	}
	if !strings.Contains(received.Messages[1].Content, "Recommend only accident-free cars") {
		t.Fatalf("custom prompt missing from request: %q", received.Messages[1].Content)
	}
	if strings.Contains(received.Messages[1].Content, `"index": 0`) || !strings.Contains(received.Messages[1].Content, `"index": 20`) {
		t.Fatalf("history was not limited to the newest posts: %q", received.Messages[1].Content)
	}
}

func TestEvaluatePostRequiresGoal(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("called the AI without a goal")
		return nil, nil
	})}
	if _, err := evaluatePost(context.Background(), client, map[string]string{"title": "home"}, nil, " "); err == nil {
		t.Fatal("evaluated without a goal")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
