package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/uppu/jito/internal/agent"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree [create|list|clean] [args]",
		Short: "Manage git worktrees (isolated workspaces)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "create <branch>",
		Short: "Create a new worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			repoRoot, err := findRepoRoot(cwd)
			if err != nil {
				return err
			}
			branch := args[0]
			wt, err := agent.NewWorktree(repoRoot, branch)
			if err != nil {
				return err
			}
			fmt.Printf("✅ Created worktree: %s\n", wt)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			repoRoot, err := findRepoRoot(cwd)
			if err != nil {
				return err
			}
			names, err := agent.List(repoRoot)
			if err != nil {
				return err
			}
			for _, n := range names {
				fmt.Println(" -", n)
			}
			return nil
		},
	})
	return cmd
}

// findRepoRoot walks up looking for .git directory.
func findRepoRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a git repo: %s", start)
		}
		dir = parent
	}
}