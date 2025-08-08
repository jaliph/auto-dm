package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

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
}

// NewHandler creates a new API handler
func NewHandler(userStoreManager *store.UserStoreManager, gormDB *database.GormDB, db *database.Database, clientManager *whatsapp.ClientManager, qrManager *whatsapp.QRManager, baseURL string, qrExpiryMinutes int) *Handler {
	return &Handler{
		userStoreManager: userStoreManager,
		gormDB:           gormDB,
		db:               db,
		clientManager:    clientManager,
		qrManager:        qrManager,
		baseURL:          baseURL,
		qrExpiryMinutes:  qrExpiryMinutes,
	}
}

// HandleRegister handles the /register API endpoint
func (h *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Parse JSON request body
	var request models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  "Invalid JSON format",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

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

	// Create QR code session
	session, err := h.qrManager.CreateQRCodeSession(request.Phone, h.baseURL, h.qrExpiryMinutes)
	if err != nil {
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

	// Create QR URL
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
func (h *Handler) HandleGetQRCode(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: HandleGetQRCode called with path: %s", r.URL.Path)

	if r.Method != "GET" {
		log.Printf("DEBUG: Invalid method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract token from URL path
	// Assuming URL pattern is /qr/{token}
	path := r.URL.Path
	log.Printf("DEBUG: Path length: %d", len(path))
	if len(path) < 5 { // /qr/ is 4 characters
		log.Printf("DEBUG: Path too short: %s", path)
		http.Error(w, "Invalid QR code URL", http.StatusBadRequest)
		return
	}
	token := path[4:] // Remove /qr/ prefix
	log.Printf("DEBUG: Extracted token: %s", token)

	// Check if HTML response is requested
	format := r.URL.Query().Get("format")
	log.Printf("DEBUG: Format parameter: %s", format)

	// Get QR code session
	log.Printf("DEBUG: Calling qrManager.GetQRCode with token: %s", token)
	session, err := h.qrManager.GetQRCode(token)
	log.Printf("DEBUG: GetQRCode returned err: %v", err)
	if err != nil {
		log.Printf("DEBUG: Error type: %T, Error message: %s", err, err.Error())

		// Check if the error is due to expired session
		if strings.Contains(err.Error(), "QR code session expired") {
			log.Printf("DEBUG: Detected expired session error")
			response := models.QRCodeResponse{
				Status:  "error",
				Error:   "QR code session expired",
				Expired: true,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGone)
			json.NewEncoder(w).Encode(response)
			log.Printf("DEBUG: Sent expired response")
			return
		}

		// Handle other errors (session not found, etc.)
		log.Printf("DEBUG: Handling other error type")
		response := models.QRCodeResponse{
			Status:  "error",
			Error:   fmt.Sprintf("QR code not found: %v", err),
			Expired: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		log.Printf("DEBUG: Sent not found response")
		return
	}

	// Check if authenticated
	if session.Status == "authenticated" {
		if format == "html" {
			h.sendHTMLResponse(w, "Already Authenticated", "This phone number is already authenticated.", "", false)
			return
		}
		response := models.QRCodeResponse{
			Status:  "success",
			Error:   "Phone number already authenticated",
			Expired: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Generate PNG image for QR code
	qrCodePNG, err := h.qrManager.GetQRCodePNGBase64(session.QRCode)
	if err != nil {
		log.Printf("DEBUG: Failed to generate PNG for QR code: %v", err)
		if format == "html" {
			h.sendHTMLResponse(w, "QR Code Error", "Failed to generate QR code image. Please try again.", "", true)
			return
		}
		// Fallback to text QR code
		response := models.QRCodeResponse{
			Status:  "success",
			QRCode:  session.QRCode,
			Expired: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if HTML response is requested
	if format == "html" {
		h.sendHTMLResponse(w, "QR Code Ready", "Scan the QR code below with your WhatsApp app to authenticate.", qrCodePNG, false)
		return
	}

	// Return QR code with PNG image
	response := models.QRCodeResponse{
		Status:    "success",
		QRCode:    session.QRCode, // Keep for backward compatibility
		QRCodePNG: qrCodePNG,      // Base64 encoded PNG
		Expired:   false,
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

	// Use MSSQL database for sender reports and consumption
	senders, err := h.gormDB.GetAllSenders()
	if err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to get senders: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(senders)
}

// HandleSendMessage handles the /send API endpoint
func (h *Handler) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Parse JSON request body
	var request struct {
		Sender    string `json:"sender"`
		Recipient string `json:"recipient"`
		Message   string `json:"message"`
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
	if request.Sender == "" || request.Recipient == "" || request.Message == "" {
		response := models.APIResponse{
			Status: "error",
			Error:  "Missing required fields: sender, recipient, message",
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

	// Send the message
	err := h.sendMessage(request.Sender, request.Recipient, request.Message)
	if err != nil {
		response := models.APIResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to send message: %v", err),
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

// sendMessage sends a WhatsApp message using a registered user client
func (h *Handler) sendMessage(senderPhone, recipient, message string) error {
	return h.clientManager.SendMessage(senderPhone, recipient, message)
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
