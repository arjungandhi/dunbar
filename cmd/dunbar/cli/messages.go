package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/arjungandhi/dunbar/pkg/config"
	"github.com/arjungandhi/dunbar/pkg/messages"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	Z "github.com/rwxrob/bonzai/z"
	"github.com/rwxrob/help"
)

var Messages = &Z.Cmd{
	Name:     "messages",
	Summary:  "Manage your messages and conversations",
	Commands: []*Z.Cmd{help.Cmd, MessagesInit, MessagesList, MessagesSync},
	Call: func(x *Z.Cmd, args ...string) error {
		// Default action: open TUI
		return runMessagesTUI(x, args...)
	},
}

var MessagesInit = &Z.Cmd{
	Name:    "init",
	Summary: "Initialize messages provider",
	Call: func(x *Z.Cmd, args ...string) error {
		cfg := config.New()
		if err := cfg.EnsureDunbarDir(); err != nil {
			return fmt.Errorf("failed to create dunbar directory: %w", err)
		}

		// Run provider selection in Bubble Tea
		m := newMessageProviderSelectModel()
		p := tea.NewProgram(m)
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("provider selection failed: %w", err)
		}

		providerModel := result.(messageProviderSelectModel)
		if providerModel.cancelled {
			return fmt.Errorf("initialization cancelled")
		}

		providerType := providerModel.selectedProvider

		// Initialize the selected provider
		switch providerType {
		case "beeper":
			return initBeeperProvider(cfg)
		default:
			return fmt.Errorf("unsupported provider: %s", providerType)
		}
	},
}

// Message provider selection model
type messageProviderSelectModel struct {
	providers        []string
	cursor           int
	selectedProvider string
	cancelled        bool
}

func newMessageProviderSelectModel() messageProviderSelectModel {
	return messageProviderSelectModel{
		providers: []string{"beeper"},
		cursor:    0,
	}
}

func (m messageProviderSelectModel) Init() tea.Cmd {
	return nil
}

func (m messageProviderSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.providers)-1 {
				m.cursor++
			}

		case "enter":
			m.selectedProvider = m.providers[m.cursor]
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m messageProviderSelectModel) View() string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	sb.WriteString(titleStyle.Render("Select a messages provider:"))
	sb.WriteString("\n\n")

	normalStyle := lipgloss.NewStyle()
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))

	providerNames := map[string]string{
		"beeper": "Beeper (Multi-platform messaging)",
	}

	for i, provider := range m.providers {
		cursor := " "
		style := normalStyle

		if i == m.cursor {
			cursor = ">"
			style = selectedStyle
		}

		sb.WriteString(style.Render(fmt.Sprintf("%s %s\n", cursor, providerNames[provider])))
	}

	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString("\n")
	sb.WriteString(footerStyle.Render("j/k: navigate • enter: select • q: cancel"))

	return sb.String()
}

func initBeeperProvider(cfg *config.Config) error {
	// Create Beeper provider
	provider, err := messages.NewBeeperProvider(cfg.DunbarDir)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Check if credentials already exist
	existingCreds, _ := provider.LoadCredentials()
	hasExistingCreds := existingCreds != nil && existingCreds.AccessToken != ""

	var deleteExisting bool
	if hasExistingCreds {
		// Ask if user wants to delete existing credentials
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Existing credentials found").
					Description("Delete and enter new access token?").
					Affirmative("Yes, delete").
					Negative("No, keep existing").
					Value(&deleteExisting),
			),
		)

		if err := form.Run(); err != nil {
			return fmt.Errorf("prompt failed: %w", err)
		}

		// If keeping existing creds, just verify they work
		if !deleteExisting {
			fmt.Println("Keeping existing credentials.")
			fmt.Println("Run 'dunbar messages sync' to sync your messages.")
			return nil
		}
	}

	// Prompt for Access Token using huh
	var accessToken string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Beeper Setup").
				Description("To use Beeper, you need an access token.\n\n" +
					"Setup steps:\n" +
					"1. Open Beeper Desktop\n" +
					"2. Go to Settings > Developer\n" +
					"3. Copy your Access Token"),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Access Token").
				Value(&accessToken).
				Password(true).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("access token cannot be empty")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("setup cancelled: %w", err)
	}

	// Save credentials
	creds := &messages.BeeperCredentials{
		AccessToken: strings.TrimSpace(accessToken),
	}
	if err := provider.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	// Initialize provider with new credentials
	if err := provider.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Test the connection by attempting a sync
	fmt.Println("\nTesting connection to Beeper...")
	_, _, err = provider.Sync()
	if err != nil {
		return fmt.Errorf("failed to connect to Beeper: %w", err)
	}

	fmt.Println("✓ Beeper provider initialized successfully!")
	fmt.Println("Run 'dunbar messages sync' to sync your messages.")

	return nil
}

var MessagesList = &Z.Cmd{
	Name:    "list",
	Summary: "List all conversations",
	Call: func(x *Z.Cmd, args ...string) error {
		cfg := config.New()
		mm, err := getMessageManager(cfg)
		if err != nil {
			return err
		}
		defer mm.Close()

		// Get all conversations from the database
		conversations, err := getAllConversations(mm)
		if err != nil {
			return fmt.Errorf("failed to list conversations: %w", err)
		}

		// Output in a bash-friendly format: one conversation per line
		// Format: ID|Title|Platform|ParticipantCount|UnreadCount|LastActivity
		for _, conv := range conversations {
			fmt.Printf("%s|%s|%s|%d|%d|%s\n",
				conv.ID,
				conv.Title,
				conv.Platform,
				conv.ParticipantCount,
				conv.UnreadCount,
				conv.LastActivity.Format(time.RFC3339),
			)
		}

		return nil
	},
}

var MessagesSync = &Z.Cmd{
	Name:    "sync",
	Summary: "Sync messages with Beeper",
	Call: func(x *Z.Cmd, args ...string) error {
		cfg := config.New()
		mm, err := getMessageManager(cfg)
		if err != nil {
			return err
		}
		defer mm.Close()

		// Sync will print its own progress
		if err := mm.Sync(); err != nil {
			return fmt.Errorf("failed to sync messages: %w", err)
		}

		return nil
	},
}

// Helper function to get or create MessageManager
func getMessageManager(cfg *config.Config) (*messages.MessageManager, error) {
	if err := cfg.EnsureDunbarDir(); err != nil {
		return nil, fmt.Errorf("failed to create dunbar directory: %w", err)
	}

	// Create Beeper provider
	provider, err := messages.NewBeeperProvider(cfg.DunbarDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create Beeper provider: %w", err)
	}

	// Initialize provider (loads credentials from file)
	if err := provider.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize provider: %w. Run 'dunbar messages init' first", err)
	}

	// Create MessageManager
	return messages.NewMessageManager(provider, *cfg)
}

// getAllConversations gets all conversations from the database
func getAllConversations(mm *messages.MessageManager) ([]messages.Conversation, error) {
	return mm.ListAllConversations()
}

// TUI implementation
func runMessagesTUI(x *Z.Cmd, args ...string) error {
	cfg := config.New()
	mm, err := getMessageManager(cfg)
	if err != nil {
		return err
	}
	defer mm.Close()

	conversations, err := getAllConversations(mm)
	if err != nil {
		return fmt.Errorf("failed to list conversations: %w", err)
	}

	m := newMessagesModel(conversations, mm)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

// Bubble Tea model for messages TUI
type messagesModel struct {
	conversations    []messages.Conversation
	cursor           int
	viewportTop      int
	height           int
	width            int
	mm               *messages.MessageManager
	viewMode         string // "conversations" or "messages"
	selectedConvID   string
	messages         []messages.Message
	messagesCursor   int
	messagesViewTop  int
	confirmingDelete bool
	deleteConvID     string
}

// DateSeparator represents a date divider in message list
type DateSeparator struct {
	Text string
	Date time.Time
}

// displayItem is a union type for messages and date separators
type displayItem struct {
	message       *messages.Message
	dateSeparator *DateSeparator
}

func (d displayItem) isMessage() bool {
	return d.message != nil
}

func (d displayItem) isSeparator() bool {
	return d.dateSeparator != nil
}

func newMessagesModel(conversations []messages.Conversation, mm *messages.MessageManager) messagesModel {
	// Sort conversations by last activity (most recent first)
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].LastActivity.After(conversations[j].LastActivity)
	})

	return messagesModel{
		conversations:    conversations,
		cursor:           0,
		viewportTop:      0,
		height:           25,
		width:            80,
		mm:               mm,
		viewMode:         "conversations",
		confirmingDelete: false,
		deleteConvID:     "",
	}
}

func (m messagesModel) Init() tea.Cmd {
	return nil
}

func (m messagesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height - 3
		m.width = msg.Width

	case tea.KeyMsg:
		// Handle delete confirmation
		if m.confirmingDelete {
			switch msg.String() {
			case "y", "Y":
				// For now, we don't actually delete from database
				// Just remove from local list
				for i, c := range m.conversations {
					if c.ID == m.deleteConvID {
						m.conversations = append(m.conversations[:i], m.conversations[i+1:]...)
						break
					}
				}
				if m.cursor >= len(m.conversations) && len(m.conversations) > 0 {
					m.cursor = len(m.conversations) - 1
				}
				m.confirmingDelete = false
				m.deleteConvID = ""
				return m, nil

			case "n", "N", "esc":
				m.confirmingDelete = false
				m.deleteConvID = ""
				return m, nil
			}
			return m, nil
		}

		// Mode-specific key handling
		if m.viewMode == "messages" {
			switch msg.String() {
			case "q", "esc":
				// Go back to conversations view
				m.viewMode = "conversations"
				m.messages = nil
				m.messagesCursor = 0
				m.messagesViewTop = 0
				return m, nil

			case "up", "k":
				if m.messagesCursor > 0 {
					m.messagesCursor--
					if m.messagesCursor < m.messagesViewTop {
						m.messagesViewTop = m.messagesCursor
					}
				}

			case "down", "j":
				if m.messagesCursor < len(m.messages)-1 {
					m.messagesCursor++
					// Calculate exactly how many messages fit in viewport
					availableHeight := max(1, m.height-4)
					visibleMessages := calculateVisibleMessageCount(m.messages, m.messagesViewTop, m.width-4, availableHeight)

					if m.messagesCursor >= m.messagesViewTop+visibleMessages {
						m.messagesViewTop++
					}
				}

			case "g", "home":
				m.messagesCursor = 0
				m.messagesViewTop = 0

			case "G", "end":
				m.messagesCursor = len(m.messages) - 1
				// Calculate exact visible messages and position viewport at the end
				availableHeight := max(1, m.height-4)
				// Try different starting positions to find where the last message is visible
				for startIdx := len(m.messages) - 1; startIdx >= 0; startIdx-- {
					visibleCount := calculateVisibleMessageCount(m.messages, startIdx, m.width-4, availableHeight)
					if startIdx+visibleCount >= len(m.messages) {
						m.messagesViewTop = startIdx
						break
					}
				}
			}
		} else {
			// Conversations view
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit

			case "d":
				if len(m.conversations) > 0 && m.cursor < len(m.conversations) {
					m.confirmingDelete = true
					m.deleteConvID = m.conversations[m.cursor].ID
				}

			case "enter":
				// View messages for selected conversation
				if m.cursor < len(m.conversations) {
					conv := m.conversations[m.cursor]
					m.viewMode = "messages"
					m.selectedConvID = conv.ID

					// Load messages for this conversation
					msgs, err := m.mm.GetMessagesForConversation(conv.ID)
					if err == nil {
						m.messages = msgs
					} else {
						m.messages = []messages.Message{}
					}
					m.messagesCursor = 0
					m.messagesViewTop = 0
				}

			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.viewportTop {
						m.viewportTop = m.cursor
					}
				}

			case "down", "j":
				if m.cursor < len(m.conversations)-1 {
					m.cursor++
					if m.cursor >= m.viewportTop+m.height {
						m.viewportTop = m.cursor - m.height + 1
					}
				}

			case "g", "home":
				m.cursor = 0
				m.viewportTop = 0

			case "G", "end":
				m.cursor = len(m.conversations) - 1
				m.viewportTop = max(0, len(m.conversations)-m.height)

			case "pgup":
				m.cursor = max(0, m.cursor-m.height)
				m.viewportTop = max(0, m.viewportTop-m.height)

			case "pgdown":
				m.cursor = min(len(m.conversations)-1, m.cursor+m.height)
				m.viewportTop = min(max(0, len(m.conversations)-m.height), m.viewportTop+m.height)
			}
		}
	}

	return m, nil
}

func (m messagesModel) View() string {
	if m.viewMode == "messages" {
		return m.renderMessagesView()
	}

	if len(m.conversations) == 0 {
		return "No conversations found. Run 'dunbar messages sync' to sync your messages.\n\nPress 'q' to quit."
	}

	// Show delete confirmation dialog
	if m.confirmingDelete {
		var conv messages.Conversation
		for _, c := range m.conversations {
			if c.ID == m.deleteConvID {
				conv = c
				break
			}
		}

		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")).
			Padding(0, 1)

		nameStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Padding(0, 1)

		buttonStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(0, 1)

		yesButtonStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("46")).
			Background(lipgloss.Color("22")).
			Padding(0, 2)

		noButtonStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")).
			Background(lipgloss.Color("52")).
			Padding(0, 2)

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 2).
			Width(60)

		var dialogContent strings.Builder
		dialogContent.WriteString(titleStyle.Render("⚠️  Delete Conversation?"))
		dialogContent.WriteString("\n\n")
		dialogContent.WriteString("Are you sure you want to delete:\n")
		dialogContent.WriteString(nameStyle.Render(conv.Title))
		dialogContent.WriteString("\n\n")
		dialogContent.WriteString(buttonStyle.Render("This action cannot be undone."))
		dialogContent.WriteString("\n\n\n")
		dialogContent.WriteString(yesButtonStyle.Render("Y") + "  " + noButtonStyle.Render("N"))

		dialog := boxStyle.Render(dialogContent.String())

		return lipgloss.Place(m.width, m.height+3,
			lipgloss.Center, lipgloss.Center,
			dialog)
	}

	return m.renderConversationsView()
}

func (m messagesModel) renderConversationsView() string {
	leftWidth := max(40, m.width*2/5)

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	normalStyle := lipgloss.NewStyle()
	selectedStyle := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("240"))
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Build left pane (conversation list)
	var leftPane strings.Builder
	leftPane.WriteString(headerStyle.Render(fmt.Sprintf("Conversations (%d)", len(m.conversations))))
	leftPane.WriteString("\n")

	end := min(m.viewportTop+m.height, len(m.conversations))

	for i := m.viewportTop; i < end; i++ {
		conv := m.conversations[i]
		style := normalStyle

		if i == m.cursor {
			style = selectedStyle
		}

		// Format: [Platform] Title (unread)
		label := fmt.Sprintf("[%s] %s", conv.Platform, conv.Title)
		if conv.UnreadCount > 0 {
			label += fmt.Sprintf(" (%d)", conv.UnreadCount)
		}

		line := fmt.Sprintf(" %s", truncate(label, leftWidth-2))
		leftPane.WriteString(style.Render(line))
		leftPane.WriteString("\n")
	}

	// Build right pane (conversation details)
	var rightPane strings.Builder
	if m.cursor < len(m.conversations) {
		conv := m.conversations[m.cursor]

		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

		fieldLabelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

		dividerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

		divider := dividerStyle.Render("─────────────────────────────────")

		// Title with platform and time info
		platformInfo := fmt.Sprintf("[%s]", conv.Platform)
		if conv.UnreadCount > 0 {
			platformInfo += fmt.Sprintf(" (%d unread)", conv.UnreadCount)
		}
		rightPane.WriteString(titleStyle.Render(conv.Title))
		rightPane.WriteString("\n")
		rightPane.WriteString(fieldLabelStyle.Render(platformInfo))
		rightPane.WriteString("\n")
		rightPane.WriteString(divider)
		rightPane.WriteString("\n")

		// Load and display conversation messages
		convMessages, err := m.mm.GetMessagesForConversation(conv.ID)
		if err != nil || len(convMessages) == 0 {
			rightPane.WriteString(fieldLabelStyle.Render("No messages found"))
			rightPane.WriteString("\n")
		} else {
			// Calculate how many messages actually fit in the preview pane
			// Account for: title (1) + platform info (1) + divider (1) = 3 lines used
			rightPaneWidth := m.width - leftWidth - 4
			availableHeight := max(1, m.height-5) // Conservative estimate for preview
			maxMessages := calculateVisibleMessageCount(convMessages, 0, rightPaneWidth, availableHeight)
			maxMessages = min(maxMessages, len(convMessages))

			var prevMsg *messages.Message
			for i := 0; i < maxMessages; i++ {
				msg := convMessages[i]

				// Truncate very long messages in preview
				if len(msg.Text) > 200 {
					msg.Text = msg.Text[:197] + "..."
				}

				rightPane.WriteString(formatMessage(msg, rightPaneWidth, prevMsg))
				prevMsg = &convMessages[i]
			}
		}
	}

	// Combine panes
	leftLines := strings.Split(leftPane.String(), "\n")
	rightLines := strings.Split(rightPane.String(), "\n")

	maxLines := max(len(leftLines), len(rightLines))
	var combined strings.Builder

	for i := 0; i < maxLines; i++ {
		if i < len(leftLines) {
			combined.WriteString(padRight(leftLines[i], leftWidth))
		} else {
			combined.WriteString(strings.Repeat(" ", leftWidth))
		}

		combined.WriteString(separatorStyle.Render(" │ "))

		if i < len(rightLines) {
			combined.WriteString(rightLines[i])
		}

		combined.WriteString("\n")
	}

	// Footer
	combined.WriteString("\n")
	footer := "j/k: down/up • g/G: top/bottom • enter: fullscreen • d: delete • q: quit"
	combined.WriteString(footerStyle.Render(footer))

	return combined.String()
}

func (m messagesModel) renderMessagesView() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var sb strings.Builder

	// Header
	var convTitle string
	for _, c := range m.conversations {
		if c.ID == m.selectedConvID {
			convTitle = c.Title
			break
		}
	}
	sb.WriteString(headerStyle.Render(convTitle))
	sb.WriteString("\n\n")

	if len(m.messages) == 0 {
		sb.WriteString("No messages found\n")
	} else {
		// Insert date separators into message list
		displayItems := insertDateSeparators(m.messages)

		// Reserve space for: header (2 lines) + footer (2 lines) = 4 lines
		availableHeight := m.height - 4
		linesUsed := 0

		// Track message index separately from display item index
		messageIndex := 0
		var prevMsg *messages.Message
		inViewport := false

		// Render display items until we run out of space
		for i, item := range displayItems {
			if item.isMessage() {
				// Check if this message should be rendered
				if messageIndex < m.messagesViewTop {
					messageIndex++
					continue
				}

				// We're now in the viewport
				inViewport = true

				// Render message
				isSelected := messageIndex == m.messagesCursor
				rendered := formatMessage(*item.message, m.width-4, prevMsg, isSelected)

				lineCount := strings.Count(rendered, "\n")
				if linesUsed+lineCount > availableHeight {
					break
				}

				sb.WriteString(rendered)
				linesUsed += lineCount
				prevMsg = item.message
				messageIndex++

			} else if item.isSeparator() {
				// Only render date separator if we're in viewport OR if the next message will be in viewport
				// Look ahead to see if any messages from this date will be visible
				nextMessageInViewport := false
				tempMessageIndex := messageIndex

				for j := i + 1; j < len(displayItems); j++ {
					if displayItems[j].isMessage() {
						if tempMessageIndex >= m.messagesViewTop {
							nextMessageInViewport = true
							break
						}
						tempMessageIndex++
						// Stop looking ahead after checking a few messages
						if tempMessageIndex > m.messagesViewTop+2 {
							break
						}
					} else {
						// Hit another separator, stop looking
						break
					}
				}

				// Only render separator if we're already in viewport or next message will be
				if inViewport || nextMessageInViewport {
					rendered := renderDateSeparator(*item.dateSeparator, m.width-4)
					lineCount := strings.Count(rendered, "\n") + 1

					if linesUsed+lineCount > availableHeight {
						break
					}

					sb.WriteString(rendered)
					linesUsed += lineCount
					prevMsg = nil // Reset grouping after date separator
				}
			}
		}
	}

	// Footer
	sb.WriteString("\n")
	footer := "j/k: down/up • g/G: top/bottom • esc/q: back to conversations"
	sb.WriteString(footerStyle.Render(footer))

	return sb.String()
}

// formatMessage formats a single message with consistent styling
// Now supports message grouping and right-alignment for sent messages
func formatMessage(msg messages.Message, width int, prevMsg *messages.Message, isSelected ...bool) string {
	var sb strings.Builder

	selected := false
	if len(isSelected) > 0 {
		selected = isSelected[0]
	}

	// Updated color scheme for better readability
	receivedTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	sentTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // Slightly dimmer white
	senderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true) // Light blue
	myMessageSenderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true) // Light purple
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")) // Medium gray (improved from 237)
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Subtle gray for middot

	// Apply selection background
	selectionBg := lipgloss.Color("235") // Subtle dark gray
	if selected {
		receivedTextStyle = receivedTextStyle.Background(selectionBg)
		sentTextStyle = sentTextStyle.Background(selectionBg)
		senderStyle = senderStyle.Background(selectionBg)
		myMessageSenderStyle = myMessageSenderStyle.Background(selectionBg)
		timeStyle = timeStyle.Background(selectionBg)
		separatorStyle = separatorStyle.Background(selectionBg)
	}

	// Determine if message should group with previous
	shouldGroup := shouldGroupWithPrevious(msg, prevMsg)

	// Add spacing between different senders (but not for grouped messages)
	if !shouldGroup && prevMsg != nil {
		sb.WriteString("\n")
	}

	// Format sender/timestamp line (skip if grouping with previous message)
	if !shouldGroup {
		timeStr := formatTime(msg.Timestamp)

		if msg.IsSent {
			// Right-aligned: "You · 3:04 PM"
			senderPart := myMessageSenderStyle.Render("You")
			sepPart := separatorStyle.Render(" · ")
			timePart := timeStyle.Render(timeStr)

			// Calculate combined width for alignment
			combinedText := "You · " + timeStr
			combinedWidth := calculateDisplayWidth(combinedText)

			padding := width - combinedWidth - 2
			if padding < 0 {
				padding = 0
			}

			line := strings.Repeat(" ", padding) + senderPart + sepPart + timePart
			sb.WriteString(line)
			sb.WriteString("\n")
		} else {
			// Left-aligned: "SenderName · 3:04 PM"
			senderPart := senderStyle.Render(msg.SenderName)
			sepPart := separatorStyle.Render(" · ")
			timePart := timeStyle.Render(timeStr)

			line := senderPart + sepPart + timePart
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	// Prepare message text with attachments
	msgText := msg.Text

	// Add attachment indicators
	if len(msg.Attachments) > 0 {
		var attachmentIndicators []string
		attachmentCounts := make(map[string]int)

		for _, att := range msg.Attachments {
			attachmentCounts[att.Type]++
		}

		for attType, count := range attachmentCounts {
			var indicator string
			switch attType {
			case "img":
				if count == 1 {
					indicator = "📷 Image"
				} else {
					indicator = fmt.Sprintf("📷 %d Images", count)
				}
			case "video":
				if count == 1 {
					indicator = "🎥 Video"
				} else {
					indicator = fmt.Sprintf("🎥 %d Videos", count)
				}
			case "audio":
				if count == 1 {
					indicator = "🎵 Audio"
				} else {
					indicator = fmt.Sprintf("🎵 %d Audio", count)
				}
			default:
				if count == 1 {
					indicator = "📎 File"
				} else {
					indicator = fmt.Sprintf("📎 %d Files", count)
				}
			}
			attachmentIndicators = append(attachmentIndicators, indicator)
		}

		// Add to message text
		if msgText != "" {
			msgText = fmt.Sprintf("[%s] %s", strings.Join(attachmentIndicators, ", "), msgText)
		} else {
			msgText = fmt.Sprintf("[%s]", strings.Join(attachmentIndicators, ", "))
		}
	}

	// Wrap and render message text with proper alignment
	wrappedLines := wrapText(msgText, width-4) // leave room for margins

	for _, line := range wrappedLines {
		var textStyle lipgloss.Style
		if msg.IsSent {
			textStyle = sentTextStyle
		} else {
			textStyle = receivedTextStyle
		}

		if msg.IsSent {
			// Right-align sent messages
			lineWidth := calculateDisplayWidth(line)
			indent := 2 // Default indent
			padding := width - lineWidth - indent - 2 // room for indent + right margin
			if padding < 0 {
				padding = 0
			}

			paddedLine := strings.Repeat(" ", padding) + strings.Repeat(" ", indent) + line
			sb.WriteString(textStyle.Render(paddedLine))
		} else {
			// Left-align received messages
			indent := 2 // Default indent
			sb.WriteString(textStyle.Render(strings.Repeat(" ", indent) + line))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// wrapText wraps text to fit within a specified width
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		// Check if adding this word would exceed the width
		if len(currentLine)+1+len(word) > width {
			lines = append(lines, currentLine)
			currentLine = word
		} else {
			currentLine += " " + word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// calculateVisibleMessageCount calculates how many messages can fit in the viewport
// starting from startIndex, accounting for actual message heights
func calculateVisibleMessageCount(msgs []messages.Message, startIndex int, width int, availableHeight int) int {
	if len(msgs) == 0 || startIndex >= len(msgs) {
		return 0
	}

	displayItems := insertDateSeparators(msgs)
	linesUsed := 0
	messageCount := 0
	messageIndex := 0
	var prevMsg *messages.Message

	for _, item := range displayItems {
		if item.isMessage() {
			// Skip messages before startIndex
			if messageIndex < startIndex {
				messageIndex++
				continue
			}

			// Calculate how many lines this message will take
			rendered := formatMessage(*item.message, width, prevMsg, false)
			lineCount := strings.Count(rendered, "\n")

			// Check if adding this message would exceed available height
			if linesUsed+lineCount > availableHeight {
				break
			}

			linesUsed += lineCount
			messageCount++
			prevMsg = item.message
			messageIndex++

		} else if item.isSeparator() && messageIndex >= startIndex {
			// Account for date separator lines too
			rendered := renderDateSeparator(*item.dateSeparator, width)
			lineCount := strings.Count(rendered, "\n") + 1

			if linesUsed+lineCount > availableHeight {
				break
			}

			linesUsed += lineCount
			prevMsg = nil
		}
	}

	return max(1, messageCount)
}

// Helper functions for conversation list

// formatTimeAgo formats a time as a relative string (e.g., "2m ago", "3h ago", "yesterday")
func formatTimeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	} else if diff < 48*time.Hour {
		return "yesterday"
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	} else if diff < 30*24*time.Hour {
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1w ago"
		}
		return fmt.Sprintf("%dw ago", weeks)
	} else {
		return t.Format("Jan 2")
	}
}

// getPlatformIcon returns a text prefix for the given platform
func getPlatformIcon(platform string) string {
	platform = strings.ToLower(platform)
	switch {
	case strings.Contains(platform, "whatsapp"):
		return "[WA]"
	case strings.Contains(platform, "telegram"):
		return "[TG]"
	case strings.Contains(platform, "signal"):
		return "[SG]"
	case strings.Contains(platform, "discord"):
		return "[DC]"
	case strings.Contains(platform, "slack"):
		return "[SK]"
	case strings.Contains(platform, "imessage"):
		return "[IM]"
	case strings.Contains(platform, "sms"):
		return "[SMS]"
	case strings.Contains(platform, "messenger"):
		return "[MSG]"
	case strings.Contains(platform, "instagram"):
		return "[IG]"
	case strings.Contains(platform, "twitter") || strings.Contains(platform, "x"):
		return "[X]"
	default:
		return "[??]"
	}
}

// Helper functions for date and time formatting

// sameDay returns true if two times are on the same day
func sameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// formatTime formats a timestamp based on recency
func formatTime(t time.Time) string {
	now := time.Now()

	// Today: show time only
	if sameDay(t, now) {
		return t.Format("3:04 PM")
	}

	// This week: show day + time
	if now.Sub(t) < 7*24*time.Hour && now.Sub(t) >= 0 {
		return t.Format("Mon 3:04 PM")
	}

	// This year: show date without year
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}

	// Older: show full date
	return t.Format("Jan 2, 2006")
}

// formatDateSeparator formats a date for use in separator
func formatDateSeparator(t time.Time) string {
	now := time.Now()

	// Today
	if sameDay(t, now) {
		return "Today"
	}

	// Yesterday
	yesterday := now.AddDate(0, 0, -1)
	if sameDay(t, yesterday) {
		return "Yesterday"
	}

	// This week (within last 7 days AND same week)
	// Calculate days since Sunday
	daysSinceSunday := int(now.Weekday())
	startOfWeek := now.AddDate(0, 0, -daysSinceSunday)

	if t.After(startOfWeek) && t.Before(now) {
		return t.Format("Monday")
	}

	// This year (not this week) - include day of week
	if t.Year() == now.Year() {
		return t.Format("Mon, Jan 2")
	}

	// Older years - include day of week and year
	return t.Format("Mon, Jan 2, 2006")
}

// shouldGroupWithPrevious determines if a message should group with the previous one
func shouldGroupWithPrevious(msg messages.Message, prevMsg *messages.Message) bool {
	if prevMsg == nil {
		return false
	}

	// Different sender = don't group
	if msg.SenderUID != prevMsg.SenderUID {
		return false
	}

	// Different day = don't group (date separator will appear)
	if !sameDay(msg.Timestamp, prevMsg.Timestamp) {
		return false
	}

	// More than 5 minutes apart = don't group
	timeDiff := msg.Timestamp.Sub(prevMsg.Timestamp)
	if timeDiff > 5*time.Minute || timeDiff < -5*time.Minute {
		return false
	}

	return true
}

// calculateDisplayWidth calculates the display width of a string, accounting for emojis
func calculateDisplayWidth(s string) int {
	width := 0
	for _, r := range s {
		if isEmoji(r) {
			width += 2
		} else {
			width += 1
		}
	}
	return width
}

// isEmoji returns true if the rune is an emoji
func isEmoji(r rune) bool {
	// Basic emoji detection - covers most common emoji ranges
	return (r >= 0x1F600 && r <= 0x1F64F) || // Emoticons
		(r >= 0x1F300 && r <= 0x1F5FF) || // Misc Symbols and Pictographs
		(r >= 0x1F680 && r <= 0x1F6FF) || // Transport and Map
		(r >= 0x1F1E0 && r <= 0x1F1FF) || // Regional country flags
		(r >= 0x2600 && r <= 0x26FF) || // Misc symbols
		(r >= 0x2700 && r <= 0x27BF) || // Dingbats
		(r >= 0xFE00 && r <= 0xFE0F) || // Variation Selectors
		(r >= 0x1F900 && r <= 0x1F9FF) || // Supplemental Symbols and Pictographs
		(r >= 0x1FA00 && r <= 0x1FA6F) // Chess Symbols
}

// insertDateSeparators inserts date separators between messages from different days
func insertDateSeparators(msgs []messages.Message) []displayItem {
	if len(msgs) == 0 {
		return []displayItem{}
	}

	var items []displayItem
	var lastDate time.Time

	for i := range msgs {
		msgDate := msgs[i].Timestamp

		// Check if we need a date separator
		if i == 0 || !sameDay(msgDate, lastDate) {
			// Add date separator
			items = append(items, displayItem{
				dateSeparator: &DateSeparator{
					Text: formatDateSeparator(msgDate),
					Date: msgDate,
				},
			})
			lastDate = msgDate
		}

		// Add the message
		items = append(items, displayItem{
			message: &msgs[i],
		})
	}

	return items
}

// renderDateSeparator renders a date separator line
func renderDateSeparator(sep DateSeparator, width int) string {
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	text := sep.Text
	textWidth := len(text) + 2 // " Text "

	if textWidth >= width-4 {
		// Not enough space for decorative lines
		return textStyle.Render(text) + "\n"
	}

	lineWidth := (width - textWidth) / 2
	leftLine := strings.Repeat("─", lineWidth)
	rightLine := strings.Repeat("─", width-textWidth-lineWidth)

	result := lineStyle.Render(leftLine) +
		textStyle.Render(" "+text+" ") +
		lineStyle.Render(rightLine)

	return result + "\n"
}
