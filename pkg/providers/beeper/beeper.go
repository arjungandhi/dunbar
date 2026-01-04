package beeper

import (
	"context"
	"fmt"
	"os"
	"time"

	beeperapi "github.com/beeper/desktop-api-go"
	"github.com/beeper/desktop-api-go/option"
	"github.com/arjungandhi/dunbar/pkg/messages"
)

// BeeperProvider implements the MessageProvider interface for Beeper Desktop API
type BeeperProvider struct {
	client      *beeperapi.Client
	accessToken string
	accountIDs  []string // Optional filter for specific accounts
}

// Config holds configuration for the Beeper provider
type Config struct {
	AccessToken string   // Beeper Desktop API access token
	AccountIDs  []string // Optional: specific account IDs to fetch messages from
}

// NewBeeperProvider creates a new Beeper message provider
func NewBeeperProvider(cfg Config) (*BeeperProvider, error) {
	// Use provided token or fall back to environment variable
	token := cfg.AccessToken
	if token == "" {
		token = os.Getenv("BEEPER_ACCESS_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("BEEPER_ACCESS_TOKEN not set")
		}
	}

	// Initialize Beeper API client
	client := beeperapi.NewClient(
		option.WithAccessToken(token),
	)

	return &BeeperProvider{
		client:      client,
		accessToken: token,
		accountIDs:  cfg.AccountIDs,
	}, nil
}

// FetchMessages retrieves messages from Beeper Desktop API
func (bp *BeeperProvider) FetchMessages() ([]messages.Message, error) {
	ctx := context.Background()
	var allMessages []messages.Message

	// If no specific account IDs provided, we'll fetch from all accounts
	accountIDs := bp.accountIDs
	if len(accountIDs) == 0 {
		// List all accounts
		accounts, err := bp.client.Accounts.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list accounts: %w", err)
		}

		for _, account := range accounts.Items {
			accountIDs = append(accountIDs, account.ID)
		}
	}

	// Fetch messages from each account
	for _, accountID := range accountIDs {
		// Use auto-paging to fetch all messages
		iter := bp.client.Messages.ListAutoPaging(ctx, beeperapi.MessageListParams{
			AccountID: beeperapi.String(accountID),
			Limit:     beeperapi.Int(100), // Fetch 100 messages per page
		})

		for iter.Next() {
			msg := iter.Current()

			// Convert Beeper message to our Message struct
			dunbarMsg := messages.Message{
				ID:              msg.ID,
				ContactUID:      msg.SenderID, // Map sender ID to contact UID
				Timestamp:       time.UnixMilli(msg.Timestamp),
				SenderUID:       msg.SenderID,
				ConversationUID: msg.ChatID,
				Content:         msg.Text,
			}

			allMessages = append(allMessages, dunbarMsg)
		}

		if iter.Err() != nil {
			return nil, fmt.Errorf("failed to fetch messages for account %s: %w", accountID, iter.Err())
		}
	}

	return allMessages, nil
}

// FetchMessagesForContact retrieves messages for a specific contact
func (bp *BeeperProvider) FetchMessagesForContact(contactUID string) ([]messages.Message, error) {
	ctx := context.Background()
	var contactMessages []messages.Message

	// Search for messages from this specific contact
	iter := bp.client.Messages.SearchAutoPaging(ctx, beeperapi.MessageSearchParams{
		AccountIDs: bp.accountIDs,
		Limit:      beeperapi.Int(100),
		// Note: The search might need to be done differently depending on API capabilities
	})

	for iter.Next() {
		msg := iter.Current()

		// Filter by contact UID
		if msg.SenderID == contactUID {
			dunbarMsg := messages.Message{
				ID:              msg.ID,
				ContactUID:      msg.SenderID,
				Timestamp:       time.UnixMilli(msg.Timestamp),
				SenderUID:       msg.SenderID,
				ConversationUID: msg.ChatID,
				Content:         msg.Text,
			}

			contactMessages = append(contactMessages, dunbarMsg)
		}
	}

	if iter.Err() != nil {
		return nil, fmt.Errorf("failed to search messages: %w", iter.Err())
	}

	return contactMessages, nil
}

// GetChats retrieves all chats from Beeper
func (bp *BeeperProvider) GetChats() ([]Chat, error) {
	ctx := context.Background()

	chats, err := bp.client.Chats.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list chats: %w", err)
	}

	var result []Chat
	for _, chat := range chats.Items {
		result = append(result, Chat{
			ID:   chat.ID,
			Name: chat.Name,
		})
	}

	return result, nil
}

// Chat represents a Beeper conversation
type Chat struct {
	ID   string
	Name string
}
