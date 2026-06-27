package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// OpenAICompat implements an OpenAI-compatible HTTP provider (Minimax, OpenRouter, etc.)
// using the official openai-go SDK for streaming + tool support.
type OpenAICompat struct {
	name string
	client *openai.Client
	model string
}

// NewOpenAICompat creates a provider for any OpenAI-compatible endpoint.
func NewOpenAICompat(name, baseURL, model, apiKey string) *OpenAICompat {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &OpenAICompat{
		name:   name,
		client: openai.NewClientWithConfig(cfg),
		model:  model,
	}
}

func (p *OpenAICompat) Name() string { return p.name }

// Chat calls the chat completions endpoint (non-streaming).
func (p *OpenAICompat) Chat(ctx context.Context, system, user string) (string, error) {
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: p.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}
	return resp.Choices[0].Message.Content, nil
}

// StreamChat streams chunks via callback.
func (p *OpenAICompat) StreamChat(ctx context.Context, system, user string, onChunk func(string) error) error {
	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: p.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: true,
	})
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	defer stream.Close()

	for {
		resp, err := stream.Recv()
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			return err
		}
		if len(resp.Choices) > 0 {
			chunk := resp.Choices[0].Delta.Content
			if chunk != "" {
				if err := onChunk(chunk); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// --- Env helpers (used by package main when wiring up) ---

func envOrAny(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}