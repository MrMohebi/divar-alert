package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// EvaluationResult represents the AI's evaluation of a rental post
type EvaluationResult struct {
	Score         int    `json:"score"`          // 1-100
	PriceScore    int    `json:"price_score"`    // 1-100
	LocationScore int    `json:"location_score"` // 1-100
	FeaturesScore int    `json:"features_score"` // 1-100
	Decision      string `json:"decision"`       // "Worth Renting", "Worth Buying", "Not Worth It"
	Explanation   string `json:"explanation"`    // Brief explanation
}

// OpenAIRequest represents the structure for OpenAI API requests
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIResponse represents the structure for OpenAI API responses
type OpenAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// EvaluateRentalPost sends the post data and history to OpenAI for evaluation
func EvaluateRentalPost(newPost interface{}, history []interface{}) (*EvaluationResult, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	model := os.Getenv("OPENAI_MODEL")

	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	// Marshal data to JSON
	newPostJSON, err := json.MarshalIndent(newPost, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new post: %w", err)
	}

	historyJSON, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history: %w", err)
	}

	// Construct the prompt
	prompt := fmt.Sprintf(`
You are an AI real-estate evaluation agent focused on the Iranian market.

You receive:

1. A new property post (structured data)
2. A history of previous posts for the same alert

Your tasks:

- Carefully read and understand the history of previous posts
- Compare the new post with previous ones
- Evaluate its quality, value, and attractiveness
- Consider the typical Iranian real-estate market conditions: 60%% of your evaluation should reflect local market trends, norms, and prices

⚠️ Scoring Rules:
- 70%% of the final score MUST be based on price compared to similar properties in the history
- The remaining 30%% should consider:
  - Location quality
  - Property features (size, rooms, condition, amenities)
  - Overall value and attractiveness
- Include 60%% of the influence of Iranian market context in your assessment

New Post:
%s

History of Previous Posts:
%s

Output your response ONLY as a raw JSON object with this exact structure (no markdown, no code blocks, no extra text):

{
  "score": <number from 1 to 100>,
  "price_score": <number from 1 to 100>,
  "location_score": <number from 1 to 100>,
  "features_score": <number from 1 to 100>,
  "decision": "<Worth Renting|Worth Buying|Not Worth It>",
  "explanation": "<brief explanation in Persian, 2–3 sentences>"
}

Rules:
- Be objective, practical, and concise
- score must be computed as:
  score = 0.7 * price_score + 0.15 * location_score + 0.15 * features_score
- Base your judgment mainly on price vs similar posts
- RESPOND ONLY IN PERSIAN
- Do NOT add anything outside the JSON object
`, string(newPostJSON), string(historyJSON))

	// Create OpenAI request
	reqBody := OpenAIRequest{
		Model: model,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := fmt.Sprintf("%s/chat/completions", baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OpenAI response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in OpenAI response")
	}

	// Parse the evaluation result from the AI's response
	content := openAIResp.Choices[0].Message.Content

	var result EvaluationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse AI evaluation result: %w (content: %s)", err, content)
	}

	return &result, nil
}
