// Package loop wires jito's spawn sub-system into CEO-Profile's
// Loop Engineering layer.
//
// The Loop Engineering layer (owner: CEO-Profile, parent:
// agent:main:dashboard:c52dba07-51a0-44a7-812c-61bf51113d09)
// maintains two durable artifacts under
// ~/.openclaw/workspace/state/loop-engineering/:
//
//   - STATE.md      — last-write-wins scope/budget table
//   - run-log-YYYY-MM-DD.md — append-only status feed
//
// This package exposes a tiny, dependency-free reader/writer for those
// files so that:
//
//   1. internal/agent/spawn.go can announce every spawn as a heartbeat
//      entry (STARTED / DONE / FAILED / BLOCKED) without coupling to
//      the loop-engineering directory layout.
//   2. `jito loop status|run-log|state` can render the same files for
//      humans.
//
// The strict run-log format (CEO directive 2026-06-28) is:
//
//	^HH:MM:SS GMT\+7 \| LOOP#\d+ \| .+ \| (STARTED|READING|PLANNING|EXECUTING .+|BLOCKED .+|DONE .+|STALLED|ABORTED .+) \| .+$
//
// All appends that don't match the regex return ErrFormatViolation so
// the spawn pipeline aborts rather than silently writes garbage.
package loop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultStateDir is the conventional location for loop-engineering
// state. Overridable per-call via Config.StateDir.
const DefaultStateDir = "~/.openclaw/workspace/state/loop-engineering"

// ErrFormatViolation is returned when an entry would not match the
// strict heartbeat regex. Per the CEO directive 2026-06-28, broken
// format must abort the operation, not silently coerce.
var ErrFormatViolation = errors.New("loop: heartbeat entry violates strict format")

// ErrStateMissing is returned when STATE.md is absent (the loop
// engineering layer has not been initialised).
var ErrStateMissing = errors.New("loop: STATE.md not found")

// ErrRunLogMissing is returned when the expected run-log file is
// absent for the requested date.
var ErrRunLogMissing = errors.New("loop: run-log file not found")

// Entry is a single heartbeat line. It is what Append receives and
// what ReadRunLog returns.
type Entry struct {
	Timestamp time.Time // local time (GMT+7 per the loop-engineering mandate)
	Loop      string    // e.g. "LOOP#4"
	TaskID    string    // e.g. "jito-rel-LOOP4"
	Status    string    // one of Status* constants
	Detail    string    // free-form detail
}

// Status constants — must match the regex alternation verbatim.
const (
	StatusStarted   = "STARTED"
	StatusReading   = "READING"
	StatusPlanning  = "PLANNING"
	StatusExecuting = "EXECUTING" // may be followed by free-form detail suffix
	StatusBlocked   = "BLOCKED"
	StatusDone      = "DONE"
	StatusStalled   = "STALLED"
	StatusAborted   = "ABORTED"
)

// validStatuses is the strict whitelist for the status field.
var validStatuses = map[string]struct{}{
	StatusStarted: {}, StatusReading: {}, StatusPlanning: {},
	StatusExecuting: {}, StatusBlocked: {}, StatusDone: {},
	StatusStalled: {}, StatusAborted: {},
}

// entryFormat is the canonical stringification. Rendered, it must
// pass StrictRegex. We format the timestamp as HH:MM:SS GMT+7 to
// match the existing entries in run-log-2026-06-28.md.
const entryFormat = "%s GMT+7 | %s | %s | %s | %s"

// StrictRegex is the canonical regex the run-log must obey. It is
// exported so that test code in `loop_test.go` and external callers
// can validate entries without re-implementing the parser.
//
//	^HH:MM:SS GMT\+7 \| LOOP#\d+ \| .+ \| (STARTED|READING|PLANNING|EXECUTING .+|BLOCKED .+|DONE .+|STALLED|ABORTED .+) \| .+$
var StrictRegex = regexp.MustCompile(
	`^(\d{2}:\d{2}:\d{2}) GMT\+7 \| (LOOP#\d+) \| (.+?) \| ((?:STARTED|READING|PLANNING|EXECUTING .+|BLOCKED .+|DONE .+|STALLED|ABORTED .+)) \| (.+)$`,
)

// Config controls where the loop engine reads/writes state. Zero
// value is usable: it falls back to DefaultStateDir.
type Config struct {
	// StateDir is the loop-engineering root (parent of STATE.md and
	// the run-log-*.md files). May use "~".
	StateDir string

	// Now is injectable for tests. Defaults to time.Now.
	Now func() time.Time

	// Loc is the timezone used for the rendered timestamp prefix.
	// Defaults to Asia/Bangkok (GMT+7).
	Loc *time.Location
}

// Engine is the loop-engineering wire-up. Cheap to construct; safe
// for concurrent use.
type Engine struct {
	mu       sync.Mutex
	cfg      Config
	stateDir string
	now      func() time.Time
	loc      *time.Location
}

// New constructs an Engine. A leading "~" in cfg.StateDir is expanded
// against $HOME. The state directory is NOT auto-created — a missing
// dir returns an error on the first read/append call so that we never
// silently write to a wrong location.
func New(cfg Config) (*Engine, error) {
	dir := cfg.StateDir
	if dir == "" {
		dir = DefaultStateDir
	}
	resolved, err := expandHome(dir)
	if err != nil {
		return nil, fmt.Errorf("loop: resolve state dir: %w", err)
	}
	if resolved == "" {
		return nil, fmt.Errorf("loop: empty state dir after expansion")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	loc := cfg.Loc
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Bangkok")
		if loc == nil {
			loc = time.FixedZone("GMT+7", 7*60*60)
		}
	}

	return &Engine{
		cfg:      cfg,
		stateDir: resolved,
		now:      now,
		loc:      loc,
	}, nil
}

// StateDir returns the resolved absolute state directory.
func (e *Engine) StateDir() string {
	return e.stateDir
}

// StateFile returns the absolute path of STATE.md.
func (e *Engine) StateFile() string {
	return filepath.Join(e.stateDir, "STATE.md")
}

// RunLogFile returns the absolute path of the run-log for the given
// UTC date (or the current UTC date when t is zero).
func (e *Engine) RunLogFile(t time.Time) string {
	if t.IsZero() {
		t = e.now().UTC()
	}
	return filepath.Join(e.stateDir, "run-log-"+t.UTC().Format("2006-01-02")+".md")
}

// Append writes one entry to today's run-log. It validates the line
// against StrictRegex before opening the file; a violating entry
// returns ErrFormatViolation without touching disk.
//
// Each line ends with "\n". The file is opened in append mode and
// created if missing (consistent with the existing run-log files).
func (e *Engine) Append(entry Entry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.validate(entry); err != nil {
		return err
	}

	line := e.format(entry)
	if !StrictRegex.MatchString(line) {
		// belt-and-braces: format() + validate() should already
		// guarantee this, but per the CEO directive we re-check.
		return fmt.Errorf("%w: rendered line %q failed regex", ErrFormatViolation, line)
	}

	path := e.RunLogFile(entry.Timestamp)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("loop: mkdir %s: %w", filepath.Dir(path), err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("loop: open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("loop: write: %w", err)
	}
	return nil
}

// AppendRaw writes a fully-formatted line to the run-log. It is a
// passthrough used by code paths that have already composed the line
// (e.g. when re-emitting a pre-existing heartbeat from STATE.md).
func (e *Engine) AppendRaw(line string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !StrictRegex.MatchString(line) {
		return fmt.Errorf("%w: %q", ErrFormatViolation, line)
	}
	path := e.RunLogFile(e.now())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("loop: mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("loop: open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("loop: write: %w", err)
	}
	return nil
}

// ReadState returns the raw contents of STATE.md.
func (e *Engine) ReadState() ([]byte, error) {
	data, err := os.ReadFile(e.StateFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrStateMissing
	}
	if err != nil {
		return nil, fmt.Errorf("loop: read STATE.md: %w", err)
	}
	return data, nil
}

// ReadRunLog returns every validated entry of the run-log for the
// given UTC date (or today when t is zero). Lines that fail the regex
// are returned in the second return slice so operators can repair
// them, but are not silently dropped.
func (e *Engine) ReadRunLog(t time.Time) (entries []Entry, invalid []string, err error) {
	path := e.RunLogFile(t)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrRunLogMissing
	}
	if err != nil {
		return nil, nil, fmt.Errorf("loop: read %s: %w", path, err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		entry, ok := parseLine(line)
		if !ok {
			invalid = append(invalid, line)
			continue
		}
		entries = append(entries, entry)
	}
	return entries, invalid, nil
}

// Format renders an entry as the canonical run-log line (no newline).
// Exposed so spawn.go can compose-then-validate in one place.
func (e *Engine) Format(entry Entry) string {
	return e.format(entry)
}

// Validate checks an Entry for strict-format compliance without
// touching disk.
func (e *Engine) Validate(entry Entry) error {
	return e.validate(entry)
}

func (e *Engine) format(entry Entry) string {
	ts := entry.Timestamp.In(e.loc)
	if entry.Timestamp.IsZero() {
		ts = e.now().In(e.loc)
	}
	return fmt.Sprintf(entryFormat,
		ts.Format("15:04:05"),
		entry.Loop,
		entry.TaskID,
		entry.Status,
		strings.TrimSpace(entry.Detail),
	)
}

func (e *Engine) validate(entry Entry) error {
	if !StrictRegex.MatchString(e.format(entry)) {
		return fmt.Errorf("%w: %+v", ErrFormatViolation, entry)
	}
	return nil
}

// parseLine is the inverse of format(). It returns false if the line
// does not match StrictRegex.
func parseLine(line string) (Entry, bool) {
	m := StrictRegex.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false
	}
	ts, _ := time.Parse("15:04:05", m[1])
	return Entry{
		Timestamp: ts,
		Loop:      m[2],
		TaskID:    m[3],
		Status:    m[4],
		Detail:    m[5],
	}, true
}

// IsValidStatus reports whether s is in the strict whitelist (with the
// optional free-form suffix that the regex allows for EXECUTING /
// BLOCKED / DONE / ABORTED). A bare suffix (e.g. "DONE ") is rejected
// so that a downstream Append cannot smuggle in an uninformative
// status line.
func IsValidStatus(s string) bool {
	if _, ok := validStatuses[s]; ok {
		return true
	}
	// Allow "<STATUS> <non-blank suffix>" for the four states that
	// permit it. The trailing runes must contain at least one
	// non-whitespace character.
	for _, prefix := range []string{StatusExecuting, StatusBlocked, StatusDone, StatusAborted} {
		rest, ok := strings.CutPrefix(s, prefix+" ")
		if !ok {
			continue
		}
		if strings.TrimSpace(rest) == "" {
			continue
		}
		return true
	}
	return false
}

// expandHome is a minimal $HOME tilde expander — duplicated rather
// than imported so this package has zero external deps (matches the
// constraint that loop.go must be importable from internal/agent
// without dragging in cobra).
func expandHome(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}
