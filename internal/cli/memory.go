package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/uppu/jito/internal/context"
)

// newMemoryCmd returns the `jito memory` command (gemini-cli analog).
// Subcommands:
//
//	show    — print all loaded JITO.md files + summary
//	reload  — force a fresh load from disk
func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and reload JITO.md context files (analog of /memory show|reload)",
		Long: `memory inspects the JITO.md context files loaded by jito.

Subcommands:
  show    list every loaded JITO.md path and the merged body
  reload  force a fresh reload from disk`,
	}

	cmd.AddCommand(newMemoryShowCmd())
	cmd.AddCommand(newMemoryReloadCmd())
	return cmd
}

// newMemoryShowCmd implements `jito memory show`.
func newMemoryShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show all loaded JITO.md context files",
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := buildContextLoader(cmd)
			if err != nil {
				return err
			}
			res, err := l.Load()
			if err != nil {
				return err
			}
			files := l.LoadedFiles()
			fmt.Printf("📚 %s\n", context.FormatSummary(res, len(files)))
			for i, p := range files {
				fmt.Printf("  %d. %s\n", i+1, p)
			}
			if len(res.Imports) > 0 {
				fmt.Println()
				fmt.Printf("  %d @import(s) resolved:\n", len(res.Imports))
				for _, im := range res.Imports {
					fmt.Printf("    @%s (depth %d) → %s\n", im.Ref, im.Depth, im.AbsPath)
				}
			}
			if len(res.Errors) > 0 {
				fmt.Println()
				fmt.Printf("  %d warning(s):\n", len(res.Errors))
				for _, e := range res.Errors {
					fmt.Printf("    ⚠ %v\n", e)
				}
			}
			return nil
		},
	}
}

// newMemoryReloadCmd implements `jito memory reload`.
func newMemoryReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Reload JITO.md files from disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := buildContextLoader(cmd)
			if err != nil {
				return err
			}
			res, err := l.Reload()
			if err != nil {
				return err
			}
			fmt.Printf("🔄 reloaded %d context file(s)\n", len(l.LoadedFiles()))
			if len(res.Errors) > 0 {
				for _, e := range res.Errors {
					fmt.Printf("  ⚠ %v\n", e)
				}
			}
			return nil
		},
	}
}

// buildContextLoader constructs a context.Loader from the current
// process's cwd + $HOME. The returned loader has NOT yet performed I/O.
func buildContextLoader(_ *cobra.Command) (*context.Loader, error) {
	return context.NewLoader("")
}
