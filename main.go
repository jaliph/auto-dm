package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/jaliph/auto-dm/config"
	"github.com/jaliph/auto-dm/database"
	"github.com/jaliph/auto-dm/server"
	"github.com/jaliph/auto-dm/store"
	"github.com/jaliph/auto-dm/whatsapp"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Create context with cancellation
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize GORM database for message storage
	gormDB, err := database.NewGormDB(
		cfg.MSSQLServer,
		cfg.MSSQLDatabase,
		cfg.MSSQLUsername,
		cfg.MSSQLPassword,
	)
	if err != nil {
		log.Fatalf("Failed to initialize GORM database: %v", err)
	}
	defer gormDB.Close()

	// Initialize SQLite database for phone mappings
	db, err := database.NewDatabase(gormDB)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite database: %v", err)
	}
	defer db.Close()

	// Initialize user store manager
	userStoreManager := store.NewUserStoreManager()
	defer userStoreManager.CloseAll()

	// Initialize WhatsApp client manager (without admin functionality)
	clientManager := whatsapp.NewClientManager(userStoreManager, db, gormDB)

	// Initialize QR manager
	qrManager := whatsapp.NewQRManager(db, userStoreManager)
	qrManager.StartCleanup()

	// Load and authenticate existing senders
	if err := clientManager.LoadAllSenders(); err != nil {
		log.Printf("Warning: Failed to load senders: %v", err)
	}

	// Force sync all senders from SQLite to MSSQL
	if err := gormDB.ForceSyncAllSenders(db); err != nil {
		log.Printf("Warning: Failed to force sync senders to MSSQL: %v", err)
	} else {
		log.Printf("Successfully force synced all senders to MSSQL")
	}

	// Start connection monitoring
	go clientManager.MonitorConnections()

	// Start REST API server
	baseURL := "http://localhost:" + cfg.APIPort
	qrExpiryMinutes := 10 // QR codes expire after 10 minutes
	apiServer := server.NewServer(userStoreManager, gormDB, db, clientManager, qrManager, baseURL, qrExpiryMinutes)
	go func() {
		if err := apiServer.Start(cfg.APIPort); err != nil {
			log.Printf("Failed to start API server: %v", err)
		}
	}()

	log.Printf("Auto-DM server started successfully!")
	log.Printf("API server running on port %s", cfg.APIPort)
	log.Printf("Register endpoint: POST %s/register", baseURL)
	log.Printf("QR code endpoint: GET %s/qr/{token}", baseURL)
	log.Printf("Senders endpoint: GET %s/senders", baseURL)
	log.Printf("Send message endpoint: POST %s/send", baseURL)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	log.Println("Shutting down...")
	clientManager.Shutdown()
}
