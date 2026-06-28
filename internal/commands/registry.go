package commands

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Decision is returned by Registry.Lookup / LookupPrefix and tells the
// caller how to handle the user's slash input.
type Decision int

const (
	// DecisionUnknown means the input is not a recognised command; the
	// caller should treat it as plain text (or surface "unknown command").
	DecisionUnknown Decision = iota
	// DecisionExact means an exact slash match was found; Prompt/Expand
	// can be used directly.
	DecisionExact
	// DecisionAmbiguous means the prefix matches multiple commands; the
	// caller should open the picker.
	DecisionAmbiguous
	// DecisionBuiltin means the input is a built-in command such as
	// /help or /clear; the TUI handles those inline.
	DecisionBuiltin
)

// BuiltinSlashes lists built-in slash tokens that the TUI consumes
// directly.  They are never stored in the user registry.
var BuiltinSlashes = map[string]bool{
	"/help":     true,
	"/clear":    true,
	"/mode":     true,
	"/quit":     true,
	"/exit":     true,
	"/commands": true, // /commands list | /commands reload
}

// IsBuiltin reports whether slash is a built-in TUI command.
func IsBuiltin(slash string) bool { return BuiltinSlashes[slash] }

// Registry holds the active set of slash commands and is safe for
// concurrent reads (Update under mu).
type Registry struct {
	mu      sync.RWMutex
	cmds    map[string]*Command // slash → cmd
	order   []string            // stable display order
	rootErr []error             // last reload errors
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{cmds: make(map[string]*Command)}
}

// LoadFromDirs performs a fresh discovery pass against global + project
// dirs and replaces the current contents.  Errors from individual files
// are collected but do not abort the load.
func (r *Registry) LoadFromDirs(globalDir, projectDir string) []error {
	cmds, errs := LoadDirs(globalDir, projectDir)
	r.Update(cmds, errs)
	return errs
}

// Update atomically replaces the registry contents.  The order slice is
// rebuilt and sorted lexicographically by slash token so callers see a
// stable enumeration regardless of how TOML files were discovered.
func (r *Registry) Update(cmds []*Command, errs []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = make(map[string]*Command, len(cmds))
	r.order = make([]string, 0, len(cmds))
	for _, c := range cmds {
		r.cmds[c.Slash] = c
		r.order = append(r.order, c.Slash)
	}
	sort.Strings(r.order)
	r.rootErr = errs
}

// All returns a stable-ordered copy of every loaded command.
func (r *Registry) All() []*Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Command, 0, len(r.order))
	for _, s := range r.order {
		out = append(out, r.cmds[s])
	}
	return out
}

// Get returns a command by exact slash token.
func (r *Registry) Get(slash string) (*Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cmds[slash]
	return c, ok
}

// Count returns the number of loaded commands.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cmds)
}

// Errors returns the errors from the last load.
func (r *Registry) Errors() []error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]error, len(r.rootErr))
	copy(out, r.rootErr)
	return out
}

// Lookup classifies a raw user input string.  Returns the decision and,
// when relevant, the matching command(s).
func (r *Registry) Lookup(input string) (Decision, []*Command) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return DecisionUnknown, nil
	}
	head := input
	if i := strings.IndexAny(input, " \t"); i >= 0 {
		head = input[:i]
	}
	if IsBuiltin(head) {
		return DecisionBuiltin, nil
	}
	if c, ok := r.Get(head); ok {
		return DecisionExact, []*Command{c}
	}
	matches := r.PrefixMatch(head)
	switch len(matches) {
	case 0:
		return DecisionUnknown, nil
	case 1:
		return DecisionExact, matches
	default:
		return DecisionAmbiguous, matches
	}
}

// PrefixMatch returns all commands whose slash begins with prefix.
// Comparison is case-sensitive (matches gemini-cli behaviour).
func (r *Registry) PrefixMatch(prefix string) []*Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Command
	for _, s := range r.order {
		if strings.HasPrefix(s, prefix) {
			out = append(out, r.cmds[s])
		}
	}
	return out
}

// FuzzyMatch returns commands whose slash contains every rune of
// query in order (subsequence match, used by the picker modal).  An
// empty query matches everything.  Matched positions are returned so the
// picker can highlight the hits.
//
// Score favours (in order): exact prefix, in-order subsequence, shorter
// slash.  Ties are broken by lexicographic order.
type FuzzyHit struct {
	Cmd  *Command
	Pos  []int // indices into cmd.Slash
	Score int
}

// FuzzyMatch returns at most limit hits ordered by descending score.
func (r *Registry) FuzzyMatch(query string, limit int) []FuzzyHit {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hits := make([]FuzzyHit, 0, len(r.order))
	q := []rune(strings.TrimPrefix(query, "/"))
	for _, s := range r.order {
		c := r.cmds[s]
		pos, ok := fuzzyIndices(c.Slash, q)
		if !ok {
			continue
		}
		hits = append(hits, FuzzyHit{Cmd: c, Pos: pos, Score: scoreHit(c.Slash, q, pos)})
	}
	sortFuzzy(hits)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// fuzzyIndices returns the matched rune positions in haystack for query.
// Empty query matches everything with no positions highlighted.
func fuzzyIndices(haystack string, query []rune) ([]int, bool) {
	if len(query) == 0 {
		return nil, true
	}
	h := []rune(haystack)
	pos := make([]int, 0, len(query))
	qi := 0
	for hi := 0; hi < len(h) && qi < len(query); hi++ {
		if h[hi] == query[qi] {
			pos = append(pos, hi)
			qi++
		}
	}
	if qi < len(query) {
		return nil, false
	}
	return pos, true
}

// scoreHit computes a score: higher is better.
//
//	+100 if the slash starts with the query (after the leading "/")
//	+10  per matched character
//	-1   per unused character in the slash (favours shorter names)
func scoreHit(slash string, query []rune, pos []int) int {
	if len(query) == 0 {
		return 0
	}
	score := 10 * len(pos)
	if len(pos) > 0 && pos[0] == 1 { // slash is "/<query...>"
		// First rune is '/', query starts at rune index 1.
		score += 100
	}
	h := []rune(slash)
	score -= len(h) - len(pos)
	return score
}

func sortFuzzy(hits []FuzzyHit) {
	// Insertion sort is fine for typical sizes (tens of commands).
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && fuzzyLess(hits[j], hits[j-1]); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
}

func fuzzyLess(a, b FuzzyHit) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.Cmd.Slash < b.Cmd.Slash
}

// FilterByQuery is a convenience for the picker: FuzzyMatch(query, 0) but
// always returning all matches (limit = 0).  Used by the headless path
// so tests do not depend on the picker UI.
func (r *Registry) FilterByQuery(query string) []*Command {
	hits := r.FuzzyMatch(query, 0)
	out := make([]*Command, len(hits))
	for i, h := range hits {
		out[i] = h.Cmd
	}
	return out
}

// String returns a multi-line description suitable for /commands list.
func (r *Registry) String() string {
	var b strings.Builder
	for _, c := range r.All() {
		fmt.Fprintf(&b, "%-30s %s  [%s]\n", c.Slash, c.Description, c.Source)
	}
	return b.String()
}