package messages

import (
	"time"

	"github.com/arjungandhi/pkg/config"
)

// Attachment represents a file attached to a message
type Attachment struct {
	Type        string  `json:"type"`          // "img", "video", "audio", "unknown"
	SrcURL      string  `json:"src_url"`       // URL or path to file
	FileName    string  `json:"file_name"`     // Original filename
	FileSize    float64 `json:"file_size"`     // Size in bytes
	MimeType    string  `json:"mime_type"`     // MIME type (e.g., 'image/png')
	Duration    float64 `json:"duration"`      // Duration in seconds (audio/video)
	Width       int     `json:"width"`         // Image/video width in pixels
	Height      int     `json:"height"`        // Image/video height in pixels
	IsGif       bool    `json:"is_gif"`        // True if GIF
	IsSticker   bool    `json:"is_sticker"`    // True if sticker
	IsVoiceNote bool    `json:"is_voice_note"` // True if voice note
}

// Conversation represents a chat or conversation thread
type Conversation struct {
	// Conversation identification
	ID        string `json:"id"`         // Unique conversation ID
	AccountID string `json:"account_id"` // Which account this belongs to
	Platform  string `json:"platform"`   // Platform name (WhatsApp, Telegram, etc.)

	// Conversation details
	Title string `json:"title"` // Display name/title of conversation
	Type  string `json:"type"`  // "single" for DMs, "group" for group chats

	// Participants
	ParticipantUIDs  []string `json:"participant_uids"`  // List of participant UIDs
	ParticipantCount int      `json:"participant_count"` // Total number of participants

	// Status
	UnreadCount  int64     `json:"unread_count"`  // Number of unread messages
	LastActivity time.Time `json:"last_activity"` // Last message timestamp

	// Settings
	IsArchived bool `json:"is_archived"` // True if archived
	IsMuted    bool `json:"is_muted"`    // True if muted
	IsPinned   bool `json:"is_pinned"`   // True if pinned
}

// Message represents a communication event with a contact
type Message struct {
	// Message identification
	ID string `json:"id"` // Unique identifier for the message

	// Message details
	ContactUID      string    `json:"contact_uid"`      // UID of the contact this message is with
	Timestamp       time.Time `json:"timestamp"`        // When the message was sent/received
	SenderUID       string    `json:"sender_uid"`       // UID of the sender
	SenderName      string    `json:"sender_name"`      // Display name of sender
	ConversationUID string    `json:"conversation_uid"` // UID of the conversation thread
	ChatTitle       string    `json:"chat_title"`       // Name of the conversation
	Text            string    `json:"content"`          // Message text content
	Platform        string    `json:"platform"`         // Platform used (WhatsApp, Telegram, etc.)
	PlatformID      string    `json:"platform_id"`      // ID on the platform

	// Message metadata
	IsSent      bool         `json:"is_sent"`     // True if you sent this message
	Attachments []Attachment `json:"attachments"` // Files, images, videos attached
	SortKey     string       `json:"sort_key"`    // Platform-specific sort key for ordering
}

type MessageManager struct {
	provider MessageProvider
	db       *DB
	config   config.Config
}

type MessageProvider interface {
	Sync() ([]Conversation, []Message, error)
}

func NewMessageManager(provider MessageProvider, db *DB, config config.Config) *MessageManager {
	return &MessageManager{
		provider: provider,
		db:       db,
		config:   config,
	}
}

// Sync fetches data from the provider and saves it to the database
func (mm *MessageManager) Sync() error {
	// Fetch from provider
	conversations, messages, err := mm.provider.Sync()
	if err != nil {
		return err
	}

	// Save conversations to database
	if err := mm.db.SaveConversations(conversations); err != nil {
		return err
	}

	// Save messages to database
	if err := mm.db.SaveMessages(messages); err != nil {
		return err
	}

	return nil
}

// Query methods that use the database

func (mm *MessageManager) GetMessagesForContact(contactUID string) ([]Message, error) {
	return mm.db.GetMessagesForContact(contactUID)
}

func (mm *MessageManager) GetLastContactDate(contactUID string) (*time.Time, error) {
	return mm.db.GetLastContactDate(contactUID)
}

func (mm *MessageManager) GetConversation(conversationUID string) (*Conversation, error) {
	return mm.db.GetConversation(conversationUID)
}

func (mm *MessageManager) GetConversationsForContact(contactUID string) ([]Conversation, error) {
	return mm.db.GetConversationsForContact(contactUID)
}
