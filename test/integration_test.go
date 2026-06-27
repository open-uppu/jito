package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/store"
	"github.com/uppu/jito/internal/tools"
)

// TestMockProvider tests the mock provider works.
func TestMockProvider(t *testing.T) {
	p := provider.NewMock("mock")
	ctx := context.Background()

	resp, err := p.Chat(ctx, "system", "hello jito")
	require.NoError(t, err)
	assert.Contains(t, resp, "jito")
	t.Logf("mock response: %s", resp)
}

// TestProviderWithMockEnv tests the NewFromConfig with mock env.
func TestProviderWithMockEnv(t *testing.T) {
	t.Setenv("JITO_MOCK", "1")
	p, err := provider.NewFromConfig("")
	require.NoError(t, err)
	assert.Equal(t, "mock", p.Name())
}

// TestModeRouter tests all 5 modes.
func TestModeRouter(t *testing.T) {
	modes := []string{"dev", "reason", "create", "audit", "universal"}
	for _, name := range modes {
		m, err := mode.Get(name)
		require.NoError(t, err, "mode %s should exist", name)
		assert.NotEmpty(t, m.SystemPrompt(), "mode %s needs system prompt", name)
		t.Logf("mode=%s desc=%s", m.Name(), m.Description())
	}
}

// TestModeNotFound tests error handling.
func TestModeNotFound(t *testing.T) {
	_, err := mode.Get("nonsense")
	assert.Error(t, err)
}

// TestSQLiteStore tests the conversation store.
func TestSQLiteStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	conv, err := store.Open(dbPath)
	require.NoError(t, err)
	defer conv.Close()

	// Append messages
	require.NoError(t, conv.Append(store.Message{Role: "user", Content: "hi", Mode: "dev"}))
	require.NoError(t, conv.Append(store.Message{Role: "assistant", Content: "hello", Mode: "dev"}))

	// Read back
	msgs := conv.Messages()
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "hi", msgs[0].Content)

	// Clear
	require.NoError(t, conv.Clear())
	assert.Len(t, conv.Messages(), 0)
}

// TestToolsRegistry tests the tool registry.
func TestToolsRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	r := tools.NewRegistry(tmpDir)

	// List tools
	names := r.List()
	assert.Contains(t, names, "read")
	assert.Contains(t, names, "write")
	assert.Contains(t, names, "bash")
	assert.Contains(t, names, "list")

	// Write file
	ctx := context.Background()
	writeTool, _ := r.Get("write")
	testFile := filepath.Join(tmpDir, "hello.txt")
	out, err := writeTool.Execute(ctx, testFile+"|hello world")
	require.NoError(t, err)
	assert.Contains(t, out, "wrote")

	// Read file
	readTool, _ := r.Get("read")
	content, err := readTool.Execute(ctx, testFile)
	require.NoError(t, err)
	assert.Equal(t, "hello world", content)

	// List directory
	listTool, _ := r.Get("list")
	out, err = listTool.Execute(ctx, tmpDir)
	require.NoError(t, err)
	assert.Contains(t, out, "hello.txt")

	// Bash
	bashTool, _ := r.Get("bash")
	out, err = bashTool.Execute(ctx, "echo hello bash")
	require.NoError(t, err)
	assert.Contains(t, out, "hello bash")
}

// TestInitCreatesConfig ensures init creates the config files.
func TestInitCreatesConfig(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	jitoDir := filepath.Join(home, ".jito")
	cfgPath := filepath.Join(jitoDir, "config.yaml")

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Skip("config not initialized; run `jito init` first")
	}

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "minimax")
}