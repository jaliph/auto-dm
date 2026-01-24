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
	authCallback     AuthenticationCallback // Callback for authentication success
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
	CreateFreshUserStore(phone string) (*whatsmeow.Client, error)
	LoadUserStore(phone, deviceID string) (*whatsmeow.Client, error)
	GetUserClient(phone string) (*whatsmeow.Client, bool)
	StoreAuthenticatedClient(phone string, client *whatsmeow.Client)
	DisconnectUser(phone string)
	GetAllUserClients() map[string]*whatsmeow.Client
	CloseAll()
}

// AuthenticationCallback interface for notifying when authentication succeeds
type AuthenticationCallback interface {
	OnAuthenticationSuccess(phone string, client *whatsmeow.Client)
}

// NewQRManager creates a new QR code manager
func NewQRManager(db Database, userStoreManager UserStoreManager, authCallback AuthenticationCallback) *QRManager {
	return &QRManager{
		sessions:         make(map[string]*QRCodeSession),
		db:               db,
		userStoreManager: userStoreManager,
		authCallback:     authCallback,
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
	log.Printf("DEBUG: CreateQRCodeSessionWithContext START - phone: %s, expiryMinutes: %d", phone, expiryMinutes)

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		log.Printf("DEBUG: Context cancelled before processing for phone: %s", phone)
		return nil, ctx.Err()
	default:
	}

	// Always allow fresh registration - remove any existing sessions first
	log.Printf("DEBUG: Processing registration for %s", phone)

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

		log.Printf("Sender %s exists with status: %s", phone, sender.Status)

		// Check if user is already authenticated
		if sender.Status == "authenticated" {
			log.Printf("User %s is already authenticated, cannot create QR session", phone)
			return nil, fmt.Errorf("user %s is already authenticated", phone)
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

	// Create fresh WhatsApp client for QR generation
	log.Printf("DEBUG: Creating fresh WhatsApp client for QR generation for phone: %s", phone)
	client, err := qm.userStoreManager.CreateFreshUserStore(phone)
	if err != nil {
		log.Printf("DEBUG: Failed to create fresh WhatsApp client for phone: %s, error: %v", phone, err)
		return nil, fmt.Errorf("failed to create fresh WhatsApp client: %v", err)
	}
	log.Printf("DEBUG: Fresh WhatsApp client created successfully for phone: %s", phone)

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
	log.Printf("DEBUG: Session stored - phone: %s, token: %s, expiresAt: %v", phone, token, session.ExpiresAt)

	// Start QR code generation in background with a separate context
	log.Printf("DEBUG: Starting QR generation goroutine for phone: %s", phone)
	go qm.generateQRCodeWithContext(context.Background(), session)

	log.Printf("DEBUG: CreateQRCodeSessionWithContext END - phone: %s, token: %s", phone, token)
	return session, nil
}

// CreateQRCodeSession creates a new QR code session for a phone number (legacy method)
func (qm *QRManager) CreateQRCodeSession(phone string, baseURL string, expiryMinutes int) (*QRCodeSession, error) {
	return qm.CreateQRCodeSessionWithContext(context.Background(), phone, baseURL, expiryMinutes)
}

// generateQRCodeWithContext generates QR code for a session with context support
func (qm *QRManager) generateQRCodeWithContext(ctx context.Context, session *QRCodeSession) {
	log.Printf("DEBUG: generateQRCode START - phone: %s, status: %s", session.Phone, session.Status)

	// Get QR channel with context
	log.Printf("DEBUG: Getting QR channel for phone: %s", session.Phone)
	qrChan, err := session.Client.GetQRChannel(ctx)
	if err != nil {
		log.Printf("DEBUG: GetQRChannel FAILED - phone: %s, error: %v", session.Phone, err)
		qm.updateSessionStatus(session, "expired")
		session.mu.Lock()
		session.Status = "expired"
		session.mu.Unlock()
		return
	}
	log.Printf("DEBUG: GetQRChannel OK - phone: %s", session.Phone)

	// Connect client
	log.Printf("DEBUG: Connecting client - phone: %s, isConnected: %v", session.Phone, session.Client.IsConnected())
	if err := session.Client.Connect(); err != nil {
		log.Printf("DEBUG: Connect FAILED - phone: %s, error: %v", session.Phone, err)
		qm.updateSessionStatus(session, "expired")
		session.mu.Lock()
		session.Status = "expired"
		session.mu.Unlock()
		return
	}
	log.Printf("DEBUG: Connect OK - phone: %s, isConnected: %v", session.Phone, session.Client.IsConnected())

	// Wait for QR code with context cancellation
	log.Printf("DEBUG: Entering QR event loop - phone: %s", session.Phone)
	qrCodeCount := 0
	for {
		select {
		case evt, ok := <-qrChan:
			if !ok {
				log.Printf("DEBUG: QR channel closed - phone: %s, qrCodesReceived: %d", session.Phone, qrCodeCount)
				qm.updateSessionStatus(session, "expired")
				return
			}

			log.Printf("DEBUG: QR event received - phone: %s, event: %s", session.Phone, evt.Event)
			session.mu.Lock()
			switch evt.Event {
			case "code":
				qrCodeCount++
				session.QRCode = evt.Code
				session.Status = "pending"
				log.Printf("DEBUG: QR code #%d received - phone: %s, codeLen: %d", qrCodeCount, session.Phone, len(evt.Code))

			case "timeout":
				session.Status = "expired"
				log.Printf("DEBUG: QR timeout - phone: %s, qrCodesReceived: %d", session.Phone, qrCodeCount)
				qm.updateSessionStatus(session, "expired")
				session.mu.Unlock()
				return

			case "err-client-outdated":
				session.Status = "expired"
				log.Printf("ERROR: WhatsApp client outdated - phone: %s. Please update whatsmeow library!", session.Phone)
				qm.updateSessionStatus(session, "expired")
				session.mu.Unlock()
				return

			case "success":
				session.Status = "authenticated"
				log.Printf("DEBUG: QR auth success - phone: %s", session.Phone)

				// Update database
				if session.Client.Store.ID != nil {
					deviceID := session.Client.Store.ID.String()
					log.Printf("DEBUG: Device ID: %s", deviceID)
					qm.db.UpdateSenderDeviceID(session.Phone, deviceID)
				} else {
					log.Printf("DEBUG: WARNING - Client.Store.ID is nil for %s", session.Phone)
				}

				session.mu.Unlock()
				qm.updateSessionStatus(session, "authenticated")
				qm.userStoreManager.StoreAuthenticatedClient(session.Phone, session.Client)

				if qm.authCallback != nil {
					qm.authCallback.OnAuthenticationSuccess(session.Phone, session.Client)
				}
				log.Printf("DEBUG: QR auth complete - phone: %s", session.Phone)
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
	log.Printf("DEBUG: Calling UpdateSenderStatus for phone: %s with status: %s", session.Phone, status)
	if err := qm.db.UpdateSenderStatus(session.Phone, status); err != nil {
		log.Printf("DEBUG: Failed to update sender status in database for phone: %s, error: %v", session.Phone, err)
		log.Printf("Failed to update sender status for %s: %v", session.Phone, err)
	} else {
		log.Printf("DEBUG: Successfully updated sender status in database for phone: %s to: %s", session.Phone, status)
		// Verify the update by reading back the status
		if sender, err := qm.db.GetSender(session.Phone); err == nil {
			log.Printf("DEBUG: Verification - Sender %s status in database: %s", session.Phone, sender.Status)
		} else {
			log.Printf("DEBUG: Verification - Failed to get sender %s from database: %v", session.Phone, err)
		}
	}
}

// GetQRCodeWithContext retrieves the latest QR code for a given token
// Client should poll this endpoint to get fresh QR codes
// Returns error only if session not found, expired, or initial QR generation times out
func (qm *QRManager) GetQRCodeWithContext(ctx context.Context, token string) (*QRCodeSession, error) {
	log.Printf("DEBUG: GetQRCode - token: %s", token)

	qm.mu.RLock()
	session, exists := qm.sessions[token]
	qm.mu.RUnlock()

	if !exists {
		log.Printf("DEBUG: GetQRCode - session not found, token: %s", token)
		return nil, fmt.Errorf("session not found")
	}

	session.mu.RLock()
	status := session.Status
	qrCode := session.QRCode
	expiresAt := session.ExpiresAt
	session.mu.RUnlock()

	log.Printf("DEBUG: GetQRCode - token: %s, phone: %s, status: %s, qrCodeLen: %d", token, session.Phone, status, len(qrCode))

	// Check if already authenticated
	if status == "authenticated" {
		log.Printf("DEBUG: GetQRCode - authenticated, token: %s", token)
		return session, nil
	}

	// Check if expired by status
	if status == "expired" {
		log.Printf("DEBUG: GetQRCode - expired by status, token: %s", token)
		return nil, fmt.Errorf("QR code session expired")
	}

	// Check if expired by time
	if time.Now().After(expiresAt) {
		log.Printf("DEBUG: GetQRCode - expired by time, token: %s, expiresAt: %v", token, expiresAt)
		qm.updateSessionStatus(session, "expired")
		return nil, fmt.Errorf("QR code session expired")
	}

	// If QR code is available, return it immediately
	if qrCode != "" {
		log.Printf("DEBUG: GetQRCode - returning QR, token: %s, qrCodeLen: %d", token, len(qrCode))
		return session, nil
	}

	// QR code not yet available - wait for initial generation (max 10 seconds)
	log.Printf("DEBUG: GetQRCode - waiting for initial QR, token: %s", token)
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("DEBUG: GetQRCode - context cancelled, token: %s", token)
			return nil, ctx.Err()
		case <-timeout:
			// Check status one more time
			session.mu.RLock()
			status = session.Status
			qrCode = session.QRCode
			session.mu.RUnlock()

			if qrCode != "" {
				return session, nil
			}
			if status == "expired" {
				return nil, fmt.Errorf("QR code session expired")
			}
			log.Printf("DEBUG: GetQRCode - initial wait timeout, token: %s, status: %s", token, status)
			return nil, fmt.Errorf("QR code not yet available, please retry")
		case <-ticker.C:
			session.mu.RLock()
			status = session.Status
			qrCode = session.QRCode
			session.mu.RUnlock()

			if qrCode != "" {
				log.Printf("DEBUG: GetQRCode - QR available, token: %s, qrCodeLen: %d", token, len(qrCode))
				return session, nil
			}
			if status == "expired" {
				log.Printf("DEBUG: GetQRCode - became expired while waiting, token: %s", token)
				return nil, fmt.Errorf("QR code session expired")
			}
			if status == "authenticated" {
				log.Printf("DEBUG: GetQRCode - became authenticated while waiting, token: %s", token)
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
