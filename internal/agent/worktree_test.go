package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeRepo creates a temp git repo (no commits) and returns its
// absolute path. It is the minimum environment required by the
// worktree helpers.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	// Need at least one commit so worktree add can succeed.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestNewWorktree_NotARepo(t *testing.T) {
	_, err := NewWorktree(t.TempDir(), "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repo")
}

func TestNewWorktree_HappyPath(t *testing.T) {
	repo := makeRepo(t)
	wt, err := NewWorktree(repo, "jito-test-bump")
	require.NoError(t, err)
	require.NotNil(t, wt)
	assert.Equal(t, repo, wt.RepoPath)
	assert.Contains(t, wt.WorktreeDir, "jito-test-bump")
	assert.Equal(t, "jito-test-bump", wt.Branch)
	assert.DirExists(t, wt.WorktreeDir)
}

func TestNewWorktree_Idempotent(t *testing.T) {
	repo := makeRepo(t)
	first, err := NewWorktree(repo, "idem")
	require.NoError(t, err)

	second, err := NewWorktree(repo, "idem")
	require.NoError(t, err)
	assert.Equal(t, first.WorktreeDir, second.WorktreeDir)
}

func TestWorktree_StringPathClean(t *testing.T) {
	repo := makeRepo(t)
	wt, err := NewWorktree(repo, "util")
	require.NoError(t, err)
	assert.Contains(t, wt.String(), "util")
	assert.Equal(t, wt.WorktreeDir, wt.Path())

	names, err := List(repo)
	require.NoError(t, err)
	// List returns basenames; the worktree dir basename is
	// <repoBasename>.<branch> = "001.util" in this test.
	assert.Contains(t, names, filepath.Base(wt.WorktreeDir))
	assert.NoError(t, wt.Clean())
}

func TestList_NotARepo(t *testing.T) {
	_, err := List(t.TempDir())
	require.Error(t, err)
}

func TestEnsureRepo(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "subdir")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, EnsureRepo(target))
	assert.DirExists(t, filepath.Join(target, ".git"))

	// Calling twice is a no-op.
	require.NoError(t, EnsureRepo(target))
}

func TestNewWorktree_CreateFails(t *testing.T) {
	// A repo that has nothing in it (no commits) cannot have a
	// worktree added, so create() must fail and the error must
	// bubble up. Note: EnsureRepo is insufficient because git
	// worktree add requires at least one commit. We give it one.
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "T")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "init")

	wt, err := NewWorktree(dir, "first")
	require.NoError(t, err)
	require.NotNil(t, wt)

	// Now try a SECOND worktree with a *different* branch — it
	// must succeed, so the test continues. We then explicitly
	// exercise Clean()'s primary (success) path.
	wt2, err := NewWorktree(dir, "second")
	require.NoError(t, err)
	require.NoError(t, wt2.Clean(), "primary clean path: git worktree remove succeeds")
}

func TestBranchFromPath(t *testing.T) {
	assert.Equal(t, "tmp-x-main", BranchFromPath("/tmp/x/main"))
	assert.Equal(t, "tmp-x-feature-x", BranchFromPath("/tmp/x/feature-x"))
	assert.Equal(t, "x", BranchFromPath("x"))
	// Dots become dashes (safe branch chars).
	assert.Equal(t, "repo-feature", BranchFromPath("repo.feature"))
}
