package session

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uppu/jito/internal/store"
)

// AuditDecision is the verdict recorded in the audit log.
type AuditDecision string

const (
	DecisionAllow  AuditDecision = "allow"
	DecisionDeny   AuditDecision = "deny"
	DecisionPrompt AuditDecision = "prompt"
	DecisionError  AuditDecision = "error"
)

// AuditEvent is a single record of a shell-tool decision.  ArgsRaw is
// the untrusted user-supplied command (or env value); the audit
// pipeline is responsible for redacting it before persistence.
type AuditEvent struct {
	Timestamp time.Time
	SessionID string
	Command   string        // canonical, post-parse command string
	Decision  AuditDecision // allow | deny | prompt | error
	Reason    string        // human-readable explanation
	ArgsRaw   string        // raw args (will be redacted before storage)
	Env       []string      // environment (will be scrubbed before storage)
}

// AuditLogger is the interface implemented by both in-memory and
// SQLite-backed audit sinks.  It is intentionally tiny so the bash
// sandbox (internal/tools) and the CLI layer can wire whichever
// implementation is most appropriate.
type AuditLogger interface {
	Record(ev AuditEvent) error
	// Recent returns the last N events (for testing / `jito audit`
	// subcommand).  limit ≤ 0 returns all.
	Recent(limit int) []AuditEvent
}

// --- Redaction ------------------------------------------------------

// envSecretPatterns match environment-variable names whose value
// should always be scrubbed, regardless of the value's shape.
var envSecretNames = []*regexp.Regexp{
	regexp.MustCompile(`(?i).*KEY.*`),
	regexp.MustCompile(`(?i).*TOKEN.*`),
	regexp.MustCompile(`(?i).*SECRET.*`),
	regexp.MustCompile(`(?i).*PASS.*`),
	regexp.MustCompile(`(?i).*CREDENTIAL.*`),
}

// envForbiddenNames match env-var names that must be removed
// outright before exec — they hijack the dynamic linker / shell.
var envForbiddenNames = []string{
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

// RedactArgs masks secrets in raw command-line text.  The original
// argument shape (whitespace, quoting) is preserved — only the
// sensitive substring is replaced by ***REDACTED***.
func RedactArgs(s string) string {
	if s == "" {
		return s
	}
	// Patterns with capture groups that should be KEPT in the output
	// (e.g. "api_key=…", "-p foo", "AWS_SECRET_ACCESS_KEY=…").  The
	// redacted form is "$1=***REDACTED***" so the variable name /
	// flag remains readable for forensic purposes.
	patterns := []*regexp.Regexp{
		// KEY=VALUE or KEY: VALUE (no bare space separator so we do
		// not catch `passwd safe` in `ln -s /etc/passwd safe`).
		regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|pwd|access[_-]?key|auth[_-]?token)\b\s*[:=]\s*["']?([A-Za-z0-9_\-\./+=]{4,})["']?`),
		regexp.MustCompile(`(?i)(?:^|[\s])(-p|--password[= ]|--pass[= ])([^\s"']+)`),
		regexp.MustCompile(`(?i)(AWS_SECRET_ACCESS_KEY)\s*=\s*["']?([A-Za-z0-9/+=]{20,})`),
		regexp.MustCompile(`(?i)(GH_TOKEN)\s*=\s*["']?([A-Za-z0-9_]{20,})`),
		regexp.MustCompile(`(?i)(Authorization:\s*Bearer)\s+[A-Za-z0-9._\-]{10,}`),
	}
	for _, re := range patterns {
		s = re.ReplaceAllString(s, "$1=***REDACTED***")
	}
	// Patterns whose whole match must be collapsed to a single
	// ***REDACTED*** (the matched text itself is the secret).
	collapsePatterns := []*regexp.Regexp{
		regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	}
	for _, re := range collapsePatterns {
		s = re.ReplaceAllString(s, "***REDACTED***")
	}
	// Also redact env=value pairs that look credential-like.
	s = redactEnvPairs(s)
	return s
}

// redactEnvPairs is a tiny helper that splits on whitespace, scans
// each token for KEY=VALUE with a secret-bearing name, and masks the
// value.
func redactEnvPairs(s string) string {
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return s
	}
	for i, tok := range tokens {
		idx := strings.IndexByte(tok, '=')
		if idx <= 0 {
			continue
		}
		name := tok[:idx]
		for _, re := range envSecretNames {
			if re.MatchString(name) {
				tokens[i] = name + "=***REDACTED***"
				break
			}
		}
	}
	return strings.Join(tokens, " ")
}

// IsEnvSecretName returns true if env-var name should be scrubbed.
func IsEnvSecretName(name string) bool {
	for _, re := range envSecretNames {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// IsEnvForbidden returns true if env-var name is forbidden (must be
// removed before exec).
func IsEnvForbidden(name string) bool {
	for _, f := range envForbiddenNames {
		if strings.EqualFold(name, f) {
			return true
		}
	}
	return false
}

// ScrubEnv removes secret-bearing and forbidden env-var entries.  A
// new slice is returned; the input slice is not mutated.  When drop is
// true the value is replaced by an empty string (preserves presence
// for shells that require a var to be set); when false the entry is
// removed entirely.
func ScrubEnv(env []string, drop bool) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			out = append(out, kv)
			continue
		}
		name := kv[:idx]
		val := kv[idx+1:]
		if IsEnvForbidden(name) {
			continue // always drop
		}
		if IsEnvSecretName(name) {
			if drop {
				out = append(out, name+"=")
			}
			continue
		}
		out = append(out, name+"="+val)
	}
	return out
}

// --- In-memory logger ----------------------------------------------

// MemoryLogger is an in-process AuditLogger.  It is used by tests and
// by short-lived CLI invocations that don't need durability.
type MemoryLogger struct {
	mu     sync.Mutex
	events []AuditEvent
}

// NewMemoryLogger returns an empty MemoryLogger.
func NewMemoryLogger() *MemoryLogger { return &MemoryLogger{} }

// Record appends ev to the in-memory log.  The args are redacted
// before storage so even the in-memory copy cannot leak secrets.
func (m *MemoryLogger) Record(ev AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	ev.ArgsRaw = RedactArgs(ev.ArgsRaw)
	ev.Command = RedactArgs(ev.Command)
	ev.Env = redactEnvSlice(ev.Env)
	m.events = append(m.events, ev)
	return nil
}

// Recent returns a copy of the last `limit` events.  limit <= 0
// returns all events.
func (m *MemoryLogger) Recent(limit int) []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	start := len(m.events) - limit
	out := make([]AuditEvent, limit)
	copy(out, m.events[start:])
	return out
}

// --- SQLite-backed logger ------------------------------------------

// StoreLogger persists events to a *store.Store.  Thread-safe via the
// store's internal mutex; no additional locking needed.
type StoreLogger struct {
	st *store.Store
}

// NewStoreLogger wraps an already-open *store.Store.
func NewStoreLogger(st *store.Store) *StoreLogger {
	return &StoreLogger{st: st}
}

// Record redacts args and writes one audit_log row.
func (l *StoreLogger) Record(ev AuditEvent) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	argsRedacted := redactEnvSlice(ev.Env)
	argsRedacted = append(argsRedacted, RedactArgs(ev.ArgsRaw))
	argsRedacted = append(argsRedacted, RedactArgs(ev.Command))
	return l.st.AppendAudit(store.AuditEntry{
		Timestamp:    ev.Timestamp,
		SessionID:    ev.SessionID,
		Command:      RedactArgs(ev.Command),
		Decision:     string(ev.Decision),
		Reason:       ev.Reason,
		ArgsRedacted: strings.Join(argsRedacted, " | "),
	})
}

// Recent fetches the last `limit` entries from the SQLite audit log.
func (l *StoreLogger) Recent(limit int) []AuditEvent {
	if limit <= 0 {
		limit = 100
	}
	entries, err := l.st.ListAudit("", limit)
	if err != nil {
		return nil
	}
	out := make([]AuditEvent, 0, len(entries))
	for _, e := range entries {
		out = append(out, AuditEvent{
			Timestamp: e.Timestamp,
			SessionID: e.SessionID,
			Command:   e.Command,
			Decision:  AuditDecision(e.Decision),
			Reason:    e.Reason,
			ArgsRaw:   e.ArgsRedacted,
		})
	}
	return out
}

// --- helpers -------------------------------------------------------

func redactEnvSlice(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			out = append(out, kv)
			continue
		}
		name := kv[:idx]
		val := kv[idx+1:]
		if IsEnvForbidden(name) {
			out = append(out, name+"=[FORBIDDEN]")
			continue
		}
		if IsEnvSecretName(name) {
			out = append(out, name+"=***REDACTED***")
			continue
		}
		out = append(out, name+"="+val)
	}
	return out
}

// FormatEvent renders an AuditEvent as a single human-readable line,
// safe to print to stdout (args already redacted).
func FormatEvent(ev AuditEvent) string {
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return fmt.Sprintf("%s | %s | session=%s | %s | reason=%s | args=%s",
		ts.Format(time.RFC3339),
		ev.Decision,
		ev.SessionID,
		ev.Command,
		ev.Reason,
		ev.ArgsRaw,
	)
}