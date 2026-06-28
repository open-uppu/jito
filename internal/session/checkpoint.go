// Package session implements LOOP #3 concerns:
//
//   - Checkpoint API: serialize/deserialize a chat session to a
//     single JSON blob (Save / Load / List / Delete).
//   - Audit logger:   append-only security log with mandatory
//     argument redaction.  Two implementations ship: an in-memory
//     version (MemoryLogger) and a SQLite-backed version that
//     delegates to store.Store.AppendAudit.
//
// The package depends only on internal/store and stdlib so it can be
// embedded by the bash sandbox (internal/tools) and the CLI layer
// (internal/cli) without circular imports.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/uppu/jito/internal/store"
)

// Snapshot is the JSON-serialized form of a session.  Mode, model,
// title, parent_id, created/updated timestamps plus the full message
// history and (optional) context references.  The schema is
// intentionally loose — extra fields are ignored on Load to keep
// forward/backward compatibility easy.
type Snapshot struct {
	Version   int               `json:"version"`
	ID        string            `json:"id"`
	Mode      string            `json:"mode"`
	Model     string            `json:"model"`
	Title     string            `json:"title"`
	ParentID  string            `json:"parent_id,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Messages  []SnapshotMessage `json:"messages"`
	Context   map[string]string `json:"context,omitempty"` // optional refs
}

// SnapshotMessage mirrors store.SessionMessage but is decoupled so
// the JSON format survives future schema changes in the store.
type SnapshotMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolCalls string    `json:"tool_calls,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SnapshotVersion is the on-disk schema version.  Bump it whenever
// the JSON shape changes incompatibly.
const SnapshotVersion = 1

// Checkpointer provides the Save/Load/List/Delete API.  Two impls
// ship: StoreCheckpointer (SQLite) and FileCheckpointer (JSON files
// in a directory — useful for tests and exporting).
type Checkpointer interface {
	Save(snap Snapshot) error
	Load(id string) (Snapshot, error)
	List() ([]Snapshot, error)
	Delete(id string) error
}

// StoreCheckpointer persists snapshots to a SQLite store.  It is the
// production implementation.
type StoreCheckpointer struct {
	mu sync.Mutex
	st *store.Store
}

// NewStoreCheckpointer wraps an already-open *store.Store.
func NewStoreCheckpointer(st *store.Store) *StoreCheckpointer {
	return &StoreCheckpointer{st: st}
}

// Save persists a snapshot: writes the session row, then appends every
// message.  Existing messages with the same role+content+timestamp
// triple are deduplicated so re-saving a snapshot is idempotent.
// Atomicity: each individual INSERT is atomic at the SQLite level; the
// whole snapshot is not transactional, but the helper returns the
// first error and stops further writes.
//
// When snap.ID is empty, a fresh UUID is generated; callers that
// need the post-Save id should call Load or List afterward.
func (c *StoreCheckpointer) Save(snap Snapshot) error {
	if snap.ID == "" {
		snap.ID = uuid.NewString()
	}
	if snap.Mode == "" {
		snap.Mode = "universal"
	}
	if snap.Version == 0 {
		snap.Version = SnapshotVersion
	}
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now().UTC()
	}
	snap.UpdatedAt = time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Upsert session: if it exists, refresh mutable fields.
	existing, err := c.st.GetSession(snap.ID)
	if err != nil {
		// Not found → create.
		if _, cerr := c.st.CreateSession(store.Session{
			ID:       snap.ID,
			Mode:     snap.Mode,
			Model:    snap.Model,
			Title:    snap.Title,
			ParentID: snap.ParentID,
		}); cerr != nil {
			return fmt.Errorf("create session: %w", cerr)
		}
	} else {
		_ = existing
		if _, uerr := c.st.DB().Exec(`
            UPDATE sessions SET mode = ?, model = ?, title = ?, parent_id = ?, updated_at = CURRENT_TIMESTAMP
            WHERE id = ?`,
			snap.Mode, snap.Model, snap.Title, nullableString(snap.ParentID), snap.ID); uerr != nil {
			return fmt.Errorf("update session: %w", uerr)
		}
	}

	// Append messages.  Use Clear+Append to keep the snapshot
	// authoritative — the snapshot represents the full desired state.
	if err := c.st.ClearSessionMessages(snap.ID); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}
	for _, m := range snap.Messages {
		_, err := c.st.AppendSessionMessage(store.SessionMessage{
			SessionID: snap.ID,
			Role:      m.Role,
			Content:   m.Content,
			ToolCalls: m.ToolCalls,
		})
		if err != nil {
			return fmt.Errorf("append message: %w", err)
		}
	}
	return nil
}

// Load fetches a snapshot by id.  The on-disk rows are re-hydrated
// into the JSON-friendly Snapshot shape.
func (c *StoreCheckpointer) Load(id string) (Snapshot, error) {
	sess, err := c.st.GetSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	msgs, err := c.st.ListSessionMessages(id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list messages: %w", err)
	}
	snap := Snapshot{
		Version:   SnapshotVersion,
		ID:        sess.ID,
		Mode:      sess.Mode,
		Model:     sess.Model,
		Title:     sess.Title,
		ParentID:  sess.ParentID,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
		Messages:  make([]SnapshotMessage, 0, len(msgs)),
	}
	for _, m := range msgs {
		snap.Messages = append(snap.Messages, SnapshotMessage{
			Role:      m.Role,
			Content:   m.Content,
			ToolCalls: m.ToolCalls,
			CreatedAt: m.CreatedAt,
		})
	}
	return snap, nil
}

// List returns one snapshot per session, ordered by most recently
// updated.
func (c *StoreCheckpointer) List() ([]Snapshot, error) {
	sessions, err := c.st.ListSessions()
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(sessions))
	for _, sess := range sessions {
		msgs, err := c.st.ListSessionMessages(sess.ID)
		if err != nil {
			return nil, err
		}
		snap := Snapshot{
			Version:   SnapshotVersion,
			ID:        sess.ID,
			Mode:      sess.Mode,
			Model:     sess.Model,
			Title:     sess.Title,
			ParentID:  sess.ParentID,
			CreatedAt: sess.CreatedAt,
			UpdatedAt: sess.UpdatedAt,
			Messages:  make([]SnapshotMessage, 0, len(msgs)),
		}
		for _, m := range msgs {
			snap.Messages = append(snap.Messages, SnapshotMessage{
				Role:      m.Role,
				Content:   m.Content,
				ToolCalls: m.ToolCalls,
				CreatedAt: m.CreatedAt,
			})
		}
		out = append(out, snap)
	}
	return out, nil
}

// Delete removes a session and all its messages.
func (c *StoreCheckpointer) Delete(id string) error {
	return c.st.DeleteSession(id)
}

// FileCheckpointer persists snapshots as individual JSON files in a
// directory.  Useful for tests, exports, and the `jito resume`
// command's `--from-file` mode.
type FileCheckpointer struct {
	mu  sync.Mutex
	dir string
}

// NewFileCheckpointer creates a file-based checkpointer rooted at
// dir; the directory is created if missing.
func NewFileCheckpointer(dir string) (*FileCheckpointer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return &FileCheckpointer{dir: dir}, nil
}

func (f *FileCheckpointer) path(id string) string {
	safe := filepath.Base(id)
	return filepath.Join(f.dir, safe+".json")
}

// Save writes the snapshot atomically (write to temp, rename).
func (f *FileCheckpointer) Save(snap Snapshot) error {
	if snap.ID == "" {
		return fmt.Errorf("snapshot id required")
	}
	if snap.Version == 0 {
		snap.Version = SnapshotVersion
	}
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now().UTC()
	}
	snap.UpdatedAt = time.Now().UTC()

	f.mu.Lock()
	defer f.mu.Unlock()

	finalPath := f.path(snap.ID)
	tmp, err := os.CreateTemp(f.dir, ".snap-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&snap); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Load reads a snapshot from disk.
func (f *FileCheckpointer) Load(id string) (Snapshot, error) {
	data, err := os.ReadFile(f.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, fmt.Errorf("snapshot %q not found", id)
		}
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("decode %s: %w", id, err)
	}
	return snap, nil
}

// List returns every snapshot in the directory.
func (f *FileCheckpointer) List() ([]Snapshot, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-len(".json")]
		snap, err := f.Load(id)
		if err != nil {
			continue // skip malformed files rather than fail the whole list
		}
		out = append(out, snap)
	}
	return out, nil
}

// Delete removes the snapshot file.
func (f *FileCheckpointer) Delete(id string) error {
	err := os.Remove(f.path(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}