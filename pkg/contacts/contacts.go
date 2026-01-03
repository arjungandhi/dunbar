package contacts

import (
	"time"

	"github.com/arjungandhi/pkg/config"
)

// Contact represents a person in the contact database
type Contact struct {
	// CardDAV sync fields
	UID string `json:"uid"` // Unique identifier for CardDAV sync

	// Basic contact information
	Name  string `json:"name"`
	Phone string `json:"phone,omitempty"`
	Email string `json:"email,omitempty"`

	// Metadata
	Tags  []string `json:"tags,omitempty"`  // Custom tags for organizing contacts
	Notes string   `json:"notes,omitempty"` // Freeform notes about the contact

	LastSynced *time.Time `json:"last_synced,omitempty"`
}

type ContactManager struct {
	provider ContactProvider
	config   config.Config
	contacts []Contact
}

type ContactProvider interface {
	FetchContacts() ([]Contact, error)
	WriteContacts([]Contact) error
}

func NewContactManager(provider ContactProvider, config config.Config) *ContactManager {
	return &ContactManager{provider: provider, config: config}
}

func (cm *ContactManager) LoadContacts() error {
	// read contacts from disk
	return nil
}

func (cm *ContactManager) SaveContacts() error {
	// write contacts to disk
	return nil
}

func (cm *ContactManager) SyncContacts() error {
	// sync contacts with provider
	return nil
}

func (cm *ContactManager) AddContact(contact Contact) {
	// add contact to list
}

func (cm *ContactManager) RemoveContact(contact Contact) {
	// remove contact from list
}
