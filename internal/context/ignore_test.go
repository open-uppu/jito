package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIgnoreSet_Empty(t *testing.T) {
	s := NewIgnoreSet()
	assert.Equal(t, 0, s.Len())
	assert.False(t, s.Match("anything"))
}

func TestParsePattern_Variants(t *testing.T) {
	cases := []struct {
		in   string
		want IgnorePattern
	}{
		{"foo", IgnorePattern{Raw: "foo", original: "foo"}},
		{"/foo", IgnorePattern{Raw: "foo", Anchored: true, original: "/foo"}},
		{"!foo", IgnorePattern{Raw: "foo", Negate: true, original: "!foo"}},
		{"dir/", IgnorePattern{Raw: "dir", DirOnly: true, original: "dir/"}},
		{"a/b", IgnorePattern{Raw: "a/b", Anchored: true, original: "a/b"}},
		{"!/a", IgnorePattern{Raw: "a", Negate: true, Anchored: true, original: "!/a"}},
		{"a/", IgnorePattern{Raw: "a", DirOnly: true, original: "a/"}},
		{"   spaces   ", IgnorePattern{Raw: "spaces", original: "   spaces   "}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parsePattern(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseIgnoreLines_SkipsCommentsAndBlanks(t *testing.T) {
	s := ParseIgnoreLines([]string{
		"# comment",
		"",
		"   ",
		"node_modules/",
		"!node_modules/keep",
		"*.log",
	})
	assert.Equal(t, 3, s.Len())
	ps := s.Patterns()
	assert.Equal(t, "node_modules", ps[0].Raw)
	assert.True(t, ps[1].Negate)
	assert.Equal(t, "*.log", ps[2].Raw)
}

func TestParseIgnoreFile_MissingReturnsEmpty(t *testing.T) {
	s, err := ParseIgnoreFile(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Equal(t, 0, s.Len())
}

func TestParseIgnoreFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, IgnoreFileName)
	body := strings.Join([]string{
		"# jito ignore",
		"node_modules/",
		"*.log",
		"!keep.log",
		"build/",
		"/secret",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	s, err := ParseIgnoreFile(p)
	require.NoError(t, err)
	assert.Equal(t, 5, s.Len())
	// Create the dirs so DirOnly patterns can stat successfully.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	assert.True(t, s.Match("node_modules", dir))
	assert.True(t, s.Match("a/b/node_modules", dir))
	assert.True(t, s.Match("app.log", dir))
	assert.True(t, s.Match("a/app.log", dir))
	assert.False(t, s.Match("keep.log", dir))
	assert.True(t, s.Match("build", dir))
	assert.True(t, s.Match("secret", dir))
	assert.False(t, s.Match("public", dir))
}

func TestIgnoreSet_MatchBasenameUnanchored(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("README")
	assert.True(t, s.Match("README"))
	assert.True(t, s.Match("a/b/README"))
	assert.True(t, s.Match("deep/nested/path/README"))
}

func TestIgnoreSet_GlobStar(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("*.log")
	assert.True(t, s.Match("foo.log"))
	assert.False(t, s.Match("foo.txt"))
	assert.True(t, s.Match("a/foo.log"))
}

func TestIgnoreSet_GlobDoubleStar(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("**/*.test")
	assert.True(t, s.Match("a/b/c/d.test"))
	assert.True(t, s.Match("x.test"))
	assert.False(t, s.Match("a/b/c.txt"))
}

func TestIgnoreSet_DirOnlyRequiresDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))

	s := NewIgnoreSet()
	s.Append("build/")
	// Real directory at "build" is ignored.
	assert.True(t, s.Match("build", dir))
}

func TestIgnoreSet_DirOnlyRejectsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build"), []byte("not a dir"), 0o644))

	s := NewIgnoreSet()
	s.Append("build/")
	// File at "build" is NOT a directory, so DirOnly pattern does not match.
	assert.False(t, s.Match("build", dir))
}

func TestIgnoreSet_NegationOrder(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("*.log")
	s.Append("!important.log")
	assert.True(t, s.Match("foo.log"))
	assert.False(t, s.Match("important.log"))
}

func TestGlobMatch_NoSpecials(t *testing.T) {
	assert.True(t, globMatch("foo", "foo"))
	assert.False(t, globMatch("foo", "bar"))
}

func TestGlobMatch_StarAndQuestion(t *testing.T) {
	assert.True(t, globMatch("a?c", "abc"))
	assert.False(t, globMatch("a?c", "ac"))
	assert.True(t, globMatch("*.md", "foo.md"))
	assert.False(t, globMatch("*.md", "foo.txt"))
}

func TestGlobMatch_DoubleStarAlone(t *testing.T) {
	assert.True(t, globMatch("**", "anything/at/all"))
	assert.True(t, globMatch("**", "x"))
}

func TestGlobMatch_DoubleStarPrefix(t *testing.T) {
	assert.True(t, globMatch("**/test", "test"))
	assert.True(t, globMatch("**/test", "a/test"))
	assert.True(t, globMatch("**/test", "a/b/test"))
	assert.False(t, globMatch("**/test", "a/testb"))
}

func TestGlobMatch_DoubleStarSuffix(t *testing.T) {
	assert.True(t, globMatch("src/**", "src"))
	assert.True(t, globMatch("src/**", "src/a"))
	assert.True(t, globMatch("src/**", "src/a/b"))
	assert.False(t, globMatch("src/**", "srcx"))
}

func TestIgnoreSet_NilSafe(t *testing.T) {
	var s *IgnoreSet
	assert.False(t, s.Match("anything"))
}

func TestIgnoreSet_AppendRawRoundTrip(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("a")
	s.Append("b/c")
	s.Append("!d")
	ps := s.Patterns()
	require.Len(t, ps, 3)
	assert.Equal(t, "a", ps[0].Raw)
	assert.Equal(t, "b/c", ps[1].Raw)
	assert.True(t, ps[2].Negate)
}

func TestIgnorePattern_String(t *testing.T) {
	p := parsePattern("!foo/")
	assert.Equal(t, "!foo/", p.String())
	p2 := parsePattern("/bar")
	assert.Equal(t, "/bar", p2.String())
}

func TestGlobMatch_InvalidPattern(t *testing.T) {
	// '[' without ']' is an invalid pattern; filepath.Match returns error.
	assert.False(t, globMatch("[invalid", "foo"))
}

func TestIgnoreSet_AnchoredSlashPattern(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("/build")
	// Anchored: must match at root only.
	assert.True(t, s.Match("build"))
	assert.False(t, s.Match("a/build"))
	assert.False(t, s.Match("a/b/build"))
}

func TestIgnoreSet_MatchWithoutBase(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("*.log")
	assert.True(t, s.Match("foo.log"))
}

func TestIgnoreSet_EmptyRawNeverMatches(t *testing.T) {
	s := NewIgnoreSet()
	s.Append("")
	s.Append(" ")
	// Whitespace stripped to empty Raw.
	ps := s.Patterns()
	for _, p := range ps {
		assert.False(t, matchOne(p, "anything", ""))
	}
}

func TestIgnoreSet_DirOnlyAnchored(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "x", "build"), 0o755))
	s := NewIgnoreSet()
	s.Append("/build/")
	assert.True(t, s.Match("build", dir))
	assert.False(t, s.Match("x/build", dir))
}
