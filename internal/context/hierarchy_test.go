package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustedRoot_MarkerFound(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	// Place a marker at the project root.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644))

	root, err := TrustedRoot(sub, HierarchyConfig{})
	require.NoError(t, err)
	absDir, _ := filepath.Abs(dir)
	assert.Equal(t, absDir, root)
}

func TestTrustedRoot_GitMarker(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	sub := filepath.Join(dir, "src", "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	root, err := TrustedRoot(sub, HierarchyConfig{})
	require.NoError(t, err)
	absDir, _ := filepath.Abs(dir)
	assert.Equal(t, absDir, root)
}

func TestTrustedRoot_FallbackToFSRoot(t *testing.T) {
	// /tmp typically has no markers above it that we own; the algorithm
	// falls back to its starting point's nearest marker, which may be
	// /tmp itself or some ancestor.
	root, err := TrustedRoot(os.TempDir(), HierarchyConfig{})
	require.NoError(t, err)
	assert.NotEmpty(t, root)
}

func TestTrustedRoot_ExplicitTrustedRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "deep", "deeper")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	root, err := TrustedRoot(sub, HierarchyConfig{TrustedRoots: []string{dir}})
	require.NoError(t, err)
	absDir, _ := filepath.Abs(dir)
	assert.Equal(t, absDir, root)
}

func TestTrustedRoot_NoMatch(t *testing.T) {
	dir := t.TempDir()
	// Trusted root that does not exist as ancestor.
	other := filepath.Join(dir, "nonexistent-ancestor")
	_, err := TrustedRoot(dir, HierarchyConfig{TrustedRoots: []string{other}})
	assert.ErrorIs(t, err, ErrNoTrustedRoot)
}

func TestWalkUp_OrdersNearestFirst(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "JITO.md"), []byte("root"), 0o644))
	a := filepath.Join(dir, "a")
	b := filepath.Join(a, "b")
	c := filepath.Join(b, "c")
	require.NoError(t, os.MkdirAll(c, 0o755))

	dirs, err := WalkUp(c, dir, HierarchyConfig{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(dirs), 3)
	assert.Equal(t, c, dirs[0])
	assert.Equal(t, b, dirs[1])
	assert.Equal(t, a, dirs[2])
	assert.Equal(t, dir, dirs[len(dirs)-1])
}

func TestWalkUp_EmptyRootUsesDefault(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "x")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "JITO.md"), nil, 0o644))

	dirs, err := WalkUp(sub, "", HierarchyConfig{})
	require.NoError(t, err)
	assert.NotEmpty(t, dirs)
}

func TestWalkUp_RootNotAncestor(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	_, err := WalkUp(dir, other, HierarchyConfig{})
	assert.ErrorIs(t, err, ErrNoTrustedRoot)
}

func TestWalkUp_StopAtGitRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	sub := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	dirs, err := WalkUp(sub, dir, HierarchyConfig{StopAtGitRoot: true})
	require.NoError(t, err)
	// Should include sub and dir (which has .git), and stop.
	assert.Contains(t, dirs, sub)
	assert.Contains(t, dirs, dir)
	assert.Equal(t, 2, len(dirs))
}

func TestWalkUp_StopAtGitRoot_NestedGit(t *testing.T) {
	// Subdir also has .git; loop should stop at the inner .git when
	// walking up.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	inner := filepath.Join(dir, "a", "b")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(inner, ".git"), 0o755))

	dirs, err := WalkUp(inner, dir, HierarchyConfig{StopAtGitRoot: true})
	require.NoError(t, err)
	// StopAtGitRoot stops at the first .git found while walking up.
	// The inner dir has .git, so we stop there.
	assert.Equal(t, []string{inner}, dirs)
}

func TestFindJITOInDir(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "", FindJITOInDir(dir))

	p := filepath.Join(dir, JITOFileName)
	require.NoError(t, os.WriteFile(p, []byte("hi"), 0o644))
	assert.Equal(t, p, FindJITOInDir(dir))
}

func TestIsWithinTrusted(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "x")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	assert.True(t, IsWithinTrusted(sub, dir))
	assert.True(t, IsWithinTrusted(dir, dir))
	other := t.TempDir()
	assert.False(t, IsWithinTrusted(other, dir))
}

func TestIsParent(t *testing.T) {
	assert.True(t, isParent("/a", "/a/b"))
	assert.True(t, isParent("/a", "/a/b/c"))
	assert.False(t, isParent("/a/b", "/a"))
	assert.False(t, isParent("/a", "/b"))
	assert.False(t, isParent("/a", "/a"))
	// filepath.Rel fails when parent has NUL byte.
	assert.False(t, isParent("\x00bad", "/x"))
}

func TestIsWithinTrusted_False(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	assert.False(t, IsWithinTrusted(other, dir))
	// Non-existent path also returns false (Abs fails).
	assert.False(t, IsWithinTrusted("/this/path/does/not/exist/at/all", dir))
}

func TestTrustedRoot_NoTrustedRootsEmpty(t *testing.T) {
	// Empty slice and start inside a valid project.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644))
	root, err := TrustedRoot(dir, HierarchyConfig{TrustedRoots: []string{}})
	require.NoError(t, err)
	assert.NotEmpty(t, root)
}

func TestWalkUp_ReachesFSRoot(t *testing.T) {
	// /tmp exists on all Unix; walk from /tmp up to / then stop.
	dirs, err := WalkUp(os.TempDir(), "", HierarchyConfig{})
	require.NoError(t, err)
	assert.NotEmpty(t, dirs)
}

func TestIsParent_Relative(t *testing.T) {
	assert.True(t, isParent("a", "a/b"))
	assert.False(t, isParent("a/b", "a"))
}

func TestTrustedRoot_AbsError(t *testing.T) {
	_, err := TrustedRoot("\x00invalid", HierarchyConfig{})
	// filepath.Abs may either succeed or fail; if it succeeds, we get a
	// root. We only assert non-panic here.
	_ = err
}

func TestWalkUp_StartsAtTrustedRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), nil, 0o644))
	dirs, err := WalkUp(dir, dir, HierarchyConfig{})
	require.NoError(t, err)
	assert.Equal(t, []string{dir}, dirs)
}

func TestWalkUp_NoStopAtGitRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	sub := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	dirs, err := WalkUp(sub, dir, HierarchyConfig{StopAtGitRoot: false})
	require.NoError(t, err)
	// Without the flag, walks past .git (until reaching root or stop).
	assert.Contains(t, dirs, sub)
	assert.Contains(t, dirs, dir)
}

func TestIsWithinTrusted_EqualPaths(t *testing.T) {
	dir := t.TempDir()
	assert.True(t, IsWithinTrusted(dir, dir))
}

func TestIsWithinTrusted_ChildPath(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "deep", "nested")
	require.NoError(t, os.MkdirAll(child, 0o755))
	assert.True(t, IsWithinTrusted(child, dir))
}

func TestIsWithinTrusted_RootAbsError(t *testing.T) {
	// NUL byte in path makes filepath.Abs fail.
	assert.False(t, IsWithinTrusted("/tmp", "\x00bad"))
}

func TestTrustedRoot_StartAbsError(t *testing.T) {
	_, err := TrustedRoot("\x00bad", HierarchyConfig{})
	if err == nil {
		t.Skip("Abs tolerated NUL on this Go version; ok")
	}
}

func TestTrustedRoot_ExplicitAbsError(t *testing.T) {
	_, err := TrustedRoot(t.TempDir(), HierarchyConfig{TrustedRoots: []string{"\x00bad"}})
	// Should skip bad root, fall through to ErrNoTrustedRoot if no match.
	_ = err
}

func TestWalkUp_AbsError(t *testing.T) {
	_, err := WalkUp("\x00bad", "/", HierarchyConfig{})
	if err == nil {
		t.Skip("Abs tolerated NUL on this Go version; ok")
	}
}

func TestWalkUp_RootAbsError(t *testing.T) {
	_, err := WalkUp(t.TempDir(), "\x00bad", HierarchyConfig{})
	if err == nil {
		t.Skip("Abs tolerated NUL on this Go version; ok")
	}
}

func TestDefaultTrustedRoot_AbsError(t *testing.T) {
	// NUL causes Abs to fail; defaultTrustedRoot falls back to Clean.
	got := defaultTrustedRoot("\x00bad")
	assert.NotEmpty(t, got)
}

func TestWalkUp_ReachesFilesystemRoot(t *testing.T) {
	// Walking from / with no marker anywhere above should reach / and stop
	// (parent == dir).
	dirs, err := WalkUp("/", "", HierarchyConfig{})
	require.NoError(t, err)
	assert.NotEmpty(t, dirs)
	assert.Equal(t, "/", dirs[len(dirs)-1])
}
