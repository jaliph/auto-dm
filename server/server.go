package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/jaliph/auto-dm/api"
	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/store"
	"github.com/jaliph/auto-dm/whatsapp"
)

// Server represents the HTTP server
type Server struct {
	userStoreManager *store.UserStoreManager
	gormDB           *database.GormDB
	db               *database.Database
	clientManager    *whatsapp.ClientManager
	qrManager        *whatsapp.QRManager
	handler          *api.Handler
	baseURL          string
	qrExpiryMinutes  int
	fileShareFolder  string
}

// NewServer creates a new HTTP server
func NewServer(userStoreManager *store.UserStoreManager, gormDB *database.GormDB, db *database.Database, clientManager *whatsapp.ClientManager, qrManager *whatsapp.QRManager, baseURL string, qrExpiryMinutes int, fileShareFolder string) *Server {
	handler := api.NewHandler(userStoreManager, gormDB, db, clientManager, qrManager, baseURL, qrExpiryMinutes, fileShareFolder)
	return &Server{
		userStoreManager: userStoreManager,
		gormDB:           gormDB,
		db:               db,
		clientManager:    clientManager,
		qrManager:        qrManager,
		handler:          handler,
		baseURL:          baseURL,
		qrExpiryMinutes:  qrExpiryMinutes,
		fileShareFolder:  fileShareFolder,
	}
}

// Start starts the HTTP server
func (s *Server) Start(addr string) error {
	// Register routes
	http.HandleFunc("/", s.handleHealth)
	http.HandleFunc("/register", s.handler.HandleRegister)
	http.HandleFunc("/qr/", s.handleQRCode)
	http.HandleFunc("/senders", s.handler.HandleGetSenders)
	http.HandleFunc("/senders/", s.handleDeleteSender)
	http.HandleFunc("/send", s.handler.HandleSendMessage)
	http.HandleFunc("/messages", s.handler.HandleGetMessages)
	http.HandleFunc("/stats", s.handler.HandleGetStats)

	log.Printf("Starting REST API server on %s", addr)
	return http.ListenAndServe(addr, nil)
}

// handleHealth handles the root endpoint for health checks
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	log.Printf("DEBUG: Health check request received")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Auto-DM server is running",
	})
}

// handleQRCode handles QR code requests with dynamic token extraction
func (s *Server) handleQRCode(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: handleQRCode called with path: %s", r.URL.Path)

	// Extract token from URL path
	path := r.URL.Path
	if !strings.HasPrefix(path, "/qr/") {
		log.Printf("DEBUG: Path doesn't start with /qr/: %s", path)
		http.Error(w, "Invalid QR code URL", http.StatusBadRequest)
		return
	}

	// Remove /qr/ prefix to get token
	token := strings.TrimPrefix(path, "/qr/")
	log.Printf("DEBUG: Extracted token: %s", token)
	if token == "" {
		log.Printf("DEBUG: Empty token")
		http.Error(w, "Missing QR code token", http.StatusBadRequest)
		return
	}

	// Create a new request with the token in the path for the handler
	r.URL.Path = "/qr/" + token
	log.Printf("DEBUG: Calling HandleGetQRCode with modified path: %s", r.URL.Path)
	s.handler.HandleGetQRCode(w, r)
}

// handleDeleteSender handles DELETE requests for /senders/{phone}
func (s *Server) handleDeleteSender(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: handleDeleteSender called with path: %s", r.URL.Path)

	// Extract phone number from URL path
	path := r.URL.Path
	if !strings.HasPrefix(path, "/senders/") {
		log.Printf("DEBUG: Path doesn't start with /senders/: %s", path)
		http.Error(w, "Invalid sender URL", http.StatusBadRequest)
		return
	}

	// Remove /senders/ prefix to get phone number
	phone := strings.TrimPrefix(path, "/senders/")
	log.Printf("DEBUG: Extracted phone: %s", phone)
	if phone == "" {
		log.Printf("DEBUG: Empty phone number")
		http.Error(w, "Missing phone number", http.StatusBadRequest)
		return
	}

	// Create a new request with the phone in the path for the handler
	r.URL.Path = "/senders/" + phone
	log.Printf("DEBUG: Calling HandleDeleteSender with modified path: %s", r.URL.Path)
	s.handler.HandleDeleteSender(w, r)
}
