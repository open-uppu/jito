package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCmd is a tiny helper that builds the named subcommand and
// invokes it with the provided args, capturing stdout/stderr. The
// runCmd helper relies on cobra's SetOut/SetErr — commands in this
// package use cmd.OutOrStdout() rather than bare fmt.Println, so we
// do NOT redirect os.Stdout (which would race with the buffer).
func runCmd(t *testing.T, root *cobra.Command, args []string) (stdout, stderr string, err error) {
	t.Helper()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestLoop_State_PrettyAndJSON(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "STATE.md"),
		[]byte("# State\n\n| Loop | Target | Status |\n| #1 | X | done |\n| #2 | Y | wip |\n\nBlockers:\n- B-1 docker\n"), 0o644))

	root := NewRootCmd("test", "deadbeef", "2026-06-28")

	stdout, _, err := runCmd(t, root, []string{"loop", "state", "--state-dir", tmp})
	require.NoError(t, err)
	assert.Contains(t, stdout, "| #1 | X | done |", "raw STATE.md content is printed verbatim")
	assert.Contains(t, stdout, "B-1 docker")

	// --json is registered on the loop parent; on the state
	// sub-command it is accepted but has no effect (state always
	// prints the raw file).
	stdout, _, err = runCmd(t, root, []string{"loop", "state", "--state-dir", tmp, "--json"})
	require.NoError(t, err)
	assert.Contains(t, stdout, "Blockers")
}

func TestLoop_RunLog_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "run-log-2026-06-28.md")
	body := strings.Join([]string{
		"20:45:00 GMT+7 | LOOP#4 | t1 | STARTED | go",
		"20:46:00 GMT+7 | LOOP#4 | t1 | EXECUTING build | linking",
		"20:47:00 GMT+7 | LOOP#4 | t1 | DONE ship-ready | all metrics",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	root := NewRootCmd("test", "deadbeef", "2026-06-28")

	stdout, _, err := runCmd(t, root, []string{"loop", "run-log", "--state-dir", tmp})
	require.NoError(t, err)
	assert.Contains(t, stdout, "20:45:00 GMT+7 | LOOP#4 | t1 | STARTED | go")
	assert.Contains(t, stdout, "20:47:00 GMT+7 | LOOP#4 | t1 | DONE ship-ready | all metrics")

	// JSON output
	stdout, _, err = runCmd(t, root, []string{"loop", "run-log", "--state-dir", tmp, "--json", "--date", "2026-06-28"})
	require.NoError(t, err)
	var payload struct {
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	assert.Len(t, payload.Entries, 3)
}

func TestLoop_RunLog_MissingDate(t *testing.T) {
	tmp := t.TempDir()
	root := NewRootCmd("test", "deadbeef", "2026-06-28")
	_, _, err := runCmd(t, root, []string{"loop", "run-log", "--state-dir", tmp, "--date", "1999-01-01"})
	require.Error(t, err)
}

func TestLoop_Status_BadDateFails(t *testing.T) {
	tmp := t.TempDir()
	root := NewRootCmd("test", "deadbeef", "2026-06-28")
	_, _, err := runCmd(t, root, []string{"loop", "run-log", "--state-dir", tmp, "--date", "not-a-date"})
	require.Error(t, err)
}

func TestLoop_Status_Summary(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "STATE.md"),
		[]byte("| Loop | Target | Status |\n| #1 | X | done |\n| #2 | Y | wip |\n\n| B-1 | docker |\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "run-log-2026-06-28.md"),
		[]byte("20:45:00 GMT+7 | LOOP#4 | t1 | STARTED | go\n20:46:00 GMT+7 | LOOP#4 | t1 | DONE ship-ready | all metrics\n"),
		0o644))

	root := NewRootCmd("test", "deadbeef", "2026-06-28")

	stdout, _, err := runCmd(t, root, []string{"loop", "status", "--state-dir", tmp})
	require.NoError(t, err)
	assert.Contains(t, stdout, "state_dir:")
	assert.Contains(t, stdout, "loops:")
	assert.Contains(t, stdout, "blockers:")
	assert.Contains(t, stdout, "entries:")
	assert.Contains(t, stdout, "last:")

	stdout, _, err = runCmd(t, root, []string{"loop", "status", "--state-dir", tmp, "--json"})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	assert.NotZero(t, payload["loops"])
}

func TestLoop_Status_MissingState_OK(t *testing.T) {
	tmp := t.TempDir()
	// No STATE.md -> status still works, prints state_error field.
	root := NewRootCmd("test", "deadbeef", "2026-06-28")

	stdout, _, err := runCmd(t, root, []string{"loop", "status", "--state-dir", tmp, "--json"})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	assert.NotNil(t, payload["state_error"])
}

func TestSummariseState(t *testing.T) {
	body := "| Loop | Target | Status |\n| #1 | X | done |\n| #2 | Y | wip |\n"
	loops, blockers := summariseState(body)
	// Three lines match the loop-row heuristic (header + 2 data
	// rows); the header is decremented to leave 2.
	assert.Equal(t, 2, loops, "two data rows after header subtraction")
	assert.Equal(t, 0, blockers)

	body2 := "| Loop | Target | Status |\n| #1 | X | done |\n| #2 | Y | wip |\n| B-1 | docker daemon |\n"
	loops, blockers = summariseState(body2)
	assert.Equal(t, 2, loops)
	assert.Equal(t, 1, blockers)

	loops, blockers = summariseState("")
	assert.Equal(t, 0, loops)
	assert.Equal(t, 0, blockers)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 10))
	assert.Equal(t, "ab…", truncate("abcdef", 3))
	assert.Equal(t, "a…", truncate("abcdef", 2))
	assert.Equal(t, "a", truncate("abcdef", 1))
	assert.Equal(t, "", truncate("abcdef", 0))
}

func TestSpawn_DryRun(t *testing.T) {
	tmp := t.TempDir()
	root := NewRootCmd("test", "deadbeef", "2026-06-28")

	stdout, _, err := runCmd(t, root, []string{
		"spawn", "ghost-agent", "do something",
		"--loop", "4",
		"--task", "ghost-test",
		"--mode", "audit",
		"--dry-run",
		"--state-dir", tmp,
	})
	require.NoError(t, err)
	assert.Contains(t, stdout, "dry-run")

	// Confirm two heartbeat lines were written.
	statuses := readRunLogStatuses(t, tmp, time.Time{})
	require.Len(t, statuses, 2)
	assert.Equal(t, "STARTED", statuses[0].status)
	assert.Equal(t, "DONE dry-run", statuses[1].status, "DONE must carry a suffix per strict regex")
	assert.Equal(t, "ghost-test", statuses[0].taskID, "--task overrides the agent name")
	assert.Contains(t, statuses[0].detail, "ghost-agent", "agent name appears in detail")
}

// hbLine is a tiny parsed-run-log record used only by the dry-run
// test. Keeping it local avoids coupling the CLI tests to the
// internal/loop package's API.
type hbLine struct {
	status string
	loop   string
	taskID string
	detail string
}

func readRunLogStatuses(t *testing.T, stateDir string, when time.Time) []hbLine {
	t.Helper()
	if when.IsZero() {
		when = time.Now().UTC()
	}
	path := filepath.Join(stateDir, "run-log-"+when.Format("2006-01-02")+".md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []hbLine
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " | ")
		if len(parts) != 5 {
			continue
		}
		out = append(out, hbLine{
			status: parts[3],
			loop:   parts[1],
			taskID: parts[2],
			detail: parts[4],
		})
	}
	return out
}
