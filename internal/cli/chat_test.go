package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildRootWithChat builds a fresh cobra root with persistent flags
// (mode/model/store) and the chat command attached — mirrors what
// root.go does in production.
func buildRootWithChat() *cobra.Command {
	root := &cobra.Command{Use: "jito"}
	root.PersistentFlags().StringP("mode", "m", "universal", "")
	root.PersistentFlags().StringP("model", "M", "", "")
	root.PersistentFlags().String("store", "", "")
	root.AddCommand(newChatCmd())
	return root
}

// TestNewChatCmd verifies the cobra command shape + flags.
func TestNewChatCmd(t *testing.T) {
	cmd := newChatCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "chat", cmd.Use)
	assert.NotNil(t, cmd.RunE)
}

// TestChatCmd_ContextLoaderSeedsEnv exercises the chat startup path
// that builds the JITO.md loader, sets JITO_CONTEXT_FILES, and prints
// the loaded-file line. We do not launch the TUI here — the RunE
// calls tui.Run which needs a real terminal; instead we capture the
// stdout the loader side-effect produces by invoking newChatCmd
// through cobra with a fake store path that will fail fast AFTER the
// loader side-effect runs.
func TestChatCmd_ContextLoaderSeedsEnv(t *testing.T) {
	// Build a project with a JITO.md so the loader finds 1 file.
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "JITO.md"),
		[]byte("# CTX\n"), 0o644))

	// Capture stdout (the loader prints "[jito] context: N files loaded").
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	// Run the chat command from `sub` with a bogus store path so the
	// store.Open fails fast — but the loader side-effect has already
	// happened by then.
	cmd := buildRootWithChat()
	cmd.SetArgs([]string{"chat", "--store", "/nonexistent/store.db"})
	cmd.SetIn(nil)
	cmd.SetOut(w)
	cmd.SetErr(w)

	// Change cwd so the loader sees our fixture.
	origCwd, _ := os.Getwd()
	require.NoError(t, os.Chdir(sub))
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	// Run; we expect an error (store open fails) but the loader
	// side-effect must still have run.
	runErr := cmd.Execute()
	_ = w.Close()

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	// The store-open error is expected — what we care about is the
	// loader line.
	if runErr != nil {
		t.Logf("chat cmd error (expected, store path invalid): %v", runErr)
	}
	assert.Contains(t, out, "[jito] context:",
		"loader side-effect should print '[jito] context: N files loaded'; got %q", out)
	assert.Equal(t, "1", os.Getenv("JITO_CONTEXT_FILES"),
		"JITO_CONTEXT_FILES must be set after loader runs")
}

// TestChatCmd_NoJITONoLeak ensures that without any JITO.md, the
// loader still runs but reports 0 files (no spurious context section
// in the prompt itself, which is verified by TestExecuteRun_NoJITO_NoLeak
// in the integration tests).
func TestChatCmd_NoJITONoLeak(t *testing.T) {
	tmp := t.TempDir()

	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	origCwd, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	cmd := buildRootWithChat()
	cmd.SetArgs([]string{"chat", "--store", "/nonexistent/store.db"})
	cmd.SetIn(nil)
	cmd.SetOut(w)
	cmd.SetErr(w)
	_ = cmd.Execute()
	_ = w.Close()

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	// Loader still runs, but reports 0 files.
	assert.Contains(t, out, "[jito] context: 0 files loaded")
	assert.Equal(t, "0", os.Getenv("JITO_CONTEXT_FILES"),
		"JITO_CONTEXT_FILES must still be set to '0' when no JITO.md")
}