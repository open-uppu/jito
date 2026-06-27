package provider

import (
	"context"
	"fmt"
	"strings"
)

// Mock is a fallback provider for testing/offline mode.
// Returns canned responses based on input.
type Mock struct {
	name string
}

// NewMock creates a mock provider.
func NewMock(name string) *Mock {
	if name == "" {
		name = "mock"
	}
	return &Mock{name: name}
}

func (m *Mock) Name() string { return m.name }

// Chat returns a canned response (for testing TUI without API key).
func (m *Mock) Chat(ctx context.Context, system, user string) (string, error) {
	return m.craftResponse(user), nil
}

// StreamChat streams canned chunks.
func (m *Mock) StreamChat(ctx context.Context, system, user string, onChunk func(string) error) error {
	resp := m.craftResponse(user)
	words := strings.Fields(resp)
	for _, w := range words {
		if err := onChunk(w + " "); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mock) craftResponse(user string) string {
	u := strings.ToLower(user)
	switch {
	case strings.Contains(u, "help"):
		return "I'm jito in mock mode. I don't need an API key to demonstrate. Try asking about modes, slash commands, or tools!"
	case strings.Contains(u, "mode"):
		return "jito has 5 modes: dev (coding), reason (planning), create (writing), audit (security), universal (default). Use /mode <name> to switch."
	case strings.Contains(u, "tool"):
		return "jito has 4 tools: read, write, bash, list. They let me interact with your filesystem and run commands."
	case strings.Contains(u, "hello") || strings.Contains(u, "hi") || strings.Contains(u, "สวัสดี"):
		return "Hello! I'm jito, a multi-mode AI agent CLI. This is mock mode (no API key set). Set JITO_API_KEY for live mode."
	case strings.Contains(u, "who are you"):
		return "I'm jito ⚡ — a multi-mode AI agent CLI built for open-uppu Enterprise IT. Open source stack: Bubble Tea + openai-go + SQLite."
	default:
		return fmt.Sprintf("(mock) I received your message (%d chars). I'm running offline because no API key is valid. Set JITO_API_KEY for live mode.", len(user))
	}
}