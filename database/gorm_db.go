package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/jaliph/auto-dm/models"
)

// GormDB represents the GORM database connection
type GormDB struct {
	db *gorm.DB
}

// NewGormDB creates a new GORM database connection
func NewGormDB(server, database, username, password string) (*GormDB, error) {
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s?database=%s", username, password, server, database)

	db, err := gorm.Open(sqlserver.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MSSQL: %v", err)
	}

	gormDB := &GormDB{db: db}

	// Auto migrate the database
	if err := gormDB.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %v", err)
	}

	log.Println("GORM database connected successfully")
	return gormDB, nil
}

// migrate runs database migrations
func (gdb *GormDB) migrate() error {
	return gdb.db.AutoMigrate(&models.Message{}, &models.Sender{})
}

// StoreMessage stores a WhatsApp message in the database
func (gdb *GormDB) StoreMessage(message *models.Message) error {
	result := gdb.db.Create(message)
	if result.Error != nil {
		return fmt.Errorf("failed to store message: %v", result.Error)
	}
	return nil
}

// GetMessagesByPhone retrieves messages for a specific phone number
func (gdb *GormDB) GetMessagesByPhone(phone string, limit int) ([]models.Message, error) {
	var messages []models.Message
	result := gdb.db.Where("sender_phone = ? OR recipient_phone = ?", phone, phone).
		Order("timestamp DESC").
		Limit(limit).
		Find(&messages)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get messages: %v", result.Error)
	}
	return messages, nil
}

// GetMessagesByChat retrieves messages for a specific chat
func (gdb *GormDB) GetMessagesByChat(chatID string, limit int) ([]models.Message, error) {
	var messages []models.Message
	result := gdb.db.Where("chat_id = ?", chatID).
		Order("timestamp DESC").
		Limit(limit).
		Find(&messages)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get chat messages: %v", result.Error)
	}
	return messages, nil
}

// GetMessageStats retrieves message statistics
func (gdb *GormDB) GetMessageStats() (*models.MessageStats, error) {
	var stats models.MessageStats

	// Total messages
	if err := gdb.db.Model(&models.Message{}).Count(&stats.TotalMessages).Error; err != nil {
		return nil, fmt.Errorf("failed to count total messages: %v", err)
	}

	// Messages today
	today := time.Now().Truncate(24 * time.Hour)
	if err := gdb.db.Model(&models.Message{}).Where("timestamp >= ?", today).Count(&stats.MessagesToday).Error; err != nil {
		return nil, fmt.Errorf("failed to count today's messages: %v", err)
	}

	// Messages this week
	weekAgo := time.Now().AddDate(0, 0, -7)
	if err := gdb.db.Model(&models.Message{}).Where("timestamp >= ?", weekAgo).Count(&stats.MessagesThisWeek).Error; err != nil {
		return nil, fmt.Errorf("failed to count this week's messages: %v", err)
	}

	// Messages this month
	monthAgo := time.Now().AddDate(0, -1, 0)
	if err := gdb.db.Model(&models.Message{}).Where("timestamp >= ?", monthAgo).Count(&stats.MessagesThisMonth).Error; err != nil {
		return nil, fmt.Errorf("failed to count this month's messages: %v", err)
	}

	return &stats, nil
}

// GetRecentMessages retrieves recent messages
func (gdb *GormDB) GetRecentMessages(limit int) ([]models.Message, error) {
	var messages []models.Message
	result := gdb.db.Order("timestamp DESC").Limit(limit).Find(&messages)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get recent messages: %v", result.Error)
	}
	return messages, nil
}

// GetAllSenders retrieves all senders from the database
func (gdb *GormDB) GetAllSenders() ([]models.Sender, error) {
	var senders []models.Sender
	result := gdb.db.Order("created_at DESC").Find(&senders)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get senders: %v", result.Error)
	}
	return senders, nil
}

// UpdateSenderStatus updates the status of a sender
func (gdb *GormDB) UpdateSenderStatus(phone, status string) error {
	var query string
	switch status {
	case "authenticated":
		query = "UPDATE senders SET status = ?, authenticated_at = CURRENT_TIMESTAMP WHERE phone = ?"
	case "invalidated":
		query = "UPDATE senders SET status = ?, invalidated_at = CURRENT_TIMESTAMP WHERE phone = ?"
	default:
		query = "UPDATE senders SET status = ? WHERE phone = ?"
	}

	result := gdb.db.Exec(query, status, phone)
	if result.Error != nil {
		return fmt.Errorf("failed to update sender status: %v", result.Error)
	}
	return nil
}

// Close closes the database connection
func (gdb *GormDB) Close() error {
	sqlDB, err := gdb.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SyncSenderToMSSQL syncs a sender from SQLite to MSSQL
func (gdb *GormDB) SyncSenderToMSSQL(sender *models.Sender) error {
	var existingSender models.Sender
	result := gdb.db.Where("phone = ?", sender.Phone).First(&existingSender)

	if result.Error != nil {
		if result.Error.Error() == "record not found" {
			// Create new sender
			return gdb.db.Create(sender).Error
		}
		return result.Error
	}

	// Update existing sender with specific fields using WHERE clause
	return gdb.db.Model(&models.Sender{}).Where("phone = ?", sender.Phone).Updates(map[string]interface{}{
		"device_id":        sender.DeviceID,
		"status":           sender.Status,
		"authenticated_at": sender.AuthenticatedAt,
		"invalidated_at":   sender.InvalidatedAt,
	}).Error
}

// SyncAllSendersToMSSQL syncs all senders from SQLite to MSSQL
func (gdb *GormDB) SyncAllSendersToMSSQL(senders []*models.Sender) error {
	for _, sender := range senders {
		if err := gdb.SyncSenderToMSSQL(sender); err != nil {
			return fmt.Errorf("failed to sync sender %s: %v", sender.Phone, err)
		}
	}
	return nil
}

// ForceSyncAllSenders forces a complete sync of all senders from SQLite to MSSQL
func (gdb *GormDB) ForceSyncAllSenders(sqliteDB interface {
	GetAllSenders() ([]*models.Sender, error)
}) error {
	senders, err := sqliteDB.GetAllSenders()
	if err != nil {
		return fmt.Errorf("failed to get senders from SQLite: %v", err)
	}

	log.Printf("Force syncing %d senders from SQLite to MSSQL", len(senders))
	return gdb.SyncAllSendersToMSSQL(senders)
}

// DeleteSender deletes a sender from MSSQL database
func (gdb *GormDB) DeleteSender(phone string) error {
	result := gdb.db.Where("phone = ?", phone).Delete(&models.Sender{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete sender from MSSQL: %v", result.Error)
	}
	return nil
}
