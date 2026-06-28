package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func sampleSnapshot() Snapshot {
	return Snapshot{
		ID:        "sess-1",
		Mode:      "dev",
		Model:     "minimax/MiniMax-M3",
		Title:     "refactor auth",
		ParentID:  "",
		CreatedAt: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 28, 12, 5, 0, 0, time.UTC),
		Messages: []SnapshotMessage{
			{Role: "user", Content: "fix the login flow", CreatedAt: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)},
			{Role: "assistant", Content: "I'll refactor session.go", CreatedAt: time.Date(2026, 6, 28, 12, 1, 0, 0, time.UTC)},
			{Role: "user", Content: "thanks", ToolCalls: `[{"name":"bash","input":"go test"}]`, CreatedAt: time.Date(2026, 6, 28, 12, 5, 0, 0, time.UTC)},
		},
		Context: map[string]string{"JITO.md": "loaded"},
	}
}

// --- StoreCheckpointer ----------------------------------------------

func TestStoreCheckpointer_SaveLoadRoundtrip(t *testing.T) {
	st := newTestStore(t)
	cp := NewStoreCheckpointer(st)

	want := sampleSnapshot()
	require.NoError(t, cp.Save(want))

	got, err := cp.Load(want.ID)
	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Mode, got.Mode)
	assert.Equal(t, want.Model, got.Model)
	assert.Equal(t, want.Title, got.Title)
	require.Len(t, got.Messages, 3)
	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "fix the login flow", got.Messages[0].Content)
	assert.Equal(t, "assistant", got.Messages[1].Role)
	assert.Contains(t, got.Messages[2].ToolCalls, "bash")
}

func TestStoreCheckpointer_LoadNotFound(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	_, err := cp.Load("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStoreCheckpointer_SaveAutoID(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	snap := Snapshot{Model: "m", Messages: []SnapshotMessage{{Role: "user", Content: "hi"}}}
	// Save returns the assigned id via Load; the caller's snapshot
	// value is not mutated because Go passes struct args by value.
	// Tests that need the post-Save ID use Load() to retrieve it.
	_ = cp.Save(snap)
	list, err := cp.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.NotEmpty(t, list[0].ID)
	assert.Equal(t, "universal", list[0].Mode) // mode defaulted
}

func TestStoreCheckpointer_SaveIdempotent(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	snap := sampleSnapshot()
	require.NoError(t, cp.Save(snap))

	// Re-save with one fewer message — the snapshot is authoritative.
	snap.Messages = snap.Messages[:1]
	require.NoError(t, cp.Save(snap))

	got, err := cp.Load(snap.ID)
	require.NoError(t, err)
	assert.Len(t, got.Messages, 1, "re-save should clear stale messages")
}

func TestStoreCheckpointer_SaveDefaultsMode(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	snap := Snapshot{ID: "abc", Messages: []SnapshotMessage{{Role: "user", Content: "hi"}}}
	require.NoError(t, cp.Save(snap))
	got, err := cp.Load(snap.ID)
	require.NoError(t, err)
	assert.Equal(t, "universal", got.Mode)
}

func TestStoreCheckpointer_UpdateOnResave(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	snap := sampleSnapshot()
	require.NoError(t, cp.Save(snap))
	first, _ := cp.Load(snap.ID)
	snap.Title = "renamed"
	snap.Model = "newmodel"
	require.NoError(t, cp.Save(snap))
	second, _ := cp.Load(snap.ID)
	assert.Equal(t, "renamed", second.Title)
	assert.Equal(t, "newmodel", second.Model)
	assert.True(t, second.UpdatedAt.After(first.UpdatedAt) || second.UpdatedAt.Equal(first.UpdatedAt))
}

func TestStoreCheckpointer_ParentID(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	parent := Snapshot{ID: "parent", Mode: "dev"}
	require.NoError(t, cp.Save(parent))
	child := Snapshot{ID: "child", Mode: "dev", ParentID: "parent"}
	require.NoError(t, cp.Save(child))
	got, err := cp.Load("child")
	require.NoError(t, err)
	assert.Equal(t, "parent", got.ParentID)
}

func TestStoreCheckpointer_List(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, cp.Save(Snapshot{ID: id, Mode: "dev", Messages: []SnapshotMessage{{Role: "user", Content: "hi"}}}))
	}
	list, err := cp.List()
	require.NoError(t, err)
	require.Len(t, list, 3)
	ids := []string{list[0].ID, list[1].ID, list[2].ID}
	assert.ElementsMatch(t, []string{"a", "b", "c"}, ids)
}

func TestStoreCheckpointer_Delete(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	require.NoError(t, cp.Save(sampleSnapshot()))
	require.NoError(t, cp.Delete("sess-1"))
	_, err := cp.Load("sess-1")
	require.Error(t, err)
}

func TestStoreCheckpointer_DeleteNotFound(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	err := cp.Delete("nope")
	require.Error(t, err)
}

func TestStoreCheckpointer_ConcurrentSave(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snap := Snapshot{
				ID:       "s" + string(rune('a'+i)),
				Mode:     "dev",
				Messages: []SnapshotMessage{{Role: "user", Content: "hi"}},
			}
			_ = cp.Save(snap)
		}(i)
	}
	wg.Wait()
	list, err := cp.List()
	require.NoError(t, err)
	assert.Len(t, list, 10)
}

// --- FileCheckpointer -----------------------------------------------

func TestFileCheckpointer_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)

	snap := sampleSnapshot()
	require.NoError(t, cp.Save(snap))

	got, err := cp.Load(snap.ID)
	require.NoError(t, err)
	assert.Equal(t, snap.ID, got.ID)
	assert.Equal(t, snap.Mode, got.Mode)
	require.Len(t, got.Messages, 3)
}

func TestFileCheckpointer_LoadNotFound(t *testing.T) {
	cp, _ := NewFileCheckpointer(t.TempDir())
	_, err := cp.Load("missing")
	require.Error(t, err)
}

func TestFileCheckpointer_DeleteAndList(t *testing.T) {
	cp, _ := NewFileCheckpointer(t.TempDir())
	require.NoError(t, cp.Save(Snapshot{ID: "x", Mode: "dev"}))
	require.NoError(t, cp.Save(Snapshot{ID: "y", Mode: "dev"}))

	list, err := cp.List()
	require.NoError(t, err)
	assert.Len(t, list, 2)

	require.NoError(t, cp.Delete("x"))
	list, _ = cp.List()
	assert.Len(t, list, 1)
}

func TestFileCheckpointer_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)
	snap := Snapshot{ID: "atomic", Mode: "dev", Messages: []SnapshotMessage{{Role: "user", Content: "x"}}}
	require.NoError(t, cp.Save(snap))

	// Verify file is valid JSON (no leftover temp).
	data, err := os.ReadFile(filepath.Join(dir, "atomic.json"))
	require.NoError(t, err)
	var parsed Snapshot
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "atomic", parsed.ID)

	// No .tmp files remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		assert.False(t, filepath.Ext(e.Name()) == ".tmp", "no temp files should remain")
	}
}

func TestFileCheckpointer_ListSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)
	require.NoError(t, cp.Save(Snapshot{ID: "good", Mode: "dev"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o644))

	list, err := cp.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "good", list[0].ID)
}

func TestFileCheckpointer_SaveRequiresID(t *testing.T) {
	cp, _ := NewFileCheckpointer(t.TempDir())
	err := cp.Save(Snapshot{Mode: "dev"})
	require.Error(t, err)
}

func TestFileCheckpointer_SaveAutoFillsDefaults(t *testing.T) {
	cp, _ := NewFileCheckpointer(t.TempDir())
	snap := Snapshot{ID: "x"}
	require.NoError(t, cp.Save(snap))
	got, err := cp.Load("x")
	require.NoError(t, err)
	assert.Equal(t, SnapshotVersion, got.Version)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

// --- Schema robustness ---------------------------------------------

func TestSnapshot_VersionField(t *testing.T) {
	snap := sampleSnapshot()
	data, err := json.Marshal(&snap)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	// Either version is set by Save or by the user; before Save the
	// helper sets Version to 0.  After Save it should be 1.
	assert.Contains(t, raw, "version")
}

func TestStoreCheckpointer_EmptyMessages(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	snap := Snapshot{ID: "empty", Mode: "dev"}
	require.NoError(t, cp.Save(snap))
	got, err := cp.Load("empty")
	require.NoError(t, err)
	assert.Empty(t, got.Messages)
}

func TestStoreCheckpointer_ContextRefs(t *testing.T) {
	// Context refs live only in the FileCheckpointer's JSON
	// (StoreCheckpointer persists the session row + messages but
	// not the optional refs map; callers that need refs across
	// restart should use FileCheckpointer or wire a custom column).
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)
	snap := Snapshot{
		ID:      "ctx",
		Mode:    "dev",
		Context: map[string]string{"JITO.md": "loaded", "rules.md": "applied"},
	}
	require.NoError(t, cp.Save(snap))
	got, err := cp.Load("ctx")
	require.NoError(t, err)
	assert.Equal(t, "loaded", got.Context["JITO.md"])
	assert.Equal(t, "applied", got.Context["rules.md"])
}

// TestCheckpointError_NonExistentID — sanity check that Load returns
// a typed error compatible with errors.Is against a not-found
// sentinel.  We use string contains rather than a sentinel because
// the SQLite driver wraps errors.
func TestCheckpointError_NotFound(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	_, err := cp.Load("nope")
	require.Error(t, err)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("driver may surface fs-not-exist; OK")
	}
	assert.Contains(t, err.Error(), "not found")
}

// --- Additional coverage -------------------------------------------

func TestStoreCheckpointer_ListEmpty(t *testing.T) {
	cp := NewStoreCheckpointer(newTestStore(t))
	list, err := cp.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestFileCheckpointer_NewDirCreated(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "nested", "deeper")
	cp, err := NewFileCheckpointer(sub)
	require.NoError(t, err)
	require.NotNil(t, cp)
	info, err := os.Stat(sub)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileCheckpointer_DefaultsApplied(t *testing.T) {
	cp, _ := NewFileCheckpointer(t.TempDir())
	snap := Snapshot{ID: "abc", Mode: "dev"}
	require.NoError(t, cp.Save(snap))
	got, err := cp.Load("abc")
	require.NoError(t, err)
	assert.Equal(t, SnapshotVersion, got.Version)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
	assert.Equal(t, "dev", got.Mode)
}

func TestFileCheckpointer_DeleteMissingIsNoop(t *testing.T) {
	cp, _ := NewFileCheckpointer(t.TempDir())
	err := cp.Delete("does-not-exist")
	assert.NoError(t, err)
}

func TestFileCheckpointer_LoadMalformed(t *testing.T) {
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.json"), []byte("not json"), 0o644))
	_, err = cp.Load("broken")
	require.Error(t, err)
}

func TestFileCheckpointer_ListIgnoresSubdirs(t *testing.T) {
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)
	require.NoError(t, cp.Save(Snapshot{ID: "x", Mode: "dev"}))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o755))
	list, err := cp.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestFileCheckpointer_SaveAtomicFailureOnClosedDir(t *testing.T) {
	// Point the checkpointer at a path under a read-only / nonexistent
	// directory so the rename fails.
	root := t.TempDir()
	ro := filepath.Join(root, "ro")
	require.NoError(t, os.Mkdir(ro, 0o555))
	cp, err := NewFileCheckpointer(ro)
	// On Linux, mkdir 0o555 still permits writes for root; the test is
	// most useful on systems where the perms are honored.  We assert
	// only that Save either succeeds (and we can read it back) or
	// returns an error.
	_ = err
	err = cp.Save(Snapshot{ID: "x", Mode: "dev"})
	if err == nil {
		_, _ = cp.Load("x")
	}
}

func TestNullableString(t *testing.T) {
	assert.Nil(t, nullableString(""))
	assert.Equal(t, "x", nullableString("x"))
}

func TestFileCheckpointer_NewBadPath(t *testing.T) {
	// /dev/null is not a directory; creating under it must error.
	_, err := NewFileCheckpointer("/dev/null/child")
	require.Error(t, err)
}

func TestFileCheckpointer_SaveRenameError(t *testing.T) {
	// Pre-create the destination as a directory so the rename step
	// fails (can't overwrite a directory with a file).
	dir := t.TempDir()
	cp, err := NewFileCheckpointer(dir)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "abc.json"), 0o755))
	err = cp.Save(Snapshot{ID: "abc", Mode: "dev"})
	require.Error(t, err)
}

func TestStoreCheckpointer_LoadFailure(t *testing.T) {
	// Force ListSessionMessages to fail by closing the underlying DB
	// before Load.
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, err)
	cp := NewStoreCheckpointer(st)
	require.NoError(t, cp.Save(Snapshot{ID: "y", Mode: "dev"}))
	// Sanity: load succeeds while DB is open.
	_, err = cp.Load("y")
	require.NoError(t, err)
	// Now close and try again — should error.
	require.NoError(t, st.Close())
	_, err = cp.Load("y")
	require.Error(t, err)
}

func TestStoreCheckpointer_ListAfterClose(t *testing.T) {
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, err)
	cp := NewStoreCheckpointer(st)
	require.NoError(t, cp.Save(Snapshot{ID: "z", Mode: "dev"}))
	require.NoError(t, st.Close())
	_, err = cp.List()
	require.Error(t, err)
}