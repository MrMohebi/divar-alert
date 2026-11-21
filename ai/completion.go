package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// chatChunk is one choice of a chat completion, either complete (message) or
// streamed (delta).
type chatChunk struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// completionContent returns the assistant text of a chat completion body.
// Some OpenAI-compatible providers stream server-sent events even when the
// request does not ask for it, so both a JSON object and an SSE stream are
// accepted.
func completionContent(body []byte) (string, error) {
	trimmed := bytes.TrimSpace(body)
	if isEventStream(trimmed) {
		return streamContent(trimmed)
	}
	var chunk chatChunk
	if err := json.Unmarshal(trimmed, &chunk); err != nil {
		return "", fmt.Errorf("decode AI provider response: %w (body starts with %q)", err, snippet(trimmed))
	}
	if chunk.Error != nil {
		return "", fmt.Errorf("AI provider error: %s", chunk.Error.Message)
	}
	if len(chunk.Choices) == 0 {
		return "", errors.New("AI provider response has no choices")
	}
	return stripCodeFence(chunk.Choices[0].Message.Content), nil
}

func isEventStream(body []byte) bool {
	return bytes.HasPrefix(body, []byte("data:")) || bytes.HasPrefix(body, []byte("event:")) || bytes.HasPrefix(body, []byte(":"))
}

func streamContent(body []byte) (string, error) {
	var text strings.Builder
	sawChoice := false
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64<<10), len(body)+1)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("decode AI provider stream event: %w (event starts with %q)", err, snippet([]byte(data)))
		}
		if chunk.Error != nil {
			return "", fmt.Errorf("AI provider error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		sawChoice = true
		choice := chunk.Choices[0]
		text.WriteString(choice.Delta.Content)
		text.WriteString(choice.Message.Content)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read AI provider stream: %w", err)
	}
	if !sawChoice {
		return "", errors.New("AI provider stream has no choices")
	}
	return stripCodeFence(text.String()), nil
}

func stripCodeFence(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// snippet shortens a response body for error messages.
func snippet(body []byte) string {
	const max = 120
	runes := []rune(string(body))
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return string(runes)
}
