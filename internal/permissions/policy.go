// Package permissions implements jito's mode-aware shell-command allowlist
// and approval-decision bookkeeping.  The policy answers one question:
//
//	Is this bash command allowed to run, given the active mode and the
//	user's optional override file?
//
// Defaults follow the jito-tui spec:
//
//	dev        → git, go, npm, pnpm, make, ls, cat, grep, find
//	audit      → ls, cat, grep, git diff, git log
//	reason     → ∅  (always require approval)
//	create     → ∅  (always require approval)
//	universal  → ∅  (always require approval)
//
// Override file: ~/.jito/permissions.yaml (YAML mapping mode → []string
// of additional allowed commands).  Empty list resets the mode's
// allowlist to defaults.  Unknown modes are allowed to fall through to
// the YAML-defined allowlist.
package permissions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Mode is the string name of the active jito mode (dev, audit, …).
type Mode string

const (
	ModeDev       Mode = "dev"
	ModeAudit     Mode = "audit"
	ModeReason    Mode = "reason"
	ModeCreate    Mode = "create"
	ModeUniversal Mode = "universal"
)

// Decision is the verdict returned by Policy.Check.
type Decision int

const (
	// DecisionAllow means the command is on the mode's allowlist and
	// may run without prompting.
	DecisionAllow Decision = iota
	// DecisionPrompt means the command is not on the allowlist; the TUI
	// must surface an approval modal before executing.
	DecisionPrompt
	// DecisionDeny means the command is permanently forbidden (e.g. an
	// empty mode allowlist with a "deny" override).  It must not run
	// even with user approval.
	DecisionDeny
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionPrompt:
		return "prompt"
	case DecisionDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// defaultAllowlists holds the per-mode defaults from the spec.
var defaultAllowlists = map[Mode][]string{
	ModeDev: {
		"git", "go", "npm", "pnpm", "make",
		"ls", "cat", "grep", "find",
	},
	ModeAudit: {
		"ls", "cat", "grep",
		"git diff", "git log",
	},
	ModeReason:    {},
	ModeCreate:    {},
	ModeUniversal: {},
}

// DefaultAllowlist returns a copy of the built-in allowlist for mode.  An
// unknown mode returns an empty allowlist (always require approval).
func DefaultAllowlist(mode Mode) []string {
	src, ok := defaultAllowlists[mode]
	if !ok {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// Policy encapsulates the active allowlists and a per-mode override map
// loaded from disk.  It is safe for concurrent reads; writes go through
// LoadOverride.
type Policy struct {
	mu        sync.RWMutex
	overrides map[Mode][]string // ad-hoc additions; nil = use default
	denyList  map[Mode][]string // commands that are always forbidden
}

// NewPolicy returns a Policy with no overrides loaded.
func NewPolicy() *Policy {
	return &Policy{
		overrides: make(map[Mode][]string),
		denyList:  make(map[Mode][]string),
	}
}

// LoadOverride reads a YAML file with the shape:
//
//	modes:
//	  dev:    [git, go]
//	  audit:  []
//
// and merges its entries with the built-in defaults.  A nil or empty
// list removes the built-in defaults for that mode (effectively forcing
// approval).  An unknown mode name is accepted verbatim so users can
// pre-register custom modes.
func (p *Policy) LoadOverride(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // missing override file is fine
		}
		return fmt.Errorf("read override %s: %w", path, err)
	}
	return p.LoadOverrideYAML(data)
}

// LoadOverrideYAML parses YAML bytes (see LoadOverride for schema).
func (p *Policy) LoadOverrideYAML(data []byte) error {
	var doc struct {
		Modes map[string][]string `yaml:"modes"`
		Deny  map[string][]string `yaml:"deny"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse override yaml: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if doc.Modes != nil {
		for k, v := range doc.Modes {
			m := Mode(strings.ToLower(k))
			// nil signals "force approval for this mode".
			if v == nil {
				p.overrides[m] = []string{}
			} else {
				cp := make([]string, len(v))
				copy(cp, v)
				p.overrides[m] = cp
			}
		}
	}
	if doc.Deny != nil {
		for k, v := range doc.Deny {
			m := Mode(strings.ToLower(k))
			cp := make([]string, len(v))
			copy(cp, v)
			p.denyList[m] = cp
		}
	}
	return nil
}

// allowlist returns the effective allowlist for mode (default + override).
func (p *Policy) allowlist(mode Mode) []string {
	if ovr, ok := p.overrides[mode]; ok {
		// Override fully replaces defaults: this matches the spec's
		// "override file" wording.
		out := make([]string, len(ovr))
		copy(out, ovr)
		return out
	}
	return DefaultAllowlist(mode)
}

// Check inspects a single bash command and returns the decision.  The
// command is the raw shell string the user (or LLM) wants to run.
//
// Decision order (first match wins):
//
//  1. Empty command     → DecisionDeny
//  2. Network command   → DecisionDeny (LOOP #3)
//  3. Dangerous command → DecisionDeny (LOOP #3, e.g. sudo, eval,
//     curl|sh, $(...), | bash)
//  4. Deny list         → DecisionDeny
//  5. Allow list match  → DecisionAllow
//  6. Empty allowlist   → DecisionPrompt
//  7. Default           → DecisionPrompt
func (p *Policy) Check(mode Mode, command string) Decision {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return DecisionDeny
	}
	// Hard-deny network egress and dangerous commands regardless of
	// mode or override file (LOOP #3 hardening).
	if HasNetworkCommand(cmd) {
		return DecisionDeny
	}
	if IsDangerousCommand(cmd) {
		return DecisionDeny
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	// Deny list wins next.
	if denied, ok := p.denyList[mode]; ok {
		for _, d := range denied {
			if matches(cmd, d) {
				return DecisionDeny
			}
		}
	}
	list := p.allowlist(mode)
	if len(list) == 0 {
		return DecisionPrompt
	}
	for _, allowed := range list {
		if matches(cmd, allowed) {
			return DecisionAllow
		}
	}
	return DecisionPrompt
}

// Allowlist returns a copy of the effective allowlist for mode.  Useful
// for the approval modal which wants to display "the following commands
// are pre-approved: …".
func (p *Policy) Allowlist(mode Mode) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.allowlist(mode)
}

// AllowedFor returns the list of commands pre-approved for mode.  If the
// override replaced the defaults, this is the override; otherwise it is
// the built-in defaults.
func (p *Policy) AllowedFor(mode Mode) []string { return p.Allowlist(mode) }

// matches reports whether cmd starts with prefix.  prefix may be a
// multi-word token such as "git diff"; the match is on the leading
// token sequence of cmd (whitespace-trimmed).  This keeps both
// "git diff HEAD" and "git diff" matching the "git diff" allowlist entry.
func matches(cmd, prefix string) bool {
	cmd = strings.TrimSpace(cmd)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	if strings.HasPrefix(cmd, prefix) {
		// Either exact match or the next character is whitespace.
		if len(cmd) == len(prefix) {
			return true
		}
		c := cmd[len(prefix)]
		if c == ' ' || c == '\t' {
			return true
		}
	}
	return false
}

// DefaultOverridePath returns ~/.jito/permissions.yaml.  Empty when $HOME
// cannot be resolved.
func DefaultOverridePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jito", "permissions.yaml")
}