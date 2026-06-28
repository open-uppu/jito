package loop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime returns a clock that always returns t.
func fixedTime(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestNew_DefaultStateDir(t *testing.T) {
	e, err := New(Config{})
	require.NoError(t, err)
	require.NotNil(t, e)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".openclaw/workspace/state/loop-engineering"), e.StateDir())
	assert.NotEmpty(t, e.StateFile())
}

func TestNew_TildeExpansion(t *testing.T) {
	e, err := New(Config{StateDir: "~/loop-test-state"})
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "loop-test-state"), e.StateDir())
}

func TestNew_AbsolutePath(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	assert.Equal(t, tmp, e.StateDir())
}

func TestNew_EmptyStateDirAfterExpansion(t *testing.T) {
	t.Setenv("HOME", "")
	// When HOME is unset and path is "~", UserHomeDir errors.
	// Test the path where we strip tilde but leave empty.
	_, err := New(Config{StateDir: "~"})
	// Either succeeds (env has a fallback) or errors; both are acceptable.
	if err != nil {
		assert.Contains(t, err.Error(), "HOME")
	}
}

func TestNew_RelativePath(t *testing.T) {
	// Relative paths should be returned as-is (no resolution).
	e, err := New(Config{StateDir: "relative/dir"})
	require.NoError(t, err)
	assert.Equal(t, "relative/dir", e.StateDir())
}

func TestEngine_RunLogFile_Today(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	now := time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC)
	e.now = fixedTime(now)
	got := e.RunLogFile(time.Time{})
	assert.Equal(t, filepath.Join(tmp, "run-log-2026-06-28.md"), got)
}

func TestEngine_RunLogFile_ExplicitDate(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	when := time.Date(2026, 1, 5, 7, 0, 0, 0, time.UTC)
	assert.Equal(t, filepath.Join(tmp, "run-log-2026-01-05.md"), e.RunLogFile(when))
}

func TestEngine_Format(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	e.now = fixedTime(time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC))

	entry := Entry{
		Loop:   "LOOP#4",
		TaskID: "jito-rel-LOOP4",
		Status: "STARTED",
		Detail: "first line",
	}
	got := e.Format(entry)
	assert.Equal(t, "20:45:00 GMT+7 | LOOP#4 | jito-rel-LOOP4 | STARTED | first line", got)
	assert.True(t, StrictRegex.MatchString(got))
}

func TestEngine_Format_ZeroTimestamp_UsesNow(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 7, 30, 0, 0, time.UTC))

	got := e.Format(Entry{Loop: "LOOP#1", TaskID: "x", Status: "DONE", Detail: "ok"})
	assert.Regexp(t, `^14:30:00 GMT\+7 \| LOOP#1 \| x \| DONE \| ok$`, got)
}

func TestEngine_Format_CustomLoc(t *testing.T) {
	tmp := t.TempDir()
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	e, err := New(Config{StateDir: tmp, Loc: ny})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)) // NY is UTC-4

	got := e.Format(Entry{Loop: "LOOP#1", TaskID: "x", Status: "DONE", Detail: "ok"})
	assert.Contains(t, got, "GMT", "should print a timezone suffix")
}

func TestEngine_Validate_OK(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC))

	cases := []Entry{
		{Loop: "LOOP#4", TaskID: "x", Status: "STARTED", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "EXECUTING step 1", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "DONE all good", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "BLOCKED on docker", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "ABORTED timeout", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "STALLED", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "READING", Detail: "d"},
		{Loop: "LOOP#4", TaskID: "x", Status: "PLANNING", Detail: "d"},
	}
	for i, c := range cases {
		require.NoErrorf(t, e.Validate(c), "case %d: %+v", i, c)
	}
}

func TestEngine_Validate_BadStatus(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	err = e.Validate(Entry{Loop: "LOOP#4", TaskID: "x", Status: "WAT", Detail: "d"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFormatViolation))
}

func TestEngine_Validate_BadLoop(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	err = e.Validate(Entry{Loop: "loop#4", TaskID: "x", Status: "DONE", Detail: "d"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFormatViolation))
}

func TestEngine_Append_CreatesFileAndAppends(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC))

	err = e.Append(Entry{Loop: "LOOP#4", TaskID: "jito-rel", Status: "STARTED", Detail: "go"})
	require.NoError(t, err)
	err = e.Append(Entry{Loop: "LOOP#4", TaskID: "jito-rel", Status: "DONE ship-ready", Detail: "all metrics"})
	require.NoError(t, err)

	path := e.RunLogFile(time.Time{})
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)
	assert.Contains(t, body, "20:45:00 GMT+7 | LOOP#4 | jito-rel | STARTED | go\n")
	assert.Contains(t, body, "20:45:00 GMT+7 | LOOP#4 | jito-rel | DONE ship-ready | all metrics\n")
}

func TestEngine_Append_FormatViolation_NoWrite(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	err = e.Append(Entry{Loop: "BAD", TaskID: "x", Status: "DONE", Detail: "d"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFormatViolation))

	// Confirm no file was created.
	path := e.RunLogFile(time.Time{})
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestEngine_AppendRaw_OK(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC))

	line := "20:45:00 GMT+7 | LOOP#4 | jito-rel | EXECUTING tests | running"
	require.NoError(t, e.AppendRaw(line))

	data, err := os.ReadFile(e.RunLogFile(time.Time{}))
	require.NoError(t, err)
	assert.Contains(t, string(data), line+"\n")
}

func TestEngine_AppendRaw_BadFormat(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	err = e.AppendRaw("not a valid line")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFormatViolation))
}

func TestEngine_ReadState_OK(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "STATE.md"), []byte("# hi\n"), 0o644))

	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	data, err := e.ReadState()
	require.NoError(t, err)
	assert.Equal(t, "# hi\n", string(data))
}

func TestEngine_ReadState_Missing(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	_, err = e.ReadState()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStateMissing))
}

func TestEngine_ReadRunLog_OK(t *testing.T) {
	tmp := t.TempDir()
	when := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(tmp, "run-log-2026-06-28.md")
	body := strings.Join([]string{
		"20:45:00 GMT+7 | LOOP#4 | jito-rel | STARTED | go",
		"20:46:00 GMT+7 | LOOP#4 | jito-rel | EXECUTING build | linking",
		"20:47:00 GMT+7 | LOOP#4 | jito-rel | DONE ship-ready | shipped",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	entries, invalid, err := e.ReadRunLog(when)
	require.NoError(t, err)
	assert.Empty(t, invalid)
	require.Len(t, entries, 3)
	assert.Equal(t, "STARTED", entries[0].Status)
	assert.Equal(t, "EXECUTING build", entries[1].Status)
	assert.Equal(t, "DONE ship-ready", entries[2].Status)
	assert.Equal(t, "shipped", entries[2].Detail)
}

func TestEngine_ReadRunLog_Missing(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	_, _, err = e.ReadRunLog(time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRunLogMissing))
}

func TestEngine_ReadRunLog_InvalidLines(t *testing.T) {
	tmp := t.TempDir()
	when := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(tmp, "run-log-2026-06-28.md")
	body := strings.Join([]string{
		"20:45:00 GMT+7 | LOOP#4 | jito-rel | STARTED | go",
		"this is junk",
		"another|bad|line|with|no|gmtsuffix",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	entries, invalid, err := e.ReadRunLog(when)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "go", entries[0].Detail)
	require.Len(t, invalid, 2)
	assert.Contains(t, invalid[0], "junk")
}

func TestEngine_Append_EmptyDetailRejected(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC))

	// The strict regex requires .+ for detail; an empty detail is
	// therefore a format violation. This is intentional — the
	// heartbeat must always carry meaningful information.
	err = e.Append(Entry{Loop: "LOOP#4", TaskID: "x", Status: "DONE", Detail: ""})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFormatViolation))
}

func TestIsValidStatus(t *testing.T) {
	cases := map[string]bool{
		"STARTED":           true,
		"READING":           true,
		"PLANNING":          true,
		"EXECUTING":         true,
		"EXECUTING step 1":  true,
		"BLOCKED":           true,
		"BLOCKED on docker": true,
		"DONE":              true,
		"DONE ship-ready":   true,
		"STALLED":           true,
		"ABORTED":           true,
		"ABORTED timeout":   true,
		"WAT":               false,
		"":                  false,
		"start":             false, // lowercase not in whitelist
		"DONE ":             false, // trailing space alone is not allowed
		"DONE  ":            false, // multiple trailing spaces not allowed
		"EXECUTING ":        false, // trailing space alone is not allowed
	}
	for s, want := range cases {
		assert.Equalf(t, want, IsValidStatus(s), "status %q", s)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"", "", false},
		{"/abs/path", "/abs/path", false},
		{"relative/path", "relative/path", false},
		{"~", home, false},
		{"~/foo/bar", filepath.Join(home, "foo/bar"), false},
	}
	for _, c := range cases {
		got, err := expandHome(c.in)
		if c.wantErr {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, c.want, got, "input=%q", c.in)
	}
}

func TestParseLine(t *testing.T) {
	line := "20:45:00 GMT+7 | LOOP#4 | jito-rel | STARTED | first line"
	e, ok := parseLine(line)
	require.True(t, ok)
	assert.Equal(t, "LOOP#4", e.Loop)
	assert.Equal(t, "jito-rel", e.TaskID)
	assert.Equal(t, "STARTED", e.Status)
	assert.Equal(t, "first line", e.Detail)

	_, ok = parseLine("garbage")
	assert.False(t, ok)
}

// TestStrictRegex_Baseline confirms the regex matches the canonical
// line shapes a Loop Engineering sub-agent is expected to emit. The
// samples use the suffix form (DONE/ABORTED/BLOCKED/EXECUTING <…>)
// mandated by the CEO directive 2026-06-28; bare-form entries (e.g.
// "DONE |") are deliberately NOT in the baseline because they are
// NOT permitted under the strict spec — sub-agents that emit them
// must be updated.
func TestStrictRegex_Baseline(t *testing.T) {
	samples := []string{
		"14:48:15 GMT+7 | LOOP#2 | commands-loader-impl | STARTED | jito-tui first run, loop2/jito-commands branch @ 2e66d7f, spec read, exploring baseline",
		"14:53:43 GMT+7 | LOOP#2 | commands-loader-impl | DONE ship-ready | internal/commands pkg created: loader.go (14.5KB) + registry.go (7.2KB) + shlex.go (4.0KB); tests pass race-clean; coverage: loader/registry/shlex all >=90% (pkg avg 96.0%); go.mod has BurntSushi/toml v1.6.0",
		"20:42:00 GMT+7 | LOOP#3 | jito-sec-LOOP3-respawn-2 | DONE merge-to-main | commit 1e18953, +3710/-83 LOC, 18 files; sandbox 47 checks pass, 0 bypass, OWASP covered; coverage session 91.1%, permissions 79.2% (critical paths); build OK",
		"20:45:00 GMT+7 | LOOP#4 | jito-rel-LOOP4 | STARTED | PM spawning jito-rel in worktree loop4/jito-loop (non-stop) — ship v0.2.0 + README redesign",
		"15:39:30 GMT+7 | LOOP#3 | sandbox-hardening-impl | DONE permissions-sandbox | permissions/sandbox.go (canonicalize, env scrub, network block, dangerous cmd) + policy.Check extended with hard-deny (network/dangerous); 96.5% cov; race-clean",
		"15:42:30 GMT+7 | LOOP#3 | sandbox-wrap-impl | BLOCKED failover-timeout | LLM request timed out at Step 4 — work preserved in worktree, partial deliverables intact (schema, session pkg 91.1%, permissions 96.5%)",
	}
	for _, s := range samples {
		assert.Truef(t, StrictRegex.MatchString(s), "must match: %q", s)
	}
}

// TestStrictRegex_Negative confirms junk is rejected.
func TestStrictRegex_Negative(t *testing.T) {
	bad := []string{
		"",
		"hello world",
		"20:45 GMT+7 | LOOP#4 | x | DONE | d",  // missing seconds
		"20:45:00 | LOOP#4 | x | DONE | d",     // missing GMT+7
		"20:45:00 GMT+7 | loop#4 | x | DONE | d", // lowercase loop
		"20:45:00 GMT+7 | LOOP#4 | x | d | d",    // status not in whitelist
	}
	for _, s := range bad {
		assert.Falsef(t, StrictRegex.MatchString(s), "must not match: %q", s)
	}
}

func TestEngine_Append_MkdirFailure(t *testing.T) {
	// A path whose parent is an existing *file* makes MkdirAll fail
	// deterministically, exercising the error branch in Append.
	tmp := t.TempDir()
	blocking := filepath.Join(tmp, "blocker")
	require.NoError(t, os.WriteFile(blocking, []byte("x"), 0o644))

	e, err := New(Config{StateDir: filepath.Join(blocking, "nope")})
	require.NoError(t, err)
	e.now = fixedTime(time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC))

	err = e.Append(Entry{Loop: "LOOP#4", TaskID: "x", Status: "DONE ok", Detail: "d"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}

func TestEngine_AppendRaw_MkdirFailure(t *testing.T) {
	tmp := t.TempDir()
	blocking := filepath.Join(tmp, "blocker")
	require.NoError(t, os.WriteFile(blocking, []byte("x"), 0o644))

	e, err := New(Config{StateDir: filepath.Join(blocking, "nope")})
	require.NoError(t, err)

	line := "20:45:00 GMT+7 | LOOP#4 | x | DONE ok | d"
	err = e.AppendRaw(line)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}

func TestEngine_ReadState_IOError(t *testing.T) {
	// A directory (not a regular file) makes os.ReadFile return
	// EISDIR — distinct from os.ErrNotExist — which exercises the
	// "general I/O error" branch in ReadState.
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "STATE.md"), 0o755))

	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)

	_, err = e.ReadState()
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrStateMissing))
}

func TestEngine_ReadRunLog_CRLFHandling(t *testing.T) {
	// Windows-style CRLF endings should be normalised before regex.
	tmp := t.TempDir()
	when := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(tmp, "run-log-2026-06-28.md")
	body := "20:45:00 GMT+7 | LOOP#4 | x | STARTED | go\r\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	entries, invalid, err := e.ReadRunLog(when)
	require.NoError(t, err)
	assert.Empty(t, invalid)
	require.Len(t, entries, 1)
	assert.Equal(t, "go", entries[0].Detail)
}

func TestEngine_FullRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	e, err := New(Config{StateDir: tmp})
	require.NoError(t, err)
	when := time.Date(2026, 6, 28, 13, 45, 0, 0, time.UTC)
	e.now = fixedTime(when)

	require.NoError(t, e.Append(Entry{Loop: "LOOP#4", TaskID: "t1", Status: "STARTED", Detail: "begin"}))
	// Advance clock
	e.now = fixedTime(when.Add(time.Minute))
	require.NoError(t, e.Append(Entry{Loop: "LOOP#4", TaskID: "t1", Status: "EXECUTING tests", Detail: "running"}))
	e.now = fixedTime(when.Add(2 * time.Minute))
	require.NoError(t, e.Append(Entry{Loop: "LOOP#4", TaskID: "t1", Status: "DONE ship-ready", Detail: "all metrics"}))

	entries, invalid, err := e.ReadRunLog(time.Time{})
	require.NoError(t, err)
	assert.Empty(t, invalid)
	require.Len(t, entries, 3)
	assert.Equal(t, "STARTED", entries[0].Status)
	assert.Equal(t, "EXECUTING tests", entries[1].Status)
	assert.Equal(t, "DONE ship-ready", entries[2].Status)
}
