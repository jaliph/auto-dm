package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/store"
	"github.com/jaliph/auto-dm/utils"
)

// ClientManager manages WhatsApp clients for senders
type ClientManager struct {
	userStoreManager *store.UserStoreManager
	db               *database.Database
	gormDB           *database.GormDB
	messageHandler   *MessageHandler
	clientToPhone    map[*whatsmeow.Client]string // Maps client to phone number
	receiveFolder    string                       // Folder to store received media files
	mu               sync.RWMutex
}

// NewClientManager creates a new WhatsApp client manager
func NewClientManager(userStoreManager *store.UserStoreManager, db *database.Database, gormDB *database.GormDB, receiveFolder string, ollamaClient *utils.OllamaClient) *ClientManager {
	messageHandler := NewMessageHandler(gormDB, receiveFolder, ollamaClient, nil) // Will set clientManager reference after creation
	clientManager := &ClientManager{
		userStoreManager: userStoreManager,
		db:               db,
		gormDB:           gormDB,
		messageHandler:   messageHandler,
		clientToPhone:    make(map[*whatsmeow.Client]string),
		receiveFolder:    receiveFolder,
	}
	// Set the client manager reference in the message handler
	messageHandler.clientManager = clientManager
	return clientManager
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
			// Get the client for this sender
			cm.mu.RLock()
			client, exists := cm.getClientForPhone(authenticatedSenderPhone)
			cm.mu.RUnlock()

			if !exists {
				log.Printf("Client not found for phone %s, cannot download media", authenticatedSenderPhone)
				client = nil
			}

			// Store message in database with the authenticated sender's phone number
			if err := cm.messageHandler.HandleMessageEvent(v, authenticatedSenderPhone, client); err != nil {
				log.Printf("Failed to store user message: %v", err)
			}
		}
	}
}

// MonitorConnections periodically checks if clients are still connected
func (cm *ClientManager) MonitorConnections() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cm.checkConnections()
		// Periodically sync to MSSQL
		cm.syncToMSSQL()
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
// This is a more conservative sync that doesn't overwrite authenticated status
func (cm *ClientManager) syncToMSSQL() {
	// Only sync senders that are not authenticated to avoid overwriting authenticated status
	senders, err := cm.db.GetAllSenders()
	if err != nil {
		log.Printf("Warning: Failed to get senders for sync: %v", err)
		return
	}

	for _, sender := range senders {
		// Only sync non-authenticated senders to avoid race conditions
		if sender.Status != "authenticated" {
			if err := cm.gormDB.SyncSenderToMSSQL(sender); err != nil {
				log.Printf("Warning: Failed to sync sender %s to MSSQL: %v", sender.Phone, err)
			}
		}
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

// SendFile sends a WhatsApp file using a registered user client
func (cm *ClientManager) SendFile(senderPhone, recipient, filePath string) error {
	cm.mu.RLock()
	client, exists := cm.userStoreManager.GetUserClient(senderPhone)
	cm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("sender %s is not registered", senderPhone)
	}

	if !client.IsConnected() {
		return fmt.Errorf("sender %s is not connected", senderPhone)
	}

	// Read file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}

	// Upload file to WhatsApp
	uploaded, err := client.Upload(context.Background(), fileData, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}

	// Create document message
	msg := &waProto.Message{
		DocumentMessage: &waProto.DocumentMessage{
			URL:           &uploaded.URL,
			Mimetype:      proto.String("application/octet-stream"),
			FileName:      proto.String(fileInfo.Name()),
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		},
	}

	// Send file
	_, err = client.SendMessage(context.Background(), types.JID{
		User:   recipient,
		Server: types.DefaultUserServer,
	}, msg)
	if err != nil {
		return fmt.Errorf("failed to send file: %v", err)
	}

	log.Printf("File sent from %s to %s: %s", senderPhone, recipient, fileInfo.Name())
	return nil
}

// getClientForPhone returns the client for a specific phone number
func (cm *ClientManager) getClientForPhone(phone string) (*whatsmeow.Client, bool) {
	for client, clientPhone := range cm.clientToPhone {
		if clientPhone == phone {
			return client, true
		}
	}
	return nil, false
}

// removeClientForPhone removes a client from the clientToPhone map
func (cm *ClientManager) removeClientForPhone(phone string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for client, clientPhone := range cm.clientToPhone {
		if clientPhone == phone {
			delete(cm.clientToPhone, client)
			log.Printf("Removed client mapping for phone %s", phone)
			return
		}
	}
}

// OnAuthenticationSuccess is called when a user successfully authenticates via QR code
func (cm *ClientManager) OnAuthenticationSuccess(phone string, client *whatsmeow.Client) {
	log.Printf("DEBUG: OnAuthenticationSuccess called for %s", phone)
	log.Printf("Authentication success callback for %s", phone)

	// Add the authenticated client to the client manager
	cm.mu.Lock()
	cm.clientToPhone[client] = phone
	cm.mu.Unlock()

	// Register message handler for the authenticated client
	client.AddEventHandler(cm.createMessageHandler(phone))

	// Update status to authenticated in database
	log.Printf("DEBUG: OnAuthenticationSuccess - Updating status to authenticated for %s", phone)
	cm.db.UpdateSenderStatus(phone, "authenticated")

	// Immediately sync this specific sender to MSSQL to avoid race conditions
	log.Printf("DEBUG: OnAuthenticationSuccess - Getting sender from database for %s", phone)
	if sender, err := cm.db.GetSender(phone); err == nil {
		log.Printf("DEBUG: OnAuthenticationSuccess - Got sender from database: %s, status: %s", phone, sender.Status)
		if err := cm.gormDB.SyncSenderToMSSQL(sender); err != nil {
			log.Printf("Warning: Failed to sync authenticated sender %s to MSSQL: %v", phone, err)
		} else {
			log.Printf("Successfully synced authenticated sender %s to MSSQL", phone)
		}
	} else {
		log.Printf("DEBUG: OnAuthenticationSuccess - Failed to get sender from database for %s: %v", phone, err)
	}

	log.Printf("✅ Successfully integrated authenticated client for %s", phone)
}

// DeleteUser handles the complete deletion of a user from the client manager
func (cm *ClientManager) DeleteUser(phone string) {
	log.Printf("DEBUG: DeleteUser called for phone: %s", phone)

	// Remove from client mapping
	cm.removeClientForPhone(phone)

	// The actual logout and cleanup is handled by UserStoreManager.LogoutUser()
	log.Printf("Removed user %s from client manager", phone)
}

// Shutdown gracefully shuts down all clients
func (cm *ClientManager) Shutdown() {
	cm.userStoreManager.CloseAll()
}
