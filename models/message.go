package models

import (
	"time"

	"gorm.io/gorm"
)

// Message represents a WhatsApp message stored in the database
type Message struct {
	ID             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	SenderPhone    string         `gorm:"size:20;not null;index" json:"sender_phone"`
	RecipientPhone string         `gorm:"size:20;not null;index" json:"recipient_phone"`
	MessageType    string         `gorm:"size:50;not null" json:"message_type"` // text, image, video, etc.
	Content        string         `gorm:"type:text" json:"content"`
	MediaURL       string         `gorm:"size:500" json:"media_url,omitempty"`
	Timestamp      time.Time      `gorm:"not null;index" json:"timestamp"`
	IsFromMe       bool           `gorm:"not null;default:false" json:"is_from_me"`
	ChatID         string         `gorm:"size:100;not null;index" json:"chat_id"`
	MessageID      string         `gorm:"size:100;uniqueIndex" json:"message_id"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for the Message model
func (Message) TableName() string {
	return "whatsapp_messages"
}

// MessageStats represents message statistics
type MessageStats struct {
	TotalMessages     int64 `json:"total_messages"`
	MessagesToday     int64 `json:"messages_today"`
	MessagesThisWeek  int64 `json:"messages_this_week"`
	MessagesThisMonth int64 `json:"messages_this_month"`
}
