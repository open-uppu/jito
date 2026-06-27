package cli

import (
	"github.com/spf13/cobra"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/store"
	"github.com/uppu/jito/internal/tui"
)

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Launch interactive TUI chat (Bubble Tea)",
		RunE: func(cmd *cobra.Command, args []string) error {
			modeName, _ := cmd.Flags().GetString("mode")
			modelOverride, _ := cmd.Flags().GetString("model")
			storePath, _ := cmd.Flags().GetString("store")

			m, err := mode.Get(modeName)
			if err != nil {
				return err
			}

			p, err := provider.NewFromConfig(modelOverride)
			if err != nil {
				return err
			}

			conv, err := store.Open(storePath)
			if err != nil {
				return err
			}
			defer conv.Close()

			return tui.Run(p, m, conv)
		},
	}
}