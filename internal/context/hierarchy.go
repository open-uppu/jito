// Package context implements the JITO.md hierarchical context loader
// (analog of gemini-cli's GEMINI.md system) with @import syntax and
// .jitoignore support.
package context

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoTrustedRoot is returned when no trusted root can be determined
// for the working directory.
var ErrNoTrustedRoot = errors.New("no trusted root found")

// HierarchyConfig controls how the hierarchy walker behaves.
type HierarchyConfig struct {
	// TrustedRoots are paths the walker must not walk above.
	// If empty, defaults to filesystem root.
	TrustedRoots []string
	// StopAtGitRoot stops the walk when a .git directory is encountered.
	StopAtGitRoot bool
	// StopAtFirstJITO stops walking upward after the first JITO.md is
	// found (gemini-cli behavior); if false, all ancestors are collected.
	StopAtFirstJITO bool
}

// defaultTrustedRoot returns the first existing parent directory that
// contains a project marker (`.git`, `go.mod`, or `package.json`); falls
// back to the filesystem root if none is found.
func defaultTrustedRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return filepath.Clean(start)
	}
	dir := abs
	for {
		if isTrustedMarker(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return dir
		}
		dir = parent
	}
}

// isTrustedMarker reports whether dir contains any of the markers that
// indicate the directory is a trusted project boundary. Note that
// JITO.md is intentionally NOT a trusted marker (a JITO.md inside the
// start directory is data, not a project-root signal).
func isTrustedMarker(dir string) bool {
	for _, name := range []string{".git", "go.mod", "package.json"} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return true
		}
		// .git is a directory, not a file.
		if name == ".git" {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return true
			}
		}
	}
	return false
}

// TrustedRoot returns the trusted root for start using cfg. If
// cfg.TrustedRoots is non-empty, the first existing root that is an
// ancestor (or equal to) start is returned. Otherwise, the walk-up
// defaultTrustedRoot heuristic is used.
func TrustedRoot(start string, cfg HierarchyConfig) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if len(cfg.TrustedRoots) > 0 {
		for _, root := range cfg.TrustedRoots {
			rAbs, rErr := filepath.Abs(root)
			if rErr != nil {
				continue
			}
			// Allow root == start or root be a parent of start.
			if abs == rAbs || isParent(rAbs, abs) {
				if _, statErr := os.Stat(rAbs); statErr == nil {
					return rAbs, nil
				}
			}
		}
		return "", ErrNoTrustedRoot
	}
	return defaultTrustedRoot(abs), nil
}

// isParent reports whether parent is a (strict) ancestor of child.
func isParent(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	if strings.HasPrefix(rel, "..") || strings.Contains(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// WalkUp returns every directory from start up to (and including) root,
// ordered from nearest to farthest. If root is empty, defaultTrustedRoot
// is used. The slice is allocated freshly on each call.
func WalkUp(start, root string, cfg HierarchyConfig) ([]string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	var r string
	if root == "" {
		r, err = TrustedRoot(abs, cfg)
		if err != nil {
			return nil, err
		}
	} else {
		r, err = filepath.Abs(root)
		if err != nil {
			return nil, err
		}
	}
	if !isParent(r, abs) && abs != r {
		return nil, ErrNoTrustedRoot
	}
	out := make([]string, 0, 8)
	dir := abs
	for {
		out = append(out, dir)
		if dir == r {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		// StopAtGitRoot: do not walk above .git.
		if cfg.StopAtGitRoot {
			if info, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil && info.IsDir() {
				// Current dir contains .git, parent is above project.
				break
			}
		}
		dir = parent
	}
	return out, nil
}

// JITOFileName is the canonical context file name.
const JITOFileName = "JITO.md"

// FindJITOInDir returns the path to JITO.md inside dir, or "" if absent.
func FindJITOInDir(dir string) string {
	p := filepath.Join(dir, JITOFileName)
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

// IsWithinTrusted reports whether path is the same as or a descendant of
// root. Symlinks are not resolved; this is purely lexical.
func IsWithinTrusted(path, root string) bool {
	absP, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absR, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	return absP == absR || isParent(absR, absP)
}
