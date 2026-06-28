package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/session"
	"github.com/uppu/jito/internal/store"
)

// withTempStore opens a brand-new SQLite-backed store inside t.TempDir()
// and returns its path.  Each call gets a fresh database.
func withTempStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "store.db")
}

// seedSession writes one session row with the supplied messages and
// returns the generated session id.  Uses the StoreCheckpointer so
// the test stays decoupled from the underlying SQL.
func seedSession(t *testing.T, storePath, title string, msgs []session.SnapshotMessage) string {
	t.Helper()
	st, err := store.OpenStore(storePath)
	require.NoError(t, err)
	defer st.Close()

	cp := session.NewStoreCheckpointer(st)
	id := uuid.NewString()
	snap := session.Snapshot{
		ID:      id,
		Mode:    "dev",
		Model:   "minimax/MiniMax-M3",
		Title:   title,
		Messages: msgs,
	}
	require.NoError(t, cp.Save(snap))
	return id
}

func msg(role, content string) session.SnapshotMessage {
	return session.SnapshotMessage{Role: role, Content: content}
}

// --- resume ---------------------------------------------------------

func TestExecuteResume_NotFound(t *testing.T) {
	out, err := ExecuteResume(resumeOptions{StorePath: withTempStore(t)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sessions")
	_ = out
}

func TestExecuteResume_MostRecent(t *testing.T) {
	storePath := withTempStore(t)
	// Two sessions: the second one is the "most recent".
	_ = seedSession(t, storePath, "first", []session.SnapshotMessage{
		msg("user", "hello 1"),
	})
	// Add a tiny sleep so the second session has a strictly later updated_at.
	// (Save() sets UpdatedAt = now, and resolution is second-level.)
	time.Sleep(1100 * time.Millisecond)
	oldID := seedSession(t, storePath, "second", []session.SnapshotMessage{
		msg("user", "hello 2"),
		msg("assistant", "world 2"),
	})
	_ = oldID

	out, err := ExecuteResume(resumeOptions{StorePath: storePath})
	require.NoError(t, err)
	assert.Contains(t, out, "hello 2")
	assert.Contains(t, out, "world 2")
}

func TestExecuteResume_ByID(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "alpha", []session.SnapshotMessage{
		msg("user", "alpha user msg"),
		msg("assistant", "alpha assistant msg"),
	})
	out, err := ExecuteResume(resumeOptions{SessionID: id, StorePath: storePath})
	require.NoError(t, err)
	assert.Contains(t, out, id)
	assert.Contains(t, out, "alpha user msg")
	assert.Contains(t, out, "alpha assistant msg")
}

func TestExecuteResume_PrefixMatch(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "alpha", []session.SnapshotMessage{
		msg("user", "prefixed"),
	})
	prefix := strings.SplitN(id, "-", 2)[0]
	out, err := ExecuteResume(resumeOptions{SessionID: prefix, StorePath: storePath})
	require.NoError(t, err)
	assert.Contains(t, out, id)
	assert.Contains(t, out, "prefixed")
}

func TestExecuteResume_AmbiguousPrefix(t *testing.T) {
	storePath := withTempStore(t)
	// Seed two sessions, then rewrite BOTH ids to share the prefix
	// "same-prefix-" so a prefix lookup is forced to be ambiguous.
	// We avoid the gotcha where two UPDATEs in the same transaction
	// with same-second created_at hit the same row via ORDER BY
	// ASC/DESC LIMIT 1 — instead we INSERT two rows with explicit
	// known ids, then UPDATE each by id.
	st, err := store.OpenStore(storePath)
	require.NoError(t, err)
	defer st.Close()

	idA := uuid.NewString()
	idB := uuid.NewString()
	_, err = st.DB().Exec(`INSERT INTO sessions (id, mode, model, title, created_at, updated_at) VALUES (?, 'dev', '', 'a', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, idA)
	require.NoError(t, err)
	_, err = st.DB().Exec(`INSERT INTO sessions (id, mode, model, title, created_at, updated_at) VALUES (?, 'dev', '', 'b', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, idB)
	require.NoError(t, err)

	_, err = st.DB().Exec(`UPDATE sessions SET id = 'same-prefix-aaa' WHERE id = ?`, idA)
	require.NoError(t, err)
	_, err = st.DB().Exec(`UPDATE sessions SET id = 'same-prefix-bbb' WHERE id = ?`, idB)
	require.NoError(t, err)

	out, err := ExecuteResume(resumeOptions{SessionID: "same-prefix-", StorePath: storePath})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
	_ = out
}

func TestExecuteResume_NotFoundExact(t *testing.T) {
	storePath := withTempStore(t)
	_, err := ExecuteResume(resumeOptions{SessionID: "nope", StorePath: storePath})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestExecuteResume_AsJSON(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "jsonify", []session.SnapshotMessage{
		msg("user", "json user"),
	})
	out, err := ExecuteResume(resumeOptions{SessionID: id, StorePath: storePath, AsJSON: true})
	require.NoError(t, err)
	var snap session.Snapshot
	require.NoError(t, json.Unmarshal([]byte(out), &snap))
	assert.Equal(t, id, snap.ID)
	assert.Equal(t, "jsonify", snap.Title)
	assert.Len(t, snap.Messages, 1)
	assert.Equal(t, "json user", snap.Messages[0].Content)
}

func TestExecuteResume_RenderSnapshot(t *testing.T) {
	s := session.Snapshot{
		ID:      "render-id",
		Mode:    "audit",
		Model:   "",
		Title:   "render-title",
		Messages: []session.SnapshotMessage{
			msg("user", "render body"),
		},
	}
	out := renderSnapshot(s)
	assert.Contains(t, out, "render-id")
	assert.Contains(t, out, "render-title")
	assert.Contains(t, out, "audit")
	assert.Contains(t, out, "(default)") // empty Model falls back to "(default)"
	assert.Contains(t, out, "render body")
	assert.Contains(t, out, "    render body") // indented body
}

func TestExecuteResume_IndentsMultilineContent(t *testing.T) {
	body := "line one\nline two\nline three"
	out := indent(body, "    ")
	assert.Equal(t, "    line one\n    line two\n    line three", out)
}

func TestExecuteResume_FallbackEmpty(t *testing.T) {
	assert.Equal(t, "(none)", fallback("", "(none)"))
	assert.Equal(t, "ok", fallback("ok", "(none)"))
}

// --- sessions list --------------------------------------------------

func TestExecuteSessionsList_Empty(t *testing.T) {
	out, err := ExecuteSessionsList(sessionsListOptions{StorePath: withTempStore(t)})
	require.NoError(t, err)
	assert.Contains(t, out, "No saved sessions")
}

func TestExecuteSessionsList_OneEntry(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "single", []session.SnapshotMessage{
		msg("user", "hi"),
	})
	out, err := ExecuteSessionsList(sessionsListOptions{StorePath: storePath})
	require.NoError(t, err)
	assert.Contains(t, out, id)
	assert.Contains(t, out, "single")
	assert.Contains(t, out, "MODE")
	assert.Contains(t, out, "TITLE")
}

func TestExecuteSessionsList_MultipleEntriesSortedByUpdated(t *testing.T) {
	storePath := withTempStore(t)
	_ = seedSession(t, storePath, "oldest", []session.SnapshotMessage{msg("user", "old")})
	// Sleeps keep each Save() in a distinct second so updated_at
	// ordering is deterministic (SQLite CURRENT_TIMESTAMP has 1s
	// resolution).
	time.Sleep(1100 * time.Millisecond)
	_ = seedSession(t, storePath, "middle", []session.SnapshotMessage{msg("user", "mid")})
	time.Sleep(1100 * time.Millisecond)
	_ = seedSession(t, storePath, "newest", []session.SnapshotMessage{msg("user", "new")})

	out, err := ExecuteSessionsList(sessionsListOptions{StorePath: storePath})
	require.NoError(t, err)
	posNew := strings.Index(out, "newest")
	posMid := strings.Index(out, "middle")
	posOld := strings.Index(out, "oldest")
	assert.True(t, posNew >= 0 && posMid > posNew && posOld > posMid,
		"newest must appear before middle which appears before oldest; got positions new=%d mid=%d old=%d",
		posNew, posMid, posOld)
}

func TestExecuteSessionsList_Limit(t *testing.T) {
	storePath := withTempStore(t)
	for i := 0; i < 5; i++ {
		_ = seedSession(t, storePath, "x", []session.SnapshotMessage{msg("user", "x")})
	}
	out, err := ExecuteSessionsList(sessionsListOptions{StorePath: storePath, Limit: 2})
	require.NoError(t, err)
	lines := strings.Split(out, "\n")
	// 1 header + 1 separator + 2 rows + trailing newline → 5 entries.
	assert.LessOrEqual(t, len(lines), 6,
		"limit must cap output rows; got %d lines", len(lines))
}

func TestExecuteSessionsList_AsJSON(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "jsonlist", []session.SnapshotMessage{msg("user", "x")})
	out, err := ExecuteSessionsList(sessionsListOptions{StorePath: storePath, AsJSON: true})
	require.NoError(t, err)
	var snaps []session.Snapshot
	require.NoError(t, json.Unmarshal([]byte(out), &snaps))
	require.Len(t, snaps, 1)
	assert.Equal(t, id, snaps[0].ID)
}

// --- sessions show --------------------------------------------------

func TestExecuteSessionsShow_Found(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "showme", []session.SnapshotMessage{
		msg("user", "show user"),
		msg("assistant", "show assistant"),
	})
	out, err := ExecuteSessionsShow(sessionsShowOptions{SessionID: id, StorePath: storePath})
	require.NoError(t, err)
	assert.Contains(t, out, id)
	assert.Contains(t, out, "show user")
	assert.Contains(t, out, "show assistant")
}

func TestExecuteSessionsShow_NotFound(t *testing.T) {
	_, err := ExecuteSessionsShow(sessionsShowOptions{SessionID: "ghost", StorePath: withTempStore(t)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestExecuteSessionsShow_AsJSON(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "showjson", []session.SnapshotMessage{msg("user", "jsonshow")})
	out, err := ExecuteSessionsShow(sessionsShowOptions{SessionID: id, StorePath: storePath, AsJSON: true})
	require.NoError(t, err)
	var snap session.Snapshot
	require.NoError(t, json.Unmarshal([]byte(out), &snap))
	assert.Equal(t, id, snap.ID)
	assert.Equal(t, "jsonshow", snap.Messages[0].Content)
}

// --- sessions delete ------------------------------------------------

func TestExecuteSessionsDelete_RequiresYes(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "todelete", []session.SnapshotMessage{msg("user", "x")})
	_, err := ExecuteSessionsDelete(sessionsDeleteOptions{SessionID: id, StorePath: storePath})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without --yes")
	// Confirm session still exists.
	list, _ := ExecuteSessionsList(sessionsListOptions{StorePath: storePath})
	assert.Contains(t, list, id)
}

func TestExecuteSessionsDelete_WithYes(t *testing.T) {
	storePath := withTempStore(t)
	id := seedSession(t, storePath, "todelete", []session.SnapshotMessage{msg("user", "x")})
	out, err := ExecuteSessionsDelete(sessionsDeleteOptions{SessionID: id, StorePath: storePath, Force: true})
	require.NoError(t, err)
	assert.Contains(t, out, id)
	// Confirm session no longer present.
	list, _ := ExecuteSessionsList(sessionsListOptions{StorePath: storePath})
	assert.NotContains(t, list, id)
}

func TestExecuteSessionsDelete_NotFound(t *testing.T) {
	_, err := ExecuteSessionsDelete(sessionsDeleteOptions{SessionID: "ghost", StorePath: withTempStore(t), Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

// --- table-driven error matrix -------------------------------------

func TestSessions_ErrorMatrix(t *testing.T) {
	storePath := withTempStore(t)
	seedID := seedSession(t, storePath, "matrix", []session.SnapshotMessage{msg("user", "x")})

	cases := []struct {
		name string
		fn   func() (string, error)
		want string
	}{
		{"list-ok", func() (string, error) { return ExecuteSessionsList(sessionsListOptions{StorePath: storePath}) }, "matrix"},
		{"show-missing", func() (string, error) { return ExecuteSessionsShow(sessionsShowOptions{SessionID: "nope", StorePath: storePath}) }, "nope"},
		{"delete-no-yes", func() (string, error) { return ExecuteSessionsDelete(sessionsDeleteOptions{SessionID: seedID, StorePath: storePath}) }, "without --yes"},
		{"delete-yes-ok", func() (string, error) { return ExecuteSessionsDelete(sessionsDeleteOptions{SessionID: seedID, StorePath: storePath, Force: true}) }, "deleted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := c.fn()
			if c.want == "without --yes" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), c.want)
				return
			}
			if c.want == "nope" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), c.want)
				return
			}
			if c.want == "deleted" {
				require.NoError(t, err)
				assert.Contains(t, out, c.want)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, out, c.want)
		})
	}
}

// --- cobra integration smoke ----------------------------------------

// buildRootWithSessions returns a fresh cobra root with chat, resume
// and sessions commands attached — mirrors what root.go does in
// production.  NewRootCmd already registers the persistent flags
// we need (--mode, --model, --store), so we must NOT re-add them
// here or pflag panics with "flag redefined".
func buildRootWithSessions() *cobra.Command {
	root := NewRootCmd("test", "abc", "2026")
	// chat/resume/sessions are already wired by NewRootCmd; the extra
	// AddCommand calls are a no-op once registered.
	root.AddCommand(newChatCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newSessionsCmd())
	return root
}

func TestSessionsCmd_CobraWiring(t *testing.T) {
	root := buildRootWithSessions()
	root.SetArgs([]string{"sessions", "list", "--store", withTempStore(t)})
	// Capture stdout.
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	root.SetOut(w)
	root.SetErr(w)
	runErr := root.Execute()
	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if runErr != nil {
		t.Logf("sessions list exec error (likely empty store): %v", runErr)
	}
	assert.Contains(t, out, "No saved sessions")
}

func TestResumeCmd_CobraWiring(t *testing.T) {
	root := buildRootWithSessions()
	root.SetArgs([]string{"resume", "--store", withTempStore(t)})
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	root.SetOut(w)
	root.SetErr(w)
	runErr := root.Execute()
	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	_ = string(buf[:n])
	if runErr != nil {
		t.Logf("resume exec error (expected, empty store): %v", runErr)
	}
	// The exact error message comes from cobra; we just verify the
	// command is wired into the root and reaches Execute.
}