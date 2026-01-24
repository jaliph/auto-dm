package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/models"
	"github.com/jaliph/auto-dm/store"
	"github.com/jaliph/auto-dm/whatsapp"
)

// Handler handles HTTP requests
type Handler struct {
	userStoreManager *store.UserStoreManager
	gormDB           *database.GormDB
	db               *database.Database
	clientManager    *whatsapp.ClientManager
	qrManager        *whatsapp.QRManager
	baseURL          string
	qrExpiryMinutes  int
	fileShareFolder  string
}

// NewHandler creates a new API handler
func NewHandler(userStoreManager *store.UserStoreManager, gormDB *database.GormDB, db *database.Database, clientManager *whatsapp.ClientManager, qrManager *whatsapp.QRManager, baseURL string, qrExpiryMinutes int, fileShareFolder string) *Handler {
	return &Handler{
		userStoreManager: userStoreManager,
		gormDB:           gormDB,
		db:               db,
		clientManager:    clientManager,
		qrManager:        qrManager,
		baseURL:          baseURL,
		qrExpiryMinutes:  qrExpiryMinutes,
		fileShareFolder:  fileShareFolder,
	}
}

// HandleRegister handles the /register API endpoint
func (h *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: HandleRegister called with method: %s", r.Method)
	if r.Method != "POST" {
		log.Printf("DEBUG: Invalid method for /register: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Check if request is cancelled
	select {
	case <-r.Context().Done():
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
		return
	default:
	}

	// Parse JSON request body
	var request models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Printf("DEBUG: Failed to parse JSON request body: %v", err)
		response := models.APIResponse{
			Status: "error",
			Error:  "Invalid JSON format",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	log.Printf("DEBUG: Parsed register request for phone: %s", request.Phone)

	// Validate phone number
	if request.Phone == "" {
		response := models.APIResponse{
			Status: "error",
			Error:  "Phone number is required",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create QR code session with context
	log.Printf("DEBUG: Creating QR code session for phone: %s", request.Phone)
	session, err := h.qrManager.CreateQRCodeSessionWithContext(r.Context(), request.Phone, h.baseURL, h.qrExpiryMinutes)
	if err != nil {
		log.Printf("DEBUG: Failed to create QR code session for phone: %s, error: %v", request.Phone, err)
		// Check if context was cancelled
		if r.Context().Err() != nil {
			log.Printf("DEBUG: Request context cancelled for phone: %s", request.Phone)
			http.Error(w, "Request cancelled", http.StatusRequestTimeout)
			return
		}

		// Check for specific error types
		if strings.Contains(err.Error(), "already authenticated") {
			response := models.APIResponse{
				Status: "error",
				Error:  fmt.Sprintf("Sender %s is already authenticated", request.Phone),
			}
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(response)
			return
		}

		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to create QR session: %v", err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create QR URL (cross-platform URL construction)
	qrURL := fmt.Sprintf("%s/qr/%s", h.baseURL, session.Token)

	response := models.RegisterResponse{
		Status:    "success",
		Message:   "QR code session created successfully",
		QRURL:     qrURL,
		ExpiresAt: session.ExpiresAt,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleGetQRCode handles the /qr/{token} API endpoint
// Client should poll this endpoint to get the latest QR code and check authentication status
func (h *Handler) HandleGetQRCode(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: HandleGetQRCode - path: %s", r.URL.Path)

	// Check if HTML response is requested
	format := r.URL.Query().Get("format")

	if r.Method != "GET" {
		log.Printf("DEBUG: Invalid method: %s", r.Method)
		if format == "html" {
			h.sendHTMLResponse(w, "Method Not Allowed", "Only GET requests are allowed for this endpoint.", "", true)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract token from URL path
	// Assuming URL pattern is /qr/{token}
	path := r.URL.Path
	log.Printf("DEBUG: HandleGetQRCode called with path: %s, length: %d", path, len(path))
	if len(path) < 5 { // /qr/ is 4 characters
		log.Printf("DEBUG: Path too short: %s", path)
		if format == "html" {
			h.sendHTMLResponse(w, "Invalid URL", "The QR code URL is invalid. Please check the URL and try again.", "", true)
			return
		}
		http.Error(w, "Invalid QR code URL", http.StatusBadRequest)
		return
	}
	token := path[4:] // Remove /qr/ prefix
	log.Printf("DEBUG: HandleGetQRCode - token: %s", token)

	// Get QR code session with context
	session, err := h.qrManager.GetQRCodeWithContext(r.Context(), token)
	if err != nil {
		log.Printf("DEBUG: HandleGetQRCode - error: %v", err)

		// Check if context was cancelled
		if r.Context().Err() != nil {
			if format == "html" {
				h.sendHTMLResponse(w, "Request Cancelled", "The request was cancelled or timed out. Please try again.", "", true)
				return
			}
			http.Error(w, "Request cancelled", http.StatusRequestTimeout)
			return
		}

		// Check if the error is due to expired session
		if strings.Contains(err.Error(), "expired") {
			log.Printf("DEBUG: HandleGetQRCode - session expired, token: %s", token)
			if format == "html" {
				h.sendHTMLResponse(w, "QR Code Expired", "This QR code has expired. Please register again to get a new QR code.", "", true)
				return
			}
			response := models.QRCodeResponse{
				Status:  "error",
				Error:   "QR code session expired. Please register again.",
				Expired: true,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGone)
			json.NewEncoder(w).Encode(response)
			return
		}

		// Check if QR not yet available (should retry)
		if strings.Contains(err.Error(), "not yet available") {
			log.Printf("DEBUG: HandleGetQRCode - QR not yet available, token: %s", token)
			response := models.QRCodeResponse{
				Status:  "pending",
				Message: "QR code is being generated. Please retry in a moment.",
				Expired: false,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(response)
			return
		}

		// Handle other errors (session not found, etc.)
		log.Printf("DEBUG: HandleGetQRCode - other error: %v", err)
		if format == "html" {
			h.sendHTMLResponse(w, "QR Code Not Found", "The requested QR code was not found. Please check the URL or register again.", "", true)
			return
		}
		response := models.QRCodeResponse{
			Status:  "error",
			Error:   fmt.Sprintf("QR code error: %v", err),
			Expired: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if authenticated
	if session.Status == "authenticated" {
		log.Printf("DEBUG: HandleGetQRCode - session authenticated for token: %s", token)
		if format == "html" {
			h.sendHTMLResponse(w, "Already Authenticated", "This phone number is already authenticated.", "", false)
			return
		}
		response := models.QRCodeResponse{
			Status:        "success",
			Message:       "Phone number authenticated successfully",
			Authenticated: true,
			Expired:       false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if QR code is empty (QR generation failed)
	if session.QRCode == "" {
		log.Printf("DEBUG: QR code is empty for session, QR generation failed")
		if format == "html" {
			h.sendHTMLResponse(w, "QR Code Generation Failed", "QR code generation failed. Please try registering again.", "", true)
			return
		}
		// Return error response for QR generation failure
		response := models.QRCodeResponse{
			Status:  "error",
			Error:   "QR code generation failed",
			Expired: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Generate PNG image for QR code
	log.Printf("DEBUG: Session QRCode length: %d, QRCode: %s", len(session.QRCode), session.QRCode)
	qrCodePNG, err := h.qrManager.GetQRCodePNGBase64(session.QRCode)
	if err != nil {
		log.Printf("DEBUG: Failed to generate PNG for QR code: %v", err)
		if format == "html" {
			h.sendHTMLResponse(w, "QR Code Error", "Failed to generate QR code image. Please try again.", "", true)
			return
		}
		// Return error for PNG generation failure
		response := models.QRCodeResponse{
			Status:  "error",
			Error:   "Failed to generate QR code image",
			Expired: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if HTML response is requested
	if format == "html" {
		h.sendHTMLResponse(w, "QR Code Ready", "Scan the QR code below with your WhatsApp app to authenticate.", qrCodePNG, false)
		return
	}

	// Return QR code with PNG image
	log.Printf("DEBUG: HandleGetQRCode - returning QR code for token: %s, qrCodeLen: %d", token, len(session.QRCode))
	response := models.QRCodeResponse{
		Status:        "success",
		Message:       "Scan this QR code with WhatsApp. Poll this endpoint to check for authentication.",
		QRCode:        session.QRCode, // Keep for backward compatibility
		QRCodePNG:     qrCodePNG,      // Base64 encoded PNG
		Expired:       false,
		Authenticated: false,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleGetSenders handles the /senders API endpoint
func (h *Handler) HandleGetSenders(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Use SQLite database for sender reports to get immediate status updates
	log.Printf("DEBUG: HandleGetSenders - Getting senders from SQLite database")
	senders, err := h.db.GetAllSenders()
	if err != nil {
		log.Printf("DEBUG: HandleGetSenders - Failed to get senders: %v", err)
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to get senders: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	log.Printf("DEBUG: HandleGetSenders - Got %d senders from database", len(senders))
	for _, sender := range senders {
		log.Printf("DEBUG: HandleGetSenders - Sender: %s, Status: %s, DeviceID: %s", sender.Phone, sender.Status, sender.DeviceID)
	}

	// Ensure we always return an array, even if empty
	if senders == nil {
		log.Printf("DEBUG: HandleGetSenders - senders is nil, converting to empty slice")
		senders = []*models.Sender{}
	}

	log.Printf("DEBUG: HandleGetSenders - About to encode %d senders", len(senders))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(senders)
}

// HandleDeleteSender handles the /senders/{phone} DELETE API endpoint
func (h *Handler) HandleDeleteSender(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract phone number from URL path
	// Assuming URL pattern is /senders/{phone}
	path := r.URL.Path
	if !strings.HasPrefix(path, "/senders/") {
		http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		return
	}

	phone := strings.TrimPrefix(path, "/senders/")
	if phone == "" {
		response := models.APIResponse{
			Status: "error",
			Error:  "Phone number is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if sender exists
	_, err := h.db.GetSender(phone)
	if err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Sender not found: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Logout and cleanup the sender's WhatsApp session
	if err := h.userStoreManager.LogoutUser(phone); err != nil {
		log.Printf("Warning: Failed to logout user %s: %v", phone, err)
		// Continue with deletion even if logout fails
	}

	// Remove from client manager
	h.clientManager.DeleteUser(phone)

	// Delete sender from SQLite database
	if err := h.db.DeleteSender(phone); err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to delete sender: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Sync deletion to MSSQL
	if err := h.gormDB.DeleteSender(phone); err != nil {
		log.Printf("Warning: Failed to delete sender from MSSQL: %v", err)
		// Don't return error as the main deletion succeeded
	}

	response := models.APIResponse{
		Status:  "success",
		Message: fmt.Sprintf("Sender %s deleted successfully", phone),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleSendMessage handles the /send API endpoint
func (h *Handler) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if request is cancelled
	select {
	case <-r.Context().Done():
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
		return
	default:
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Parse JSON request body
	var request struct {
		Sender    string `json:"sender"`
		Recipient string `json:"recipient"`
		Message   string `json:"message"`
		Type      string `json:"type"`      // "text" or "file"
		FileName  string `json:"file_name"` // filename when type is "file"
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  "Invalid JSON format",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Validate required fields
	if request.Sender == "" || request.Recipient == "" {
		response := models.APIResponse{
			Status: "error",
			Error:  "Missing required fields: sender, recipient",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Validate message type
	if request.Type == "" {
		request.Type = "text" // default to text
	}

	if request.Type == "text" && request.Message == "" {
		response := models.APIResponse{
			Status: "error",
			Error:  "Message is required for text type",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if request.Type == "file" && request.FileName == "" {
		response := models.APIResponse{
			Status: "error",
			Error:  "File name is required for file type",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if sender is registered and active
	client, exists := h.userStoreManager.GetUserClient(request.Sender)
	if !exists {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Sender %s is not registered", request.Sender),
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if !client.IsConnected() {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Sender %s is not connected", request.Sender),
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Send the message or file with context
	var err error
	if request.Type == "file" {
		err = h.sendFileWithContext(r.Context(), request.Sender, request.Recipient, request.FileName)
	} else {
		err = h.sendMessageWithContext(r.Context(), request.Sender, request.Recipient, request.Message)
	}

	if err != nil {
		// Check if context was cancelled
		if r.Context().Err() != nil {
			http.Error(w, "Request cancelled", http.StatusRequestTimeout)
			return
		}

		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to send %s: %v", request.Type, err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := models.APIResponse{
		Status:  "success",
		Message: "Message sent successfully",
	}
	json.NewEncoder(w).Encode(response)
}

// sendMessageWithContext sends a WhatsApp message with context support
func (h *Handler) sendMessageWithContext(ctx context.Context, senderPhone, recipient, message string) error {
	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Send the message (this will also store it in the database)
	err := h.clientManager.SendMessage(senderPhone, recipient, message)
	if err != nil {
		return err
	}

	// Message is automatically stored by SendMessage function
	log.Printf("DEBUG: Message sent and stored via API - Sender: %s, Recipient: %s", senderPhone, recipient)

	return nil
}

// sendFileWithContext sends a WhatsApp file with context support
func (h *Handler) sendFileWithContext(ctx context.Context, senderPhone, recipient, fileName string) error {
	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Construct full file path (cross-platform)
	filePath := filepath.Join(h.fileShareFolder, fileName)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", fileName)
	}

	// Send file using client manager (this will also store it in the database)
	err := h.clientManager.SendFile(senderPhone, recipient, filePath)
	if err != nil {
		return err
	}

	// File message is automatically stored by SendFile function
	log.Printf("DEBUG: File sent and stored via API - Sender: %s, Recipient: %s, File: %s", senderPhone, recipient, fileName)

	return nil
}

// sendMessage sends a WhatsApp message using a registered user client (legacy method)
func (h *Handler) sendMessage(senderPhone, recipient, message string) error {
	return h.sendMessageWithContext(context.Background(), senderPhone, recipient, message)
}

// HandleGetMessages handles the /messages API endpoint
func (h *Handler) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	phone := r.URL.Query().Get("phone")
	limitStr := r.URL.Query().Get("limit")

	limit := 50 // default limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	var messages []models.Message
	var err error

	if phone != "" {
		messages, err = h.gormDB.GetMessagesByPhone(phone, limit)
	} else {
		messages, err = h.gormDB.GetRecentMessages(limit)
	}

	if err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to get messages: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// HandleGetStats handles the /stats API endpoint
func (h *Handler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats, err := h.gormDB.GetMessageStats()
	if err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to get stats: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// HandleGetChatParticipants handles the /chat-participants GET API endpoint
func (h *Handler) HandleGetChatParticipants(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	participants, err := h.gormDB.GetAllChatParticipants()
	if err != nil {
		response := models.ChatParticipantsResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to get chat participants: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Convert to slice of ChatParticipant (not pointers)
	var data []models.ChatParticipant
	for _, p := range participants {
		data = append(data, *p)
	}

	response := models.ChatParticipantsResponse{
		Status:  "success",
		Message: "Chat participants retrieved successfully",
		Data:    data,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleCreateChatParticipant handles the /chat-participants POST API endpoint
func (h *Handler) HandleCreateChatParticipant(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request models.ChatParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Invalid JSON format",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Validate required fields
	if request.Phone == "" {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Phone number is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if participant already exists
	if h.gormDB.ChatParticipantExists(request.Phone) {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Chat participant with phone %s already exists", request.Phone),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Set default auto-reply enabled to true if not specified
	autoReplyEnabled := true
	if request.AutoReplyEnabled != nil {
		autoReplyEnabled = *request.AutoReplyEnabled
	}

	// Create chat participant
	err := h.gormDB.CreateChatParticipant(request.Phone, request.Name, autoReplyEnabled)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to create chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get the created participant
	participant, err := h.gormDB.GetChatParticipant(request.Phone)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to retrieve created chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := models.ChatParticipantResponse{
		Status:  "success",
		Message: "Chat participant created successfully",
		Data:    participant,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// HandleUpdateChatParticipant handles the /chat-participants/{phone} PUT API endpoint
func (h *Handler) HandleUpdateChatParticipant(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract phone number from URL path
	path := r.URL.Path
	if !strings.HasPrefix(path, "/chat-participants/") {
		http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		return
	}

	phone := strings.TrimPrefix(path, "/chat-participants/")
	if phone == "" {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Phone number is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if participant exists
	if !h.gormDB.ChatParticipantExists(phone) {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Chat participant with phone %s not found", phone),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	var request models.ChatParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Invalid JSON format",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get current participant to preserve existing values
	currentParticipant, err := h.gormDB.GetChatParticipant(phone)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to get current chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Update fields if provided
	name := currentParticipant.Name
	if request.Name != "" {
		name = request.Name
	}

	autoReplyEnabled := currentParticipant.AutoReplyEnabled
	if request.AutoReplyEnabled != nil {
		autoReplyEnabled = *request.AutoReplyEnabled
	}

	// Update chat participant
	err = h.gormDB.UpdateChatParticipant(phone, name, autoReplyEnabled)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to update chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get the updated participant
	updatedParticipant, err := h.gormDB.GetChatParticipant(phone)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to retrieve updated chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := models.ChatParticipantResponse{
		Status:  "success",
		Message: "Chat participant updated successfully",
		Data:    updatedParticipant,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleToggleAutoReply handles the /chat-participants/{phone}/auto-reply POST API endpoint
func (h *Handler) HandleToggleAutoReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract phone number from URL path
	path := r.URL.Path
	if !strings.HasPrefix(path, "/chat-participants/") || !strings.HasSuffix(path, "/auto-reply") {
		http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		return
	}

	phone := strings.TrimPrefix(path, "/chat-participants/")
	phone = strings.TrimSuffix(phone, "/auto-reply")
	if phone == "" {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Phone number is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if participant exists
	if !h.gormDB.ChatParticipantExists(phone) {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Chat participant with phone %s not found", phone),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Parse request body for auto-reply setting
	var request struct {
		AutoReplyEnabled bool `json:"auto_reply_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Invalid JSON format",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Update auto-reply setting
	err := h.gormDB.UpdateChatParticipantAutoReply(phone, request.AutoReplyEnabled)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to update auto-reply setting: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get the updated participant
	updatedParticipant, err := h.gormDB.GetChatParticipant(phone)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to retrieve updated chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	status := "enabled"
	if !request.AutoReplyEnabled {
		status = "disabled"
	}

	response := models.ChatParticipantResponse{
		Status:  "success",
		Message: fmt.Sprintf("Auto-reply %s for chat participant %s", status, phone),
		Data:    updatedParticipant,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleDeleteChatParticipant handles the /chat-participants/{phone} DELETE API endpoint
func (h *Handler) HandleDeleteChatParticipant(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract phone number from URL path
	path := r.URL.Path
	if !strings.HasPrefix(path, "/chat-participants/") {
		http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		return
	}

	phone := strings.TrimPrefix(path, "/chat-participants/")
	if phone == "" {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  "Phone number is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if participant exists
	if !h.gormDB.ChatParticipantExists(phone) {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Chat participant with phone %s not found", phone),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Delete chat participant
	err := h.gormDB.DeleteChatParticipant(phone)
	if err != nil {
		response := models.ChatParticipantResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to delete chat participant: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := models.ChatParticipantResponse{
		Status:  "success",
		Message: fmt.Sprintf("Chat participant %s deleted successfully", phone),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// sendHTMLResponse sends an HTML response with QR code display
func (h *Handler) sendHTMLResponse(w http.ResponseWriter, title, message, qrCodePNG string, isError bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>QR Code - ` + title + `</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 600px;
            margin: 0 auto;
            padding: 20px;
            text-align: center;
        }
        .container {
            background: #f9f9f9;
            border-radius: 10px;
            padding: 30px;
            margin: 20px 0;
        }
        .qr-container {
            margin: 20px 0;
        }
        .qr-image {
            border: 2px solid #ddd;
            border-radius: 10px;
            padding: 20px;
            background: white;
            display: inline-block;
        }
        .error {
            color: #d32f2f;
            background: #ffebee;
            border: 1px solid #ffcdd2;
            border-radius: 5px;
            padding: 15px;
            margin: 20px 0;
        }
        .success {
            color: #388e3c;
            background: #e8f5e8;
            border: 1px solid #c8e6c9;
            border-radius: 5px;
            padding: 15px;
            margin: 20px 0;
        }
        .info {
            color: #1976d2;
            background: #e3f2fd;
            border: 1px solid #bbdefb;
            border-radius: 5px;
            padding: 15px;
            margin: 20px 0;
        }
        .instructions {
            text-align: left;
            background: #f5f5f5;
            padding: 20px;
            border-radius: 5px;
            margin: 20px 0;
        }
        .instructions ol {
            margin: 10px 0;
            padding-left: 20px;
        }
        .instructions li {
            margin: 5px 0;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>QR Code Authentication</h1>
        <h2>` + title + `</h2>
        
        <div class="` + func() string {
		if isError {
			return "error"
		}
		return "info"
	}() + `">
            <p>` + message + `</p>
        </div>`

	if qrCodePNG != "" {
		html += `
        <div class="qr-container">
            <h3>Scan this QR code with WhatsApp</h3>
            <div class="qr-image">
                <img src="data:image/png;base64,` + qrCodePNG + `" alt="QR Code" style="max-width: 300px;">
            </div>
        </div>`
	}

	html += `
        <div class="instructions">
            <h3>Instructions:</h3>
            <ol>
                <li>Open WhatsApp on your phone</li>
                <li>Go to Settings > Linked Devices</li>
                <li>Tap "Link a Device"</li>
                <li>Point your camera at the QR code above</li>
                <li>Wait for the authentication to complete</li>
            </ol>
        </div>
        
        <div class="info">
            <p><strong>Note:</strong> This QR code will expire in ` + fmt.Sprintf("%d", h.qrExpiryMinutes) + ` minutes.</p>
        </div>
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

// generateMessageID generates a unique message ID
func generateMessageID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
