package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/uppu/jito/internal/config"
)

// Provider is the interface every LLM provider must implement.
type Provider interface {
	Name() string
	Chat(ctx context.Context, system, user string) (string, error)
	StreamChat(ctx context.Context, system, user string, onChunk func(string) error) error
}

// NewFromConfig returns a provider based on config + env.
// Defaults to Minimax if no config found.
func NewFromConfig(modelOverride string) (Provider, error) {
	cfg, err := config.Load()
	if err != nil {
		// fallback to env-only config
		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:      "minimax",
				BaseURL:   "https://api.minimax.io/v1",
				Model:     "MiniMax-M3",
				APIKeyEnv: "JITO_API_KEY",
			},
		}
	}

	apiKey := os.Getenv(cfg.Provider.APIKeyEnv)
	if apiKey == "" {
		// Try common fallbacks
		if k := os.Getenv("MINIMAX_API_KEY"); k != "" {
			apiKey = k
		} else if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			apiKey = k
		}
	}
	_ = apiKey // currently unused when fallback to mock

	// If JITO_MOCK=1 or no API key found, use mock provider
	if os.Getenv("JITO_MOCK") == "1" {
		return NewMock("mock"), nil
	}

	model := cfg.Provider.Model
	if modelOverride != "" {
		model = modelOverride
	}

	switch strings.ToLower(cfg.Provider.Name) {
	case "mock":
		return NewMock("mock"), nil
	case "minimax", "openai", "openai-compatible":
		if apiKey == "" {
			return nil, fmt.Errorf("no API key found (set %s or MINIMAX_API_KEY env var, or use JITO_MOCK=1)", cfg.Provider.APIKeyEnv)
		}
		return NewOpenAICompat(cfg.Provider.Name, cfg.Provider.BaseURL, model, apiKey), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider.Name)
	}
}