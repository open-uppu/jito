// Package telemetry implements opt-in usage tracking.
package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is a single telemetry event.
type Event struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Mode      string                 `json:"mode,omitempty"`
	Provider  string                 `json:"provider,omitempty"`
	Duration  int64                  `json:"duration_ms,omitempty"`
	Tokens    int                    `json:"tokens,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

// Tracker writes events to a local NDJSON file.
type Tracker struct {
	enabled bool
	path    string
	mu      sync.Mutex
	file    *os.File
}

// New creates a tracker. Disabled if JITO_TELEMETRY=0.
func New() *Tracker {
	t := &Tracker{}
	if os.Getenv("JITO_TELEMETRY") == "0" {
		return t
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return t
	}
	dir := filepath.Join(home, ".jito", "telemetry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return t
	}
	path := filepath.Join(dir, fmt.Sprintf("events-%s.ndjson", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return t
	}
	t.enabled = true
	t.path = path
	t.file = f
	return t
}

// Track writes an event.
func (t *Tracker) Track(e Event) {
	if !t.enabled || t.file == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	data, _ := json.Marshal(e)
	data = append(data, '\n')
	t.file.Write(data)
}

// TrackCall is a helper for chat/stream calls.
func (t *Tracker) TrackCall(eventType, mode, provider string, duration time.Duration, err error) {
	e := Event{
		Type:      eventType,
		Mode:      mode,
		Provider:  provider,
		Duration:  duration.Milliseconds(),
		Timestamp: time.Now(),
	}
	if err != nil {
		e.Error = err.Error()
	}
	t.Track(e)
}

// Enabled returns whether telemetry is active.
func (t *Tracker) Enabled() bool { return t.enabled }

// Path returns the log file path.
func (t *Tracker) Path() string { return t.path }

// Close flushes and closes the log file.
func (t *Tracker) Close() error {
	if !t.enabled || t.file == nil {
		return nil
	}
	return t.file.Close()
}