package config

import (
	"log"
	"os"
	"strconv"

	"gopkg.in/ini.v1"
)

// Config holds application configuration
type Config struct {
	// Database settings
	MSSQLServer   string
	MSSQLDatabase string
	MSSQLUsername string
	MSSQLPassword string

	// API settings
	APIPort string

	// WhatsApp settings
	ConnectionCheckInterval int // in minutes
}

// LoadConfig loads configuration from config.ini file or environment variables
func LoadConfig() *Config {
	config := &Config{
		// Database settings
		MSSQLServer:   getEnv("MSSQL_SERVER", "localhost"),
		MSSQLDatabase: getEnv("MSSQL_DATABASE", "whatsapp_automation"),
		MSSQLUsername: getEnv("MSSQL_USERNAME", "sa"),
		MSSQLPassword: getEnv("MSSQL_PASSWORD", "YourStrong@Passw0rd"),

		// API settings
		APIPort: getEnv("API_PORT", ":8080"),

		// WhatsApp settings
		ConnectionCheckInterval: 1, // 1 minute
	}

	// Try to load from config.ini file
	if err := loadFromINI(config); err != nil {
		log.Printf("Warning: Failed to load config.ini: %v", err)
		log.Println("Using environment variables or defaults")
	}

	return config
}

// loadFromINI loads configuration from config.ini file
func loadFromINI(config *Config) error {
	cfg, err := ini.Load("config.ini")
	if err != nil {
		return err
	}

	// Database section
	if dbSection := cfg.Section("database"); dbSection != nil {
		if server := dbSection.Key("mssql_server").String(); server != "" {
			config.MSSQLServer = server
		}
		if database := dbSection.Key("mssql_database").String(); database != "" {
			config.MSSQLDatabase = database
		}
		if username := dbSection.Key("mssql_username").String(); username != "" {
			config.MSSQLUsername = username
		}
		if password := dbSection.Key("mssql_password").String(); password != "" {
			config.MSSQLPassword = password
		}
	}

	// API section
	if apiSection := cfg.Section("api"); apiSection != nil {
		if port := apiSection.Key("port").String(); port != "" {
			config.APIPort = port
		}
	}

	// WhatsApp section
	if waSection := cfg.Section("whatsapp"); waSection != nil {
		if interval := waSection.Key("connection_check_interval").String(); interval != "" {
			if val, err := strconv.Atoi(interval); err == nil {
				config.ConnectionCheckInterval = val
			}
		}
	}

	return nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
