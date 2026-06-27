// Package agent provides worktree management and sub-agent spawning.
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree manages git worktrees for isolated agent workspaces.
type Worktree struct {
	RepoPath   string
	WorktreeDir string
	Branch     string
}

// NewWorktree creates a new worktree.
// repoPath: path to existing git repo
// branch: name of branch to create (e.g. "jito-sprint-b-fe")
func NewWorktree(repoPath, branch string) (*Worktree, error) {
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return nil, fmt.Errorf("not a git repo: %s", repoPath)
	}

	wtDir := filepath.Join(repoPath, "..", filepath.Base(repoPath)+"."+branch)
	wtDir, _ = filepath.Abs(wtDir)

	wt := &Worktree{
		RepoPath:    repoPath,
		WorktreeDir: wtDir,
		Branch:      branch,
	}

	if err := wt.create(); err != nil {
		return nil, err
	}
	return wt, nil
}

func (w *Worktree) create() error {
	// Check if worktree already exists
	if _, err := os.Stat(w.WorktreeDir); err == nil {
		return nil // already exists
	}

	// Use git CLI (more reliable than go-git for worktree ops)
	cmd := exec.Command("git", "worktree", "add", "-b", w.Branch, w.WorktreeDir)
	cmd.Dir = w.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %w (%s)", err, string(out))
	}
	return nil
}

// List returns all worktrees in a repo.
func List(repoPath string) ([]string, error) {
	// Use git CLI (more reliable than go-git for worktree ops)
	cmd := exec.Command("git", "worktree", "list")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, filepath.Base(parts[0]))
		}
	}
	return names, nil
}

// Clean removes a worktree (calls `git worktree remove`).
func (w *Worktree) Clean() error {
	cmd := exec.Command("git", "worktree", "remove", w.WorktreeDir, "--force")
	cmd.Dir = w.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		// try harder: rm -rf directory + prune
		os.RemoveAll(w.WorktreeDir)
		prune := exec.Command("git", "worktree", "prune")
		prune.Dir = w.RepoPath
		_ = prune.Run()
		return fmt.Errorf("git worktree remove (pruned): %w (%s)", err, string(out))
	}
	return nil
}

// Path returns the worktree directory.
func (w *Worktree) Path() string {
	return w.WorktreeDir
}

// String returns a human-readable description.
func (w *Worktree) String() string {
	return fmt.Sprintf("worktree(branch=%s, path=%s)", w.Branch, w.WorktreeDir)
}

// EnsureRepo initializes a git repo at the given path if not already one.
func EnsureRepo(path string) error {
	gitDir := filepath.Join(path, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil // already a repo
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w (%s)", err, string(out))
	}
	return nil
}

// BranchFromPath makes a safe branch name from a path.
func BranchFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, "/", "-")
	path = strings.ReplaceAll(path, ".", "-")
	return strings.ToLower(path)
}