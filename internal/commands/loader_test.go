package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTOML(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

func TestLoadFile_Basic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.toml")
	require.NoError(t, os.WriteFile(p, []byte(`description = "say hi"`+"\nprompt = \"echo {{args}} hi\"\n"), 0o644))

	c, err := LoadFile(p, dir, false)
	require.NoError(t, err)
	assert.Equal(t, "/hello", c.Slash)
	assert.Equal(t, "hello", c.Name)
	assert.Equal(t, "say hi", c.Description)
	assert.Equal(t, "echo {{args}} hi", c.Prompt)
	assert.Equal(t, SourceGlobal, c.Source)
	assert.Equal(t, p, c.Path)
}

func TestLoadFile_NestedPath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "git")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	p := filepath.Join(sub, "commit.toml")
	require.NoError(t, os.WriteFile(p, []byte("prompt = \"git commit -m '{{args}}'\"\n"), 0o644))

	c, err := LoadFile(p, dir, false)
	require.NoError(t, err)
	assert.Equal(t, "/git:commit", c.Slash, "nested file → colon-separated slash")
}

func TestLoadFile_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.toml")
	require.NoError(t, os.WriteFile(p, []byte(`description = "no prompt"`+"\n"), 0o644))

	_, err := LoadFile(p, dir, false)
	assert.ErrorIs(t, err, ErrNoPrompt)
}

func TestLoadFile_BadTOML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage.toml")
	require.NoError(t, os.WriteFile(p, []byte("this = is not valid toml ==="), 0o644))

	_, err := LoadFile(p, dir, false)
	require.Error(t, err)
}

func TestLoadDir_Recursive(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, dir, "a.toml", `description = "a"`+"\nprompt = \"a {{args}}\"\n")
	writeTOML(t, dir, "sub/b.toml", `description = "b"`+"\nprompt = \"b {{args}}\"\n")
	// Non-toml file should be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644))

	cmds, errs := LoadDir(dir, false)
	assert.Empty(t, errs)
	require.Len(t, cmds, 2)
	assert.Equal(t, "/a", cmds[0].Slash)
	assert.Equal(t, "/sub:b", cmds[1].Slash)
}

func TestLoadDir_MissingDir(t *testing.T) {
	cmds, errs := LoadDir("/nonexistent/path/please", false)
	assert.Empty(t, cmds)
	assert.Empty(t, errs)
}

func TestLoadDir_SkipsBadFilesButKeepsGood(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, dir, "good.toml", `description = "ok"`+"\nprompt = \"ok\"\n")
	writeTOML(t, dir, "broken.toml", `description = "x"`+"\n") // missing prompt
	writeTOML(t, dir, "junk.toml", "==not toml==")

	cmds, errs := LoadDir(dir, false)
	require.Len(t, cmds, 1)
	assert.Equal(t, "/good", cmds[0].Slash)
	require.Len(t, errs, 2)
}

func TestLoadDirs_ProjectOverridesGlobal(t *testing.T) {
	g := t.TempDir()
	p := t.TempDir()
	writeTOML(t, g, "x.toml", `description = "global"`+"\nprompt = \"global {{args}}\"\n")
	writeTOML(t, p, "x.toml", `description = "project"`+"\nprompt = \"project {{args}}\"\n")

	cmds, errs := LoadDirs(g, p)
	require.Empty(t, errs)
	require.Len(t, cmds, 1, "project file must override global")
	assert.Equal(t, "project", cmds[0].Description)
	assert.Equal(t, SourceProject, cmds[0].Source)
}

func TestLoadDirs_StableOrder(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, dir, "z.toml", `description = "z"`+"\nprompt = \"z\"\n")
	writeTOML(t, dir, "a.toml", `description = "a"`+"\nprompt = \"a\"\n")
	writeTOML(t, dir, "m.toml", `description = "m"`+"\nprompt = \"m\"\n")

	cmds, _ := LoadDirs(dir, "")
	require.Len(t, cmds, 3)
	assert.Equal(t, "/a", cmds[0].Slash)
	assert.Equal(t, "/m", cmds[1].Slash)
	assert.Equal(t, "/z", cmds[2].Slash)
}

func TestPathToName(t *testing.T) {
	cases := map[string]string{
		"hello":        "hello",
		"git/commit":   "git:commit",
		"./foo/bar":    "foo:bar",
		"a/b/c":        "a:b:c",
		"deep/nested/x": "deep:nested:x",
	}
	for in, want := range cases {
		assert.Equal(t, want, pathToName(in), in)
	}
}

func TestDefaultGlobalDir(t *testing.T) {
	d := DefaultGlobalDir()
	if d == "" {
		t.Skip("no $HOME available on this platform")
	}
	assert.True(t, filepath.IsAbs(d))
	assert.Equal(t, filepath.Join(".jito", "commands"), filepath.Base(filepath.Dir(d))+"/"+filepath.Base(d))
}

func TestDefaultProjectDir(t *testing.T) {
	d := DefaultProjectDir("/tmp/work")
	assert.Equal(t, "/tmp/work/.jito/commands", d)
	assert.Equal(t, "", DefaultProjectDir(""))
}

func TestLoadFile_SourceProject(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.toml")
	require.NoError(t, os.WriteFile(p, []byte("prompt = \"x\""), 0o644))
	c, err := LoadFile(p, dir, true)
	require.NoError(t, err)
	assert.Equal(t, SourceProject, c.Source)
}