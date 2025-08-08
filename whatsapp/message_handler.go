package whatsapp

import (
	"fmt"
	"log"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/models"
)

// MessageHandler handles WhatsApp message events and stores them in the database
type MessageHandler struct {
	gormDB *database.GormDB
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(gormDB *database.GormDB) *MessageHandler {
	return &MessageHandler{
		gormDB: gormDB,
	}
}

// HandleMessageEvent processes a WhatsApp message event
func (mh *MessageHandler) HandleMessageEvent(evt *events.Message, authenticatedSenderPhone string) error {
	// Determine the actual sender and recipient based on the message direction
	var senderPhone, recipientPhone string

	if evt.Info.IsFromMe {
		// Message sent by our authenticated sender
		senderPhone = authenticatedSenderPhone
		recipientPhone = evt.Info.Chat.User
	} else {
		// Message received by our authenticated sender
		senderPhone = evt.Info.Sender.User
		recipientPhone = authenticatedSenderPhone
	}

	// Create message model
	message := &models.Message{
		SenderPhone:    senderPhone,
		RecipientPhone: recipientPhone,
		MessageType:    mh.getMessageType(evt.Message),
		Content:        mh.getMessageContent(evt.Message),
		MediaURL:       mh.getMediaURL(evt.Message),
		Timestamp:      time.Unix(evt.Info.Timestamp.Unix(), 0),
		IsFromMe:       evt.Info.IsFromMe,
		ChatID:         evt.Info.Chat.String(),
		MessageID:      evt.Info.ID,
	}

	// Store message in database
	if err := mh.gormDB.StoreMessage(message); err != nil {
		return fmt.Errorf("failed to store message: %v", err)
	}

	log.Printf("Stored message from %s to %s: %s (IsFromMe: %v)",
		message.SenderPhone, message.RecipientPhone, message.Content, evt.Info.IsFromMe)

	return nil
}

// getMessageType determines the type of message
func (mh *MessageHandler) getMessageType(msg *waE2E.Message) string {
	if msg.Conversation != nil {
		return "text"
	}
	if msg.ImageMessage != nil {
		return "image"
	}
	if msg.VideoMessage != nil {
		return "video"
	}
	if msg.AudioMessage != nil {
		return "audio"
	}
	if msg.DocumentMessage != nil {
		return "document"
	}
	if msg.StickerMessage != nil {
		return "sticker"
	}
	if msg.ContactMessage != nil {
		return "contact"
	}
	if msg.LocationMessage != nil {
		return "location"
	}
	return "unknown"
}

// getMessageContent extracts the text content from a message
func (mh *MessageHandler) getMessageContent(msg *waE2E.Message) string {
	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	return ""
}

// getMediaURL extracts media URL from a message
func (mh *MessageHandler) getMediaURL(msg *waE2E.Message) string {
	if msg.ImageMessage != nil && msg.ImageMessage.URL != nil {
		return *msg.ImageMessage.URL
	}
	if msg.VideoMessage != nil && msg.VideoMessage.URL != nil {
		return *msg.VideoMessage.URL
	}
	if msg.AudioMessage != nil && msg.AudioMessage.URL != nil {
		return *msg.AudioMessage.URL
	}
	if msg.DocumentMessage != nil && msg.DocumentMessage.URL != nil {
		return *msg.DocumentMessage.URL
	}
	return ""
}
