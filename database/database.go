package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jaliph/auto-dm/models"
	_ "github.com/mattn/go-sqlite3"
)

// Database represents the database connection and operations
type Database struct {
	db     *sql.DB
	gormDB *GormDB
}

// NewDatabase creates a new database instance
func NewDatabase(gormDB *GormDB) (*Database, error) {
	// Ensure db directory exists (cross-platform permissions)
	if err := os.MkdirAll("db", 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %v", err)
	}

	db, err := sql.Open("sqlite3", filepath.Join("db", "store.db")+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	database := &Database{db: db, gormDB: gormDB}
	if err := database.init(); err != nil {
		return nil, err
	}

	return database, nil
}

// init initializes the database tables
func (d *Database) init() error {
	// Create phone_map table
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS phone_map (
			phone TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create phone_map table: %v", err)
	}

	// Create senders table
	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS senders (
			phone TEXT PRIMARY KEY,
			device_id TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			authenticated_at TIMESTAMP,
			invalidated_at TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create senders table: %v", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

// StorePhoneMapping stores a phone number to device ID mapping
func (d *Database) StorePhoneMapping(phone, deviceID string) error {
	_, err := d.db.Exec("INSERT INTO phone_map (phone, device_id) VALUES (?, ?)", phone, deviceID)
	if err != nil {
		return fmt.Errorf("failed to store phone mapping: %v", err)
	}
	return nil
}

// GetPhoneMapping retrieves a device ID for a given phone number
func (d *Database) GetPhoneMapping(phone string) (string, error) {
	var deviceID string
	err := d.db.QueryRow("SELECT device_id FROM phone_map WHERE phone = ?", phone).Scan(&deviceID)
	if err != nil {
		return "", fmt.Errorf("phone mapping not found: %v", err)
	}
	return deviceID, nil
}

// GetAllPhoneMappings retrieves all phone mappings
func (d *Database) GetAllPhoneMappings() (map[string]string, error) {
	rows, err := d.db.Query("SELECT phone, device_id FROM phone_map")
	if err != nil {
		return nil, fmt.Errorf("failed to query phone_map: %v", err)
	}
	defer rows.Close()

	mappings := make(map[string]string)
	for rows.Next() {
		var phone, deviceID string
		if err := rows.Scan(&phone, &deviceID); err != nil {
			log.Printf("Failed to scan phone mapping: %v", err)
			continue
		}
		mappings[phone] = deviceID
	}

	return mappings, nil
}

// DeletePhoneMapping removes a phone mapping from the database
func (d *Database) DeletePhoneMapping(phone string) error {
	_, err := d.db.Exec("DELETE FROM phone_map WHERE phone = ?", phone)
	if err != nil {
		return fmt.Errorf("failed to delete phone mapping: %v", err)
	}
	return nil
}

// PhoneExists checks if a phone number is already registered
func (d *Database) PhoneExists(phone string) bool {
	var deviceID string
	err := d.db.QueryRow("SELECT device_id FROM phone_map WHERE phone = ?", phone).Scan(&deviceID)
	return err == nil
}

// Sender methods
// CreateSender creates a new sender record
func (d *Database) CreateSender(phone string) error {
	_, err := d.db.Exec("INSERT INTO senders (phone, status) VALUES (?, 'pending')", phone)
	if err != nil {
		return fmt.Errorf("failed to create sender: %v", err)
	}

	// Sync to MSSQL if available
	if d.gormDB != nil {
		sender := &models.Sender{
			Phone:     phone,
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		return d.gormDB.SyncSenderToMSSQL(sender)
	}
	return nil
}

// GetSender retrieves a sender by phone number
func (d *Database) GetSender(phone string) (*models.Sender, error) {
	var s models.Sender
	var deviceID sql.NullString
	var authenticatedAt, invalidatedAt sql.NullTime

	err := d.db.QueryRow(`
		SELECT phone, device_id, status, created_at, authenticated_at, invalidated_at 
		FROM senders WHERE phone = ?
	`, phone).Scan(&s.Phone, &deviceID, &s.Status, &s.CreatedAt, &authenticatedAt, &invalidatedAt)

	if err != nil {
		return nil, fmt.Errorf("sender not found: %v", err)
	}

	// Handle NULL device_id
	if deviceID.Valid {
		s.DeviceID = deviceID.String
	} else {
		s.DeviceID = ""
	}

	if authenticatedAt.Valid {
		s.AuthenticatedAt = &authenticatedAt.Time
	}
	if invalidatedAt.Valid {
		s.InvalidatedAt = &invalidatedAt.Time
	}

	return &s, nil
}

// UpdateSenderStatus updates the status of a sender
func (d *Database) UpdateSenderStatus(phone, status string) error {
	var query string
	switch status {
	case "authenticated":
		query = "UPDATE senders SET status = ?, authenticated_at = CURRENT_TIMESTAMP WHERE phone = ?"
	case "invalidated":
		query = "UPDATE senders SET status = ?, invalidated_at = CURRENT_TIMESTAMP WHERE phone = ?"
	default:
		query = "UPDATE senders SET status = ? WHERE phone = ?"
	}

	_, err := d.db.Exec(query, status, phone)
	if err != nil {
		return fmt.Errorf("failed to update sender status: %v", err)
	}

	// Sync to MSSQL if available
	if d.gormDB != nil {
		sender, err := d.GetSender(phone)
		if err != nil {
			return fmt.Errorf("failed to get sender for sync: %v", err)
		}
		return d.gormDB.SyncSenderToMSSQL(sender)
	}
	return nil
}

// UpdateSenderDeviceID updates the device ID of a sender
func (d *Database) UpdateSenderDeviceID(phone, deviceID string) error {
	_, err := d.db.Exec("UPDATE senders SET device_id = ? WHERE phone = ?", deviceID, phone)
	if err != nil {
		return fmt.Errorf("failed to update sender device ID: %v", err)
	}
	return nil
}

// GetAllSenders retrieves all senders
func (d *Database) GetAllSenders() ([]*models.Sender, error) {
	rows, err := d.db.Query(`
		SELECT phone, device_id, status, created_at, authenticated_at, invalidated_at 
		FROM senders ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query senders: %v", err)
	}
	defer rows.Close()

	var senders []*models.Sender
	for rows.Next() {
		var s models.Sender
		var deviceID sql.NullString
		var authenticatedAt, invalidatedAt sql.NullTime

		err := rows.Scan(&s.Phone, &deviceID, &s.Status, &s.CreatedAt, &authenticatedAt, &invalidatedAt)
		if err != nil {
			log.Printf("Failed to scan sender: %v", err)
			continue
		}

		// Handle NULL device_id
		if deviceID.Valid {
			s.DeviceID = deviceID.String
		} else {
			s.DeviceID = ""
		}

		if authenticatedAt.Valid {
			s.AuthenticatedAt = &authenticatedAt.Time
		}
		if invalidatedAt.Valid {
			s.InvalidatedAt = &invalidatedAt.Time
		}

		senders = append(senders, &s)
	}

	return senders, nil
}

// SenderExists checks if a sender exists
func (d *Database) SenderExists(phone string) bool {
	var status string
	err := d.db.QueryRow("SELECT status FROM senders WHERE phone = ?", phone).Scan(&status)
	return err == nil
}

// DeleteSender deletes a sender from the database
func (d *Database) DeleteSender(phone string) error {
	// Delete from senders table
	_, err := d.db.Exec("DELETE FROM senders WHERE phone = ?", phone)
	if err != nil {
		return fmt.Errorf("failed to delete sender: %v", err)
	}

	// Delete from phone_map table (if exists)
	_, err = d.db.Exec("DELETE FROM phone_map WHERE phone = ?", phone)
	if err != nil {
		log.Printf("Warning: Failed to delete phone mapping for %s: %v", phone, err)
		// Don't return error as the main deletion succeeded
	}

	return nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}
