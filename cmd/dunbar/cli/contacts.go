package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/arjungandhi/dunbar/pkg/config"
	"github.com/arjungandhi/dunbar/pkg/contacts"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	Z "github.com/rwxrob/bonzai/z"
	"github.com/rwxrob/help"
)

var Contacts = &Z.Cmd{
	Name:     "contacts",
	Summary:  "Manage your contacts",
	Commands: []*Z.Cmd{help.Cmd, ContactsInit, ContactsList, ContactsSync},
	Call: func(x *Z.Cmd, args ...string) error {
		// Default action: open TUI
		return runContactsTUI(x, args...)
	},
}

var ContactsInit = &Z.Cmd{
	Name:    "init",
	Summary: "Initialize contacts provider",
	Call: func(x *Z.Cmd, args ...string) error {
		cfg := config.New()
		if err := cfg.EnsureDunbarDir(); err != nil {
			return fmt.Errorf("failed to create dunbar directory: %w", err)
		}

		// Run provider selection in Bubble Tea
		m := newProviderSelectModel()
		p := tea.NewProgram(m)
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("provider selection failed: %w", err)
		}

		providerModel := result.(providerSelectModel)
		if providerModel.cancelled {
			return fmt.Errorf("initialization cancelled")
		}

		providerType := providerModel.selectedProvider

		// Save provider type to config
		configPath := filepath.Join(cfg.DunbarDir, "config.json")
		configData := map[string]string{
			"provider": providerType,
		}
		data, err := json.MarshalIndent(configData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}

		// Initialize the selected provider
		switch providerType {
		case "google":
			return initGoogleProvider(cfg)
		default:
			return fmt.Errorf("unsupported provider: %s", providerType)
		}
	},
}

// Provider selection model
type providerSelectModel struct {
	providers        []string
	cursor           int
	selectedProvider string
	cancelled        bool
}

func newProviderSelectModel() providerSelectModel {
	return providerSelectModel{
		providers: []string{"google"},
		cursor:    0,
	}
}

func (m providerSelectModel) Init() tea.Cmd {
	return nil
}

func (m providerSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m providerSelectModel) View() string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	sb.WriteString(titleStyle.Render("Select a contacts provider:"))
	sb.WriteString("\n\n")

	normalStyle := lipgloss.NewStyle()
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))

	providerNames := map[string]string{
		"google": "Google Contacts (CardDAV)",
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

func initGoogleProvider(cfg *config.Config) error {
	// Check if credentials already exist
	provider, _ := contacts.NewGoogleContactsProvider(cfg.DunbarDir)
	existingCreds, _ := provider.LoadCredentials()
	hasExistingCreds := existingCreds != nil && existingCreds.ClientID != ""

	var deleteExisting bool
	if hasExistingCreds {
		// Ask if user wants to delete existing credentials
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Existing credentials found").
					Description(fmt.Sprintf("Client ID: %s\n\nDelete and enter new credentials?", existingCreds.ClientID)).
					Affirmative("Yes, delete").
					Negative("No, keep and re-authorize").
					Value(&deleteExisting),
			),
		)

		if err := form.Run(); err != nil {
			return fmt.Errorf("prompt failed: %w", err)
		}

		// If keeping existing creds, just re-authorize
		if !deleteExisting {
			return reauthorizeGoogleProvider(cfg, provider)
		}
	}

	// Prompt for Client ID and Secret using huh
	var clientID, clientSecret string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Google Contacts Setup").
				Description("To use Google Contacts, you need OAuth 2.0 credentials.\n\n" +
					"Setup steps:\n" +
					"1. Enable People API at: console.cloud.google.com/apis/library/people.googleapis.com\n" +
					"2. Go to: console.cloud.google.com/apis/credentials\n" +
					"3. Create OAuth 2.0 Client ID (Application type: Desktop app)\n" +
					"4. No redirect URIs needed (auto-includes urn:ietf:wg:oauth:2.0:oob)"),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Client ID").
				Value(&clientID).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("client ID cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Title("Client Secret").
				Value(&clientSecret).
				Password(true).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("client secret cannot be empty")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("setup cancelled: %w", err)
	}

	// Create and initialize provider
	provider, err := contacts.NewGoogleContactsProvider(cfg.DunbarDir)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Save credentials
	creds := &contacts.GoogleCredentials{
		ClientID:     strings.TrimSpace(clientID),
		ClientSecret: strings.TrimSpace(clientSecret),
	}
	if err := provider.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	// Initialize provider
	if err := provider.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Get auth URL and open browser
	authURL := provider.GetAuthURL()
	_ = openBrowser(authURL)

	fmt.Println("\nOpening your browser for authorization...")
	fmt.Println("If the browser doesn't open, copy this URL manually:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	// Prompt for auth code
	var authCode string
	authForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Authorization Code").
				Description("Enter the authorization code from Google:").
				Value(&authCode).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("authorization code cannot be empty")
					}
					return nil
				}),
		),
	)

	if err := authForm.Run(); err != nil {
		return fmt.Errorf("setup cancelled: %w", err)
	}

	// Exchange auth code for token
	ctx := context.Background()
	if err := provider.ExchangeAuthCode(ctx, strings.TrimSpace(authCode)); err != nil {
		return fmt.Errorf("failed to exchange auth code: %w", err)
	}

	fmt.Println("\nGoogle Contacts provider initialized successfully!")
	fmt.Println("Run 'dunbar contacts sync' to sync your contacts.")

	return nil
}

// reauthorizeGoogleProvider re-authorizes with existing credentials
func reauthorizeGoogleProvider(cfg *config.Config, provider *contacts.GoogleContactsProvider) error {
	// Initialize provider with existing credentials
	if err := provider.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Get auth URL
	authURL := provider.GetAuthURL()

	// Open browser
	_ = openBrowser(authURL)

	fmt.Println("Opening your browser for authorization...")
	fmt.Println("If the browser doesn't open, copy this URL manually:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	// Prompt for auth code using huh
	var authCode string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Authorization Code").
				Description("Enter the authorization code from Google:").
				Value(&authCode),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	// Exchange auth code for token
	ctx := context.Background()
	if err := provider.ExchangeAuthCode(ctx, strings.TrimSpace(authCode)); err != nil {
		return fmt.Errorf("failed to exchange auth code: %w", err)
	}

	fmt.Println("\nGoogle Contacts provider re-authorized successfully!")
	fmt.Println("Run 'dunbar contacts sync' to sync your contacts.")

	return nil
}

var ContactsList = &Z.Cmd{
	Name:    "list",
	Summary: "List all contacts",
	Call: func(x *Z.Cmd, args ...string) error {
		cfg := config.New()
		cm, err := getContactManager(cfg)
		if err != nil {
			return err
		}

		contacts, err := cm.ListContacts()
		if err != nil {
			return fmt.Errorf("failed to list contacts: %w", err)
		}

		// Output in a bash-friendly format: one contact per line
		// Format: UID|FullName|PrimaryEmail|PrimaryPhone
		for _, contact := range contacts {
			fmt.Printf("%s|%s|%s|%s\n",
				contact.UID,
				contact.FullName,
				contact.PrimaryEmail(),
				contact.PrimaryPhone(),
			)
		}

		return nil
	},
}

var ContactsSync = &Z.Cmd{
	Name:    "sync",
	Summary: "Sync contacts with provider",
	Call: func(x *Z.Cmd, args ...string) error {
		cfg := config.New()
		cm, err := getContactManager(cfg)
		if err != nil {
			return err
		}

		fmt.Println("Syncing contacts...")
		if err := cm.SyncContacts(); err != nil {
			return fmt.Errorf("failed to sync contacts: %w", err)
		}

		contacts, err := cm.ListContacts()
		if err != nil {
			return fmt.Errorf("failed to list contacts: %w", err)
		}

		fmt.Printf("Sync complete! Total contacts: %d\n", len(contacts))
		return nil
	},
}

// Helper function to get or create ContactManager
func getContactManager(cfg *config.Config) (*contacts.ContactManager, error) {
	if err := cfg.EnsureDunbarDir(); err != nil {
		return nil, fmt.Errorf("failed to create dunbar directory: %w", err)
	}

	// Read provider config
	configPath := filepath.Join(cfg.DunbarDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("contacts not initialized. Run 'dunbar contacts init' first")
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var configData map[string]string
	if err := json.Unmarshal(data, &configData); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	providerType := configData["provider"]
	if providerType != "google" {
		return nil, fmt.Errorf("unsupported provider: %s", providerType)
	}

	// Create Google provider
	provider, err := contacts.NewGoogleContactsProvider(cfg.DunbarDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	if err := provider.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Create ContactManager
	return contacts.NewContactManager(provider, *cfg, cfg.DunbarDir)
}

// TUI implementation
func runContactsTUI(x *Z.Cmd, args ...string) error {
	cfg := config.New()
	cm, err := getContactManager(cfg)
	if err != nil {
		return err
	}

	contactsList, err := cm.ListContacts()
	if err != nil {
		return fmt.Errorf("failed to list contacts: %w", err)
	}

	m := newContactsModel(contactsList, cm)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

// Bubble Tea model for contacts TUI
type contactsModel struct {
	contacts         []contacts.Contact
	cursor           int
	viewportTop      int
	height           int
	width            int
	cm               *contacts.ContactManager
	confirmingDelete bool
	deleteUID        string
}

func newContactsModel(contactsList []contacts.Contact, cm *contacts.ContactManager) contactsModel {
	// Sort contacts alphabetically by name
	sort.Slice(contactsList, func(i, j int) bool {
		return strings.ToLower(contactsList[i].FullName) < strings.ToLower(contactsList[j].FullName)
	})

	return contactsModel{
		contacts:         contactsList,
		cursor:           0,
		viewportTop:      0,
		height:           25, // Default height, will be updated with window size
		width:            80, // Default width, will be updated with window size
		cm:               cm,
		confirmingDelete: false,
		deleteUID:        "",
	}
}

func (m contactsModel) Init() tea.Cmd {
	return nil
}

func (m contactsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height - 3 // Reserve space for header and footer
		m.width = msg.Width

	case tea.KeyMsg:
		// Handle delete confirmation
		if m.confirmingDelete {
			switch msg.String() {
			case "y", "Y":
				// Delete the contact
				if err := m.cm.DeleteContact(m.deleteUID); err == nil {
					// Remove from local list
					for i, c := range m.contacts {
						if c.UID == m.deleteUID {
							m.contacts = append(m.contacts[:i], m.contacts[i+1:]...)
							break
						}
					}
					// Adjust cursor if needed
					if m.cursor >= len(m.contacts) && len(m.contacts) > 0 {
						m.cursor = len(m.contacts) - 1
					}
				}
				m.confirmingDelete = false
				m.deleteUID = ""
				return m, nil

			case "n", "N", "esc":
				// Cancel deletion
				m.confirmingDelete = false
				m.deleteUID = ""
				return m, nil
			}
			return m, nil
		}

		// Normal key handling
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "d":
			// Start delete confirmation
			if len(m.contacts) > 0 && m.cursor < len(m.contacts) {
				m.confirmingDelete = true
				m.deleteUID = m.contacts[m.cursor].UID
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.viewportTop {
					m.viewportTop = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.contacts)-1 {
				m.cursor++
				if m.cursor >= m.viewportTop+m.height {
					m.viewportTop = m.cursor - m.height + 1
				}
			}

		case "g", "home":
			m.cursor = 0
			m.viewportTop = 0

		case "G", "end":
			m.cursor = len(m.contacts) - 1
			m.viewportTop = max(0, len(m.contacts)-m.height)

		case "pgup":
			m.cursor = max(0, m.cursor-m.height)
			m.viewportTop = max(0, m.viewportTop-m.height)

		case "pgdown":
			m.cursor = min(len(m.contacts)-1, m.cursor+m.height)
			m.viewportTop = min(max(0, len(m.contacts)-m.height), m.viewportTop+m.height)
		}
	}

	return m, nil
}

func (m contactsModel) View() string {
	if len(m.contacts) == 0 {
		return "No contacts found. Run 'dunbar contacts sync' to sync your contacts.\n\nPress 'q' to quit."
	}

	// Show delete confirmation dialog
	if m.confirmingDelete {
		var contact contacts.Contact
		for _, c := range m.contacts {
			if c.UID == m.deleteUID {
				contact = c
				break
			}
		}

		// Styles for the dialog
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

		// Build the dialog content
		var dialogContent strings.Builder
		dialogContent.WriteString(titleStyle.Render("⚠️  Delete Contact?"))
		dialogContent.WriteString("\n\n")
		dialogContent.WriteString("Are you sure you want to delete:\n")
		dialogContent.WriteString(nameStyle.Render(contact.FullName))
		dialogContent.WriteString("\n\n")
		dialogContent.WriteString(buttonStyle.Render("This action cannot be undone."))
		dialogContent.WriteString("\n\n\n")
		dialogContent.WriteString(yesButtonStyle.Render("Y") + "  " + noButtonStyle.Render("N"))

		dialog := boxStyle.Render(dialogContent.String())

		// Center the dialog
		return lipgloss.Place(m.width, m.height+3,
			lipgloss.Center, lipgloss.Center,
			dialog)
	}

	// Calculate pane widths - left pane takes 40%, right pane takes 60%
	leftWidth := max(30, m.width*2/5)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	normalStyle := lipgloss.NewStyle()
	selectedStyle := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("240"))
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Build left pane (contact list)
	var leftPane strings.Builder
	leftPane.WriteString(headerStyle.Render(fmt.Sprintf("Contacts (%d)", len(m.contacts))))
	leftPane.WriteString("\n")

	// Calculate viewport
	end := min(m.viewportTop+m.height, len(m.contacts))

	for i := m.viewportTop; i < end; i++ {
		contact := m.contacts[i]
		style := normalStyle

		if i == m.cursor {
			style = selectedStyle
		}

		line := fmt.Sprintf(" %s", truncate(contact.FullName, leftWidth-2))
		leftPane.WriteString(style.Render(line))
		leftPane.WriteString("\n")
	}

	// Build right pane (contact details)
	var rightPane strings.Builder
	if m.cursor < len(m.contacts) {
		contact := m.contacts[m.cursor]

		// Enhanced styles for detail view
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

		sectionHeaderStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170")).
			MarginTop(1)

		fieldLabelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

		fieldValueStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

		dividerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

		divider := dividerStyle.Render("─────────────────────────────────")

		// Title with name
		rightPane.WriteString(titleStyle.Render("👤 " + contact.FullName))
		rightPane.WriteString("\n")

		if contact.Nickname != "" {
			rightPane.WriteString(fieldLabelStyle.Render("   aka "))
			rightPane.WriteString(fieldValueStyle.Render(contact.Nickname))
			rightPane.WriteString("\n")
		}

		// Phone numbers
		if len(contact.PhoneNumbers) > 0 {
			rightPane.WriteString("\n")
			rightPane.WriteString(divider)
			rightPane.WriteString("\n")
			rightPane.WriteString(sectionHeaderStyle.Render("📞 Phone"))
			rightPane.WriteString("\n\n")
			for _, phone := range contact.PhoneNumbers {
				rightPane.WriteString(fieldLabelStyle.Render("  " + phone.Type + ":"))
				rightPane.WriteString(" ")
				rightPane.WriteString(fieldValueStyle.Render(phone.Value))
				rightPane.WriteString("\n")
			}
		}

		// Email addresses
		if len(contact.EmailAddresses) > 0 {
			rightPane.WriteString("\n")
			rightPane.WriteString(divider)
			rightPane.WriteString("\n")
			rightPane.WriteString(sectionHeaderStyle.Render("📧 Email"))
			rightPane.WriteString("\n\n")
			for _, email := range contact.EmailAddresses {
				rightPane.WriteString(fieldLabelStyle.Render("  " + email.Type + ":"))
				rightPane.WriteString(" ")
				rightPane.WriteString(fieldValueStyle.Render(email.Value))
				rightPane.WriteString("\n")
			}
		}

		// Organization
		if contact.Organization != nil && contact.Organization.Name != "" {
			rightPane.WriteString("\n")
			rightPane.WriteString(divider)
			rightPane.WriteString("\n")
			rightPane.WriteString(sectionHeaderStyle.Render("💼 Work"))
			rightPane.WriteString("\n\n")
			rightPane.WriteString(fieldLabelStyle.Render("  Company:"))
			rightPane.WriteString(" ")
			rightPane.WriteString(fieldValueStyle.Render(contact.Organization.Name))
			rightPane.WriteString("\n")
			if contact.Organization.Title != "" {
				rightPane.WriteString(fieldLabelStyle.Render("  Title:"))
				rightPane.WriteString(" ")
				rightPane.WriteString(fieldValueStyle.Render(contact.Organization.Title))
				rightPane.WriteString("\n")
			}
			if contact.Organization.Department != "" {
				rightPane.WriteString(fieldLabelStyle.Render("  Department:"))
				rightPane.WriteString(" ")
				rightPane.WriteString(fieldValueStyle.Render(contact.Organization.Department))
				rightPane.WriteString("\n")
			}
		}

		// Addresses
		if len(contact.Addresses) > 0 {
			rightPane.WriteString("\n")
			rightPane.WriteString(divider)
			rightPane.WriteString("\n")
			rightPane.WriteString(sectionHeaderStyle.Render("🏠 Address"))
			rightPane.WriteString("\n\n")
			for _, addr := range contact.Addresses {
				rightPane.WriteString(fieldLabelStyle.Render("  " + addr.Type + ":"))
				rightPane.WriteString("\n")
				if addr.Street != "" {
					rightPane.WriteString(fieldValueStyle.Render("    " + addr.Street))
					rightPane.WriteString("\n")
				}
				cityState := []string{}
				if addr.City != "" {
					cityState = append(cityState, addr.City)
				}
				if addr.State != "" {
					cityState = append(cityState, addr.State)
				}
				if addr.PostalCode != "" {
					cityState = append(cityState, addr.PostalCode)
				}
				if len(cityState) > 0 {
					rightPane.WriteString(fieldValueStyle.Render("    " + strings.Join(cityState, ", ")))
					rightPane.WriteString("\n")
				}
				if addr.Country != "" {
					rightPane.WriteString(fieldValueStyle.Render("    " + addr.Country))
					rightPane.WriteString("\n")
				}
			}
		}

		// Birthday
		if contact.Birthday != nil {
			rightPane.WriteString("\n")
			rightPane.WriteString(divider)
			rightPane.WriteString("\n")
			rightPane.WriteString(sectionHeaderStyle.Render("🎂 Birthday"))
			rightPane.WriteString("\n\n")
			rightPane.WriteString(fieldValueStyle.Render("  " + contact.Birthday.Format("January 2, 2006")))
			rightPane.WriteString("\n")
		}

		// Notes
		if contact.Notes != "" {
			rightPane.WriteString("\n")
			rightPane.WriteString(divider)
			rightPane.WriteString("\n")
			rightPane.WriteString(sectionHeaderStyle.Render("📝 Notes"))
			rightPane.WriteString("\n\n")
			rightPane.WriteString(fieldValueStyle.Render("  " + contact.Notes))
			rightPane.WriteString("\n")
		}
	}

	// Combine panes with separator
	leftLines := strings.Split(leftPane.String(), "\n")
	rightLines := strings.Split(rightPane.String(), "\n")

	maxLines := max(len(leftLines), len(rightLines))
	var combined strings.Builder

	for i := 0; i < maxLines; i++ {
		// Left pane content
		if i < len(leftLines) {
			combined.WriteString(padRight(leftLines[i], leftWidth))
		} else {
			combined.WriteString(strings.Repeat(" ", leftWidth))
		}

		// Separator
		combined.WriteString(separatorStyle.Render(" │ "))

		// Right pane content
		if i < len(rightLines) {
			combined.WriteString(rightLines[i])
		}

		combined.WriteString("\n")
	}

	// Footer
	combined.WriteString("\n")
	footer := "j/k: down/up • g/G: top/bottom • pgup/pgdn: page up/down • d: delete • q: quit"
	combined.WriteString(footerStyle.Render(footer))

	return combined.String()
}

// Helper functions
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func padRight(s string, width int) string {
	// Strip ANSI codes to get actual length
	visualLen := lipgloss.Width(s)
	if visualLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visualLen)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// openBrowser opens the specified URL in the default browser
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}
