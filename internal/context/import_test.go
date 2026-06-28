package context

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractImports(t *testing.T) {
	body := "hello\n@./foo.md\nworld @./bar.md more\n@../up.md\n@/abs.md\nbare @something.md here"
	got := ExtractImports(body)
	assert.Equal(t, []string{"./foo.md", "./bar.md", "../up.md", "/abs.md", "something.md"}, got)
}

func TestExtractImports_Empty(t *testing.T) {
	assert.Empty(t, ExtractImports(""))
	assert.Empty(t, ExtractImports("plain text without imports"))
	assert.Empty(t, ExtractImports("@invalid_no_md_extension"))
	assert.Empty(t, ExtractImports("email@user.com"))
	assert.Empty(t, ExtractImports("plain text @ without extension"))
}

func TestResolveImport_Relative(t *testing.T) {
	r := NewImportResolver()
	base := t.TempDir()
	got, err := r.ResolveImport("@./sub.md", base)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "sub.md"), got)
}

func TestResolveImport_Parent(t *testing.T) {
	r := NewImportResolver()
	base := filepath.Join(t.TempDir(), "a", "b")
	got, err := r.ResolveImport("@../sibling.md", base)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(got, filepath.Join("a", "sibling.md")))
}

func TestResolveImport_Absolute(t *testing.T) {
	r := NewImportResolver()
	got, err := r.ResolveImport("@/etc/notes.md", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "/etc/notes.md", got)
}

func TestResolveImport_RejectsNonMD(t *testing.T) {
	r := NewImportResolver()
	_, err := r.ResolveImport("@./foo.txt", t.TempDir())
	assert.Error(t, err)
}

func TestResolveImport_EmptyRef(t *testing.T) {
	r := NewImportResolver()
	_, err := r.ResolveImport("@", t.TempDir())
	assert.Error(t, err)
}

func TestResolveImport_NotImport(t *testing.T) {
	r := NewImportResolver()
	_, err := r.ResolveImport("./foo.md", t.TempDir())
	assert.Error(t, err)
}

func TestResolveImport_NilResolver(t *testing.T) {
	var r *ImportResolver
	_, err := r.ResolveImport("@./foo.md", t.TempDir())
	assert.Error(t, err)
}

func TestLoadImports_SimpleChain(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "JITO.md")
	sub := filepath.Join(dir, "sub.md")
	leaf := filepath.Join(dir, "leaf.md")

	require.NoError(t, os.WriteFile(leaf, []byte("leaf content"), 0o644))
	require.NoError(t, os.WriteFile(sub, []byte("sub content\n@./leaf.md\n"), 0o644))
	require.NoError(t, os.WriteFile(root, []byte("root content\n@./sub.md\n"), 0o644))

	r := NewImportResolver()
	res, err := r.LoadImports(root)
	require.NoError(t, err)
	assert.Contains(t, res.Body, "root content")
	assert.Contains(t, res.Body, "sub content")
	assert.Contains(t, res.Body, "leaf content")
	require.Len(t, res.Imports, 2)
	assert.Equal(t, 1, res.Imports[0].Depth)
	assert.Equal(t, 2, res.Imports[1].Depth)
	assert.Empty(t, res.Errors)
}

func TestLoadImports_CycleDetection(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	require.NoError(t, os.WriteFile(a, []byte("@./b.md"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("@./a.md"), 0o644))

	r := NewImportResolver()
	res, err := r.LoadImports(a)
	require.NoError(t, err)
	require.NotEmpty(t, res.Errors)
	hasCycle := false
	for _, e := range res.Errors {
		if errors.Is(e, ErrImportCycle) {
			hasCycle = true
		}
	}
	assert.True(t, hasCycle, "expected ErrImportCycle in errors: %v", res.Errors)
}

func TestLoadImports_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, MaxImportDepth+2)
	for i := range files {
		files[i] = filepath.Join(dir, "f"+string(rune('0'+i))+".md")
	}
	for i := 0; i < len(files)-1; i++ {
		next := "@./" + filepath.Base(files[i+1])
		require.NoError(t, os.WriteFile(files[i], []byte(next), 0o644))
	}
	require.NoError(t, os.WriteFile(files[len(files)-1], []byte("bottom"), 0o644))

	r := NewImportResolver()
	res, err := r.LoadImports(files[0])
	require.NoError(t, err)
	hasTooDeep := false
	for _, e := range res.Errors {
		if errors.Is(e, ErrImportTooDeep) {
			hasTooDeep = true
		}
	}
	assert.True(t, hasTooDeep, "expected ErrImportTooDeep in errors: %v", res.Errors)
}

func TestLoadImports_MissingFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "JITO.md")
	require.NoError(t, os.WriteFile(root, []byte("@./missing.md"), 0o644))

	r := NewImportResolver()
	res, err := r.LoadImports(root)
	require.NoError(t, err)
	require.NotEmpty(t, res.Errors)
	assert.Contains(t, res.Errors[0].Error(), "missing.md")
}

func TestLoadImports_NilResolver(t *testing.T) {
	var r *ImportResolver
	_, err := r.LoadImports("/tmp/nonexistent.md")
	assert.Error(t, err)
}

func TestLoadImports_CachesReads(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "JITO.md")
	sub := filepath.Join(dir, "sub.md")
	require.NoError(t, os.WriteFile(sub, []byte("sub"), 0o644))
	require.NoError(t, os.WriteFile(root, []byte("@./sub.md\n@./sub.md\n"), 0o644))

	r := NewImportResolver()
	res, err := r.LoadImports(root)
	require.NoError(t, err)
	// Same file imported twice via @-ref dedup (Visited blocks cycle).
	// The first occurrence loads, the second is flagged ErrImportCycle.
	assert.NotEmpty(t, res.Errors)
}

func TestLoadImports_CustomMaxDepth(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "JITO.md")
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	require.NoError(t, os.WriteFile(b, []byte("leaf"), 0o644))
	require.NoError(t, os.WriteFile(a, []byte("@./b.md"), 0o644))
	require.NoError(t, os.WriteFile(root, []byte("@./a.md"), 0o644))

	r := NewImportResolver()
	r.MaxDepth = 1
	res, err := r.LoadImports(root)
	require.NoError(t, err)
	hasTooDeep := false
	for _, e := range res.Errors {
		if errors.Is(e, ErrImportTooDeep) {
			hasTooDeep = true
		}
	}
	assert.True(t, hasTooDeep)
}

func TestLoadImports_RootMissing(t *testing.T) {
	r := NewImportResolver()
	_, err := r.LoadImports("/tmp/definitely-does-not-exist.md")
	assert.Error(t, err)
}

func TestLoadImports_BarePath(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "JITO.md")
	sub := filepath.Join(dir, "sub.md")
	require.NoError(t, os.WriteFile(sub, []byte("bare path body"), 0o644))
	require.NoError(t, os.WriteFile(root, []byte("@sub.md\n"), 0o644))
	r := NewImportResolver()
	res, err := r.LoadImports(root)
	require.NoError(t, err)
	assert.Contains(t, res.Body, "bare path body")
}

func TestExtractImports_LineStart(t *testing.T) {
	body := "@./a.md\n@./b.md"
	got := ExtractImports(body)
	assert.Equal(t, []string{"./a.md", "./b.md"}, got)
}
