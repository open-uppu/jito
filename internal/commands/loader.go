// Package commands implements custom slash-command discovery and parsing for
// the jito TUI. Commands are declared as TOML files in two locations:
//
//   - User-global:    ~/.jito/commands/*.toml
//   - Project-local:  <cwd>/.jito/commands/*.toml    (overrides global)
//
// File path → slash name mapping: the file's basename without ".toml"
// becomes the slash token.  Nested directories produce nested names; for
// example "git/commit.toml" → "/git:commit".
//
// TOML schema:
//
//	description = "Generates a fix for a given issue"
//	prompt     = "Please provide a code fix for: {{args}}"
//
// The placeholder {{args}} is substituted at invocation time:
//   - raw (un-quoted) when it appears outside !{...} blocks
//   - shell-escaped (single-quoted with embedded single-quote escaping)
//     when it appears inside !{...} blocks.  This mirrors gemini-cli's
//     convention so that user input never breaks out of a shell snippet.
package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Source identifies where a command was loaded from.
type Source string

const (
	SourceGlobal   Source = "global"
	SourceProject  Source = "project"
	SourceBuiltIn  Source = "builtin"
)

// Command is a parsed slash command.
type Command struct {
	// Slash is the invocation token, e.g. "/git:commit".  Always begins
	// with a leading slash.
	Slash string
	// Name is the bare token without the leading slash, e.g. "git:commit".
	Name string
	// Description is a short human-readable summary.
	Description string
	// Prompt is the raw template body (may contain {{args}} / !{...}).
	Prompt string
	// Path is the file the command was loaded from (empty for built-in).
	Path string
	// Source identifies origin.
	Source Source
}

// fileDoc is the on-disk TOML schema.
type fileDoc struct {
	Description string `toml:"description"`
	Prompt     string `toml:"prompt"`
}

// ErrNoPrompt is returned when a TOML file is missing the prompt field.
var ErrNoPrompt = errors.New("commands: missing 'prompt' field")

// LoadDir reads every *.toml file in dir, recursively, and parses them as
// commands.  Slash names are derived from the file path relative to dir.
//
// projectOverride controls the Source label: when true, files are tagged as
// SourceProject; otherwise SourceGlobal.  dir must exist (use LoadDirs to
// skip missing dirs).
//
// Parse errors for individual files are collected into errs but loading
// continues so a single bad file does not blank out the entire registry.
func LoadDir(dir string, projectOverride bool) (cmds []*Command, errs []error) {
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, nil
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			errs = append(errs, fmt.Errorf("walk %s: %w", path, walkErr))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".toml") {
			return nil
		}
		cmd, perr := LoadFile(path, dir, projectOverride)
		if perr != nil {
			errs = append(errs, perr)
			return nil
		}
		cmds = append(cmds, cmd)
		return nil
	})
	return cmds, errs
}

// LoadFile parses a single TOML command file.  base is the parent directory
// used to derive the slash name (the file's path relative to base).
func LoadFile(path, base string, projectOverride bool) (*Command, error) {
	var doc fileDoc
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if strings.TrimSpace(doc.Prompt) == "" {
		return nil, fmt.Errorf("%s: %w", path, ErrNoPrompt)
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return nil, fmt.Errorf("rel %s: %w", path, err)
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	name := pathToName(rel)
	src := SourceGlobal
	if projectOverride {
		src = SourceProject
	}
	return &Command{
		Slash:       "/" + name,
		Name:        name,
		Description: doc.Description,
		Prompt:      doc.Prompt,
		Path:        path,
		Source:      src,
	}, nil
}

// LoadDirs is a convenience that loads global + project in one call.
// Either dir may be empty (skipped silently).  errs collects per-file
// problems.
func LoadDirs(globalDir, projectDir string) (cmds []*Command, errs []error) {
	if globalDir != "" {
		g, ge := LoadDir(globalDir, false)
		cmds = append(cmds, g...)
		errs = append(errs, ge...)
	}
	if projectDir != "" {
		p, pe := LoadDir(projectDir, true)
		// Project overrides global: drop any global command with the
		// same slash.
		pMap := make(map[string]struct{}, len(p))
		for _, c := range p {
			pMap[c.Slash] = struct{}{}
		}
		filtered := cmds[:0]
		for _, c := range cmds {
			if _, dup := pMap[c.Slash]; !dup {
				filtered = append(filtered, c)
			}
		}
		cmds = append(filtered, p...)
		errs = append(errs, pe...)
	}
	// Stable order for diff-friendly tests / picker rendering.
	sort.SliceStable(cmds, func(i, j int) bool { return cmds[i].Slash < cmds[j].Slash })
	return cmds, errs
}

// pathToName converts "git/commit" → "git:commit".  Separator normalisation
// matches gemini-cli's slash-name format so users get predictable names
// across both CLIs.
func pathToName(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	return strings.ReplaceAll(rel, "/", ":")
}

// DefaultGlobalDir returns the user-global commands directory
// (~/.jito/commands).  Returns "" on platforms where $HOME cannot be
// determined.
func DefaultGlobalDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jito", "commands")
}

// DefaultProjectDir returns the project-local commands directory
// (<cwd>/.jito/commands).
func DefaultProjectDir(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Join(cwd, ".jito", "commands")
}