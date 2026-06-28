// Package store provides SQLite-backed conversation history,
// session checkpoints, and audit log persistence.
//
// The schema covers three concerns:
//
//   - conversation + messages: backward-compatible with LOOP #1.
//   - sessions + session_messages: LOOP #3 session checkpoint/resume,
//     with tool_calls JSON column and parent_id for session trees.
//   - audit_log: append-only security log of every shell-tool
//     decision, with redacted args.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Message is a single chat message.
type Message struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"` // user | assistant | system
	Content   string    `json:"content"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
}

// --- LOOP #3 additions: Session, SessionMessage, AuditEntry ---

// Session is a single resumable chat session.  The combination of
// mode + model + history defines a conversation that can be
// checkpointed to disk and replayed later.
type Session struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	Model     string    `json:"model"`
	Title     string    `json:"title"`
	ParentID  string    `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionMessage is a single chat message belonging to a session.
// ToolCalls is a JSON-encoded list (may be empty) of structured tool
// invocations produced by the LLM.
type SessionMessage struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolCalls string    `json:"tool_calls"` // JSON array string
	CreatedAt time.Time `json:"created_at"`
}

// AuditEntry is one row of the security audit log.  ArgsRedacted
// stores the (sanitized) command line; sensitive substrings (api keys,
// tokens, etc.) have already been masked before persistence.
type AuditEntry struct {
	ID           int64     `json:"id"`
	Timestamp    time.Time `json:"ts"`
	SessionID    string    `json:"session_id"`
	Command      string    `json:"command"`
	Decision     string    `json:"decision"` // allow | deny | prompt
	Reason       string    `json:"reason"`
	ArgsRedacted string    `json:"args_redacted"`
}

// schemaDDL is the full schema applied on Open().  Every CREATE
// statement uses IF NOT EXISTS so existing databases are upgraded
// in place.  New tables added in LOOP #3 are at the end.
const schemaDDL = `
CREATE TABLE IF NOT EXISTS conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'universal',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id)
);
CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id, id);

-- LOOP #3 ------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL DEFAULT 'universal',
    model TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    parent_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);

CREATE TABLE IF NOT EXISTS session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_calls TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_session_messages ON session_messages(session_id, id);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    session_id TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    args_redacted TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_log(session_id, ts DESC);
`

// Conversation holds messages in a single SQLite conversation
// (LOOP #1 API, preserved for backward compatibility).  Internally it
// shares a single *sql.DB across all conversations and sessions in
// the same file.
type Conversation struct {
	db   *sql.DB
	id   int64
	name string
}

// Store is the unified SQLite handle for LOOP #3 callers
// (sessions, audit log, full checkpoint API).  It is safe for
// concurrent use; the embedded RWMutex serializes writes and the
// in-memory caches.  Modernc.org/sqlite is already serialized at
// the driver level; the explicit lock documents the contract.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// rawDB returns the underlying *sql.DB for code paths that need to
// run custom SQL without going through Store helpers (used by the
// legacy Conversation methods).
func (s *Store) rawDB() *sql.DB { return s.db }

// DB returns the underlying *sql.DB so callers (tests, audit writer)
// can run custom queries.  Schema mutations from outside Open are
// strongly discouraged.
func (s *Store) DB() *sql.DB { return s.db }

// Open opens (or creates) a SQLite-backed conversation store at
// path.  If path is empty, defaults to ~/.jito/store.db.  Returns a
// *Conversation wrapping the "default" conversation for backward
// compatibility with the LOOP #1 API.  All tables from LOOP #1 and
// LOOP #3 are created if missing.
//
// For the full session/audit API, call OpenStore instead.
func Open(path string) (*Conversation, error) {
	st, err := OpenStore(path)
	if err != nil {
		return nil, err
	}
	return st.DefaultConversation()
}

// OpenStore opens (or creates) a SQLite store and returns the full
// *Store.  All session, message, and audit operations hang off this
// value.
func OpenStore(path string) (*Store, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".jito", "store.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the SQLite handle.
func (s *Store) Close() error { return s.db.Close() }

// DefaultConversation returns the singleton "default" conversation
// for callers that still use the LOOP #1 API.  It auto-inserts the
// row if missing.
func (s *Store) DefaultConversation() (*Conversation, error) {
	const name = "default"
	res, err := s.db.Exec("INSERT OR IGNORE INTO conversations (name) VALUES (?)", name)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		_ = s.db.QueryRow("SELECT id FROM conversations WHERE name = ?", name).Scan(&id)
	}
	return &Conversation{db: s.db, id: id, name: name}, nil
}

// ConversationMessages returns all messages in the given conversation
// id, oldest first.
func (s *Store) ConversationMessages(convID int64) ([]Message, error) {
	rows, err := s.db.Query(`
        SELECT id, role, content, mode, created_at
        FROM messages WHERE conversation_id = ?
        ORDER BY id ASC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.Mode, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// AppendMessage writes a message into the named conversation.
func (s *Store) AppendMessage(convID int64, m Message) error {
	_, err := s.db.Exec(`
        INSERT INTO messages (conversation_id, role, content, mode)
        VALUES (?, ?, ?, ?)`,
		convID, m.Role, m.Content, m.Mode)
	return err
}

// --- LOOP #1 backward-compatible Conversation API ------------------

// Messages returns all messages in this conversation.
func (c *Conversation) Messages() []Message {
	rows, err := c.db.Query(`
        SELECT id, role, content, mode, created_at
        FROM messages WHERE conversation_id = ?
        ORDER BY id ASC`, c.id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.Mode, &m.CreatedAt); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// Append adds a message to the conversation.
func (c *Conversation) Append(m Message) error {
	_, err := c.db.Exec(`
        INSERT INTO messages (conversation_id, role, content, mode)
        VALUES (?, ?, ?, ?)`,
		c.id, m.Role, m.Content, m.Mode)
	return err
}

// AppendMany adds multiple messages.
func (c *Conversation) AppendMany(msgs []Message) error {
	for _, m := range msgs {
		if err := c.Append(m); err != nil {
			return err
		}
	}
	return nil
}

// Clear removes all messages in the conversation.
func (c *Conversation) Clear() error {
	_, err := c.db.Exec("DELETE FROM messages WHERE conversation_id = ?", c.id)
	return err
}

// Close closes the underlying database.
func (c *Conversation) Close() error { return c.db.Close() }

// String renders messages for system prompt building.
func (c *Conversation) String() string {
	var sb strings.Builder
	for _, m := range c.Messages() {
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.Role, m.Content))
	}
	return sb.String()
}

// --- LOOP #3 Session API --------------------------------------------

// CreateSession inserts a new session row and returns the populated
// Session (with timestamps refreshed from the DB defaults).
func (s *Store) CreateSession(sess Session) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.ID == "" {
		return Session{}, fmt.Errorf("session id required")
	}
	if sess.Mode == "" {
		sess.Mode = "universal"
	}
	_, err := s.db.Exec(`
        INSERT INTO sessions (id, mode, model, title, parent_id)
        VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.Mode, sess.Model, sess.Title, nullableString(sess.ParentID))
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return s.getSessionLocked(sess.ID)
}

// GetSession loads a session by id (with the read lock).
func (s *Store) GetSession(id string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getSessionLocked(id)
}

func (s *Store) getSessionLocked(id string) (Session, error) {
	row := s.db.QueryRow(`
        SELECT id, mode, model, title, COALESCE(parent_id, ''), created_at, updated_at
        FROM sessions WHERE id = ?`, id)
	var sess Session
	var parent sql.NullString
	if err := row.Scan(&sess.ID, &sess.Mode, &sess.Model, &sess.Title, &parent, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return Session{}, fmt.Errorf("session %q not found", id)
		}
		return Session{}, err
	}
	if parent.Valid {
		sess.ParentID = parent.String
	}
	return sess, nil
}

// TouchSession updates the updated_at timestamp on a session.
func (s *Store) TouchSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// ListSessions returns all sessions ordered by most recently updated.
func (s *Store) ListSessions() ([]Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`
        SELECT id, mode, model, title, COALESCE(parent_id, ''), created_at, updated_at
        FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var parent sql.NullString
		if err := rows.Scan(&sess.ID, &sess.Mode, &sess.Model, &sess.Title, &parent, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
		if parent.Valid {
			sess.ParentID = parent.String
		}
	}
	return out, nil
}

// DeleteSession removes a session and cascades to its messages.
func (s *Store) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}

// AppendSessionMessage inserts a message into a session and bumps
// the session's updated_at.
func (s *Store) AppendSessionMessage(msg SessionMessage) (SessionMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.SessionID == "" {
		return SessionMessage{}, fmt.Errorf("session_id required")
	}
	if msg.Role == "" {
		return SessionMessage{}, fmt.Errorf("role required")
	}
	res, err := s.db.Exec(`
        INSERT INTO session_messages (session_id, role, content, tool_calls)
        VALUES (?, ?, ?, ?)`,
		msg.SessionID, msg.Role, msg.Content, msg.ToolCalls)
	if err != nil {
		return SessionMessage{}, fmt.Errorf("append message: %w", err)
	}
	id, _ := res.LastInsertId()
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, msg.SessionID); err != nil {
		return SessionMessage{}, err
	}
	row := s.db.QueryRow(`SELECT created_at FROM session_messages WHERE id = ?`, id)
	var created time.Time
	_ = row.Scan(&created)
	return SessionMessage{
		ID:        id,
		SessionID: msg.SessionID,
		Role:      msg.Role,
		Content:   msg.Content,
		ToolCalls: msg.ToolCalls,
		CreatedAt: created,
	}, nil
}

// ListSessionMessages returns every message in a session, oldest first.
func (s *Store) ListSessionMessages(sessionID string) ([]SessionMessage, error) {
	rows, err := s.db.Query(`
        SELECT id, session_id, role, content, tool_calls, created_at
        FROM session_messages WHERE session_id = ?
        ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionMessage
	for rows.Next() {
		var m SessionMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.ToolCalls, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// ClearSessionMessages removes all messages from a session (keeps the
// session row).
func (s *Store) ClearSessionMessages(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM session_messages WHERE session_id = ?`, sessionID)
	return err
}

// --- LOOP #3 Audit API ----------------------------------------------

// AppendAudit writes an entry to the audit log.  The caller is
// responsible for redacting the args before calling.
func (s *Store) AppendAudit(entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Decision == "" {
		entry.Decision = "deny"
	}
	_, err := s.db.Exec(`
        INSERT INTO audit_log (session_id, command, decision, reason, args_redacted)
        VALUES (?, ?, ?, ?, ?)`,
		entry.SessionID, entry.Command, entry.Decision, entry.Reason, entry.ArgsRedacted)
	return err
}

// ListAudit returns audit entries for a session, newest first.  If
// sessionID is empty, returns entries across all sessions.
func (s *Store) ListAudit(sessionID string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if sessionID == "" {
		rows, err = s.db.Query(`
            SELECT id, ts, session_id, command, decision, reason, args_redacted
            FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`
            SELECT id, ts, session_id, command, decision, reason, args_redacted
            FROM audit_log WHERE session_id = ? ORDER BY id DESC LIMIT ?`, sessionID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.SessionID, &e.Command, &e.Decision, &e.Reason, &e.ArgsRedacted); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}