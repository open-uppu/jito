package heartbeat

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// readLines slurps every line of path into a slice. Test helper.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	require.NoError(t, sc.Err())
	return out
}

// TestNew_TableDriven covers logDir resolution & mkdir behaviour.
func TestNew_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		logDir  string
		envHOME string
		wantErr bool
		check   func(t *testing.T, h *Heartbeat, tmp string)
	}{
		{
			name:    "explicit absolute dir",
			logDir:  "", // filled per-iteration
			wantErr: false,
		},
		{
			name:    "tilde expands to HOME",
			logDir:  "~/.jito-test-hb-1",
			envHOME: "", // set per-iteration
			wantErr: false,
			check: func(t *testing.T, h *Heartbeat, _ string) {
				wd, _ := os.Getwd()
				home := os.Getenv("HOME")
				assert.True(t, strings.HasPrefix(h.LogDir(), home),
					"LogDir=%q should start with HOME=%q (cwd=%q)", h.LogDir(), home, wd)
				assert.False(t, strings.Contains(h.LogDir(), "~"),
					"LogDir=%q should have ~ expanded", h.LogDir())
			},
		},
		{
			name:    "empty defaults to ~/.jito/heartbeat",
			logDir:  "",
			wantErr: false,
			check: func(t *testing.T, h *Heartbeat, _ string) {
				assert.True(t, strings.HasSuffix(h.LogDir(), ".jito/heartbeat"),
					"LogDir=%q", h.LogDir())
			},
		},
		{
			name:    "tilde without HOME errors",
			logDir:  "~/.jito-test-hb-2",
			envHOME: "__unset__",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			dir := tc.logDir
			if dir == "" || strings.HasPrefix(dir, "~") {
				if tc.envHOME == "__unset__" {
					t.Setenv("HOME", "")
				} else {
					t.Setenv("HOME", tmp)
					if dir != "" {
						dir = filepath.Join(tmp, dir[2:]) // simulate expand against HOME
						// but expandHome reads $HOME itself, so we keep the tilde form
						dir = tc.logDir
					} else {
						dir = "" // test default
					}
				}
			} else {
				dir = filepath.Join(tmp, "sub")
			}

			h, err := New(dir)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, h)
			t.Cleanup(func() { _ = h.Close() })

			// LogDir should exist on disk after New.
			st, statErr := os.Stat(h.LogDir())
			require.NoError(t, statErr)
			assert.True(t, st.IsDir())

			if tc.check != nil {
				tc.check(t, h, tmp)
			}
		})
	}
}

// TestBeatAt_LineFormat locks the wire format.
func TestBeatAt_LineFormat(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	at := time.Date(2026, 6, 28, 7, 9, 19, 123_000_000, time.UTC)
	require.NoError(t, h.BeatAt(at, "jito-test", "ALIVE", "tick 1"))

	lines := readLines(t, h.LogFile())
	require.Len(t, lines, 1)
	line := lines[0]

	// Format: "<RFC3339Nano-UTC> <TASKID> <STATE> <msg>"
	parts := strings.SplitN(line, " ", 4)
	require.Len(t, parts, 4)
	assert.Equal(t, "2026-06-28T07:09:19.123Z", parts[0], "RFC3339Nano UTC")
	assert.Equal(t, "JITO-TEST", parts[1], "taskID upper-cased")
	assert.Equal(t, "ALIVE", parts[2], "state upper-cased")
	assert.Equal(t, "tick 1", parts[3], "msg preserved")

	// Also confirm Beat() (no explicit time) writes a parseable line.
	require.NoError(t, h.Beat("jito-test", "DONE", "finalized"))
	lines = readLines(t, h.LogFile())
	require.Len(t, lines, 2)
	_, err = time.Parse(time.RFC3339Nano, strings.SplitN(lines[1], " ", 2)[0])
	assert.NoError(t, err, "Beat() must use RFC3339Nano: %q", lines[1])
}

// TestBeatAt_DayRotation ensures the log file rolls over when UTC day
// changes between two BeatAt calls.
func TestBeatAt_DayRotation(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	d1 := time.Date(2026, 6, 28, 23, 59, 59, 0, time.UTC)
	d2 := time.Date(2026, 6, 29, 0, 0, 1, 0, time.UTC)

	require.NoError(t, h.BeatAt(d1, "jito-test", "ALIVE", "end of day"))
	require.NoError(t, h.BeatAt(d2, "jito-test", "ALIVE", "new day"))

	fileA := filepath.Join(tmp, "2026-06-28.log")
	fileB := filepath.Join(tmp, "2026-06-29.log")

	assert.FileExists(t, fileA)
	assert.FileExists(t, fileB)

	aLines := readLines(t, fileA)
	bLines := readLines(t, fileB)
	require.Len(t, aLines, 1)
	require.Len(t, bLines, 1)
	assert.Contains(t, aLines[0], "end of day")
	assert.Contains(t, bLines[0], "new day")

	// LogFile() should now report the new day's file.
	assert.Equal(t, fileB, h.LogFile(), "LogFile() should track rotation")
}

// TestBeatAt_TrimNewlines stops user msg from injecting extra blank
// lines on the right side (we strip trailing newlines only — embedded
// newlines are preserved verbatim, since msg content is user-trusted).
func TestBeatAt_TrimNewlines(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	at := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	require.NoError(t, h.BeatAt(at, "jito-test", "ALIVE", "hello\n"))

	lines := readLines(t, h.LogFile())
	require.Len(t, lines, 1, "trailing newlines must collapse so we don't emit blank lines")
	assert.Contains(t, lines[0], "hello")
}

// TestBeatAt_Validation rejects empty taskID/state.
func TestBeatAt_Validation(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	at := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	assert.Error(t, h.BeatAt(at, "", "ALIVE", "x"))
	assert.Error(t, h.BeatAt(at, "  ", "ALIVE", "x"))
	assert.Error(t, h.BeatAt(at, "jito-test", "", "x"))
	assert.Error(t, h.BeatAt(at, "jito-test", "  ", "x"))
}

// TestBeatAt_WhitespaceStrips taskID/state.
func TestBeatAt_WhitespaceStrips(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	at := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	require.NoError(t, h.BeatAt(at, "  jito-test  ", "  alive  ", "msg"))
	lines := readLines(t, h.LogFile())
	require.Len(t, lines, 1)
	assert.Equal(t, "JITO-TEST ALIVE msg", strings.SplitN(lines[0], " ", 4)[1]+" "+strings.SplitN(lines[0], " ", 4)[2]+" "+strings.SplitN(lines[0], " ", 4)[3])
}

// TestExpandHome covers edge cases of the home-expansion helper.
func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"/abs/path", "/abs/path", false},
		{"relative/path", "relative/path", false},
		{"~", "/home/tester", false},
		{"~/foo", "/home/tester/foo", false},
		{"~"+string(os.PathSeparator) + "bar", "/home/tester/bar", false},
		{"~root/x", "~root/x", false}, // unsupported
	}

	for _, tc := range cases {
		got, err := expandHome(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input=%q", tc.in)
			continue
		}
		require.NoError(t, err, "input=%q", tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}

	// HOME unset + tilde → error.
	t.Setenv("HOME", "")
	_, err := expandHome("~/x")
	assert.Error(t, err)
}

// TestExpandHome_NoHomeNoTilde is the happy path when no expansion is needed.
func TestExpandHome_NoHomeNoTilde(t *testing.T) {
	t.Setenv("HOME", "")
	got, err := expandHome("/abs/path")
	require.NoError(t, err)
	assert.Equal(t, "/abs/path", got)
}

// TestConcurrent_AppendIsAtomic fires N goroutines that all append a
// unique line; the resulting file must contain all N intact lines
// (no torn writes).
func TestConcurrent_AppendIsAtomic(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	at := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			_ = h.BeatAt(at, "jito-test", "ALIVE", "msg-"+itoa(i))
		}(i)
	}
	wg.Wait()

	lines := readLines(t, h.LogFile())
	require.Len(t, lines, N, "all %d concurrent appends must land", N)
	seen := make(map[string]bool, N)
	for _, l := range lines {
		parts := strings.SplitN(l, " ", 4)
		require.Len(t, parts, 4)
		seen[parts[3]] = true
	}
	assert.Len(t, seen, N, "each msg must appear exactly once")
}

// TestSetClock verifies the injectable clock is honored by Beat().
func TestSetClock(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	fixed := time.Date(2030, 1, 2, 3, 4, 5, 6_000_000, time.UTC)
	prev := h.SetClock(func() time.Time { return fixed })
	t.Cleanup(func() { h.SetClock(prev) })

	require.NoError(t, h.Beat("jito-test", "ALIVE", "frozen"))
	lines := readLines(t, h.LogFile())
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "2030-01-02T03:04:05.006Z")
}

// TestWriteTo copies the active log to a buffer.
func TestWriteTo(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)

	at := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	require.NoError(t, h.BeatAt(at, "jito-test", "DONE", "ok"))

	var sb strings.Builder
	n, err := h.WriteTo(&sb)
	require.NoError(t, err)
	assert.Greater(t, n, int64(0))
	assert.Contains(t, sb.String(), "DONE ok")
}

// TestWriteTo_MissingFile errors gracefully (defensive).
func TestWriteTo_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)
	// Point at a non-existent file by mutating the field via closure.
	h.mu.Lock()
	h.logFile = filepath.Join(tmp, "nope.log")
	h.mu.Unlock()

	var sb strings.Builder
	_, err = h.WriteTo(&sb)
	assert.Error(t, err)
}

// TestClose_Noop ensures Close is safe to defer.
func TestClose_Noop(t *testing.T) {
	tmp := t.TempDir()
	h, err := New(tmp)
	require.NoError(t, err)
	assert.NoError(t, h.Close())
	assert.NoError(t, h.Close()) // idempotent
}

// Property: appending N records always yields exactly N lines, all
// parseable by RFC3339Nano and matching the expected task/state/msg.
func TestProperty_AppendCountIsExact(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 30).Draw(rt, "n")
		task := rapid.StringMatching(`[a-z][a-z0-9-]{0,7}`).Draw(rt, "task")
		state := rapid.SampledFrom([]string{"ALIVE", "DONE", "STARTED", "BLOCKED"}).Draw(rt, "state")
		msg := rapid.StringMatching(`[a-zA-Z0-9 ]{1,20}`).Draw(rt, "msg")

		tmp := t.TempDir()
		h, err := New(tmp)
		require.NoError(t, err)

		at := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
		for i := 0; i < n; i++ {
			require.NoError(t, h.BeatAt(at, task, state, msg))
		}
		lines := readLines(t, h.LogFile())
		require.Len(t, lines, n)
		for _, l := range lines {
			parts := strings.SplitN(l, " ", 4)
			require.Len(t, parts, 4)
			_, err := time.Parse(time.RFC3339Nano, parts[0])
			require.NoError(t, err)
			require.Equal(t, strings.ToUpper(task), parts[1])
			require.Equal(t, state, parts[2])
			require.Equal(t, strings.TrimRight(msg, "\n"), parts[3])
		}
	})
}

// itoa avoids pulling in strconv for one call site (keeps import block
// tight in this tiny test file).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}