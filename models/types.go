package models

import "time"

// PhoneMapping represents a phone number to device ID mapping
type PhoneMapping struct {
	Phone    string `json:"phone"`
	DeviceID string `json:"device_id"`
}

// MessageRequest represents a message send request
type MessageRequest struct {
	Phone   string `json:"phone"`
	To      string `json:"to"`
	Message string `json:"message"`
}

// APIResponse represents a standard API response
type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// Sender represents a registered sender with authentication status
type Sender struct {
	Phone           string     `json:"phone"`
	DeviceID        string     `json:"device_id,omitempty"`
	Status          string     `json:"status"` // "pending", "authenticated", "invalidated"
	CreatedAt       time.Time  `json:"created_at"`
	AuthenticatedAt *time.Time `json:"authenticated_at,omitempty"`
	InvalidatedAt   *time.Time `json:"invalidated_at,omitempty"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Phone string `json:"phone"`
}

// RegisterResponse represents a registration response with QR URL
type RegisterResponse struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	QRURL     string    `json:"qr_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// QRCodeResponse represents a QR code response
type QRCodeResponse struct {
	Status    string `json:"status"`
	QRCode    string `json:"qr_code,omitempty"`     // QR code string (for backward compatibility)
	QRCodePNG string `json:"qr_code_png,omitempty"` // Base64 encoded PNG image
	Error     string `json:"error,omitempty"`
	Expired   bool   `json:"expired"`
}

// ChatParticipant represents a chat participant with auto-reply settings
type ChatParticipant struct {
	ID               uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Phone            string    `gorm:"size:20;not null;uniqueIndex" json:"phone"`
	Name             string    `gorm:"size:100" json:"name,omitempty"`
	AutoReplyEnabled bool      `gorm:"not null;default:true" json:"auto_reply_enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ChatParticipantRequest represents a request to create or update a chat participant
type ChatParticipantRequest struct {
	Phone            string `json:"phone"`
	Name             string `json:"name,omitempty"`
	AutoReplyEnabled *bool  `json:"auto_reply_enabled,omitempty"`
}

// ChatParticipantResponse represents a response for chat participant operations
type ChatParticipantResponse struct {
	Status  string           `json:"status"`
	Message string           `json:"message"`
	Data    *ChatParticipant `json:"data,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// ChatParticipantsResponse represents a response for multiple chat participants
type ChatParticipantsResponse struct {
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Data    []ChatParticipant `json:"data,omitempty"`
	Error   string            `json:"error,omitempty"`
}
