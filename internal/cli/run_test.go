package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider records the (system, prompt) of the latest Chat call.
type stubProvider struct {
	lastSystem string
	lastPrompt string
	resp       string
	err        error
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Chat(_ context.Context, sys, p string) (string, error) {
	s.lastSystem = sys
	s.lastPrompt = p
	if s.err != nil {
		return "", s.err
	}
	if s.resp != "" {
		return s.resp, nil
	}
	return "ok", nil
}
func (s *stubProvider) StreamChat(ctx context.Context, sys, p string, onChunk func(string) error) error {
	out, err := s.Chat(ctx, sys, p)
	if err != nil {
		return err
	}
	return onChunk(out)
}

func TestExecuteRun_PropagatesPrompt(t *testing.T) {
	p := &stubProvider{resp: "hi"}
	resp, err := ExecuteRun(RunOptions{
		Prompt:   "ping",
		ModeName: "audit",
		Model:    "stub",
		CWD:      t.TempDir(),
		Provider: p,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)
	assert.Equal(t, "hi", resp)
	assert.Equal(t, "ping", p.lastPrompt)
}

func TestExecuteRun_AppendsContextWhenPresent(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "JITO.md"),
		[]byte("# ROOT MARKER\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "JITO.md"),
		[]byte("# SUB MARKER\n"), 0o644))

	p := &stubProvider{}
	_, err := ExecuteRun(RunOptions{
		Prompt:   "x",
		ModeName: "audit",
		Model:    "stub",
		CWD:      sub,
		Provider: p,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)
	assert.Contains(t, p.lastSystem, "ROOT MARKER")
	assert.Contains(t, p.lastSystem, "SUB MARKER")
}

func TestExecuteRun_NoContextWhenAbsent(t *testing.T) {
	p := &stubProvider{}
	_, err := ExecuteRun(RunOptions{
		Prompt:   "x",
		ModeName: "audit",
		Model:    "stub",
		CWD:      t.TempDir(),
		Provider: p,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)
	assert.NotContains(t, p.lastSystem, "## JITO.md context")
}

func TestExecuteRun_InvalidModeErrors(t *testing.T) {
	p := &stubProvider{}
	_, err := ExecuteRun(RunOptions{
		Prompt:   "x",
		ModeName: "nope",
		Model:    "stub",
		CWD:      t.TempDir(),
		Provider: p,
		Ctx:      context.Background(),
	})
	require.Error(t, err)
}

func TestExecuteRun_ProviderErrorPropagates(t *testing.T) {
	p := &stubProvider{err: assertErr{}}
	_, err := ExecuteRun(RunOptions{
		Prompt:   "x",
		ModeName: "audit",
		Model:    "stub",
		CWD:      t.TempDir(),
		Provider: p,
		Ctx:      context.Background(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider call")
}

type assertErr struct{}

func (assertErr) Error() string { return "stub fail" }

func TestModelOrDefault(t *testing.T) {
	assert.Equal(t, "minimax/MiniMax-M3", modelOrDefault(""))
	assert.Equal(t, "custom", modelOrDefault("custom"))
}

func TestExecuteRun_VerbosePrintsLoadedCount(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "JITO.md"),
		[]byte("# MARKER\n"), 0o644))
	p := &stubProvider{}

	// Capture stdout by redirecting.
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	_, err := ExecuteRun(RunOptions{
		Prompt:   "x",
		ModeName: "audit",
		Model:    "stub",
		CWD:      root,
		Provider: p,
		Ctx:      context.Background(),
		Verbose:  true,
	})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	got := string(buf[:n])
	assert.Contains(t, got, "context: 1 files loaded")
	assert.Contains(t, got, "system=")
	assert.True(t, strings.Contains(got, "jito") || len(got) > 0)
}

func TestExecuteRun_NilContextFallsBackToBackground(t *testing.T) {
	p := &stubProvider{}
	_, err := ExecuteRun(RunOptions{
		Prompt:   "x",
		ModeName: "audit",
		Model:    "stub",
		CWD:      t.TempDir(),
		Provider: p,
		// Ctx nil → falls back to Background
	})
	require.NoError(t, err)
}

func TestExecuteRun_NilProviderInvokesNewFromConfig(t *testing.T) {
	t.Setenv("JITO_MOCK", "1")
	_, err := ExecuteRun(RunOptions{
		Prompt:   "hello jito",
		ModeName: "audit",
		Model:    "",
		CWD:      t.TempDir(),
		// no Provider
		Ctx: context.Background(),
	})
	require.NoError(t, err)
}

func TestNewRunCmd_Shape(t *testing.T) {
	cmd := newRunCmd()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "run")
	assert.NotNil(t, cmd.RunE)
	// Confirm required arg.
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err, "run requires at least 1 arg")
}

func TestNewRunCmd_ExecutesEndToEnd(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "JITO.md"),
		[]byte("# MARKER\n"), 0o644))

	t.Setenv("JITO_MOCK", "1")
	origCwd, _ := os.Getwd()
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	// Build a fake root with persistent flags + run command attached.
	rootCmd := &cobra.Command{Use: "jito"}
	rootCmd.PersistentFlags().StringP("mode", "m", "audit", "")
	rootCmd.PersistentFlags().StringP("model", "M", "", "")
	rootCmd.AddCommand(newRunCmd())
	rootCmd.SetArgs([]string{"run", "ping"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetIn(strings.NewReader(""))
	require.NoError(t, rootCmd.Execute())
}