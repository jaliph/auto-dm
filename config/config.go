package config

import (
	"os"
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

// LoadConfig loads configuration from environment variables
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

	return config
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
