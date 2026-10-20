package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPlannerRepairsMalformedOutputAndUsesConfiguredProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_MODEL", "chosen-model")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://example.test/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("wrong provider configuration")
		}
		var request OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_object" {
			t.Error("conversation must request JSON output")
		}
		if request.Model != "chosen-model" || !strings.Contains(request.Messages[0].Content, "Every search result is sent") {
			t.Error("missing model or commentary-only instruction")
		}
		content := "invalid"
		if calls == 2 {
			if !strings.Contains(request.Messages[1].Content, "validation_error") {
				t.Error("missing repair feedback")
			}
			content = `{"title":"خودرو در کرمان","city_ids":["13"],"category_slug":"light","interval_seconds":60,"selections":{},"question":null}`
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
		return jsonResponse(string(body)), nil
	})}
	result, err := planAlertTurn(context.Background(), client, AlertTurnInput{QuestionsRemaining: 5})
	if err != nil || calls != 2 || result.Category != "light" {
		t.Fatalf("result=%+v, calls=%d, err=%v", result, calls, err)
	}
}

func TestPlannerRejectsInvalidQuestionsAndEnglish(t *testing.T) {
	for _, p := range []AlertPlan{
		{Title: "English title"},
		{Title: "اعلان", AdditionalPreferences: []string{"English preference"}},
		{Title: "اعلان", Question: &AlertQuestion{Kind: "filter", Text: "کدام مورد؟", Options: []string{"اول", "دوم"}}},
		{Title: "اعلان", Question: &AlertQuestion{Kind: "filter", Text: "کدام مورد؟", Options: []string{"اول", "اول", "سوم"}}},
		{Title: "اعلان", Question: &AlertQuestion{Kind: "arbitrary", Text: "کدام مورد؟"}},
	} {
		if err := ValidateAlertPlan(p); err == nil {
			t.Fatalf("invalid plan accepted: %+v", p)
		}
	}
	if err := ValidateAlertPlan(AlertPlan{Title: "اعلان", Question: &AlertQuestion{Kind: "city", Text: "کدام شهر؟"}}); err != nil {
		t.Fatal(err)
	}
}

func TestConversationMissingCredentialsAndProviderFailure(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := PlanAlertTurn(context.Background(), AlertTurnInput{}); err == nil {
		t.Fatal("missing credentials accepted")
	}
	t.Setenv("OPENAI_API_KEY", "key")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		res := jsonResponse("unavailable")
		res.StatusCode = 503
		return res, nil
	})}
	if _, err := planAlertTurn(context.Background(), client, AlertTurnInput{}); err == nil {
		t.Fatal("provider failure accepted")
	}
}

func TestCommentaryRequiresPersian(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "key")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"choices":[{"message":{"content":"{\"score\":5,\"decision\":\"Skip\",\"explanation\":\"Does not match.\"}"}}]}`), nil
	})}
	if _, err := evaluatePost(context.Background(), client, nil, nil, "خودرو ارزان"); err == nil {
		t.Fatal("English commentary accepted")
	}
}

func TestConversationFallsBackOnlyWhenProviderRejectsJSONMode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var request OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			if request.ResponseFormat == nil {
				t.Fatal("JSON mode was not requested")
			}
			response := jsonResponse(`{"error":{"message":"response_format json_object is not supported"}}`)
			response.StatusCode = 400
			return response, nil
		}
		if request.ResponseFormat != nil {
			t.Fatal("unsupported JSON mode was not removed")
		}
		return jsonResponse(`{"choices":[{"message":{"content":"{\"title\":\"اعلان خودرو\",\"category_slug\":\"light\",\"city_ids\":[\"13\"]}"}}]}`), nil
	})}
	result, err := planAlertTurn(context.Background(), client, AlertTurnInput{})
	if err != nil || calls != 2 || result.Category != "light" {
		t.Fatalf("fallback failed: calls=%d result=%+v error=%v", calls, result, err)
	}
	calls = 0
	client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		response := jsonResponse(`{"error":{"message":"invalid request"}}`)
		response.StatusCode = 400
		return response, nil
	})}
	if _, err := planAlertTurn(context.Background(), client, AlertTurnInput{}); err == nil || calls != 1 {
		t.Fatal("unrelated error incorrectly retried without JSON mode")
	}
}
