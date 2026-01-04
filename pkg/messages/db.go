package messages

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// DB wraps a SQLite database for storing messages and conversations
type DB struct {
	db *sql.DB
}

// OpenDB opens or creates a SQLite database at the given path
func OpenDB(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys and WAL mode for better performance
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	d := &DB{db: db}
	if err := d.createTables(); err != nil {
		return nil, err
	}

	return d, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}

// createTables creates the necessary tables if they don't exist
func (d *DB) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL,
		platform TEXT NOT NULL,
		title TEXT NOT NULL,
		type TEXT NOT NULL,
		participant_uids TEXT, -- JSON array
		participant_count INTEGER NOT NULL,
		unread_count INTEGER NOT NULL,
		last_activity INTEGER NOT NULL, -- Unix timestamp
		is_archived BOOLEAN NOT NULL DEFAULT 0,
		is_muted BOOLEAN NOT NULL DEFAULT 0,
		is_pinned BOOLEAN NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		contact_uid TEXT NOT NULL,
		timestamp INTEGER NOT NULL, -- Unix timestamp
		sender_uid TEXT NOT NULL,
		sender_name TEXT NOT NULL,
		conversation_uid TEXT NOT NULL,
		chat_title TEXT NOT NULL,
		content TEXT NOT NULL,
		platform TEXT NOT NULL,
		platform_id TEXT NOT NULL,
		is_sent BOOLEAN NOT NULL,
		attachments TEXT, -- JSON array
		sort_key TEXT NOT NULL,
		FOREIGN KEY (conversation_uid) REFERENCES conversations(id)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_uid);
	CREATE INDEX IF NOT EXISTS idx_messages_contact ON messages(contact_uid);
	CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender_uid);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	return nil
}

// SaveConversations upserts conversations into the database
func (d *DB) SaveConversations(conversations []Conversation) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO conversations (
			id, account_id, platform, title, type,
			participant_uids, participant_count,
			unread_count, last_activity,
			is_archived, is_muted, is_pinned
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, conv := range conversations {
		// Convert participant UIDs to JSON
		participantUIDs, err := json.Marshal(conv.ParticipantUIDs)
		if err != nil {
			return fmt.Errorf("failed to marshal participant UIDs: %w", err)
		}

		_, err = stmt.Exec(
			conv.ID,
			conv.AccountID,
			conv.Platform,
			conv.Title,
			conv.Type,
			string(participantUIDs),
			conv.ParticipantCount,
			conv.UnreadCount,
			conv.LastActivity.Unix(),
			conv.IsArchived,
			conv.IsMuted,
			conv.IsPinned,
		)
		if err != nil {
			return fmt.Errorf("failed to insert conversation %s: %w", conv.ID, err)
		}
	}

	return tx.Commit()
}

// SaveMessages inserts new messages into the database (ignores duplicates)
func (d *DB) SaveMessages(messages []Message) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO messages (
			id, contact_uid, timestamp, sender_uid, sender_name,
			conversation_uid, chat_title, content, platform, platform_id,
			is_sent, attachments, sort_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, msg := range messages {
		// Convert attachments to JSON
		attachmentsJSON, err := json.Marshal(msg.Attachments)
		if err != nil {
			return fmt.Errorf("failed to marshal attachments: %w", err)
		}

		_, err = stmt.Exec(
			msg.ID,
			msg.ContactUID,
			msg.Timestamp.Unix(),
			msg.SenderUID,
			msg.SenderName,
			msg.ConversationUID,
			msg.ChatTitle,
			msg.Text,
			msg.Platform,
			msg.PlatformID,
			msg.IsSent,
			string(attachmentsJSON),
			msg.SortKey,
		)
		if err != nil {
			return fmt.Errorf("failed to insert message %s: %w", msg.ID, err)
		}
	}

	return tx.Commit()
}

// GetMessagesForContact retrieves all messages for a specific contact
func (d *DB) GetMessagesForContact(contactUID string) ([]Message, error) {
	rows, err := d.db.Query(`
		SELECT id, contact_uid, timestamp, sender_uid, sender_name,
		       conversation_uid, chat_title, content, platform, platform_id,
		       is_sent, attachments, sort_key
		FROM messages
		WHERE contact_uid = ?
		ORDER BY timestamp DESC
	`, contactUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetLastContactDate returns the timestamp of the most recent message with a contact
func (d *DB) GetLastContactDate(contactUID string) (*time.Time, error) {
	var timestamp int64
	err := d.db.QueryRow(`
		SELECT MAX(timestamp)
		FROM messages
		WHERE contact_uid = ?
	`, contactUID).Scan(&timestamp)

	if err == sql.ErrNoRows || timestamp == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query last contact date: %w", err)
	}

	t := time.Unix(timestamp, 0)
	return &t, nil
}

// GetConversation retrieves a specific conversation by ID
func (d *DB) GetConversation(conversationUID string) (*Conversation, error) {
	var conv Conversation
	var participantUIDs string
	var lastActivityUnix int64

	err := d.db.QueryRow(`
		SELECT id, account_id, platform, title, type,
		       participant_uids, participant_count,
		       unread_count, last_activity,
		       is_archived, is_muted, is_pinned
		FROM conversations
		WHERE id = ?
	`, conversationUID).Scan(
		&conv.ID,
		&conv.AccountID,
		&conv.Platform,
		&conv.Title,
		&conv.Type,
		&participantUIDs,
		&conv.ParticipantCount,
		&conv.UnreadCount,
		&lastActivityUnix,
		&conv.IsArchived,
		&conv.IsMuted,
		&conv.IsPinned,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query conversation: %w", err)
	}

	// Parse participant UIDs from JSON
	if err := json.Unmarshal([]byte(participantUIDs), &conv.ParticipantUIDs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal participant UIDs: %w", err)
	}

	conv.LastActivity = time.Unix(lastActivityUnix, 0)

	return &conv, nil
}

// GetConversationsForContact retrieves all conversations that include a specific contact
func (d *DB) GetConversationsForContact(contactUID string) ([]Conversation, error) {
	rows, err := d.db.Query(`
		SELECT DISTINCT c.id, c.account_id, c.platform, c.title, c.type,
		       c.participant_uids, c.participant_count,
		       c.unread_count, c.last_activity,
		       c.is_archived, c.is_muted, c.is_pinned
		FROM conversations c
		WHERE c.participant_uids LIKE ?
	`, "%"+contactUID+"%") // Simple LIKE search in JSON array
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations: %w", err)
	}
	defer rows.Close()

	var conversations []Conversation
	for rows.Next() {
		var conv Conversation
		var participantUIDs string
		var lastActivityUnix int64

		err := rows.Scan(
			&conv.ID,
			&conv.AccountID,
			&conv.Platform,
			&conv.Title,
			&conv.Type,
			&participantUIDs,
			&conv.ParticipantCount,
			&conv.UnreadCount,
			&lastActivityUnix,
			&conv.IsArchived,
			&conv.IsMuted,
			&conv.IsPinned,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}

		// Parse participant UIDs
		if err := json.Unmarshal([]byte(participantUIDs), &conv.ParticipantUIDs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal participant UIDs: %w", err)
		}

		conv.LastActivity = time.Unix(lastActivityUnix, 0)
		conversations = append(conversations, conv)
	}

	return conversations, rows.Err()
}

// scanMessages is a helper to scan message rows
func scanMessages(rows *sql.Rows) ([]Message, error) {
	var messages []Message
	for rows.Next() {
		var msg Message
		var timestampUnix int64
		var attachmentsJSON string

		err := rows.Scan(
			&msg.ID,
			&msg.ContactUID,
			&timestampUnix,
			&msg.SenderUID,
			&msg.SenderName,
			&msg.ConversationUID,
			&msg.ChatTitle,
			&msg.Text,
			&msg.Platform,
			&msg.PlatformID,
			&msg.IsSent,
			&attachmentsJSON,
			&msg.SortKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		// Parse timestamp
		msg.Timestamp = time.Unix(timestampUnix, 0)

		// Parse attachments
		if attachmentsJSON != "" {
			if err := json.Unmarshal([]byte(attachmentsJSON), &msg.Attachments); err != nil {
				return nil, fmt.Errorf("failed to unmarshal attachments: %w", err)
			}
		}

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}
