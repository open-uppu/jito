package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/uppu/jito/internal/config"
)

// Failover wraps multiple providers and tries each in order.
type Failover struct {
	name     string
	primary  Provider
	fallback []Provider
}

// NewFailover creates a failover provider.
// If primary fails, tries each fallback in order.
func NewFailover(primary Provider, fallback ...Provider) *Failover {
	return &Failover{
		name:     "failover(" + primary.Name() + ")",
		primary:  primary,
		fallback: fallback,
	}
}

func (f *Failover) Name() string { return f.name }

// Chat tries primary first, then fallbacks on error.
func (f *Failover) Chat(ctx context.Context, system, user string) (string, error) {
	resp, err := f.primary.Chat(ctx, system, user)
	if err == nil {
		return resp, nil
	}
	// Try fallbacks
	for _, fb := range f.fallback {
		resp, fbErr := fb.Chat(ctx, system, user)
		if fbErr == nil {
			return resp, nil
		}
		err = fmt.Errorf("%w; fallback %s: %v", err, fb.Name(), fbErr)
	}
	return "", err
}

// StreamChat tries primary, then fallbacks. Streams via first successful provider.
func (f *Failover) StreamChat(ctx context.Context, system, user string, onChunk func(string) error) error {
	err := f.primary.StreamChat(ctx, system, user, onChunk)
	if err == nil {
		return nil
	}
	for _, fb := range f.fallback {
		if fbErr := fb.StreamChat(ctx, system, user, onChunk); fbErr == nil {
			return nil
		}
	}
	return fmt.Errorf("all providers failed: %v", err)
}

// MultiFromConfig builds a failover from config (primary + fallbacks + mock as last resort).
func MultiFromConfig(modelOverride string) (Provider, error) {
	cfg, err := config.Load()
	if err != nil {
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
		apiKey = os.Getenv("MINIMAX_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	// If mock mode or no key, use mock directly
	if os.Getenv("JITO_MOCK") == "1" {
		return NewMock("mock"), nil
	}
	if apiKey == "" {
		// No key: degrade to mock gracefully
		return NewMock("mock"), nil
	}

	model := cfg.Provider.Model
	if modelOverride != "" {
		model = modelOverride
	}

	primary := NewOpenAICompat(cfg.Provider.Name, cfg.Provider.BaseURL, model, apiKey)
	fallbacks := []Provider{NewMock("mock")} // always have mock as last resort

	for _, fb := range cfg.FallbackProviders {
		if k := os.Getenv(strings.ToUpper(fb.Name) + "_API_KEY"); k != "" {
			fallbacks = append(fallbacks, NewOpenAICompat(fb.Name, fb.BaseURL, fb.Model, k))
		}
	}

	return NewFailover(primary, fallbacks...), nil
}