package messages

import (
	"context"
	"fmt"
	"os"

	beeperapi "github.com/beeper/desktop-api-go"
	"github.com/beeper/desktop-api-go/option"
)

// BeeperProvider implements the MessageProvider interface for Beeper Desktop API
type BeeperProvider struct {
	client      *beeperapi.Client
	accessToken string
}

// BeeperConfig holds configuration for the Beeper provider
type BeeperConfig struct {
	AccessToken string // Beeper Desktop API access token (optional, defaults to BEEPER_ACCESS_TOKEN env var)
}

// NewBeeperProvider creates a new Beeper message provider
func NewBeeperProvider(cfg BeeperConfig) (*BeeperProvider, error) {
	// Use provided token or fall back to environment variable
	token := cfg.AccessToken
	if token == "" {
		token = os.Getenv("BEEPER_ACCESS_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("BEEPER_ACCESS_TOKEN not set and no token provided in config")
		}
	}

	// Initialize Beeper API client
	client := beeperapi.NewClient(
		option.WithAccessToken(token),
	)

	return &BeeperProvider{
		client:      &client,
		accessToken: token,
	}, nil
}

// Sync fetches all conversations and messages from Beeper
func (p *BeeperProvider) Sync() ([]Conversation, []Message, error) {
	ctx := context.Background()

	// Fetch all chats/conversations
	chatsResp, err := p.client.Chats.List(ctx, beeperapi.ChatListParams{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list chats: %w", err)
	}

	var conversations []Conversation
	var allMessages []Message

	// Process each chat
	for _, chat := range chatsResp.Items {
		// Convert chat to Conversation
		conv := Conversation{
			ID:               chat.ID,
			AccountID:        chat.AccountID,
			Platform:         chat.Network,
			Title:            chat.Title,
			Type:             string(chat.Type),
			ParticipantUIDs:  extractParticipantUIDs(chat.Participants.Items),
			ParticipantCount: int(chat.Participants.Total),
			UnreadCount:      chat.UnreadCount,
			LastActivity:     chat.LastActivity,
			IsArchived:       chat.IsArchived,
			IsMuted:          chat.IsMuted,
			IsPinned:         chat.IsPinned,
		}
		conversations = append(conversations, conv)

		// Fetch messages for this chat
		messagesIter := p.client.Messages.ListAutoPaging(ctx, chat.ID, beeperapi.MessageListParams{})

		for messagesIter.Next() {
			msg := messagesIter.Current()

			// Convert Beeper message to Dunbar message
			dunbarMsg := Message{
				ID:              msg.ID,
				ContactUID:      msg.SenderID,
				Timestamp:       msg.Timestamp,
				SenderUID:       msg.SenderID,
				SenderName:      msg.SenderName,
				ConversationUID: msg.ChatID,
				ChatTitle:       chat.Title,
				Text:            msg.Text,
				Platform:        chat.Network,
				PlatformID:      msg.ID,
				IsSent:          msg.IsSender,
				Attachments:     convertAttachments(msg.Attachments),
				SortKey:         msg.SortKey,
			}

			allMessages = append(allMessages, dunbarMsg)
		}

		if messagesIter.Err() != nil {
			return nil, nil, fmt.Errorf("failed to fetch messages for chat %s: %w", chat.ID, messagesIter.Err())
		}
	}

	return conversations, allMessages, nil
}

// extractParticipantUIDs extracts user IDs from participant list
func extractParticipantUIDs(participants []beeperapi.User) []string {
	uids := make([]string, len(participants))
	for i, p := range participants {
		uids[i] = p.ID
	}
	return uids
}

// convertAttachments converts Beeper attachments to Dunbar attachments
func convertAttachments(beeperAttachments []beeperapi.Attachment) []Attachment {
	attachments := make([]Attachment, len(beeperAttachments))
	for i, a := range beeperAttachments {
		attachments[i] = Attachment{
			Type:        string(a.Type),
			SrcURL:      a.SrcURL,
			FileName:    a.FileName,
			FileSize:    a.FileSize,
			MimeType:    a.MimeType,
			Duration:    a.Duration,
			Width:       int(a.Size.Width),
			Height:      int(a.Size.Height),
			IsGif:       a.IsGif,
			IsSticker:   a.IsSticker,
			IsVoiceNote: a.IsVoiceNote,
		}
	}
	return attachments
}
