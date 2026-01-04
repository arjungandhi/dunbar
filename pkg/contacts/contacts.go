package contacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arjungandhi/dunbar/pkg/config"
	"github.com/google/uuid"
)

// PhoneNumber represents a phone number with type
type PhoneNumber struct {
	Value string `json:"value"`
	Type  string `json:"type"` // e.g., "home", "work", "mobile", "fax"
}

// EmailAddress represents an email with type
type EmailAddress struct {
	Value string `json:"value"`
	Type  string `json:"type"` // e.g., "home", "work", "other"
}

// Address represents a physical address
type Address struct {
	Street     string `json:"street,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	Country    string `json:"country,omitempty"`
	Type       string `json:"type"` // e.g., "home", "work"
}

// Organization represents work/company information
type Organization struct {
	Name       string `json:"name,omitempty"`
	Title      string `json:"title,omitempty"`
	Department string `json:"department,omitempty"`
}

// Contact represents a person in the contact database
type Contact struct {
	// CardDAV sync fields
	UID  string `json:"uid"`   // Unique identifier for CardDAV sync
	ETag string `json:"etag"`  // ETag for sync tracking
	URL  string `json:"url"`   // CardDAV resource URL

	// Name information
	GivenName  string `json:"given_name,omitempty"`  // First name
	FamilyName string `json:"family_name,omitempty"` // Last name
	FullName   string `json:"full_name"`             // Formatted full name
	Nickname   string `json:"nickname,omitempty"`

	// Contact information (multiple values)
	PhoneNumbers   []PhoneNumber  `json:"phone_numbers,omitempty"`
	EmailAddresses []EmailAddress `json:"email_addresses,omitempty"`
	Addresses      []Address      `json:"addresses,omitempty"`

	// Organization
	Organization *Organization `json:"organization,omitempty"`

	// Personal information
	Birthday     *time.Time `json:"birthday,omitempty"`
	Anniversary  *time.Time `json:"anniversary,omitempty"`
	PhotoURL     string     `json:"photo_url,omitempty"`
	PhotoData    []byte     `json:"photo_data,omitempty"` // Base64 encoded photo

	// Metadata
	Tags  []string `json:"tags,omitempty"`  // Custom tags for organizing contacts
	Notes string   `json:"notes,omitempty"` // Freeform notes about the contact

	LastModified *time.Time `json:"last_modified,omitempty"` // When contact was last modified locally
	LastSynced   *time.Time `json:"last_synced,omitempty"`   // When contact was last synced with provider
}

// PrimaryPhone returns the first phone number, preferring mobile
func (c *Contact) PrimaryPhone() string {
	if len(c.PhoneNumbers) == 0 {
		return ""
	}
	// Try to find mobile first
	for _, p := range c.PhoneNumbers {
		if p.Type == "mobile" || p.Type == "cell" {
			return p.Value
		}
	}
	return c.PhoneNumbers[0].Value
}

// PrimaryEmail returns the first email address
func (c *Contact) PrimaryEmail() string {
	if len(c.EmailAddresses) == 0 {
		return ""
	}
	return c.EmailAddresses[0].Value
}

type ContactManager struct {
	provider    ContactProvider
	config      config.Config
	storagePath string // Directory where JSON contact files are stored
}

type ContactProvider interface {
	FetchContacts() ([]Contact, error)
	WriteContact(Contact) error
	DeleteContact(uid string) error
}

func NewContactManager(provider ContactProvider, config config.Config, storagePath string) (*ContactManager, error) {
	// Create contacts people directory if it doesn't exist
	contactsDir := filepath.Join(storagePath, "contacts", "people")
	if err := os.MkdirAll(contactsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create contacts directory: %w", err)
	}

	return &ContactManager{
		provider:    provider,
		config:      config,
		storagePath: contactsDir,
	}, nil
}

// GetContact reads a single contact from disk by UID
func (cm *ContactManager) GetContact(uid string) (*Contact, error) {
	filePath := filepath.Join(cm.storagePath, uid+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Contact not found
		}
		return nil, fmt.Errorf("failed to read contact file: %w", err)
	}

	var contact Contact
	if err := json.Unmarshal(data, &contact); err != nil {
		return nil, fmt.Errorf("failed to parse contact file: %w", err)
	}

	return &contact, nil
}

// ListContacts reads all contact JSON files from disk and returns them
func (cm *ContactManager) ListContacts() ([]Contact, error) {
	entries, err := os.ReadDir(cm.storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read contacts directory: %w", err)
	}

	var contacts []Contact
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Skip non-contact files
		if entry.Name() == "google_creds.json" || entry.Name() == "config.json" {
			continue
		}

		filePath := filepath.Join(cm.storagePath, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read contact file %s: %w", entry.Name(), err)
		}

		var contact Contact
		if err := json.Unmarshal(data, &contact); err != nil {
			return nil, fmt.Errorf("failed to parse contact file %s: %w", entry.Name(), err)
		}

		contacts = append(contacts, contact)
	}

	return contacts, nil
}

// WriteContact writes a contact locally and pushes the update to the provider
func (cm *ContactManager) WriteContact(contact Contact) error {
	// Generate UID if not set
	if contact.UID == "" {
		contact.UID = uuid.New().String()
	}

	// Set LastModified timestamp
	now := time.Now()
	contact.LastModified = &now

	// Write to local storage
	data, err := json.MarshalIndent(contact, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal contact: %w", err)
	}

	filePath := filepath.Join(cm.storagePath, contact.UID+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write contact file: %w", err)
	}

	// Push update to provider
	if err := cm.provider.WriteContact(contact); err != nil {
		return fmt.Errorf("failed to write contact to provider: %w", err)
	}

	return nil
}

// WriteContacts writes multiple contacts to disk and pushes them to the provider (batch operation)
func (cm *ContactManager) WriteContacts(contacts []Contact) error {
	for _, contact := range contacts {
		if err := cm.WriteContact(contact); err != nil {
			return err
		}
	}
	return nil
}

// DeleteContact removes a contact from disk and provider by UID
func (cm *ContactManager) DeleteContact(uid string) error {
	// Delete from provider first (if it's a provider contact)
	// UIDs from Google are numeric IDs, new ones are UUIDs
	isProviderContact := !strings.Contains(uid, "-") // UUIDs have dashes, provider IDs don't
	if isProviderContact {
		if err := cm.provider.DeleteContact(uid); err != nil {
			return fmt.Errorf("failed to delete contact from provider: %w", err)
		}
	}

	// Delete from local storage
	filePath := filepath.Join(cm.storagePath, uid+".json")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("contact not found: %s", uid)
		}
		return fmt.Errorf("failed to delete contact: %w", err)
	}
	return nil
}

// SyncContacts performs a pull-only sync from the provider to local storage
// This fetches all contacts from the provider and writes them to local storage
func (cm *ContactManager) SyncContacts() error {
	// Fetch contacts from provider
	remoteContacts, err := cm.provider.FetchContacts()
	if err != nil {
		return fmt.Errorf("failed to fetch remote contacts: %w", err)
	}

	// Write all remote contacts to local storage
	for _, contact := range remoteContacts {
		if err := cm.writeContactWithoutModifyingTimestamp(contact); err != nil {
			return fmt.Errorf("failed to write local contact: %w", err)
		}
	}

	return nil
}

// writeContactWithoutModifyingTimestamp writes a contact without updating LastModified
// Used during sync to preserve modification times
func (cm *ContactManager) writeContactWithoutModifyingTimestamp(contact Contact) error {
	if contact.UID == "" {
		contact.UID = uuid.New().String()
	}

	// Update LastSynced but not LastModified
	now := time.Now()
	contact.LastSynced = &now

	data, err := json.MarshalIndent(contact, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal contact: %w", err)
	}

	filePath := filepath.Join(cm.storagePath, contact.UID+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write contact file: %w", err)
	}

	return nil
}
