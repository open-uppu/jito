// Package heartbeat provides a lightweight periodic status logger used
// by sub-agents (e.g. jito-test) to satisfy the "every N minutes,
// append a one-line status to a run-log" mandate.
//
// Design goals:
//   - Race-safe (sync.Mutex around appends).
//   - Tolerant to missing log dir (auto-create).
//   - Tolerant to a leading "~" in logDir (expand to $HOME).
//   - No external deps — pure stdlib.
//   - Append-only file format: "<RFC3339> <taskID> <state> <msg>".
package heartbeat

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultLogDir is the conventional location for sub-agent heartbeats.
// It is relative to $HOME so each user/host has its own log.
const DefaultLogDir = "~/.jito/heartbeat"

// Heartbeat is a thread-safe append-only status logger. Once created
// with New, methods may be called from any goroutine.
type Heartbeat struct {
	mu      sync.Mutex
	logDir  string
	logFile string // resolved absolute path
	now     func() time.Time // injectable clock for tests
}

// New constructs a Heartbeat rooted at logDir. If logDir is empty, it
// defaults to DefaultLogDir (~/.jito/heartbeat). A leading "~" is
// expanded against $HOME. The directory is created if missing.
//
// The returned Heartbeat writes to "<logDir>/<YYYY-MM-DD>.log" — one
// file per UTC day.
func New(logDir string) (*Heartbeat, error) {
	if logDir == "" {
		logDir = DefaultLogDir
	}
	resolved, err := expandHome(logDir)
	if err != nil {
		return nil, fmt.Errorf("heartbeat: resolve logDir: %w", err)
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return nil, fmt.Errorf("heartbeat: mkdir %s: %w", resolved, err)
	}
	return &Heartbeat{
		logDir:  resolved,
		logFile: filepath.Join(resolved, time.Now().UTC().Format("2006-01-02")+".log"),
		now:     time.Now,
	}, nil
}

// LogDir returns the resolved absolute log directory.
func (h *Heartbeat) LogDir() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.logDir
}

// LogFile returns the absolute path of the active daily log file.
func (h *Heartbeat) LogFile() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.logFile
}

// Beat writes a one-line status record using time.Now() as the
// timestamp. It is a thin wrapper around BeatAt(h.now(), ...).
func (h *Heartbeat) Beat(taskID, state, msg string) error {
	h.mu.Lock()
	clock := h.now
	h.mu.Unlock()
	return h.BeatAt(clock(), taskID, state, msg)
}

// BeatAt writes a one-line status record stamped with t. The line
// format is:
//
//	<RFC3339Nano-UTC> <taskID> <STATE> <msg>\n
//
// Returns an error only if the append fails (e.g. disk full, perms).
// taskID and state are upper-cased and trimmed; spaces in msg are
// preserved verbatim (no quoting needed because the format is
// space-separated by position).
func (h *Heartbeat) BeatAt(t time.Time, taskID, state, msg string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	taskID = strings.TrimSpace(taskID)
	state = strings.TrimSpace(state)
	msg = strings.TrimRight(msg, "\n")

	if taskID == "" {
		return errors.New("heartbeat: taskID required")
	}
	if state == "" {
		return errors.New("heartbeat: state required")
	}

	// Rotate file if the UTC day rolled over since construction.
	daily := filepath.Join(h.logDir, t.UTC().Format("2006-01-02")+".log")
	if daily != h.logFile {
		h.logFile = daily
	}

	line := fmt.Sprintf("%s %s %s %s\n",
		t.UTC().Format(time.RFC3339Nano),
		strings.ToUpper(taskID),
		strings.ToUpper(state),
		msg,
	)

	f, err := os.OpenFile(h.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("heartbeat: open %s: %w", h.logFile, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("heartbeat: write: %w", err)
	}
	return nil
}

// Close is a no-op kept for symmetry with the daemon-style consumer in
// internal/cli/heartbeat.go. Provided so callers can `defer h.Close()`.
func (h *Heartbeat) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return nil
}

// SetClock overrides the clock — used only by tests. Returns the
// previous clock function so a test can restore it.
func (h *Heartbeat) SetClock(now func() time.Time) func() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.now
	h.now = now
	return prev
}

// WriteTo copies the active daily log to w. Intended for diagnostics;
// not used in the hot path.
func (h *Heartbeat) WriteTo(w io.Writer) (int64, error) {
	h.mu.Lock()
	path := h.logFile
	h.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(w, f)
}

// expandHome resolves a leading "~" or "~/" in path against $HOME.
// Returns the original path if $HOME is unset and there is no "~".
func expandHome(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path[0] != '~' {
		return path, nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("HOME not set; cannot expand ~ in logDir")
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
		return filepath.Join(home, path[2:]), nil
	}
	// "~user/..." is intentionally not supported — too platform-specific.
	return path, nil
}