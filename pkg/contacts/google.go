package contacts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleCredentials holds OAuth 2.0 credentials for Google
type GoogleCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	Email        string `json:"email,omitempty"` // User's email for CardDAV endpoint
}

// GoogleContactsProvider implements ContactProvider for Google Contacts via CardDAV
type GoogleContactsProvider struct {
	config      *oauth2.Config
	token       *oauth2.Token
	credsPath   string
	syncToken   string
	syncTokenPath string
}

// NewGoogleContactsProvider creates a new Google Contacts provider
func NewGoogleContactsProvider(dunbarDir string) (*GoogleContactsProvider, error) {
	contactsDir := filepath.Join(dunbarDir, "contacts")
	if err := os.MkdirAll(contactsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create contacts directory: %w", err)
	}

	credsPath := filepath.Join(contactsDir, "google_creds.json")
	syncTokenPath := filepath.Join(contactsDir, "google_sync_token.txt")

	return &GoogleContactsProvider{
		credsPath:     credsPath,
		syncTokenPath: syncTokenPath,
	}, nil
}

// SaveCredentials saves OAuth credentials to the credentials file
func (g *GoogleContactsProvider) SaveCredentials(creds *GoogleCredentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	if err := os.WriteFile(g.credsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// LoadCredentials loads OAuth credentials from the credentials file
func (g *GoogleContactsProvider) LoadCredentials() (*GoogleCredentials, error) {
	data, err := os.ReadFile(g.credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("credentials file not found at %s: please run setup first", g.credsPath)
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	var creds GoogleCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	return &creds, nil
}

// Initialize sets up the OAuth2 config and loads credentials
func (g *GoogleContactsProvider) Initialize() error {
	creds, err := g.LoadCredentials()
	if err != nil {
		return err
	}

	g.config = &oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob", // For CLI/desktop apps
		Scopes: []string{
			"https://www.googleapis.com/auth/contacts", // Read/write access
			"https://www.googleapis.com/auth/userinfo.email",
		},
	}

	// If we have a refresh token, create the token
	if creds.RefreshToken != "" {
		g.token = &oauth2.Token{
			RefreshToken: creds.RefreshToken,
			AccessToken:  creds.AccessToken,
			// Set expiry to past to force refresh on first use
			Expiry: time.Now().Add(-time.Hour),
		}
	}

	// Load sync token if it exists
	if data, err := os.ReadFile(g.syncTokenPath); err == nil {
		g.syncToken = string(data)
	}

	return nil
}

// GetAuthURL returns the URL users should visit to authorize the app
func (g *GoogleContactsProvider) GetAuthURL() string {
	if g.config == nil {
		return ""
	}
	return g.config.AuthCodeURL("state-token",
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)
}

// ExchangeAuthCode exchanges an authorization code for tokens
func (g *GoogleContactsProvider) ExchangeAuthCode(ctx context.Context, code string) error {
	if g.config == nil {
		return fmt.Errorf("provider not initialized")
	}

	token, err := g.config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange auth code: %w", err)
	}

	g.token = token

	// Save the refresh token
	creds, err := g.LoadCredentials()
	if err != nil {
		return err
	}

	creds.RefreshToken = token.RefreshToken
	creds.AccessToken = token.AccessToken

	return g.SaveCredentials(creds)
}

// GetHTTPClient returns an authenticated HTTP client
func (g *GoogleContactsProvider) GetHTTPClient(ctx context.Context) (*oauth2.Config, *oauth2.Token, error) {
	if g.config == nil || g.token == nil {
		return nil, nil, fmt.Errorf("provider not initialized or not authenticated")
	}

	return g.config, g.token, nil
}

// SaveSyncToken saves the sync token for incremental syncing
func (g *GoogleContactsProvider) SaveSyncToken(token string) error {
	g.syncToken = token
	return os.WriteFile(g.syncTokenPath, []byte(token), 0600)
}

// GetSyncToken returns the current sync token
func (g *GoogleContactsProvider) GetSyncToken() string {
	return g.syncToken
}

// getUserEmail fetches the user's email from Google's userinfo API
func (g *GoogleContactsProvider) getUserEmail(httpClient *http.Client) (string, error) {
	resp, err := httpClient.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("failed to get userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("userinfo request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", fmt.Errorf("failed to decode userinfo: %w", err)
	}

	if userInfo.Email == "" {
		return "", fmt.Errorf("no email in userinfo response")
	}

	return userInfo.Email, nil
}

// People API response structures
type peopleAPIPerson struct {
	ResourceName string                   `json:"resourceName"`
	ETag         string                   `json:"etag"`
	Names        []peopleAPIName          `json:"names"`
	PhoneNumbers []peopleAPIPhoneNumber   `json:"phoneNumbers"`
	EmailAddresses []peopleAPIEmailAddress `json:"emailAddresses"`
	Addresses    []peopleAPIAddress       `json:"addresses"`
	Organizations []peopleAPIOrganization `json:"organizations"`
	Birthdays    []peopleAPIBirthday      `json:"birthdays"`
	Photos       []peopleAPIPhoto         `json:"photos"`
	Biographies  []peopleAPIBiography     `json:"biographies"`
}

type peopleAPIName struct {
	DisplayName  string `json:"displayName"`
	FamilyName   string `json:"familyName"`
	GivenName    string `json:"givenName"`
	DisplayNameLastFirst string `json:"displayNameLastFirst"`
}

type peopleAPIPhoneNumber struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

type peopleAPIEmailAddress struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

type peopleAPIAddress struct {
	StreetAddress   string `json:"streetAddress"`
	City            string `json:"city"`
	Region          string `json:"region"`
	PostalCode      string `json:"postalCode"`
	Country         string `json:"country"`
	Type            string `json:"type"`
}

type peopleAPIOrganization struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Department string `json:"department"`
}

type peopleAPIBirthday struct {
	Date struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"date"`
}

type peopleAPIPhoto struct {
	URL string `json:"url"`
}

type peopleAPIBiography struct {
	Value string `json:"value"`
}

// convertPeopleAPIToContact converts a People API person to our Contact struct
func convertPeopleAPIToContact(person peopleAPIPerson) Contact {
	// Extract just the ID from resourceName (e.g., "people/c8935729599066447265" -> "c8935729599066447265")
	uid := person.ResourceName
	if strings.Contains(uid, "/") {
		parts := strings.Split(uid, "/")
		uid = parts[len(parts)-1]
	}

	contact := Contact{
		UID:  uid,
		ETag: person.ETag,
	}

	// Names
	if len(person.Names) > 0 {
		name := person.Names[0]
		contact.FullName = name.DisplayName
		contact.GivenName = name.GivenName
		contact.FamilyName = name.FamilyName
	}

	// Phone numbers
	for _, phone := range person.PhoneNumbers {
		phoneType := "other"
		if phone.Type != "" {
			phoneType = strings.ToLower(phone.Type)
		}
		contact.PhoneNumbers = append(contact.PhoneNumbers, PhoneNumber{
			Value: phone.Value,
			Type:  phoneType,
		})
	}

	// Email addresses
	for _, email := range person.EmailAddresses {
		emailType := "other"
		if email.Type != "" {
			emailType = strings.ToLower(email.Type)
		}
		contact.EmailAddresses = append(contact.EmailAddresses, EmailAddress{
			Value: email.Value,
			Type:  emailType,
		})
	}

	// Addresses
	for _, addr := range person.Addresses {
		addrType := "other"
		if addr.Type != "" {
			addrType = strings.ToLower(addr.Type)
		}
		contact.Addresses = append(contact.Addresses, Address{
			Street:     addr.StreetAddress,
			City:       addr.City,
			State:      addr.Region,
			PostalCode: addr.PostalCode,
			Country:    addr.Country,
			Type:       addrType,
		})
	}

	// Organization
	if len(person.Organizations) > 0 {
		org := person.Organizations[0]
		contact.Organization = &Organization{
			Name:       org.Name,
			Title:      org.Title,
			Department: org.Department,
		}
	}

	// Birthday
	if len(person.Birthdays) > 0 {
		bday := person.Birthdays[0]
		if bday.Date.Year > 0 && bday.Date.Month > 0 && bday.Date.Day > 0 {
			t := time.Date(bday.Date.Year, time.Month(bday.Date.Month), bday.Date.Day, 0, 0, 0, 0, time.UTC)
			contact.Birthday = &t
		}
	}

	// Photo
	if len(person.Photos) > 0 {
		contact.PhotoURL = person.Photos[0].URL
	}

	// Biography/Notes
	if len(person.Biographies) > 0 {
		contact.Notes = person.Biographies[0].Value
	}

	return contact
}

// FetchContacts retrieves contacts from Google via People API
func (g *GoogleContactsProvider) FetchContacts() ([]Contact, error) {
	ctx := context.Background()

	if g.config == nil || g.token == nil {
		return nil, fmt.Errorf("provider not initialized or not authenticated")
	}

	httpClient := g.config.Client(ctx, g.token)

	// Force a token refresh
	newToken, err := g.config.TokenSource(ctx, g.token).Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	g.token = newToken
	httpClient = g.config.Client(ctx, g.token)

	// Fetch contacts from People API
	var allContacts []Contact
	pageToken := ""

	for {
		// Build URL with person fields
		params := url.Values{
			"personFields": []string{"names,emailAddresses,phoneNumbers,addresses,organizations,birthdays,photos,biographies"},
			"pageSize":     []string{"1000"},
			"sources":      []string{"READ_SOURCE_TYPE_CONTACT"},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		apiURL := "https://people.googleapis.com/v1/people/me/connections?" + params.Encode()

		resp, err := httpClient.Get(apiURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch contacts: %w", err)
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("People API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var result struct {
			Connections     []peopleAPIPerson `json:"connections"`
			NextPageToken   string            `json:"nextPageToken"`
			TotalPeople     int               `json:"totalPeople"`
			TotalItems      int               `json:"totalItems"`
		}

		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode People API response: %w", err)
		}

		// Convert People API persons to our Contact format
		now := time.Now()
		for _, person := range result.Connections {
			contact := convertPeopleAPIToContact(person)
			contact.LastSynced = &now
			allContacts = append(allContacts, contact)
		}

		// Check if there are more pages
		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return allContacts, nil
}

// convertContactToPeopleAPI converts our Contact struct to People API format
func convertContactToPeopleAPI(contact Contact) map[string]interface{} {
	person := make(map[string]interface{})

	// Names
	if contact.FullName != "" || contact.GivenName != "" || contact.FamilyName != "" {
		person["names"] = []map[string]interface{}{
			{
				"givenName":  contact.GivenName,
				"familyName": contact.FamilyName,
			},
		}
	}

	// Phone numbers
	if len(contact.PhoneNumbers) > 0 {
		phones := make([]map[string]interface{}, len(contact.PhoneNumbers))
		for i, phone := range contact.PhoneNumbers {
			phones[i] = map[string]interface{}{
				"value": phone.Value,
				"type":  phone.Type,
			}
		}
		person["phoneNumbers"] = phones
	}

	// Email addresses
	if len(contact.EmailAddresses) > 0 {
		emails := make([]map[string]interface{}, len(contact.EmailAddresses))
		for i, email := range contact.EmailAddresses {
			emails[i] = map[string]interface{}{
				"value": email.Value,
				"type":  email.Type,
			}
		}
		person["emailAddresses"] = emails
	}

	// Addresses
	if len(contact.Addresses) > 0 {
		addresses := make([]map[string]interface{}, len(contact.Addresses))
		for i, addr := range contact.Addresses {
			addresses[i] = map[string]interface{}{
				"streetAddress": addr.Street,
				"city":          addr.City,
				"region":        addr.State,
				"postalCode":    addr.PostalCode,
				"country":       addr.Country,
				"type":          addr.Type,
			}
		}
		person["addresses"] = addresses
	}

	// Organization
	if contact.Organization != nil {
		person["organizations"] = []map[string]interface{}{
			{
				"name":       contact.Organization.Name,
				"title":      contact.Organization.Title,
				"department": contact.Organization.Department,
			},
		}
	}

	// Birthday
	if contact.Birthday != nil {
		person["birthdays"] = []map[string]interface{}{
			{
				"date": map[string]int{
					"year":  contact.Birthday.Year(),
					"month": int(contact.Birthday.Month()),
					"day":   contact.Birthday.Day(),
				},
			},
		}
	}

	// Biography/Notes
	if contact.Notes != "" {
		person["biographies"] = []map[string]interface{}{
			{
				"value": contact.Notes,
			},
		}
	}

	return person
}

// WriteContact writes (creates or updates) a contact in Google via People API
func (g *GoogleContactsProvider) WriteContact(contact Contact) error {
	ctx := context.Background()

	if g.config == nil || g.token == nil {
		return fmt.Errorf("provider not initialized or not authenticated")
	}

	httpClient := g.config.Client(ctx, g.token)
	personData := convertContactToPeopleAPI(contact)

	var req *http.Request
	var apiURL string
	var err error

	// Check if this is an existing contact or a new one
	// UIDs from Google are numeric IDs, new ones are UUIDs
	isExistingGoogleContact := !strings.Contains(contact.UID, "-") // UUIDs have dashes, Google IDs don't

	if isExistingGoogleContact {
		// Update existing contact - reconstruct full resourceName
		resourceName := fmt.Sprintf("people/%s", contact.UID)
		apiURL = fmt.Sprintf("https://people.googleapis.com/v1/%s:updateContact", resourceName)

		// Add updatePersonFields to specify what fields to update
		params := url.Values{}
		params.Set("updatePersonFields", "names,phoneNumbers,emailAddresses,addresses,organizations,birthdays,biographies")
		apiURL += "?" + params.Encode()

		body, _ := json.Marshal(personData)
		req, err = http.NewRequest("PATCH", apiURL, strings.NewReader(string(body)))
	} else {
		// Create new contact
		apiURL = "https://people.googleapis.com/v1/people:createContact"
		body, _ := json.Marshal(personData)
		req, err = http.NewRequest("POST", apiURL, strings.NewReader(string(body)))
	}

	if err != nil {
		return fmt.Errorf("failed to create request for contact %s: %w", contact.FullName, err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update contact %s: %w", contact.FullName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update contact %s (status %d): %s", contact.FullName, resp.StatusCode, string(body))
	}

	return nil
}

// DeleteContact deletes a contact from Google via People API
func (g *GoogleContactsProvider) DeleteContact(uid string) error {
	ctx := context.Background()

	if g.config == nil || g.token == nil {
		return fmt.Errorf("provider not initialized or not authenticated")
	}

	httpClient := g.config.Client(ctx, g.token)

	// Reconstruct full resourceName
	resourceName := fmt.Sprintf("people/%s", uid)
	apiURL := fmt.Sprintf("https://people.googleapis.com/v1/%s:deleteContact", resourceName)

	req, err := http.NewRequest("DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request for contact %s: %w", uid, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete contact %s: %w", uid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete contact %s (status %d): %s", uid, resp.StatusCode, string(body))
	}

	return nil
}
