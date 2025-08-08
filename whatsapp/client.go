package whatsapp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/store"
)

// ClientManager manages WhatsApp clients for senders
type ClientManager struct {
	userStoreManager *store.UserStoreManager
	db               *database.Database
	gormDB           *database.GormDB
	messageHandler   *MessageHandler
	clientToPhone    map[*whatsmeow.Client]string // Maps client to phone number
	mu               sync.RWMutex
}

// NewClientManager creates a new WhatsApp client manager
func NewClientManager(userStoreManager *store.UserStoreManager, db *database.Database, gormDB *database.GormDB) *ClientManager {
	messageHandler := NewMessageHandler(gormDB)
	return &ClientManager{
		userStoreManager: userStoreManager,
		db:               db,
		gormDB:           gormDB,
		messageHandler:   messageHandler,
		clientToPhone:    make(map[*whatsmeow.Client]string),
	}
}

// LoadAllSenders loads all previously registered senders from database
func (cm *ClientManager) LoadAllSenders() error {
	senders, err := cm.db.GetAllSenders()
	if err != nil {
		return fmt.Errorf("failed to load senders: %v", err)
	}

	if len(senders) == 0 {
		log.Println("No registered senders found")
		return nil
	}

	log.Printf("Found %d registered senders, attempting auto-authentication...", len(senders))

	successCount := 0
	for _, sender := range senders {
		// Try to authenticate any sender that has a device_id
		if sender.DeviceID != "" {
			log.Printf("Attempting to authenticate sender %s with device_id %s (current status: %s)",
				sender.Phone, sender.DeviceID, sender.Status)

			userClient, err := cm.userStoreManager.LoadUserStore(sender.Phone, sender.DeviceID)
			if err != nil {
				log.Printf("Failed to auto-authenticate sender %s: %v", sender.Phone, err)
				// Mark as invalidated if we can't load the client
				cm.db.UpdateSenderStatus(sender.Phone, "invalidated")
				continue
			}

			// Check if client is connected
			if userClient.IsConnected() {
				// Register message handler for user client with phone number
				userClient.AddEventHandler(cm.createMessageHandler(sender.Phone))
				// Store client to phone mapping
				cm.mu.Lock()
				cm.clientToPhone[userClient] = sender.Phone
				cm.mu.Unlock()
				// Update status to authenticated
				cm.db.UpdateSenderStatus(sender.Phone, "authenticated")
				successCount++
				log.Printf("✅ Auto-authenticated sender: %s", sender.Phone)
			} else {
				log.Printf("⚠️  Sender %s client loaded but not connected", sender.Phone)
				cm.db.UpdateSenderStatus(sender.Phone, "invalidated")
			}
		} else {
			log.Printf("⚠️  Sender %s has no device_id (status: %s)", sender.Phone, sender.Status)
		}
	}

	log.Printf("Successfully auto-authenticated %d/%d sender clients", successCount, len(senders))
	return nil
}

// createMessageHandler creates a message handler for a specific authenticated sender
func (cm *ClientManager) createMessageHandler(authenticatedSenderPhone string) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			// Store message in database with the authenticated sender's phone number
			if err := cm.messageHandler.HandleMessageEvent(v, authenticatedSenderPhone); err != nil {
				log.Printf("Failed to store user message: %v", err)
			}
		}
	}
}

// MonitorConnections periodically checks if clients are still connected
func (cm *ClientManager) MonitorConnections() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.checkConnections()
			// Periodically sync to MSSQL
			cm.syncToMSSQL()
		}
	}
}

// checkConnections checks the connection status of all user clients
func (cm *ClientManager) checkConnections() {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	userClients := cm.userStoreManager.GetAllUserClients()
	for phone, client := range userClients {
		if !client.IsConnected() {
			log.Printf("🔴 Client %s is disconnected", phone)
			// Mark sender as invalidated in SQLite (which will sync to MSSQL)
			cm.db.UpdateSenderStatus(phone, "invalidated")
		}
	}
}

// syncToMSSQL periodically syncs all senders from SQLite to MSSQL
func (cm *ClientManager) syncToMSSQL() {
	if err := cm.gormDB.ForceSyncAllSenders(cm.db); err != nil {
		log.Printf("Warning: Failed to sync senders to MSSQL: %v", err)
	}
}

// SendMessage sends a WhatsApp message using a registered user client
func (cm *ClientManager) SendMessage(senderPhone, recipient, message string) error {
	cm.mu.RLock()
	client, exists := cm.userStoreManager.GetUserClient(senderPhone)
	cm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("sender %s is not registered", senderPhone)
	}

	if !client.IsConnected() {
		return fmt.Errorf("sender %s is not connected", senderPhone)
	}

	// Create message
	msg := &waProto.Message{
		Conversation: proto.String(message),
	}

	// Send message
	_, err := client.SendMessage(context.Background(), types.JID{
		User:   recipient,
		Server: types.DefaultUserServer,
	}, msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %v", err)
	}

	log.Printf("Message sent from %s to %s: %s", senderPhone, recipient, message)
	return nil
}

// Shutdown gracefully shuts down all clients
func (cm *ClientManager) Shutdown() {
	cm.userStoreManager.CloseAll()
}
