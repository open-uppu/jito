package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	jitocontext "github.com/uppu/jito/internal/context"
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

			// Load JITO.md context on startup so the TUI footer can
			// show the file count. Expose via env so the TUI model
			// (which is constructed inside tui.Run) can pick it up
			// without forcing a circular import on internal/cli.
			// Done BEFORE store.Open so a store-open failure does not
			// suppress the user-facing "context loaded" log line.
			cwd, _ := os.Getwd()
			if loader, lerr := jitocontext.NewLoader(cwd); lerr == nil {
				if _, loadErr := loader.Load(); loadErr == nil {
					fmt.Printf("[jito] context: %d files loaded\n", loader.Count())
					_ = os.Setenv("JITO_CONTEXT_FILES", fmt.Sprintf("%d", loader.Count()))
				}
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