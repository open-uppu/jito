package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/uppu/jito/internal/commands"
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

			cwd, _ := os.Getwd()

			// Load JITO.md context on startup so the TUI footer can
			// show the file count. Expose via env so the TUI model
			// (which is constructed inside tui.Run) can pick it up
			// without forcing a circular import on internal/cli.
			// Done BEFORE store.Open so a store-open failure does not
			// suppress the user-facing "context loaded" log line.
			if loader, lerr := jitocontext.NewLoader(cwd); lerr == nil {
				if _, loadErr := loader.Load(); loadErr == nil {
					fmt.Printf("[jito] context: %d files loaded\n", loader.Count())
					_ = os.Setenv("JITO_CONTEXT_FILES", fmt.Sprintf("%d", loader.Count()))
				}
			}

			// Load custom slash commands (LOOP #2).  Errors are
			// non-fatal: the chat must still launch even if the user's
			// TOML has a typo.
			reg := commands.NewRegistry()
			if errs := reg.LoadFromDirs(
				commands.DefaultGlobalDir(),
				commands.DefaultProjectDir(cwd),
			); len(errs) > 0 {
				fmt.Fprintf(os.Stderr, "[jito] commands: %d parse error(s)\n", len(errs))
				for _, e := range errs {
					fmt.Fprintf(os.Stderr, "  - %v\n", e)
				}
			}
			if reg.Count() > 0 {
				fmt.Printf("[jito] commands: %d loaded\n", reg.Count())
			}

			conv, err := store.Open(storePath)
			if err != nil {
				return err
			}
			defer conv.Close()

			return tui.RunWith(p, m, conv, reg)
		},
	}
}