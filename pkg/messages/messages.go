package messages

import (
	"time"

	"github.com/arjungandhi/pkg/config"
)

// Message represents a communication event with a contact
type Message struct {
	// Message identification
	ID string `json:"id"` // Unique identifier for the message

	// Message details
	ContactUID      string    `json:"contact_uid"`      // UID of the contact this message is with
	Timestamp       time.Time `json:"timestamp"`        // When the message was sent/received
	SenderUID       string    `json:"sender_uid"`       // UID of the sender
	ConversationUID string    `json:"conversation_uid"` // UID of the conversation thread
	Content         string    `json:"contant"`
}

type MessageManager struct {
	provider MessageProvider
	config   config.Config
	messages []Message
}

type MessageProvider interface {
	FetchMessages() ([]Message, error)
}

func NewMessageManager(provider MessageProvider, config config.Config) *MessageManager {
	return &MessageManager{provider: provider, config: config}
}

func (mm *MessageManager) LoadMessages() error {
	// read messages from disk
	return nil
}

func (mm *MessageManager) SaveMessages() error {
	// write messages to disk
	return nil
}

func (mm *MessageManager) SyncMessages() error {
	// sync messages with provider
	return nil
}

func (mm *MessageManager) AddMessage(message Message) {
	// add message to list
}

func (mm *MessageManager) GetMessagesForContact(contactUID string) []Message {
	// get all messages for a specific contact
	return nil
}

func (mm *MessageManager) GetLastContactDate(contactUID string) *time.Time {
	// get the most recent message timestamp for a contact
	return nil
}
