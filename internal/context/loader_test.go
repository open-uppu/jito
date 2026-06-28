package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	pgrapid "pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers ---

// makeProject creates a temp project with N levels of nested dirs and a
// JITO.md at every level. It returns the deepest dir and the project
// root (the level with a .git marker).
func makeProject(t *testing.T, depth int) (root, deepest string) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	cur := root
	for i := 0; i < depth; i++ {
		cur = filepath.Join(cur, fmt.Sprintf("d%d", i))
		require.NoError(t, os.MkdirAll(cur, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cur, JITOFileName),
			[]byte(fmt.Sprintf("# level %d", i)), 0o644))
	}
	return root, cur
}

// --- Basic construction tests ---

func TestNewLoader_Defaults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, JITOFileName), []byte("x"), 0o644))
	l, err := NewLoader(dir, WithHome(t.TempDir()))
	require.NoError(t, err)
	assert.NotEmpty(t, l.Root())
	assert.Equal(t, dir, l.CWD())
}

func TestLoader_Load_Hierarchical(t *testing.T) {
	_, deepest := makeProject(t, 3)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)

	res, err := l.Load()
	require.NoError(t, err)
	files := l.LoadedFiles()
	// Should find all 3 nested JITO.md files (root has none).
	assert.Len(t, files, 3)
	assert.NotNil(t, res)
	assert.Contains(t, res.Body, "level 0")
	assert.Contains(t, res.Body, "level 2")
}

func TestLoader_Load_JITAddsNewFile(t *testing.T) {
	root, deepest := makeProject(t, 2)
	// Create an extra JITO.md in a sibling dir not on the cwd walk-up.
	sibling := filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, JITOFileName),
		[]byte("# sibling"), 0o644))

	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)

	// First load: only sees the 2 ancestor files.
	_, err = l.Load()
	require.NoError(t, err)
	assert.Len(t, l.LoadedFiles(), 2)

	// JIT load for a file in sibling/ picks up sibling's JITO.md too.
	res, err := l.LoadWithJIT(sibling)
	require.NoError(t, err)
	files := l.LoadedFiles()
	assert.Len(t, files, 3)
	assert.Contains(t, res.Body, "sibling")
}

func TestLoader_LoadForFile(t *testing.T) {
	root, deepest := makeProject(t, 2)
	sibling := filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, JITOFileName), []byte("# jit"), 0o644))

	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)

	target := filepath.Join(sibling, "main.go")
	res, err := l.LoadForFile(target)
	require.NoError(t, err)
	assert.Contains(t, res.Body, "jit")
}

func TestLoader_LoadForFile_Empty(t *testing.T) {
	_, deepest := makeProject(t, 1)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.LoadForFile("")
	require.NoError(t, err)
	assert.NotEmpty(t, l.LoadedFiles())
}

func TestLoader_Reload(t *testing.T) {
	_, deepest := makeProject(t, 1)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	first := l.Count()
	_, err = l.Reload()
	require.NoError(t, err)
	assert.Equal(t, first, l.Count())
}

func TestLoader_Count(t *testing.T) {
	_, deepest := makeProject(t, 2)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	assert.Equal(t, 0, l.Count())
	_, err = l.Load()
	require.NoError(t, err)
	assert.Greater(t, l.Count(), 0)
}

func TestLoader_BodiesSnapshot(t *testing.T) {
	_, deepest := makeProject(t, 2)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	b := l.Bodies()
	assert.NotEmpty(t, b)
	for _, body := range b {
		assert.NotEmpty(t, body)
	}
}

func TestLoader_SystemPromptSection(t *testing.T) {
	_, deepest := makeProject(t, 1)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	s := l.SystemPromptSection()
	assert.Contains(t, s, "## JITO.md context")
	assert.Contains(t, s, JITOFileName)
}

func TestLoader_SystemPromptSection_Empty(t *testing.T) {
	l, err := NewLoader(t.TempDir(), WithHome(t.TempDir()))
	require.NoError(t, err)
	assert.Equal(t, "", l.SystemPromptSection())
}

func TestLoader_IgnoresExcludeFile(t *testing.T) {
	root, deepest := makeProject(t, 2)
	// Write .jitoignore at root that excludes d0 (unanchored basename match).
	require.NoError(t, os.WriteFile(filepath.Join(root, IgnoreFileName),
		[]byte("d0\n"), 0o644))

	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	files := l.LoadedFiles()
	for _, f := range files {
		assert.NotContains(t, f, "/d0/", "should not include excluded d0")
	}
	assert.Empty(t, files)
}

func TestLoader_HomeJITO(t *testing.T) {
	_, deepest := makeProject(t, 0)
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".jito"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".jito", JITOFileName),
		[]byte("# home ctx"), 0o644))

	l, err := NewLoader(deepest, WithHome(home))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	files := l.LoadedFiles()
	found := false
	for _, f := range files {
		if strings.HasSuffix(f, filepath.Join(".jito", JITOFileName)) {
			found = true
		}
	}
	assert.True(t, found, "expected ~/.jito/JITO.md in loaded files")
}

func TestLoader_StopAtFirstJITO(t *testing.T) {
	_, deepest := makeProject(t, 3)
	l, err := NewLoader(deepest,
		WithHome(t.TempDir()),
		WithHierarchy(HierarchyConfig{StopAtFirstJITO: true}),
	)
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	// StopAtFirstJITO stops at the first (closest) JITO.md, so only 1 file.
	files := l.LoadedFiles()
	assert.Len(t, files, 1)
	assert.Contains(t, files[0], "d2")
}

func TestFormatSummary(t *testing.T) {
	assert.Equal(t, "3 context files loaded", FormatSummary(nil, 3))
	res := &LoadResult{}
	assert.Equal(t, "2 context files loaded", FormatSummary(res, 2))
	res.Imports = []ResolvedImport{{Ref: "@./x.md"}, {Ref: "@./y.md"}}
	assert.Equal(t, "2 context files loaded (2 imports)", FormatSummary(res, 2))
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, fileExists(dir))
	assert.False(t, fileExists(filepath.Join(dir, "nope")))
	p := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(p, nil, 0o644))
	assert.True(t, fileExists(p))
}

func TestLoader_Getters(t *testing.T) {
	home := t.TempDir()
	l, err := NewLoader(t.TempDir(), WithHome(home))
	require.NoError(t, err)
	assert.Equal(t, home, l.Home())
	assert.NotEmpty(t, l.CWD())
	assert.NotEmpty(t, l.Root())
}

func TestLoader_WithCWD(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLoader("", WithCWD(dir))
	require.NoError(t, err)
	assert.Equal(t, dir, l.CWD())
}

func TestNewLoader_NoCWD(t *testing.T) {
	// Calling NewLoader("") with no CWD option uses os.Getwd(). Must not fail.
	l, err := NewLoader("")
	require.NoError(t, err)
	assert.NotEmpty(t, l.CWD())
}

func TestLoader_MultiLevelIgnores(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	mid := filepath.Join(root, "pkg")
	deep := filepath.Join(mid, "sub")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mid, JITOFileName), []byte("mid"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(deep, JITOFileName), []byte("deep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(mid, IgnoreFileName), []byte("sub\n"), 0o644))

	l, err := NewLoader(deep, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
	files := l.LoadedFiles()
	// "sub" should be excluded by pkg/.jitoignore.
	for _, f := range files {
		assert.NotContains(t, f, "/pkg/sub/")
	}
	// But pkg/JITO.md is still present.
	found := false
	for _, f := range files {
		if strings.HasSuffix(f, "/pkg/JITO.md") {
			found = true
		}
	}
	assert.True(t, found)
}

func TestContainsPath(t *testing.T) {
	assert.True(t, containsPath([]string{"/a", "/b"}, "/a"))
	assert.False(t, containsPath([]string{"/a", "/b"}, "/c"))
	assert.False(t, containsPath(nil, "/x"))
}

func TestFindInList(t *testing.T) {
	assert.True(t, findInList([]string{"/a/JITO.md"}, "/a"))
	assert.False(t, findInList([]string{"/a/JITO.md"}, "/b"))
	assert.False(t, findInList(nil, "/a"))
}

// --- Race-safety ---

func TestLoader_RaceSafe(t *testing.T) {
	_, deepest := makeProject(t, 2)
	l, err := NewLoader(deepest, WithHome(t.TempDir()))
	require.NoError(t, err)
	_, _ = l.Load()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.LoadedFiles()
			_ = l.Count()
			_ = l.Bodies()
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = l.Reload()
		}()
	}
	wg.Wait()
}

// --- Property-based (rapid) ---

func TestProperty_LoadNeverPanics(t *testing.T) {
	pgrapid.Check(t, func(rt *pgrapid.T) {
		depth := pgrapid.IntRange(0, 5).Draw(rt, "depth")
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
		cur := dir
		for i := 0; i < depth; i++ {
			cur = filepath.Join(cur, fmt.Sprintf("d%d", i))
			require.NoError(t, os.MkdirAll(cur, 0o755))
			body := pgrapid.String().Draw(rt, fmt.Sprintf("body%d", i))
			require.NoError(t, os.WriteFile(filepath.Join(cur, JITOFileName),
				[]byte(body), 0o644))
		}
		l, err := NewLoader(cur, WithHome(t.TempDir()))
		if err != nil {
			return // skip; cannot construct
		}
		_, _ = l.Load()
		_, _ = l.LoadWithJIT(cur)
		_, _ = l.LoadForFile(filepath.Join(cur, "x.go"))
		// Properties:
		assert.LessOrEqual(t, l.Count(), depth+1)
	})
}

func TestProperty_CountMatchesLoadedFiles(t *testing.T) {
	pgrapid.Check(t, func(rt *pgrapid.T) {
		depth := pgrapid.IntRange(1, 4).Draw(rt, "depth")
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
		cur := dir
		for i := 0; i < depth; i++ {
			cur = filepath.Join(cur, fmt.Sprintf("d%d", i))
			require.NoError(t, os.MkdirAll(cur, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(cur, JITOFileName),
				[]byte("# x"), 0o644))
		}
		l, err := NewLoader(cur, WithHome(t.TempDir()))
		require.NoError(t, err)
		_, _ = l.Load()
		assert.Equal(t, l.Count(), len(l.LoadedFiles()))
	})
}

// --- Fuzz harness (table-driven; testing.F.Add path) ---

func TestFuzzLoad(t *testing.T) {
	t.Parallel()
	cases := []string{
		"plain text",
		"@./missing.md",
		"@./a.md\n@./a.md\n@./a.md",
		"# heading only\n\n- bullet\n",
		"\x00\x01\x02 binary garbage",
		strings.Repeat("@./a.md\n", 100),
		"@./a.md\n@./b.md\n@./c.md\n",
		"@/abs/path.md\n",
		"@./\x00invalid.md\n",
		"@./../../../etc/passwd.md\n",
		"<!@#>$%^&*()\n",
	}
	for _, body := range cases {
		body := body
		t.Run("", func(t *testing.T) {
			t.Parallel()
			fuzzDir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(fuzzDir, ".git"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(fuzzDir, JITOFileName),
				[]byte(body), 0o644))
			l, err := NewLoader(fuzzDir, WithHome(t.TempDir()))
			if err != nil {
				t.Skip("loader construction failed:", err)
			}
			res, err := l.Load()
			assert.NoError(t, err)
			assert.NotNil(t, res)
		})
	}
}
