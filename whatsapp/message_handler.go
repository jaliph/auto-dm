package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/models"
	"github.com/jaliph/auto-dm/utils"
)

// MessageHandler handles WhatsApp message events and stores them in the database
type MessageHandler struct {
	gormDB        *database.GormDB
	receiveFolder string              // Folder to store received media files
	ollamaClient  *utils.OllamaClient // Ollama client for AI responses
	clientManager *ClientManager      // Reference to client manager for sending responses
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(gormDB *database.GormDB, receiveFolder string, ollamaClient *utils.OllamaClient, clientManager *ClientManager) *MessageHandler {
	return &MessageHandler{
		gormDB:        gormDB,
		receiveFolder: receiveFolder,
		ollamaClient:  ollamaClient,
		clientManager: clientManager,
	}
}

// HandleMessageEvent processes a WhatsApp message event
func (mh *MessageHandler) HandleMessageEvent(evt *events.Message, authenticatedSenderPhone string, client *whatsmeow.Client) error {
	log.Printf("DEBUG: Processing message event - ID: %s, FromMe: %v, Sender: %s, Chat: %s",
		evt.Info.ID, evt.Info.IsFromMe, evt.Info.Sender.User, evt.Info.Chat.User)

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

	// Get message type and content
	messageType := mh.getMessageType(evt.Message)
	content := mh.getMessageContent(evt.Message)

	// Download media file if present and store local path (concurrent)
	var localFilePath string
	if !evt.Info.IsFromMe && mh.hasMedia(evt.Message) {
		// Only download media for received messages (not sent by us) - do it concurrently
		go mh.downloadMediaFileAsync(evt.Message, evt.Info.ID, messageType, client, authenticatedSenderPhone)
		// For now, set a placeholder - the actual path will be updated in the database later
		localFilePath = fmt.Sprintf("downloading_%s_%s", evt.Info.ID, messageType)
	}

	// Create message model
	message := &models.Message{
		SenderPhone:    senderPhone,
		RecipientPhone: recipientPhone,
		MessageType:    messageType,
		Content:        content,
		MediaURL:       localFilePath, // Store local file path instead of URL
		Timestamp:      time.Unix(evt.Info.Timestamp.Unix(), 0),
		IsFromMe:       evt.Info.IsFromMe,
		ChatID:         evt.Info.Chat.String(),
		MessageID:      evt.Info.ID,
	}

	// Store message in database
	if err := mh.gormDB.StoreMessage(message); err != nil {
		return fmt.Errorf("failed to store message: %v", err)
	}

	log.Printf("Stored message from %s to %s: %s (IsFromMe: %v, Media: %s)",
		message.SenderPhone, message.RecipientPhone, message.Content, evt.Info.IsFromMe, localFilePath)

	// Handle auto-reply with Ollama for received text messages
	if !evt.Info.IsFromMe && messageType == "text" && content != "" {
		mh.handleAutoReply(senderPhone, recipientPhone, content)
	}

	return nil
}

// getMessageType determines the type of message
func (mh *MessageHandler) getMessageType(msg *waE2E.Message) string {
	if msg.Conversation != nil {
		return "text"
	}
	if msg.ExtendedTextMessage != nil {
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

// hasMedia checks if a message contains media
func (mh *MessageHandler) hasMedia(msg *waE2E.Message) bool {
	return msg.ImageMessage != nil || msg.VideoMessage != nil || msg.AudioMessage != nil ||
		msg.DocumentMessage != nil || msg.StickerMessage != nil
}

// downloadMediaFile downloads a media file using WhatsApp client and saves it to the receive folder
func (mh *MessageHandler) downloadMediaFile(msg *waE2E.Message, messageID, messageType string, client *whatsmeow.Client) string {
	// Ensure receive folder exists
	if err := os.MkdirAll(mh.receiveFolder, 0755); err != nil {
		log.Printf("ERROR: Failed to create receive folder %s: %v", mh.receiveFolder, err)
		return ""
	}

	// Generate filename based on message ID and type
	extension := mh.getFileExtension(messageType)
	filename := fmt.Sprintf("%s_%s%s", messageID, messageType, extension)
	filePath := filepath.Join(mh.receiveFolder, filename)

	log.Printf("DEBUG: Starting media download - Type: %s, File: %s", messageType, filePath)

	// Check if client is available for download
	if client == nil {
		log.Printf("ERROR: WhatsApp client is nil, cannot download media")
		return ""
	}

	// Create the file
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create file %s: %v", filePath, err)
		return ""
	}
	defer file.Close()

	// Create a timeout context for the download (30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Download the media using WhatsApp client based on message type
	switch messageType {
	case "image":
		if msg.ImageMessage != nil {
			log.Printf("DEBUG: Downloading image message")
			err = client.DownloadToFile(ctx, msg.ImageMessage, file)
		} else {
			log.Printf("DEBUG: Image message is nil")
			err = fmt.Errorf("image message is nil")
		}
	case "video":
		if msg.VideoMessage != nil {
			log.Printf("DEBUG: Downloading video message")
			err = client.DownloadToFile(ctx, msg.VideoMessage, file)
		} else {
			log.Printf("DEBUG: Video message is nil")
			err = fmt.Errorf("video message is nil")
		}
	case "audio":
		if msg.AudioMessage != nil {
			log.Printf("DEBUG: Downloading audio message")
			err = client.DownloadToFile(ctx, msg.AudioMessage, file)
		} else {
			log.Printf("DEBUG: Audio message is nil")
			err = fmt.Errorf("audio message is nil")
		}
	case "document":
		if msg.DocumentMessage != nil {
			log.Printf("DEBUG: Downloading document message")
			err = client.DownloadToFile(ctx, msg.DocumentMessage, file)
		} else {
			log.Printf("DEBUG: Document message is nil")
			err = fmt.Errorf("document message is nil")
		}
	case "sticker":
		if msg.StickerMessage != nil {
			log.Printf("DEBUG: Downloading sticker message")
			err = client.DownloadToFile(ctx, msg.StickerMessage, file)
		} else {
			log.Printf("DEBUG: Sticker message is nil")
			err = fmt.Errorf("sticker message is nil")
		}
	default:
		log.Printf("Unsupported media type: %s", messageType)
		return ""
	}

	// Check for context timeout
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("ERROR: Media download timed out after 30 seconds for %s", filePath)
		os.Remove(filePath)
		return ""
	}

	if err != nil {
		log.Printf("Failed to download media file %s: %v", filePath, err)
		// Clean up the file if download failed
		os.Remove(filePath)
		return ""
	}

	log.Printf("Downloaded media file: %s", filePath)
	return filePath
}

// downloadMediaFileAsync downloads media file asynchronously and updates the database
func (mh *MessageHandler) downloadMediaFileAsync(msg *waE2E.Message, messageID, messageType string, client *whatsmeow.Client, authenticatedSenderPhone string) {
	log.Printf("DEBUG: Starting async media download for message %s, type: %s", messageID, messageType)

	// Download the media file
	filePath := mh.downloadMediaFile(msg, messageID, messageType, client)

	if filePath != "" {
		// Update the database with the actual file path
		if err := mh.updateMessageMediaPath(messageID, filePath); err != nil {
			log.Printf("ERROR: Failed to update message media path in database: %v", err)
		} else {
			log.Printf("DEBUG: Successfully updated message %s with media path: %s", messageID, filePath)
		}
	} else {
		log.Printf("ERROR: Failed to download media for message %s", messageID)
		// Update database to indicate download failed
		if err := mh.updateMessageMediaPath(messageID, "download_failed"); err != nil {
			log.Printf("ERROR: Failed to update message download status in database: %v", err)
		}
	}
}

// updateMessageMediaPath updates the media URL field for a specific message
func (mh *MessageHandler) updateMessageMediaPath(messageID, mediaPath string) error {
	return mh.gormDB.UpdateMessageMediaPath(messageID, mediaPath)
}

// cleanAIResponse removes internal thinking and other unwanted content from AI responses
func (mh *MessageHandler) cleanAIResponse(response string) string {
	// Remove <think>...</think> blocks
	cleaned := response

	// Remove thinking blocks (common in some AI models)
	start := 0
	for {
		thinkStart := findSubstring(cleaned, "<think>", start)
		if thinkStart == -1 {
			break
		}

		thinkEnd := findSubstring(cleaned, "</think>", thinkStart)
		if thinkEnd == -1 {
			break
		}

		// Remove the entire <think>...</think> block
		cleaned = cleaned[:thinkStart] + cleaned[thinkEnd+8:]
		start = thinkStart
	}

	// Remove other common thinking patterns
	patterns := []string{
		"<thinking>", "</thinking>",
		"<reasoning>", "</reasoning>",
		"<internal>", "</internal>",
		"<process>", "</process>",
	}

	for i := 0; i < len(patterns); i += 2 {
		start := 0
		for {
			startTag := findSubstring(cleaned, patterns[i], start)
			if startTag == -1 {
				break
			}

			endTag := findSubstring(cleaned, patterns[i+1], startTag)
			if endTag == -1 {
				break
			}

			// Remove the entire block
			tagLen := len(patterns[i+1])
			cleaned = cleaned[:startTag] + cleaned[endTag+tagLen:]
			start = startTag
		}
	}

	// Trim whitespace and clean up
	cleaned = strings.TrimSpace(cleaned)

	// If the response is empty after cleaning, return a default message
	if cleaned == "" {
		return "I apologize, but I couldn't generate a proper response. Please try again."
	}

	return cleaned
}

// findSubstring finds a substring in a string, case-insensitive
func findSubstring(s, substr string, start int) int {
	if start >= len(s) {
		return -1
	}

	// Convert to lowercase for case-insensitive search
	sLower := strings.ToLower(s[start:])
	substrLower := strings.ToLower(substr)

	idx := strings.Index(sLower, substrLower)
	if idx == -1 {
		return -1
	}

	return start + idx
}

// handleAutoReply processes auto-replies using Ollama for received text messages
func (mh *MessageHandler) handleAutoReply(senderPhone, recipientPhone, content string) {
	// Check if Ollama is configured and working
	if mh.ollamaClient == nil || !mh.ollamaClient.IsConfigured() {
		log.Printf("DEBUG: Ollama not configured, skipping auto-reply")
		return
	}

	// Test Ollama connection before processing
	if err := mh.ollamaClient.TestConnection(); err != nil {
		log.Printf("WARNING: Ollama connection failed, disabling auto-reply: %v", err)
		// Disable Ollama client by setting it to nil
		mh.ollamaClient = nil
		return
	}

	// Generate AI response
	log.Printf("DEBUG: Generating AI response for message from %s: %s", senderPhone, content)
	aiResponse, err := mh.ollamaClient.GenerateResponse(content)
	if err != nil {
		log.Printf("ERROR: Failed to generate AI response: %v", err)
		return
	}

	// Clean the AI response to remove internal thinking
	cleanedResponse := mh.cleanAIResponse(aiResponse)
	log.Printf("DEBUG: Original AI response: %s", aiResponse)
	log.Printf("DEBUG: Cleaned AI response: %s", cleanedResponse)

	// Add AI disclaimer to the response
	responseWithDisclaimer := cleanedResponse + "\n\n_*This response is from an AI Agent*_"

	// Send the cleaned AI response with disclaimer back to the sender
	if mh.clientManager != nil {
		if err := mh.clientManager.SendMessage(recipientPhone, senderPhone, responseWithDisclaimer); err != nil {
			log.Printf("ERROR: Failed to send AI response: %v", err)
			return
		}
		log.Printf("✅ Auto-reply sent from %s to %s: %s", recipientPhone, senderPhone, cleanedResponse)
	} else {
		log.Printf("ERROR: Client manager not available for sending auto-reply")
	}
}

// getFileExtension returns the appropriate file extension based on message type
func (mh *MessageHandler) getFileExtension(messageType string) string {
	switch messageType {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	case "audio":
		return ".ogg"
	case "document":
		return ".pdf"
	case "sticker":
		return ".webp"
	default:
		return ".bin"
	}
}
