package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Launch interactive TUI chat (Bubble Tea)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TUI launcher will be implemented in internal/tui/
			fmt.Println("🌀 TUI not yet implemented — use `jito run` for now")
			fmt.Println("Coming in Phase 2: Bubble Tea interface")
			return nil
		},
	}
}