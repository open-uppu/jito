// Package context loader.go — orchestrates the hierarchical JITO.md
// loader with .jitoignore filtering and JIT-on-read lookup.
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Loader is the top-level JITO.md loader. It is safe for concurrent
// use; reads take an RLock and writes (Reload) take a Lock.
type Loader struct {
	mu       sync.RWMutex
	home     string        // user home dir for ~/.jito/JITO.md
	cwd      string        // current working dir for walk-up
	root     string        // trusted root (cached)
	hcfg     HierarchyConfig
	ignore   *IgnoreSet
	ignores  map[string]*IgnoreSet // per-dir ignore caches (absolute path -> set)
	resolver *ImportResolver
	files    []string // absolute paths of currently loaded JITO.md files
	bodies   map[string]string
}

// LoaderOption mutates a Loader at construction.
type LoaderOption func(*Loader)

// WithHome overrides the user home directory (default: $HOME).
func WithHome(home string) LoaderOption {
	return func(l *Loader) { l.home = home }
}

// WithCWD overrides the current working directory.
func WithCWD(cwd string) LoaderOption {
	return func(l *Loader) { l.cwd = cwd }
}

// WithHierarchy sets the hierarchy walk config.
func WithHierarchy(cfg HierarchyConfig) LoaderOption {
	return func(l *Loader) { l.hcfg = cfg }
}

// NewLoader constructs a Loader rooted at cwd (or os.Getwd()) and the
// current user home (or $HOME). It does not perform any I/O until
// Load() is called.
func NewLoader(cwd string, opts ...LoaderOption) (*Loader, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("abs cwd: %w", err)
	}
	home := os.Getenv("HOME")
	l := &Loader{
		home:     home,
		cwd:      absCwd,
		ignores:  make(map[string]*IgnoreSet),
		resolver: NewImportResolver(),
		bodies:   make(map[string]string),
	}
	for _, opt := range opts {
		opt(l)
	}
	root, err := TrustedRoot(absCwd, l.hcfg)
	if err != nil {
		return nil, err
	}
	l.root = root
	return l, nil
}

// Home returns the user home dir the loader was built with.
func (l *Loader) Home() string { return l.home }

// CWD returns the cwd the loader was built with.
func (l *Loader) CWD() string { return l.cwd }

// Root returns the trusted root used for walk-up.
func (l *Loader) Root() string { return l.root }

// LoadedFiles returns a snapshot of the absolute paths to the JITO.md
// files most recently loaded (or reloaded).
func (l *Loader) LoadedFiles() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, len(l.files))
	copy(out, l.files)
	return out
}

// Count returns the number of loaded context files.
func (l *Loader) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.files)
}

// Load performs a hierarchical load: global + walk-up + (optional) JIT
// file. The merged text is written into the resolver cache and into
// the Loader's body map. After Load returns, LoadedFiles() reflects the
// new state.
func (l *Loader) Load() (*LoadResult, error) {
	return l.LoadWithJIT("")
}

// LoadWithJIT performs a hierarchical load and additionally scans jitDir
// and its ancestors (up to root) for JITO.md, including any JIT-only
// context file. An empty jitDir means "no JIT scan".
func (l *Loader) LoadWithJIT(jitDir string) (*LoadResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Pre-warm ignore cache for the root so shouldLoad has it.
	l.cacheIgnoreFor(l.root)

	collected := make([]string, 0, 4)

	// Cache root-level ignore first so shouldLoad can use it.
	l.cacheIgnoreFor(l.root)

	// 1) Global: ~/.jito/JITO.md (if home set and file exists).
	if l.home != "" {
		if p := filepath.Join(l.home, ".jito", JITOFileName); fileExists(p) {
			if l.shouldLoad(p) {
				collected = append(collected, p)
			}
		}
	}

	// 2) Walk-up from cwd.
	dirs, err := WalkUp(l.cwd, l.root, l.hcfg)
	if err != nil {
		return nil, fmt.Errorf("walk-up: %w", err)
	}
	for _, d := range dirs {
		// Pre-warm ignore cache for every walk-up dir so shouldLoad works.
		l.cacheIgnoreFor(d)
		if p := FindJITOInDir(d); p != "" && l.shouldLoad(p) {
			collected = append(collected, p)
		}
		if l.hcfg.StopAtFirstJITO && findInList(collected, d) {
			break
		}
	}

	// 3) JIT: scan jitDir and ancestors up to root.
	if jitDir != "" {
		abs, absErr := filepath.Abs(jitDir)
		if absErr == nil {
			l.cacheIgnoreFor(abs)
			jitDirs, wErr := WalkUp(abs, l.root, l.hcfg)
			if wErr == nil {
				for _, d := range jitDirs {
					if p := FindJITOInDir(d); p != "" && l.shouldLoad(p) {
						if !containsPath(collected, p) {
							collected = append(collected, p)
						}
					}
				}
			}
		}
	}

	// Resolve imports per-file and aggregate.
	l.files = collected
	l.bodies = make(map[string]string, len(collected))
	l.resolver = NewImportResolver() // fresh resolver for this load
	final := &LoadResult{}
	for _, p := range collected {
		body, readErr := os.ReadFile(p)
		if readErr != nil {
			final.Errors = append(final.Errors, fmt.Errorf("read %s: %w", p, readErr))
			continue
		}
		l.bodies[p] = string(body)
		// Resolve transitive imports; reuse per-file resolver subgraph
		// so cycles across files are caught too.
		per, err := l.resolver.LoadImports(p)
		if err != nil {
			final.Errors = append(final.Errors, fmt.Errorf("imports %s: %w", p, err))
			continue
		}
		final.Imports = append(final.Imports, per.Imports...)
		final.Errors = append(final.Errors, per.Errors...)
		if final.Body == "" {
			final.Body = string(body)
		} else {
			final.Body += "\n\n" + string(body)
		}
		if per.Body != "" && per.Body != string(body) {
			final.Body += "\n\n" + per.Body
		}
	}
	return final, nil
}

// Reload forces a fresh Load() and returns the new LoadResult.
func (l *Loader) Reload() (*LoadResult, error) { return l.LoadWithJIT("") }

// LoadForFile performs a JIT load for a single file read: scans the
// file's directory and ancestors (up to root) and merges any newly
// discovered JITO.md into the current state. Already-loaded files are
// not duplicated.
func (l *Loader) LoadForFile(filePath string) (*LoadResult, error) {
	if filePath == "" {
		return l.LoadWithJIT("")
	}
	dir := filepath.Dir(filePath)
	return l.LoadWithJIT(dir)
}

// Bodies returns the body map keyed by absolute path (snapshot).
func (l *Loader) Bodies() map[string]string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]string, len(l.bodies))
	for k, v := range l.bodies {
		out[k] = v
	}
	return out
}

// shouldLoad returns true if p is not matched by any cached .jitoignore
// set on its own directory or any of its ancestors up to root.
func (l *Loader) shouldLoad(p string) bool {
	dir := filepath.Dir(p)
	for d := dir; ; d = filepath.Dir(d) {
		if set, ok := l.ignores[d]; ok {
			rel, err := filepath.Rel(d, p)
			if err != nil {
				rel = p
			}
			if set.Match(rel) {
				return false
			}
		}
		if d == l.root || d == filepath.Dir(d) {
			break
		}
	}
	return true
}

// cacheIgnoreFor loads .jitoignore for dir and every ancestor up to
// root, populating l.ignores.
func (l *Loader) cacheIgnoreFor(dir string) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, ok := l.ignores[d]; !ok {
			set, _ := ParseIgnoreFile(filepath.Join(d, IgnoreFileName))
			l.ignores[d] = set
		}
		if d == l.root || d == filepath.Dir(d) {
			break
		}
	}
}


// --- helpers ---

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func findInList(list []string, dir string) bool {
	for _, p := range list {
		if filepath.Dir(p) == dir {
			return true
		}
	}
	return false
}

func containsPath(list []string, p string) bool {
	for _, e := range list {
		if e == p {
			return true
		}
	}
	return false
}

// FormatSummary returns a single-line human-readable summary of a
// LoadResult, used by the TUI footer.
func FormatSummary(res *LoadResult, files int) string {
	if res == nil {
		return fmt.Sprintf("%d context files loaded", files)
	}
	imports := len(res.Imports)
	if imports > 0 {
		return fmt.Sprintf("%d context files loaded (%d imports)", files, imports)
	}
	return fmt.Sprintf("%d context files loaded", files)
}

// SystemPromptSection formats the loaded bodies as a single system-
// prompt section delimited by fenced code blocks. Returns "" if there
// are no bodies.
func (l *Loader) SystemPromptSection() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.bodies) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## JITO.md context\n\n")
	for _, p := range l.files {
		body, ok := l.bodies[p]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n\n%s\n\n", p, body)
	}
	return sb.String()
}
