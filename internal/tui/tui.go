package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/uppu/jito/internal/commands"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/store"
)

// Run launches the TUI program.
func Run(p provider.Provider, m mode.Mode, conv *store.Conversation) error {
	return RunWith(p, m, conv, nil)
}

// RunWith is the same as Run but additionally attaches a slash-command
// registry.  Pass nil to disable the picker.
func RunWith(p provider.Provider, m mode.Mode, conv *store.Conversation, reg *commands.Registry) error {
	model := NewModel(p, m, conv)
	if reg != nil {
		model.SetRegistry(reg)
	}
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}