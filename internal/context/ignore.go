package context

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreFileName is the canonical ignore filename.
const IgnoreFileName = ".jitoignore"

// IgnorePattern represents a single parsed gitignore-style pattern.
type IgnorePattern struct {
	// Negate is true when the pattern starts with '!'.
	Negate bool
	// DirOnly is true when the pattern ends with '/'.
	DirOnly bool
	// Anchored is true when the pattern starts with '/' or contains '/'
	// after the leading '!' (if any).
	Anchored bool
	// Raw is the original pattern text (without the leading ! or trailing /).
	Raw string
	// original is the unparsed pattern as it appeared in the file.
	original string
}

// String returns the pattern as it was parsed (with negator and trailing /).
func (p IgnorePattern) String() string { return p.original }

// IgnoreSet is an ordered collection of IgnorePattern with path matching.
// The zero value is ready to use.
type IgnoreSet struct {
	patterns []IgnorePattern
}

// NewIgnoreSet returns an empty IgnoreSet.
func NewIgnoreSet() *IgnoreSet { return &IgnoreSet{} }

// Len returns the number of patterns in the set.
func (s *IgnoreSet) Len() int { return len(s.patterns) }

// Append adds a single raw pattern line (without the trailing newline).
func (s *IgnoreSet) Append(raw string) {
	s.patterns = append(s.patterns, parsePattern(raw))
}

// Patterns returns a copy of the underlying pattern slice (for testing).
func (s *IgnoreSet) Patterns() []IgnorePattern {
	out := make([]IgnorePattern, len(s.patterns))
	copy(out, s.patterns)
	return out
}

// ParseIgnoreFile reads a .jitoignore file at path and returns the
// populated IgnoreSet. Empty file, missing file, and unreadable file
// all return an empty set and a nil error when the file simply does
// not exist. Other I/O errors propagate.
func ParseIgnoreFile(path string) (*IgnoreSet, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewIgnoreSet(), nil
		}
		return nil, err
	}
	defer f.Close()
	return ParseIgnoreReader(bufio.NewReader(f)), nil
}

// ParseIgnoreReader reads patterns line-by-line from r. Comments
// (lines starting with '#') and blank lines are skipped. Trailing
// whitespace and CR are stripped.
func ParseIgnoreReader(r io.Reader) *IgnoreSet {
	return ParseIgnoreLines(splitLines(r))
}

// ParseIgnoreLines parses patterns from a slice of pre-split lines.
func ParseIgnoreLines(lines []string) *IgnoreSet {
	set := NewIgnoreSet()
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			set.Append(line)
		}
	}
	return set
}

// splitLines reads r into a slice of newline-separated strings without
// the trailing newline.
func splitLines(r io.Reader) []string {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	var out []string
	for {
		line, err := br.ReadString('\n')
		out = append(out, line)
		if err != nil {
			break
		}
	}
	return out
}

// parsePattern converts a raw pattern line into an IgnorePattern.
func parsePattern(raw string) IgnorePattern {
	p := IgnorePattern{original: raw}
	s := strings.TrimSpace(raw)
	// Negation.
	if strings.HasPrefix(s, "!") {
		p.Negate = true
		s = strings.TrimSpace(s[1:])
	}
	// Directory-only.
	if strings.HasSuffix(s, "/") {
		p.DirOnly = true
		s = strings.TrimRight(s, "/")
	}
	// Anchored if starts with '/' or contains '/' anywhere.
	if strings.HasPrefix(s, "/") {
		p.Anchored = true
		s = strings.TrimPrefix(s, "/")
	} else if strings.Contains(s, "/") {
		p.Anchored = true
	}
	p.Raw = s
	return p
}

// Match reports whether path (relative to base) is ignored by this set.
// base must be the directory used as the root for matching.
func (s *IgnoreSet) Match(path string, base ...string) bool {
	if s == nil || len(s.patterns) == 0 {
		return false
	}
	var b string
	if len(base) > 0 && base[0] != "" {
		b = base[0]
	}
	// Normalise to forward slashes for matching.
	clean := filepath.ToSlash(path)
	clean = strings.TrimPrefix(clean, "./")
	ignored := false
	for _, p := range s.patterns {
		if matchOne(p, clean, b) {
			if p.Negate {
				ignored = false
			} else {
				ignored = true
			}
		}
	}
	return ignored
}

// matchOne tests a single pattern against a normalised relative path.
func matchOne(p IgnorePattern, rel, base string) bool {
	if p.Raw == "" {
		return false
	}
	candidates := []string{rel}
	// For UNANCHORED patterns without a '/', gitignore matches at any
	// level — try every path component as a basename candidate.
	if !p.Anchored && !strings.Contains(p.Raw, "/") {
		parts := strings.Split(rel, "/")
		for _, part := range parts {
			if part != rel {
				candidates = append(candidates, part)
			}
		}
	} else if !p.Anchored && strings.Contains(p.Raw, "/") && strings.Contains(rel, "/") {
		// Anchored by slash in pattern but not by leading '/'; match any
		// suffix of rel.
		for {
			idx := strings.Index(rel, "/")
			if idx < 0 {
				break
			}
			rel = rel[idx+1:]
			candidates = append(candidates, rel)
		}
	}
	for _, c := range candidates {
		if globMatch(p.Raw, c) {
			if p.DirOnly {
				// Only match if path is a directory. base optional.
				full := c
				if base != "" {
					full = filepath.Join(base, c)
				}
				if info, err := os.Stat(full); err == nil && info.IsDir() {
					return true
				}
				continue
			}
			return true
		}
	}
	return false
}

// globMatch implements gitignore-style glob matching: '*' matches any
// sequence (except '/'), '?' matches any single char, '**' matches any
// number of directories. We delegate to path/filepath.Match for '*'/'?'
// and handle '**' manually.
func globMatch(pattern, name string) bool {
	// Fast path: no special chars.
	if !strings.ContainsAny(pattern, "*?[") {
		return pattern == name
	}
	// '**' alone matches everything.
	if pattern == "**" {
		return true
	}
	// '**/' as prefix matches any number of dirs.
	if strings.HasPrefix(pattern, "**/") {
		rest := pattern[3:]
		// Try every suffix of name.
		for i := 0; i <= len(name); i++ {
			if i > 0 && name[i-1] != '/' {
				continue
			}
			sub := name[i:]
			if globMatch(rest, sub) {
				return true
			}
		}
		return false
	}
	// '/**' suffix matches any suffix.
	if strings.HasSuffix(pattern, "/**") {
		prefix := pattern[:len(pattern)-3]
		return name == prefix || strings.HasPrefix(name, prefix+"/")
	}
	return pathMatch(pattern, name)
}

// pathMatch wraps filepath.Match with forward-slash semantics.
func pathMatch(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	return ok
}
