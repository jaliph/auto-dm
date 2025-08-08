# WhatsApp Automation App

A modular WhatsApp automation application built with Go using the `whatsmeow` library. The app allows sender registration and authentication via QR codes with REST API functionality.

## Project Structure

The application is organized into separate packages for better maintainability:

```
auto-dm/
├── main.go                 # Main application entry point
├── models/
│   └── types.go           # Data structures and types
├── database/
│   ├── database.go        # SQLite database operations
│   └── gorm_db.go         # GORM database operations
├── store/
│   └── user_store.go      # User WhatsApp stores management
├── whatsapp/
│   ├── client.go          # WhatsApp client management
│   └── qr_manager.go      # QR code session management
├── api/
│   └── handlers.go        # HTTP API handlers
└── server/
    └── server.go          # HTTP server management
```

## Features

### Sender Registration & Authentication
- **QR Code Authentication**: Time-limited QR codes for sender authentication
- **Session Persistence**: All sessions are stored in separate SQLite databases
- **Connection Monitoring**: Automatic monitoring of sender connections
- **Status Tracking**: Track sender authentication status (pending, authenticated, invalidated)

### Message Storage
- **GORM Integration**: Uses GORM ORM for database operations
- **MSSQL Database**: Stores all WhatsApp messages in Microsoft SQL Server
- **Message Types**: Supports text, image, video, audio, document, sticker, contact, and location messages
- **Message Statistics**: Provides message statistics and analytics

### REST API
- **Register Sender**: `POST /register` with JSON body:
  ```json
  {
    "phone": "911234567890"
  }
  ```
     Returns:
   ```json
   {
     "status": "success",
     "message": "QR code session created successfully",
     "qr_url": "http://localhost:8080/qr/abc123def456",
     "expires_at": "2024-01-01T12:00:00Z"
   }
   ```
   
   **Error Responses:**
   
   **Already Authenticated:**
   ```json
   {
     "status": "error",
     "error": "Sender 911234567890 is already authenticated"
   }
   ```
   
   **General Error:**
   ```json
   {
     "status": "error",
     "error": "Failed to create QR session: some error message"
   }
   ```

- **Get QR Code**: `GET /qr/{token}` - Get QR code for authentication
- **Get Senders**: `GET /senders` - Get all registered senders with their status
- **Send Message**: `POST /send` with JSON body:
  ```json
  {
    "sender": "911234567890",
    "recipient": "919876543210", 
    "type": "text",
    "message": "Hello World"
  }
  ```
  *Note: Sent messages are automatically recorded to MSSQL database*

- **Send File**: `POST /send` with JSON body:
  ```json
  {
    "sender": "911234567890",
    "recipient": "919876543210", 
    "type": "file",
    "file_name": "document.pdf"
  }
  ```
  *Note: Sent file messages are automatically recorded to MSSQL database with filename and file path*
- **Get Messages**: `GET /messages?phone=<phone>&limit=<limit>` - Retrieve messages for a specific phone
- **Get Recent Messages**: `GET /messages?limit=<limit>` - Get recent messages
- **Get Statistics**: `GET /stats` - Get message statistics

### Database Structure
- **Sender Stores**: `db/user_<phone>.db` - Individual sender WhatsApp sessions
- **Sender Tracking**: `db/store.db` - Maps phone numbers to device IDs and tracks authentication status
- **Message Storage**: MSSQL database with `whatsapp_messages` table

### File Sharing
- **File Storage**: Files to be shared are stored in the configured `share_folder` (default: `./files`)
- **Supported Types**: Any document type supported by WhatsApp (PDF, DOC, XLS, etc.)
- **Usage**: Place files in the folder and reference them by filename in the API
- **Cross-Platform**: Paths work correctly on Windows, macOS, and Linux

## Installation

### Option 1: Download Pre-built Binary (Recommended)

Download the latest release from [GitHub Releases](https://github.com/jaliph/auto-dm/releases) for your platform:

- **Linux**: `auto-dm_Linux_amd64.tar.gz` or `auto-dm_Linux_arm64.tar.gz`
- **macOS**: `auto-dm_Darwin_amd64.tar.gz` or `auto-dm_Darwin_arm64.tar.gz`
- **Windows**: `auto-dm_Windows_amd64.zip` or `auto-dm_Windows_arm64.zip`

#### **Setup for Binary Users**:

1. **Extract the binary** to a directory of your choice
2. **Create configuration** (optional):
   ```bash
   # Copy the example config
   cp config.ini.example config.ini
   
   # Edit the configuration
   nano config.ini  # or use any text editor
   ```
3. **Create file sharing folder** (optional):
   ```bash
   mkdir files
   # Place files you want to share in this folder
   ```
4. **Run the application**:
   ```bash
   ./auto-dm  # Linux/macOS
   auto-dm.exe  # Windows
   ```

**Directory Structure After Setup**:
```
your-auto-dm-folder/
├── auto-dm              # The executable
├── config.ini           # Configuration (optional)
├── files/               # File sharing folder (auto-created)
│   └── your-files.pdf   # Files to share
└── db/                  # Database folder (auto-created)
    ├── store.db         # Main database
    └── user_*.db        # Sender databases
```

### Option 2: Build from Source

1. Install Go 1.24.5 or later
2. Install Microsoft SQL Server
3. Clone the repository
4. Install dependencies:
   ```bash
   make deps
   ```
   Or manually:
   ```bash
   go mod tidy
   ```
5. Set up environment variables (optional):
   ```bash
   export MSSQL_SERVER="localhost"
   export MSSQL_DATABASE="whatsapp_automation"
   export MSSQL_USERNAME="sa"
   export MSSQL_PASSWORD="YourPassword123!"
   export API_PORT=":8080"
   ```

## Usage

### Quick Start

1. **Download and run** (if using pre-built binary):
   ```bash
   # Extract the binary
   tar -xzf auto-dm_Linux_x86_64.tar.gz
   
   # Run the application
   ./auto-dm
   ```

2. **Build and run** (if building from source):
   ```bash
   # Build the application
   make build
   
   # Run the application
   ./build/auto-dm
   ```

3. **Docker** (if using Docker):
   ```bash
   # Run with Docker
   docker run -p 8080:8080 -v $(pwd)/config:/app/config -v $(pwd)/files:/app/files ghcr.io/jaliph/auto-dm:latest
   ```

2. **Register a Sender**:
   ```bash
   curl -X POST http://localhost:8080/register \
     -H "Content-Type: application/json" \
     -d '{"phone": "911234567890"}'
   ```
   
   **Re-registration**: If a sender's previous registration failed (expired or invalidated), they can register again with the same phone number. The system will automatically handle re-registration for failed senders.

3. **Get QR Code for Authentication**:
   ```bash
   # JSON response (default)
   curl "http://localhost:8080/qr/abc123def456"
   
   # HTML response (for browser display)
   curl "http://localhost:8080/qr/abc123def456?format=html"
   ```
   
   **JSON Response:**
   ```json
   {
     "status": "success",
     "qr_code": "2@...",
     "qr_code_png": "iVBORw0KGgoAAAANSUhEUgAA...",
     "expired": false
   }
   ```
   
   **HTML Response:**
   - Returns a complete HTML page with the QR code displayed
   - Includes instructions for scanning
   - Shows expiration time
   - Can be opened directly in a browser
   
   **Response Fields:**
   - `qr_code`: QR code string (for backward compatibility)
   - `qr_code_png`: Base64 encoded PNG image (can be displayed directly in HTML with `<img src="data:image/png;base64,{qr_code_png}">`)
   
   **Error Responses:**
   
   **Expired QR Code:**
   ```json
   {
     "status": "error",
     "error": "QR code session expired",
     "expired": true
   }
   ```
   
   **Invalid/Not Found QR Code:**
   ```json
   {
     "status": "error",
     "error": "QR code not found: session not found",
     "expired": false
   }
   ```
   
   **HTML Error Responses:**
   - Add `?format=html` to any QR code URL to get HTML error pages
   - Error pages include clear messages and instructions

4. **Check Sender Status**:
   ```bash
   curl "http://localhost:8080/senders"
   ```

5. **Send Messages via API**:
   ```bash
   curl -X POST http://localhost:8080/send \
     -H "Content-Type: application/json" \
     -d '{
       "sender": "911234567890",
       "recipient": "919876543210",
       "message": "Hello World"
     }'
   ```

6. **Retrieve Messages**:
   ```bash
   # Get messages for a specific phone
   curl "http://localhost:8080/messages?phone=1234567890&limit=10"
   
   # Get recent messages
   curl "http://localhost:8080/messages?limit=20"
   
   # Get message statistics
   curl "http://localhost:8080/stats"
   ```

## Authentication Flow

1. **Register**: Call `/register` with a phone number
2. **Get QR Code**: Use the returned QR URL to get the QR code
3. **Scan QR Code**: Scan the QR code with the target phone's WhatsApp app
4. **Authentication**: The sender is automatically authenticated and ready to send messages
5. **Auto-reconnect**: On subsequent app starts, authenticated senders will auto-reconnect

## Architecture

### Package Responsibilities

- **`models`**: Defines data structures used across the application
- **`database`**: Manages SQLite operations for sender mappings and GORM for message storage
- **`store`**: Handles WhatsApp session storage for senders
- **`whatsapp`**: Manages WhatsApp client operations and QR code sessions
- **`api`**: Handles HTTP requests for the REST API
- **`server`**: Manages the HTTP server lifecycle

### Store Separation

- **Sender Stores**: Each registered sender gets their own `user_<phone>.db` file
- **Sender Tracking**: Central `store.db` tracks phone number to device ID mappings and authentication status

### Connection Monitoring

The app automatically monitors all sender client connections every minute and marks them as invalidated if disconnected.

## Development

### Makefile Commands

The project includes a comprehensive Makefile for common development tasks:

```bash
# Build the application
make build

# Build for multiple platforms (Linux, macOS, Windows)
make build-all

# Run the application
make run

# Run with race detection
make run-race

# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Vet code
make vet

# Clean build artifacts
make clean

# Clean everything including databases
make clean-all

# Release commands
make release-dry-run    # Test GoReleaser configuration
make release-snapshot   # Create snapshot release
make release           # Create full release
make install-goreleaser # Install GoReleaser

# Show all available commands
make help
```

### Creating Releases

This project uses [GoReleaser](https://goreleaser.com/) for automated releases.

#### Prerequisites
1. Install GoReleaser:
   ```bash
   make install-goreleaser
   ```

2. Set up GitHub token for releases (if creating releases manually)

#### Creating a Release

1. **Create a new tag**:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **Create a release** (automated via GitHub Actions):
   - Push a tag starting with `v*` (e.g., `v1.0.0`)
   - GitHub Actions will automatically create a release with binaries for all platforms

3. **Manual release** (if needed):
   ```bash
   # Dry run to test
   make release-dry-run
   
   # Create snapshot release
   make release-snapshot
   
   # Create full release
   make release
   ```

#### Release Artifacts
Each release includes:
- **Binaries**: Linux (x86_64, ARM64), macOS (x86_64, ARM64), Windows (x86_64, ARM64)
- **Docker Images**: `ghcr.io/jaliph/auto-dm:latest` and `ghcr.io/jaliph/auto-dm:v1.0.0`
- **Homebrew Formula**: `jaliph/tap/auto-dm`
- **Checksums**: SHA256 checksums for all binaries

## Configuration

### **Configuration Methods**:

#### **1. Environment Variables** (Recommended for production):
```bash
export MSSQL_SERVER="localhost"
export MSSQL_DATABASE="whatsapp_automation"
export MSSQL_USERNAME="sa"
export MSSQL_PASSWORD="YourPassword123!"
export API_PORT=":8080"
export FILE_SHARE_FOLDER="./files"
```

#### **2. config.ini File** (Recommended for development):
```ini
[database]
mssql_server = localhost
mssql_database = whatsapp_automation
mssql_username = sa
mssql_password = YourPassword123!

[api]
port = :8080

[whatsapp]
connection_check_interval = 1

[files]
share_folder = ./files
```

#### **3. Default Values** (fallback):
- **API Server**: `:8080`
- **QR Code Expiry**: 10 minutes
- **Connection Check Interval**: 1 minute
- **Database Files**: SQLite files in the `db/` directory
- **File Sharing**: `./files` directory
- **Build Output**: Binary files in the `build/` directory

### **Configuration Priority**:
1. **`config.ini`** (highest priority)
2. **Environment variables** (fallback)
3. **Default values** (lowest priority)

## Dependencies

- `go.mau.fi/whatsmeow` - WhatsApp client library
- `modernc.org/sqlite` - Pure Go SQLite driver
- `github.com/skip2/go-qrcode` - QR code generation
- `google.golang.org/protobuf` - Protocol buffers
- `gorm.io/gorm` - GORM ORM
- `gorm.io/driver/sqlserver` - MSSQL driver for GORM

## License

This project is licensed under the MIT License.
