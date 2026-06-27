package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/agent"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/plugin"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/store"
	"github.com/uppu/jito/internal/telemetry"
	"github.com/uppu/jito/internal/tools"
)

// ===== Sprint A tests =====

func TestMockProvider(t *testing.T) {
	p := provider.NewMock("mock")
	ctx := context.Background()
	resp, err := p.Chat(ctx, "system", "hello jito")
	require.NoError(t, err)
	assert.Contains(t, resp, "jito")
}

func TestProviderWithMockEnv(t *testing.T) {
	t.Setenv("JITO_MOCK", "1")
	p, err := provider.NewFromConfig("")
	require.NoError(t, err)
	assert.Equal(t, "mock", p.Name())
}

func TestModeRouter(t *testing.T) {
	modes := []string{"dev", "reason", "create", "audit", "universal", "plan"}
	for _, name := range modes {
		m, err := mode.Get(name)
		require.NoError(t, err)
		assert.NotEmpty(t, m.SystemPrompt())
	}
}

func TestModeNotFound(t *testing.T) {
	_, err := mode.Get("nonsense")
	assert.Error(t, err)
}

func TestSQLiteStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	conv, err := store.Open(dbPath)
	require.NoError(t, err)
	defer conv.Close()

	require.NoError(t, conv.Append(store.Message{Role: "user", Content: "hi", Mode: "dev"}))
	require.NoError(t, conv.Append(store.Message{Role: "assistant", Content: "hello", Mode: "dev"}))

	msgs := conv.Messages()
	assert.Len(t, msgs, 2)
	require.NoError(t, conv.Clear())
	assert.Len(t, conv.Messages(), 0)
}

func TestToolsRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	r := tools.NewRegistry(tmpDir)

	names := r.List()
	for _, expected := range []string{"read", "write", "bash", "list", "diff", "patch", "log", "web"} {
		assert.Contains(t, names, expected)
	}

	ctx := context.Background()
	writeTool, _ := r.Get("write")
	testFile := filepath.Join(tmpDir, "hello.txt")
	_, err := writeTool.Execute(ctx, testFile+"|hello world")
	require.NoError(t, err)

	readTool, _ := r.Get("read")
	content, err := readTool.Execute(ctx, testFile)
	require.NoError(t, err)
	assert.Equal(t, "hello world", content)
}

func TestLogTool(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := realExec("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	require.NoError(t, os.WriteFile(tmpDir+"/a.txt", []byte("x"), 0o644))
	cmd = realExec("git", "-C", tmpDir, "add", ".")
	require.NoError(t, cmd.Run())
	cmd = realExec("git", "-C", tmpDir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	require.NoError(t, cmd.Run())

	lt := tools.NewLogTool(tmpDir)
	out, err := lt.Execute(context.Background(), "")
	require.NoError(t, err)
	assert.Contains(t, out, "init")
}

func TestInitCreatesConfig(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".jito", "config.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Skip("config not initialized; run `jito init` first")
	}
	data, _ := os.ReadFile(cfgPath)
	assert.Contains(t, string(data), "minimax")
}

// ===== Sprint B tests =====

func TestFailoverProvider(t *testing.T) {
	primary := provider.NewMock("primary")
	fallback := provider.NewMock("fallback")
	fo := provider.NewFailover(primary, fallback)
	ctx := context.Background()
	resp, err := fo.Chat(ctx, "sys", "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
}

func TestWorktreeCreate(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := realExec("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	wt, err := agent.NewWorktree(tmpDir, "test-branch")
	require.NoError(t, err)
	assert.Equal(t, "test-branch", wt.Branch)
	assert.Contains(t, wt.Path(), "test-branch")

	err = wt.Clean()
	assert.NoError(t, err)

	_, err = agent.List(tmpDir)
	require.NoError(t, err)
}

func TestSubAgentSpawn(t *testing.T) {
	if _, err := os.Stat("/home/up-ubuntu/wokrspace/open-uppu/jito/bin/jito"); err != nil {
		t.Skip("jito binary not found")
	}
	tmpDir := t.TempDir()
	ctx := context.Background()
	s, err := agent.Spawn(ctx, agent.SpawnConfig{
		Name:    "test-subagent",
		WorkDir: tmpDir,
		Mode:    "universal",
		Prompt:  "hello",
		Env:     map[string]string{"JITO_MOCK": "1", "PATH": os.Getenv("PATH")},
	})
	require.NoError(t, err)
	assert.NotZero(t, s.PID())
	out, _ := s.Wait()
	assert.NotEmpty(t, out)
}

func TestPlanMode(t *testing.T) {
	p := provider.NewMock("mock")
	ctx := context.Background()
	out, err := mode.Execute(ctx, p, "refactor this function")
	require.NoError(t, err)
	assert.NotNil(t, out)
}

// ===== Sprint C tests =====

func TestTelemetryTracker(t *testing.T) {
	tr := telemetry.New()
	if !tr.Enabled() {
		t.Skip("telemetry disabled (JITO_TELEMETRY=0)")
	}
	tr.TrackCall("chat", "dev", "mock", 100_000_000, nil)
	tr.Track(telemetry.Event{Type: "test", Mode: "dev"})
	t.Logf("telemetry path: %s", tr.Path())
	tr.Close()
}

func TestPluginLoader(t *testing.T) {
	tmpDir := t.TempDir()
	loader := plugin.NewLoader(tmpDir)
	assert.Equal(t, 0, loader.Count())

	path, err := plugin.Install(tmpDir, "myplugin")
	require.NoError(t, err)
	assert.Contains(t, path, "myplugin.json")

	loader2 := plugin.NewLoader(tmpDir)
	assert.Equal(t, 1, loader2.Count())

	modes := loader2.CustomModes()
	assert.Contains(t, modes, "myplugin")
}