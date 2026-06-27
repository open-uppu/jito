package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command for jito
func NewRootCmd(version, commit, date string) *cobra.Command {
	root := &cobra.Command{
		Use:   "jito",
		Short: "jito - multi-mode AI agent CLI (Minimax-powered)",
		Long: `jito ⚡ — Multi-mode AI agent CLI for open-uppu Enterprise IT Master Blueprint.

Modes:
  dev       Coding, refactoring, debugging
  reason    Planning, analysis, reasoning
  create    Creative, marketing copy
  audit     Security, compliance, review
  universal Catch-all (default)

Examples:
  jito --mode=dev "refactor this function"
  jito --mode=audit --task=review-pr
  jito chat                          # launch TUI
  jito --version`,
		Version: fmt.Sprintf("%s (commit %s, %s)", version, commit, date),
	}

	root.PersistentFlags().StringP("mode", "m", "universal", "agent mode: dev|reason|create|audit|universal")
	root.PersistentFlags().StringP("model", "M", "", "override model (default from config)")
	root.PersistentFlags().StringP("task", "t", "", "named task to execute")
	root.PersistentFlags().Bool("no-tui", false, "disable TUI (plain output)")
	root.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	root.PersistentFlags().Bool("heartbeat", false, "enable 2-minute heartbeat log")
	root.PersistentFlags().String("config", "", "config file path (default ~/.jito/config.yaml)")

	root.AddCommand(newChatCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newVersionCmd(version, commit, date))
	root.AddCommand(newInitCmd())
	root.AddCommand(newHeartbeatCmd())

	return root
}