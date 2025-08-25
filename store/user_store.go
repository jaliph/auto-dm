package store

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

// UserStoreManager manages individual user WhatsApp stores
type UserStoreManager struct {
	userClients map[string]*whatsmeow.Client   // phone -> client
	containers  map[string]*sqlstore.Container // phone -> container
}

// NewUserStoreManager creates a new user store manager
func NewUserStoreManager() *UserStoreManager {
	return &UserStoreManager{
		userClients: make(map[string]*whatsmeow.Client),
		containers:  make(map[string]*sqlstore.Container),
	}
}

// CreateUserStore creates a new WhatsApp store for a specific user (without connecting)
func (usm *UserStoreManager) CreateUserStore(phone string) (*whatsmeow.Client, error) {
	// Ensure db directory exists
	if err := os.MkdirAll("db", 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %v", err)
	}

	// Create user-specific database file (cross-platform)
	dbPath := filepath.Join("db", fmt.Sprintf("user_%s.db", phone))

	// Create the store container for this user
	container, err := sqlstore.New(context.Background(), "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user store container for %s: %v", phone, err)
	}

	// Get the first device store for user (loads existing session)
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		// If no device exists, create a new one
		log.Printf("No existing device found for %s, creating new one", phone)
		deviceStore = container.NewDevice()
	}

	// Create user client (without connecting)
	userClient := whatsmeow.NewClient(deviceStore, nil)

	// Store references
	usm.userClients[phone] = userClient
	usm.containers[phone] = container

	log.Printf("Created user store for %s", phone)
	return userClient, nil
}

// CreateFreshUserStore creates a completely fresh user store for QR generation
// This method deletes any existing database and creates a new one
func (usm *UserStoreManager) CreateFreshUserStore(phone string) (*whatsmeow.Client, error) {
	// Ensure db directory exists
	if err := os.MkdirAll("db", 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %v", err)
	}

	// Create user-specific database file (cross-platform)
	dbPath := filepath.Join("db", fmt.Sprintf("user_%s.db", phone))

	// Remove existing database file if it exists
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: Failed to remove existing database file for %s: %v", phone, err)
	}

	// Create the store container for this user
	container, err := sqlstore.New(context.Background(), "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user store container for %s: %v", phone, err)
	}

	// Always create a new device store (no existing session)
	deviceStore := container.NewDevice()

	// Create user client (without connecting)
	userClient := whatsmeow.NewClient(deviceStore, nil)

	// Don't store references for QR generation clients
	// They will be cleaned up after QR generation is complete

	log.Printf("Created fresh user store for QR generation for %s", phone)
	return userClient, nil
}

// StoreAuthenticatedClient stores an authenticated client for a phone number
func (usm *UserStoreManager) StoreAuthenticatedClient(phone string, client *whatsmeow.Client) {
	log.Printf("DEBUG: StoreAuthenticatedClient called for phone: %s", phone)

	// Store the authenticated client
	usm.userClients[phone] = client

	// Get the container for this client (it should already exist from QR generation)
	// We don't need to store the container again as it's already managed

	log.Printf("Stored authenticated client for %s", phone)
	log.Printf("DEBUG: StoreAuthenticatedClient - userClients map now has %d entries", len(usm.userClients))
}

// GetUserClient returns a user client by phone number
func (usm *UserStoreManager) GetUserClient(phone string) (*whatsmeow.Client, bool) {
	client, exists := usm.userClients[phone]
	return client, exists
}

// LoadUserStore loads an existing user store from database
func (usm *UserStoreManager) LoadUserStore(phone, deviceID string) (*whatsmeow.Client, error) {
	// Ensure db directory exists
	if err := os.MkdirAll("db", 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %v", err)
	}

	// Create user-specific database file (cross-platform)
	dbPath := filepath.Join("db", fmt.Sprintf("user_%s.db", phone))

	// Create the store container for this user
	container, err := sqlstore.New(context.Background(), "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user store container for %s: %v", phone, err)
	}

	// Get the first device store for user (loads existing session)
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		// If no device exists, this is unexpected for existing users
		log.Printf("User %s needs authentication, but this is unexpected for existing users", phone)
		return nil, fmt.Errorf("user %s needs authentication", phone)
	}

	// Create user client
	userClient := whatsmeow.NewClient(deviceStore, nil)

	// Check if user is already authenticated
	if userClient.Store.ID == nil {
		// User needs to authenticate - this shouldn't happen for existing users
		// but we'll handle it gracefully
		log.Printf("User %s needs authentication, but this is unexpected for existing users", phone)
		return nil, fmt.Errorf("user %s needs authentication", phone)
	} else {
		// User is already authenticated, connect them
		log.Printf("User %s already authenticated as: %s", phone, userClient.Store.ID)

		// Connect the already authenticated user client
		if err := userClient.Connect(); err != nil {
			return nil, fmt.Errorf("failed to connect authenticated user client for %s: %v", phone, err)
		}
		log.Printf("User %s connected successfully", phone)
	}

	// Store references
	usm.userClients[phone] = userClient
	usm.containers[phone] = container

	log.Printf("Loaded user store for %s", phone)
	return userClient, nil
}

// DisconnectUser disconnects a user client
func (usm *UserStoreManager) DisconnectUser(phone string) {
	if client, exists := usm.userClients[phone]; exists {
		client.Disconnect()
		delete(usm.userClients, phone)
		log.Printf("Disconnected user client: %s", phone)
	}
}

// LogoutUser logs out a user client and deletes their device store
func (usm *UserStoreManager) LogoutUser(phone string) error {
	log.Printf("DEBUG: LogoutUser called for phone: %s", phone)
	log.Printf("DEBUG: LogoutUser - userClients map has %d entries", len(usm.userClients))
	for phoneKey := range usm.userClients {
		log.Printf("DEBUG: LogoutUser - Found client for phone: %s", phoneKey)
	}

	if client, exists := usm.userClients[phone]; exists {
		// Logout from WhatsApp (this will unlink the device)
		ctx := context.Background()
		if err := client.Logout(ctx); err != nil {
			log.Printf("Warning: Failed to logout user %s from WhatsApp: %v", phone, err)
			// Continue with local cleanup even if logout fails
		} else {
			log.Printf("Successfully logged out user %s from WhatsApp", phone)
		}

		// Disconnect the client
		client.Disconnect()
		delete(usm.userClients, phone)
		log.Printf("Disconnected user client: %s", phone)

		// Delete the device store from database (only if JID is known)
		if _, exists := usm.containers[phone]; exists {
			if client.Store.ID != nil {
				if err := client.Store.Delete(ctx); err != nil {
					log.Printf("Warning: Failed to delete device store for user %s: %v", phone, err)
				} else {
					log.Printf("Successfully deleted device store for user %s", phone)
				}
			} else {
				log.Printf("Skipping device store deletion for user %s (JID not known)", phone)
			}
			delete(usm.containers, phone)
		}

		// Delete the database file
		dbPath := filepath.Join("db", fmt.Sprintf("user_%s.db", phone))
		if err := os.Remove(dbPath); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Warning: Failed to delete database file for user %s: %v", phone, err)
			}
		} else {
			log.Printf("Successfully deleted database file for user %s", phone)
		}

		return nil
	}
	return fmt.Errorf("user %s not found", phone)
}

// GetAllUserClients returns all user clients
func (usm *UserStoreManager) GetAllUserClients() map[string]*whatsmeow.Client {
	return usm.userClients
}

// CloseAll closes all user stores
func (usm *UserStoreManager) CloseAll() {
	for phone, client := range usm.userClients {
		client.Disconnect()
		log.Printf("Disconnected user client: %s", phone)
	}
	usm.userClients = make(map[string]*whatsmeow.Client)
	usm.containers = make(map[string]*sqlstore.Container)
}
