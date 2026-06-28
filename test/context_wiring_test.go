package test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/cli"
	jitocontext "github.com/uppu/jito/internal/context"
	"github.com/uppu/jito/internal/provider"
)

// recordingProvider captures (system, prompt) of the most recent Chat
// call so tests can assert that JITO.md context was injected.
type recordingProvider struct {
	name        string
	lastSystem  string
	lastPrompt  string
	lastContext context.Context
	calls       int
	resp        string
}

func (r *recordingProvider) Name() string { return r.name }
func (r *recordingProvider) Chat(ctx context.Context, sys, p string) (string, error) {
	r.lastContext = ctx
	r.lastSystem = sys
	r.lastPrompt = p
	r.calls++
	if r.resp != "" {
		return r.resp, nil
	}
	return "ok", nil
}
func (r *recordingProvider) StreamChat(ctx context.Context, sys, p string, onChunk func(string) error) error {
	// For tests, just route through Chat + a single chunk.
	out, err := r.Chat(ctx, sys, p)
	if err != nil {
		return err
	}
	return onChunk(out)
}

// makeProjectFixture builds a temp project with:
//   - <root>/JITO.md     (root context)
//   - <root>/sub/JITO.md (sub-dir context)
//
// Returns the tmp root path. Caller is responsible for cleanup.
func makeProjectFixture(t *testing.T) (root, sub string) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "JITO.md"),
		[]byte("# ROOT CONTEXT MARKER\nThis is the root context.\n"),
		0o644,
	))
	sub = filepath.Join(root, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(sub, "JITO.md"),
		[]byte("# SUB CONTEXT MARKER\nThis is the sub-dir context.\n"),
		0o644,
	))
	return root, sub
}

// TestExecuteRun_InjectsJITOContext verifies that when a JITO.md
// exists in the cwd (or an ancestor), its body is prepended to the
// system prompt passed to the provider.
func TestExecuteRun_InjectsJITOContext(t *testing.T) {
	_, sub := makeProjectFixture(t)

	prov := &recordingProvider{name: "mock"}
	resp, err := cli.ExecuteRun(cli.RunOptions{
		Prompt:   "say hi",
		ModeName: "audit",
		Model:    "mock",
		CWD:      sub,
		Provider: prov,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, 1, prov.calls, "provider must be called exactly once")

	// Both root + sub markers should appear (walk-up loads both).
	assert.Contains(t, prov.lastSystem, "ROOT CONTEXT MARKER",
		"root JITO.md must be prepended to system prompt; got sys=%q", prov.lastSystem)
	assert.Contains(t, prov.lastSystem, "SUB CONTEXT MARKER",
		"sub JITO.md must be prepended to system prompt; got sys=%q", prov.lastSystem)

	// User prompt is passed through verbatim.
	assert.Equal(t, "say hi", prov.lastPrompt)
}

// TestExecuteRun_NoJITO_NoLeak ensures that without any JITO.md in the
// project tree, the system prompt is exactly the mode's base system
// prompt (no spurious context section).
func TestExecuteRun_NoJITO_NoLeak(t *testing.T) {
	root := t.TempDir()
	// No JITO.md anywhere.

	prov := &recordingProvider{name: "mock"}
	_, err := cli.ExecuteRun(cli.RunOptions{
		Prompt:   "hello",
		ModeName: "dev",
		Model:    "mock",
		CWD:      root,
		Provider: prov,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)

	// Must NOT contain the JITO.md section header.
	assert.NotContains(t, prov.lastSystem, "## JITO.md context",
		"with no JITO.md, no context section should be appended; sys=%q", prov.lastSystem)
}

// TestExecuteRun_PrebuiltLoader verifies the test seam for an
// externally constructed Loader (e.g. with custom HierarchyConfig).
func TestExecuteRun_PrebuiltLoader(t *testing.T) {
	_, sub := makeProjectFixture(t)

	loader, err := jitocontext.NewLoader(sub)
	require.NoError(t, err)
	require.NoError(t, err)
	// Pre-warm so Count() > 0 after Load().
	_, lerr := loader.Load()
	require.NoError(t, lerr)
	require.Greater(t, loader.Count(), 0, "fixture must yield at least 1 JITO.md")

	prov := &recordingProvider{name: "mock"}
	_, err = cli.ExecuteRun(cli.RunOptions{
		Prompt:   "ping",
		ModeName: "audit",
		Model:    "mock",
		CWD:      sub,
		Provider: prov,
		Loader:   loader,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)
	assert.Contains(t, prov.lastSystem, "ROOT CONTEXT MARKER")
	assert.Contains(t, prov.lastSystem, "SUB CONTEXT MARKER")
}

// TestExecuteRun_MissingModeErrors ensures mode validation runs before
// any provider call.
func TestExecuteRun_MissingModeErrors(t *testing.T) {
	prov := &recordingProvider{name: "mock"}
	_, err := cli.ExecuteRun(cli.RunOptions{
		Prompt:   "x",
		ModeName: "this-mode-does-not-exist",
		Model:    "mock",
		CWD:      t.TempDir(),
		Provider: prov,
		Ctx:      context.Background(),
	})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "mode")
	assert.Equal(t, 0, prov.calls, "provider must not be called when mode is invalid")
}

// TestExecuteRun_ContextLoaderFailureIsNonFatal verifies the contract
// that a failing loader (e.g. permission denied) does NOT abort the
// `run` command — the provider is still called with the bare system
// prompt.
func TestExecuteRun_ContextLoaderFailureIsNonFatal(t *testing.T) {
	// Use a path with a NUL byte — invalid on every OS — to force
	// the loader constructor to fail.
	prov := &recordingProvider{name: "mock"}
	resp, err := cli.ExecuteRun(cli.RunOptions{
		Prompt:   "hello",
		ModeName: "audit",
		Model:    "mock",
		CWD:      "/\x00invalid",
		Provider: prov,
		Ctx:      context.Background(),
	})
	require.NoError(t, err, "loader failure must not abort the run")
	assert.Equal(t, "ok", resp)
	assert.Equal(t, 1, prov.calls)
	assert.NotContains(t, prov.lastSystem, "## JITO.md context",
		"no context section when loader fails; sys=%q", prov.lastSystem)
}

// TestExecuteRun_FallsBackToDefaultProvider confirms that when no
// provider is injected and JITO_MOCK=1, the real mock provider is
// used (sanity check for the production code path).
func TestExecuteRun_FallsBackToDefaultProvider(t *testing.T) {
	t.Setenv("JITO_MOCK", "1")
	_, sub := makeProjectFixture(t)
	resp, err := cli.ExecuteRun(cli.RunOptions{
		Prompt:   "hello jito",
		ModeName: "audit",
		Model:    "",
		CWD:      sub,
		Ctx:      context.Background(),
		// no Provider — must fall back to provider.NewFromConfig
	})
	require.NoError(t, err)
	// Mock provider echoes the prompt.
	assert.Contains(t, strings.ToLower(resp), "jito")
}

// TestExecuteRun_AllModesCovered is a sanity sweep over every mode
// registered with mode.Get. Ensures context wiring works regardless
// of which system prompt is in play.
func TestExecuteRun_AllModesCovered(t *testing.T) {
	_, sub := makeProjectFixture(t)

	for _, modeName := range []string{"dev", "reason", "create", "audit", "universal", "plan"} {
		modeName := modeName
		t.Run(modeName, func(t *testing.T) {
			prov := &recordingProvider{name: "mock"}
			_, err := cli.ExecuteRun(cli.RunOptions{
				Prompt:   "x",
				ModeName: modeName,
				Model:    "mock",
				CWD:      sub,
				Provider: prov,
				Ctx:      context.Background(),
			})
			require.NoError(t, err)
			assert.Contains(t, prov.lastSystem, "ROOT CONTEXT MARKER",
				"mode=%s must still receive JITO.md context", modeName)
		})
	}
}

// TestExecuteRun_OrderOfBodies confirms that hierarchical load yields
// the files in walk-up order (nearest-to-farthest: sub first, then
// root). The exact ordering matters for prompt stability: nearest
// files override farther ones contextually for the LLM.
func TestExecuteRun_OrderOfBodies(t *testing.T) {
	_, sub := makeProjectFixture(t)

	prov := &recordingProvider{name: "mock"}
	_, err := cli.ExecuteRun(cli.RunOptions{
		Prompt:   "x",
		ModeName: "audit",
		Model:    "mock",
		CWD:      sub,
		Provider: prov,
		Ctx:      context.Background(),
	})
	require.NoError(t, err)
	rootIdx := strings.Index(prov.lastSystem, "ROOT CONTEXT MARKER")
	subIdx := strings.Index(prov.lastSystem, "SUB CONTEXT MARKER")
	require.NotEqual(t, -1, rootIdx)
	require.NotEqual(t, -1, subIdx)
	assert.Less(t, subIdx, rootIdx, "nearest (sub) JITO.md must appear before root")
}

// TestExecuteRun_RealProviderInterface sanity-checks that the
// recordingProvider satisfies the provider.Provider interface, so
// refactors of the interface surface will fail compilation here.
func TestExecuteRun_RealProviderInterface(t *testing.T) {
	var _ provider.Provider = (*recordingProvider)(nil)
}