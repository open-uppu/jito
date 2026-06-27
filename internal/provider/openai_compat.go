package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAICompat implements an OpenAI-compatible HTTP provider (Minimax, OpenRouter, etc.)
type OpenAICompat struct {
	name    string
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// NewOpenAICompat creates a provider for any OpenAI-compatible endpoint.
func NewOpenAICompat(name, baseURL, model, apiKey string) *OpenAICompat {
	return &OpenAICompat{
		name:    name,
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAICompat) Name() string { return p.name }

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat calls the chat completions endpoint.
func (p *OpenAICompat) Chat(ctx context.Context, system, user string) (string, error) {
	reqBody := chatRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("provider %s returned %d: %s", p.name, resp.StatusCode, string(respBody))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal: %w (body: %s)", err, string(respBody))
	}

	if parsed.Error != nil {
		return "", fmt.Errorf("provider error: %s (%s)", parsed.Error.Message, parsed.Error.Type)
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}

	return parsed.Choices[0].Message.Content, nil
}