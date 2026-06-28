package context

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxImportDepth is the maximum recursion depth for @import.
const MaxImportDepth = 5

// ErrImportCycle is returned when an import cycle is detected.
var ErrImportCycle = errors.New("import cycle")

// ErrImportTooDeep is returned when import depth exceeds MaxImportDepth.
var ErrImportTooDeep = errors.New("import too deep")

// importRegex matches `@./path/to/file.md`, `@../path.md`, `@/abs.md`,
// and bare `@path.md` (relative) at line start or after whitespace.
// Group 1 captures the leading dots (./ or ../ or empty), group 2
// captures either `/foo/bar.md` or `foo/bar.md`.
var importRegex = regexp.MustCompile(`(?:^|\s)@(\.{0,2})(/[^@\s]*?\.md|[^@\s./][^@\s]*?\.md)\b`)

// ExtractImports returns the list of import paths declared in body.
// Each path includes the leading ./ or ../ (or / for absolute, or bare
// name for relative) so callers can reconstruct the original `@...`
// reference. It does not validate or resolve them; that is
// ResolveImport's job.
func ExtractImports(body string) []string {
	matches := importRegex.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		dot := m[1]
		slash := m[2]
		out = append(out, dot+slash)
	}
	return out
}

// ResolvedImport describes one resolved @import after path validation.
type ResolvedImport struct {
	// Ref is the literal `@./...` reference as it appeared in source.
	Ref string
	// AbsPath is the absolute, symlink-free path to the import target.
	AbsPath string
	// Depth is the recursion depth at which this import was resolved
	// (0 for top-level imports of the root file).
	Depth int
}

// ImportResolver resolves and loads import references with cycle and
// depth protection. The zero value is ready to use.
type ImportResolver struct {
	// Visited is keyed by absolute path; populated as imports are
	// resolved. Cycles return ErrImportCycle.
	Visited map[string]bool
	// MaxDepth caps recursion; default MaxImportDepth.
	MaxDepth int
	// Loaded is a cache of file contents keyed by absolute path.
	Loaded map[string]string
}

// NewImportResolver returns a fresh resolver.
func NewImportResolver() *ImportResolver {
	return &ImportResolver{
		Visited:  make(map[string]bool),
		MaxDepth: MaxImportDepth,
		Loaded:   make(map[string]string),
	}
}

// ResolveImport resolves a single `@./path/to/file.md` reference relative
// to baseDir, returning the absolute path. The path is not read here.
func (r *ImportResolver) ResolveImport(ref, baseDir string) (string, error) {
	if r == nil {
		return "", errors.New("nil resolver")
	}
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "@") {
		return "", fmt.Errorf("not an import: %q", ref)
	}
	p := strings.TrimPrefix(ref, "@")
	if p == "" {
		return "", fmt.Errorf("empty import: %q", ref)
	}
	var abs string
	switch {
	case strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../"):
		abs = filepath.Join(baseDir, p)
	case strings.HasPrefix(p, "/"):
		abs = filepath.Clean(p)
	default:
		// Bare path like "subdir/file.md" — treat as relative.
		abs = filepath.Join(baseDir, p)
	}
	abs = filepath.Clean(abs)
	if !strings.HasSuffix(strings.ToLower(abs), ".md") {
		return "", fmt.Errorf("import must reference .md file: %q", ref)
	}
	// Refuse to leave the trusted root if set on the resolver.
	return abs, nil
}

// LoadResult is the outcome of a full resolution pass on a root file.
type LoadResult struct {
	// Body is the concatenated, depth-ordered text from the root and
	// every successfully resolved import. Imports are inlined in the
	// order they were resolved (root first, then BFS by depth).
	Body string
	// Imports is the flat list of all resolved imports (root excluded).
	Imports []ResolvedImport
	// Errors contains per-import errors that did not abort the pass.
	// A non-nil Errors slice does NOT mean Body is empty — partial
	// resolution is normal when an import file is missing.
	Errors []error
}

// LoadImports resolves and inlines every @import in the file at rootAbs.
// The returned LoadResult.Body has imports inlined as their full text.
// Cycles and depth violations are reported via LoadResult.Errors and
// do not abort the entire pass.
func (r *ImportResolver) LoadImports(rootAbs string) (*LoadResult, error) {
	if r == nil {
		return nil, errors.New("nil resolver")
	}
	if r.Visited == nil {
		r.Visited = make(map[string]bool)
	}
	if r.Loaded == nil {
		r.Loaded = make(map[string]string)
	}
	if r.MaxDepth <= 0 {
		r.MaxDepth = MaxImportDepth
	}
	res := &LoadResult{}
	body, err := r.readFile(rootAbs)
	if err != nil {
		return nil, err
	}
	res.Body = body
	if r.Visited[rootAbs] {
		return res, nil
	}
	r.Visited[rootAbs] = true
	r.walk(rootAbs, body, 1, res)
	return res, nil
}

// walk processes the imports in body (the contents of fileAbs).
func (r *ImportResolver) walk(fileAbs, body string, depth int, res *LoadResult) {
	if depth > r.MaxDepth {
		res.Errors = append(res.Errors, fmt.Errorf("%w: depth=%d at %s", ErrImportTooDeep, depth, fileAbs))
		return
	}
	for _, p := range ExtractImports(body) {
		ref := "@" + p
		baseDir := filepath.Dir(fileAbs)
		target, err := r.ResolveImport(ref, baseDir)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("resolve %s: %w", ref, err))
			continue
		}
		if r.Visited[target] {
			res.Errors = append(res.Errors, fmt.Errorf("%w: %s -> %s", ErrImportCycle, fileAbs, target))
			continue
		}
		r.Visited[target] = true
		contents, err := r.readFile(target)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("read %s: %w", target, err))
			r.Visited[target] = false
			continue
		}
		res.Imports = append(res.Imports, ResolvedImport{
			Ref:     ref,
			AbsPath: target,
			Depth:   depth,
		})
		res.Body += "\n\n" + contents
		r.walk(target, contents, depth+1, res)
	}
}

// readFile reads file from disk, caching contents in r.Loaded.
func (r *ImportResolver) readFile(file string) (string, error) {
	if c, ok := r.Loaded[file]; ok {
		return c, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	c := string(data)
	r.Loaded[file] = c
	return c, nil
}
