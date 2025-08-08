package whatsapp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jaliph/auto-dm/models"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
)

// QRCodeSession represents a QR code session for a phone number
type QRCodeSession struct {
	Phone     string
	Token     string
	QRCode    string
	ExpiresAt time.Time
	Client    *whatsmeow.Client
	Status    string // "pending", "authenticated", "expired"
	mu        sync.RWMutex
}

// QRManager manages QR code sessions for sender authentication
type QRManager struct {
	sessions         map[string]*QRCodeSession // token -> session
	mu               sync.RWMutex
	db               Database
	userStoreManager UserStoreManager
}

// Database interface for QR manager
type Database interface {
	CreateSender(phone string) error
	GetSender(phone string) (*models.Sender, error)
	UpdateSenderStatus(phone, status string) error
	UpdateSenderDeviceID(phone, deviceID string) error
	SenderExists(phone string) bool
}

// UserStoreManager interface for QR manager
type UserStoreManager interface {
	CreateUserStore(phone string) (*whatsmeow.Client, error)
	LoadUserStore(phone, deviceID string) (*whatsmeow.Client, error)
	GetUserClient(phone string) (*whatsmeow.Client, bool)
	DisconnectUser(phone string)
	GetAllUserClients() map[string]*whatsmeow.Client
	CloseAll()
}

// NewQRManager creates a new QR code manager
func NewQRManager(db Database, userStoreManager UserStoreManager) *QRManager {
	return &QRManager{
		sessions:         make(map[string]*QRCodeSession),
		db:               db,
		userStoreManager: userStoreManager,
	}
}

// generateToken generates a random token for QR code session
func generateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateQRCodeSession creates a new QR code session for a phone number
func (qm *QRManager) CreateQRCodeSession(phone string, baseURL string, expiryMinutes int) (*QRCodeSession, error) {
	// Check if sender already exists and get their status
	if qm.db.SenderExists(phone) {
		sender, err := qm.db.GetSender(phone)
		if err != nil {
			return nil, fmt.Errorf("failed to get sender status: %v", err)
		}

		// Allow re-registration for failed senders (expired or invalidated)
		if sender.Status == "authenticated" {
			return nil, fmt.Errorf("sender %s is already authenticated", phone)
		}

		// For failed senders, we'll reuse the existing record but update the status
		log.Printf("Re-registering failed sender %s (previous status: %s)", phone, sender.Status)
		if err := qm.db.UpdateSenderStatus(phone, "pending"); err != nil {
			return nil, fmt.Errorf("failed to update sender status: %v", err)
		}

		// Remove any existing sessions for this phone number
		qm.removeSessionsForPhone(phone)
	} else {
		// Create new sender record
		if err := qm.db.CreateSender(phone); err != nil {
			return nil, fmt.Errorf("failed to create sender: %v", err)
		}
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %v", err)
	}

	// Create WhatsApp client
	client, err := qm.userStoreManager.CreateUserStore(phone)
	if err != nil {
		return nil, fmt.Errorf("failed to create WhatsApp client: %v", err)
	}

	// Create session
	session := &QRCodeSession{
		Phone:     phone,
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(expiryMinutes) * time.Minute),
		Client:    client,
		Status:    "pending",
	}

	// Store session
	qm.mu.Lock()
	qm.sessions[token] = session
	qm.mu.Unlock()

	// Start QR code generation in background
	go qm.generateQRCode(session)

	return session, nil
}

// generateQRCode generates QR code for a session
func (qm *QRManager) generateQRCode(session *QRCodeSession) {
	log.Printf("Starting QR code generation for %s", session.Phone)

	// Get QR channel
	qrChan, _ := session.Client.GetQRChannel(context.Background())

	// Connect client
	if err := session.Client.Connect(); err != nil {
		log.Printf("Failed to connect client for %s: %v", session.Phone, err)
		qm.updateSessionStatus(session, "expired")
		return
	}

	// Wait for QR code
	for evt := range qrChan {
		session.mu.Lock()

		switch evt.Event {
		case "code":
			session.QRCode = evt.Code
			session.Status = "pending"
			log.Printf("QR code generated for %s", session.Phone)

		case "timeout":
			session.Status = "expired"
			log.Printf("QR code timed out for %s", session.Phone)
			qm.updateSessionStatus(session, "expired")
			session.mu.Unlock()
			return

		case "success":
			session.Status = "authenticated"
			log.Printf("Authentication successful for %s", session.Phone)

			// Update database
			if session.Client.Store.ID != nil {
				qm.db.UpdateSenderDeviceID(session.Phone, session.Client.Store.ID.String())
			}
			qm.updateSessionStatus(session, "authenticated")
			session.mu.Unlock()
			return
		}

		session.mu.Unlock()
	}
}

// updateSessionStatus updates the session status and database
func (qm *QRManager) updateSessionStatus(session *QRCodeSession, status string) {
	session.mu.Lock()
	session.Status = status
	session.mu.Unlock()

	// Update database
	if err := qm.db.UpdateSenderStatus(session.Phone, status); err != nil {
		log.Printf("Failed to update sender status for %s: %v", session.Phone, err)
	}
}

// GetQRCode retrieves the QR code for a given token
func (qm *QRManager) GetQRCode(token string) (*QRCodeSession, error) {
	log.Printf("DEBUG: GetQRCode called with token: %s", token)

	qm.mu.RLock()
	session, exists := qm.sessions[token]
	qm.mu.RUnlock()

	log.Printf("DEBUG: Session exists: %v", exists)
	if !exists {
		log.Printf("DEBUG: Session not found for token: %s", token)
		return nil, fmt.Errorf("session not found")
	}

	log.Printf("DEBUG: Found session for phone: %s, status: %s, expires at: %v",
		session.Phone, session.Status, session.ExpiresAt)

	session.mu.RLock()
	defer session.mu.RUnlock()

	// Check if session is expired
	now := time.Now()
	log.Printf("DEBUG: Current time: %v, Expires at: %v", now, session.ExpiresAt)
	if now.After(session.ExpiresAt) {
		log.Printf("DEBUG: Session expired for token: %s", token)
		session.Status = "expired"
		qm.updateSessionStatus(session, "expired")
		return nil, fmt.Errorf("QR code session expired")
	}

	log.Printf("DEBUG: Session is valid, returning session")
	return session, nil
}

// CleanupExpiredSessions removes expired sessions
func (qm *QRManager) CleanupExpiredSessions() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	now := time.Now()
	for token, session := range qm.sessions {
		if now.After(session.ExpiresAt) {
			session.Status = "expired"
			qm.updateSessionStatus(session, "expired")
			delete(qm.sessions, token)
		}
	}
}

// StartCleanup starts periodic cleanup of expired sessions
func (qm *QRManager) StartCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			qm.CleanupExpiredSessions()
		}
	}()
}

// GetSessionByPhone retrieves a session by phone number
func (qm *QRManager) GetSessionByPhone(phone string) *QRCodeSession {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	for _, session := range qm.sessions {
		if session.Phone == phone {
			return session
		}
	}
	return nil
}

// removeSessionsForPhone removes all sessions for a specific phone number
func (qm *QRManager) removeSessionsForPhone(phone string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	for token, session := range qm.sessions {
		if session.Phone == phone {
			delete(qm.sessions, token)
			log.Printf("Removed existing session for phone %s", phone)
		}
	}
}

// GetQRCodeImage generates a QR code image as PNG
func (qm *QRManager) GetQRCodeImage(qrCode string) ([]byte, error) {
	qr, err := qrcode.New(qrCode, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %v", err)
	}

	return qr.PNG(256)
}

// GetQRCodePNGBase64 generates a QR code PNG image and returns it as base64 string
func (qm *QRManager) GetQRCodePNGBase64(qrCode string) (string, error) {
	pngData, err := qm.GetQRCodeImage(qrCode)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code PNG: %v", err)
	}

	// Encode PNG data to base64
	base64Data := base64.StdEncoding.EncodeToString(pngData)
	return base64Data, nil
}
