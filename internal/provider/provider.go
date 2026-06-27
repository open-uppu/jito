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
			// OpenAI-compatible endpoint, but model name needs adjustment
			apiKey = k
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key found (set %s or MINIMAX_API_KEY env var)", cfg.Provider.APIKeyEnv)
	}

	model := cfg.Provider.Model
	if modelOverride != "" {
		model = modelOverride
	}

	switch strings.ToLower(cfg.Provider.Name) {
	case "minimax", "openai", "openai-compatible":
		return NewOpenAICompat(cfg.Provider.Name, cfg.Provider.BaseURL, model, apiKey), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider.Name)
	}
}