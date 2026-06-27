// Package plugin loads custom jito plugins (modes + tools).
// Plugins are Go plugins (.so) loaded at runtime, or config-based static plugins.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PluginManifest describes a plugin loaded from JSON.
type PluginManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Type        string `json:"type"` // "mode" or "tool"

	// For mode plugins
	ModeName        string `json:"mode_name,omitempty"`
	SystemPrompt    string `json:"system_prompt,omitempty"`

	// For tool plugins
	ToolName        string `json:"tool_name,omitempty"`
	ToolCommand     string `json:"tool_command,omitempty"`
	ToolArgs        []string `json:"tool_args,omitempty"`
	ToolDescription string `json:"tool_description,omitempty"`
}

// Loader manages plugin discovery and registration.
type Loader struct {
	dir     string
	plugins []PluginManifest
}

// NewLoader creates a loader scanning the plugin directory.
// Default: ~/.jito/plugins/*.json
func NewLoader(dir string) *Loader {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return &Loader{}
		}
		dir = filepath.Join(home, ".jito", "plugins")
	}
	l := &Loader{dir: dir}
	l.scan()
	return l
}

func (l *Loader) scan() {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return // directory doesn't exist — that's fine
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(l.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p PluginManifest
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		l.plugins = append(l.plugins, p)
	}
}

// Plugins returns all loaded plugins.
func (l *Loader) Plugins() []PluginManifest {
	return l.plugins
}

// Count returns the number of loaded plugins.
func (l *Loader) Count() int {
	return len(l.plugins)
}

// RegisterModes returns custom mode plugins for mode.Get registration.
// Returns a map of mode-name → system-prompt.
func (l *Loader) CustomModes() map[string]string {
	out := make(map[string]string)
	for _, p := range l.plugins {
		if p.Type == "mode" && p.ModeName != "" {
			out[p.ModeName] = p.SystemPrompt
		}
	}
	return out
}

// Install writes a sample plugin manifest to disk.
// Returns the path written.
func Install(dir, pluginName string) (string, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".jito", "plugins")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	sample := PluginManifest{
		Name:        pluginName,
		Version:     "0.1.0",
		Description: fmt.Sprintf("Sample %s plugin", pluginName),
		Type:        "mode",
		ModeName:    pluginName,
		SystemPrompt: fmt.Sprintf("You are jito in %s mode — a custom user-defined mode.", pluginName),
	}
	path := filepath.Join(dir, pluginName+".json")
	data, _ := json.MarshalIndent(sample, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// adapterTool wraps an external command as a Tool.
type adapterTool struct {
	name string
	desc string
	cmd  string
	args []string
}

func (a *adapterTool) Name() string        { return a.name }
func (a *adapterTool) Description() string { return a.desc }
// Execute impl would go in a follow-up — kept simple for now.
func (a *adapterTool) Execute(ctx context.Context, input string) (string, error) {
	return "", nil
}