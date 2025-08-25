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

// CreateQRCodeSessionWithContext creates a new QR code session with context support
func (qm *QRManager) CreateQRCodeSessionWithContext(ctx context.Context, phone string, baseURL string, expiryMinutes int) (*QRCodeSession, error) {
	log.Printf("DEBUG: CreateQRCodeSessionWithContext called for phone: %s, baseURL: %s, expiryMinutes: %d", phone, baseURL, expiryMinutes)

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		log.Printf("DEBUG: Context cancelled before processing for phone: %s", phone)
		return nil, ctx.Err()
	default:
	}

	// Always allow fresh registration - remove any existing sessions first
	log.Printf("Processing registration for %s - allowing fresh authentication", phone)

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Remove any existing sessions for this phone number
	qm.removeSessionsForPhone(phone)

	// Check if sender already exists
	if qm.db.SenderExists(phone) {
		sender, err := qm.db.GetSender(phone)
		if err != nil {
			return nil, fmt.Errorf("failed to get sender status: %v", err)
		}

		log.Printf("Sender %s exists with status: %s - updating to pending", phone, sender.Status)

		// Update status to pending for fresh authentication
		if err := qm.db.UpdateSenderStatus(phone, "pending"); err != nil {
			return nil, fmt.Errorf("failed to update sender status: %v", err)
		}
	} else {
		// Create new sender record
		log.Printf("Creating new sender record for %s", phone)
		if err := qm.db.CreateSender(phone); err != nil {
			return nil, fmt.Errorf("failed to create sender: %v", err)
		}
	}

	// Check context again
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		log.Printf("DEBUG: Failed to generate token for phone: %s, error: %v", phone, err)
		return nil, fmt.Errorf("failed to generate token: %v", err)
	}
	log.Printf("DEBUG: Generated token for phone: %s, token: %s", phone, token)

	// Create WhatsApp client
	log.Printf("DEBUG: Creating WhatsApp client for phone: %s", phone)
	client, err := qm.userStoreManager.CreateUserStore(phone)
	if err != nil {
		log.Printf("DEBUG: Failed to create WhatsApp client for phone: %s, error: %v", phone, err)
		return nil, fmt.Errorf("failed to create WhatsApp client: %v", err)
	}
	log.Printf("DEBUG: WhatsApp client created successfully for phone: %s", phone)

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
	log.Printf("DEBUG: Session stored for phone: %s, token: %s, expires at: %v", phone, token, session.ExpiresAt)

	// Start QR code generation in background with a separate context
	// Use a background context for QR generation to avoid cancellation
	log.Printf("DEBUG: Starting background QR generation for phone: %s", phone)
	go qm.generateQRCodeWithContext(context.Background(), session)

	return session, nil
}

// CreateQRCodeSession creates a new QR code session for a phone number (legacy method)
func (qm *QRManager) CreateQRCodeSession(phone string, baseURL string, expiryMinutes int) (*QRCodeSession, error) {
	return qm.CreateQRCodeSessionWithContext(context.Background(), phone, baseURL, expiryMinutes)
}

// generateQRCodeWithContext generates QR code for a session with context support
func (qm *QRManager) generateQRCodeWithContext(ctx context.Context, session *QRCodeSession) {
	log.Printf("DEBUG: generateQRCodeWithContext started for phone: %s", session.Phone)
	log.Printf("Starting QR code generation for %s", session.Phone)

	// Get QR channel with context
	log.Printf("DEBUG: Getting QR channel for phone: %s", session.Phone)
	qrChan, err := session.Client.GetQRChannel(ctx)
	if err != nil {
		log.Printf("DEBUG: Failed to get QR channel for phone: %s, error: %v", session.Phone, err)
		log.Printf("Failed to get QR channel for %s: %v", session.Phone, err)
		qm.updateSessionStatus(session, "expired")
		return
	}
	log.Printf("DEBUG: QR channel obtained successfully for phone: %s", session.Phone)

	// Connect client
	log.Printf("DEBUG: Connecting WhatsApp client for phone: %s", session.Phone)
	if err := session.Client.Connect(); err != nil {
		log.Printf("DEBUG: Failed to connect client for phone: %s, error: %v", session.Phone, err)
		log.Printf("Failed to connect client for %s: %v", session.Phone, err)
		qm.updateSessionStatus(session, "expired")
		return
	}
	log.Printf("DEBUG: WhatsApp client connected successfully for phone: %s", session.Phone)

	// Wait for QR code with context cancellation
	log.Printf("DEBUG: Starting event loop for phone: %s", session.Phone)
	for {
		select {
		case evt, ok := <-qrChan:
			if !ok {
				// Channel closed
				log.Printf("DEBUG: QR channel closed for phone: %s", session.Phone)
				log.Printf("QR channel closed for %s", session.Phone)
				qm.updateSessionStatus(session, "expired")
				return
			}

			log.Printf("DEBUG: Received event for phone: %s, event: %s", session.Phone, evt.Event)
			log.Printf("Received event for %s: %s", session.Phone, evt.Event)
			session.mu.Lock()
			switch evt.Event {
			case "code":
				session.QRCode = evt.Code
				session.Status = "pending"
				log.Printf("QR code generated for %s, code length: %d", session.Phone, len(evt.Code))

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
			default:
				log.Printf("Unknown event for %s: %s", session.Phone, evt.Event)
			}
			session.mu.Unlock()

		case <-ctx.Done():
			log.Printf("DEBUG: QR code generation cancelled for phone: %s, error: %v", session.Phone, ctx.Err())
			log.Printf("QR code generation cancelled for %s: %v", session.Phone, ctx.Err())
			session.mu.Lock()
			session.Status = "expired"
			session.mu.Unlock()
			qm.updateSessionStatus(session, "expired")
			return
		}
	}
}

// generateQRCode generates QR code for a session (legacy method)
func (qm *QRManager) generateQRCode(session *QRCodeSession) {
	qm.generateQRCodeWithContext(context.Background(), session)
}

// updateSessionStatus updates the session status and database
func (qm *QRManager) updateSessionStatus(session *QRCodeSession, status string) {
	log.Printf("DEBUG: Updating session status for phone: %s, from: %s, to: %s", session.Phone, session.Status, status)
	session.mu.Lock()
	session.Status = status
	session.mu.Unlock()

	// Update database
	if err := qm.db.UpdateSenderStatus(session.Phone, status); err != nil {
		log.Printf("DEBUG: Failed to update sender status in database for phone: %s, error: %v", session.Phone, err)
		log.Printf("Failed to update sender status for %s: %v", session.Phone, err)
	}
}

// GetQRCodeWithContext retrieves the QR code for a given token with context support
func (qm *QRManager) GetQRCodeWithContext(ctx context.Context, token string) (*QRCodeSession, error) {
	log.Printf("DEBUG: GetQRCodeWithContext called with token: %s", token)

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		log.Printf("DEBUG: Context cancelled in GetQRCodeWithContext for token: %s", token)
		return nil, ctx.Err()
	default:
	}

	qm.mu.RLock()
	session, exists := qm.sessions[token]
	qm.mu.RUnlock()

	if !exists {
		log.Printf("DEBUG: Session not found for token: %s", token)
		return nil, fmt.Errorf("session not found")
	}

	log.Printf("DEBUG: Session found for token: %s, phone: %s, status: %s", token, session.Phone, session.Status)

	// Check if session is expired
	now := time.Now()
	log.Printf("DEBUG: Checking session expiry for token: %s, current time: %v, expires at: %v", token, now, session.ExpiresAt)
	if now.After(session.ExpiresAt) {
		log.Printf("DEBUG: Session expired for token: %s", token)
		session.Status = "expired"
		qm.updateSessionStatus(session, "expired")
		return nil, fmt.Errorf("QR code session expired")
	}

	// Wait for QR code to be generated (with timeout)
	log.Printf("DEBUG: Starting QR code wait loop for token: %s", token)
	timeout := time.After(30 * time.Second)          // 30 second timeout
	ticker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("DEBUG: Context cancelled in QR wait loop for token: %s", token)
			return nil, ctx.Err()
		case <-timeout:
			log.Printf("DEBUG: QR code generation timeout for token: %s", token)
			return nil, fmt.Errorf("QR code generation timeout")
		case <-ticker.C:
			session.mu.RLock()
			qrCode := session.QRCode
			status := session.Status
			session.mu.RUnlock()

			if qrCode != "" {
				log.Printf("DEBUG: QR code found for token: %s, length: %d", token, len(qrCode))
				return session, nil
			}

			if status == "expired" || status == "authenticated" {
				log.Printf("DEBUG: Session status changed to %s for token: %s, stopping wait", status, token)
				return session, nil
			}
		}
	}
}

// GetQRCode retrieves the QR code for a given token (legacy method)
func (qm *QRManager) GetQRCode(token string) (*QRCodeSession, error) {
	return qm.GetQRCodeWithContext(context.Background(), token)
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
