package store

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

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
