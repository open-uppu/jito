package provider

import (
	"context"
)

// Provider is the interface every LLM provider must implement.
type Provider interface {
	Name() string
	Chat(ctx context.Context, system, user string) (string, error)
	StreamChat(ctx context.Context, system, user string, onChunk func(string) error) error
}

// NewFromConfig returns a provider with automatic failover + mock fallback.
func NewFromConfig(modelOverride string) (Provider, error) {
	return MultiFromConfig(modelOverride)
}