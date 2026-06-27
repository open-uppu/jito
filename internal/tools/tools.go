// Package tools implements the tool registry for jito (read/write/bash).
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Tool is the interface every agent tool must implement.
type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, input string) (string, error)
}

// Registry holds available tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a registry with built-in tools (read, write, bash).
func NewRegistry(workDir string) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	r.Register(ReadTool{workDir: workDir})
	r.Register(WriteTool{workDir: workDir})
	r.Register(BashTool{workDir: workDir})
	r.Register(ListTool{workDir: workDir})
	return r
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all tool names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// --- Read tool ---

type ReadTool struct{ workDir string }

func (ReadTool) Name() string { return "read" }
func (ReadTool) Description() string {
	return "Read a file. Input format: <path>"
}
func (t ReadTool) Execute(ctx context.Context, input string) (string, error) {
	path := strings.TrimSpace(input)
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// --- Write tool ---

type WriteTool struct{ workDir string }

func (WriteTool) Name() string { return "write" }
func (WriteTool) Description() string {
	return "Write content to a file. Input format: <path>|<content>"
}
func (t WriteTool) Execute(ctx context.Context, input string) (string, error) {
	parts := strings.SplitN(input, "|", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("expected <path>|<content>")
	}
	path := strings.TrimSpace(parts[0])
	content := parts[1]
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workDir, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// --- Bash tool ---

type BashTool struct{ workDir string }

func (BashTool) Name() string { return "bash" }
func (BashTool) Description() string {
	return "Run a bash command. Input: command string"
}
func (t BashTool) Execute(ctx context.Context, input string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", input)
	cmd.Dir = t.workDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// --- List tool ---

type ListTool struct{ workDir string }

func (ListTool) Name() string { return "list" }
func (ListTool) Description() string {
	return "List files in a directory. Input: <path> (default: workdir)"
}
func (t ListTool) Execute(ctx context.Context, input string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		path = t.workDir
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(t.workDir, path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Name() + "\n")
	}
	return sb.String(), nil
}