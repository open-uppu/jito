package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/store"
)

// Run launches the TUI program.
func Run(p provider.Provider, m mode.Mode, conv *store.Conversation) error {
	model := NewModel(p, m, conv)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}