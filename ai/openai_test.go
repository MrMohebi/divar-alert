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
		return jsonResponse(`{"choices":[{"message":{"content":"{\"score\":91,\"decision\":\"Buy\",\"explanation\":\"Matches the requested condition.\"}"}}]}`), nil
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
	if result.Score != 91 || result.Decision != "Buy" {
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

func TestEvaluatePostUsesRentalDefaultForLegacyAlert(t *testing.T) {
	var received OpenAIRequest
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		return jsonResponse(`{"choices":[{"message":{"content":"{\"score\":80,\"price_score\":85,\"location_score\":70,\"features_score\":75,\"decision\":\"Worth Renting\",\"explanation\":\"Good value.\"}"}}]}`), nil
	})}
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "https://example.test")

	result, err := evaluatePost(context.Background(), client, map[string]string{"title": "home"}, nil, "")
	if err != nil {
		t.Fatalf("EvaluatePost() error = %v", err)
	}
	if result.PriceScore != 85 {
		t.Fatalf("price score = %d", result.PriceScore)
	}
	if !strings.Contains(received.Messages[1].Content, DefaultAnalysisPrompt) {
		t.Fatal("legacy rental prompt missing from request")
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
