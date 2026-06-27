// Package store provides SQLite-backed conversation history.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Conversation holds messages in a single SQLite session.
type Conversation struct {
	db   *sql.DB
	id   int64
	name string
}

// Open opens (or creates) a conversation store at the given path.
// If path is empty, uses ~/.jito/store.db.
func Open(path string) (*Conversation, error) {
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

	schema := `
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
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}

	// Default conversation
	name := "default"
	res, err := db.Exec("INSERT OR IGNORE INTO conversations (name) VALUES (?)", name)
	if err != nil {
		db.Close()
		return nil, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// existed, fetch id
		_ = db.QueryRow("SELECT id FROM conversations WHERE name = ?", name).Scan(&id)
	}

	return &Conversation{db: db, id: id, name: name}, nil
}

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

// Close closes the database.
func (c *Conversation) Close() error {
	return c.db.Close()
}

// String renders messages for system prompt building.
func (c *Conversation) String() string {
	var sb strings.Builder
	for _, m := range c.Messages() {
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.Role, m.Content))
	}
	return sb.String()
}