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
- **Delete Sender**: `DELETE /senders/{phone}` - Delete a registered sender (logs out from WhatsApp, unlinks device, and cleans up all data)
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

### Chat Participant Management
- **Get Chat Participants**: `GET /chat-participants` - Get all chat participants with their auto-reply settings
- **Create Chat Participant**: `POST /chat-participants` with JSON body:
  ```json
  {
    "phone": "919876543210",
    "name": "John Doe",
    "auto_reply_enabled": true
  }
  ```
  Returns:
  ```json
  {
    "status": "success",
    "message": "Chat participant created successfully",
    "data": {
      "id": 1,
      "phone": "919876543210",
      "name": "John Doe",
      "auto_reply_enabled": true,
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  }
  ```

- **Update Chat Participant**: `PUT /chat-participants/{phone}` with JSON body:
  ```json
  {
    "name": "John Smith",
    "auto_reply_enabled": false
  }
  ```

- **Toggle Auto-Reply**: `POST /chat-participants/{phone}/auto-reply` with JSON body:
  ```json
  {
    "auto_reply_enabled": false
  }
  ```
  Returns:
  ```json
  {
    "status": "success",
    "message": "Auto-reply disabled for chat participant 919876543210",
    "data": {
      "id": 1,
      "phone": "919876543210",
      "name": "John Doe",
      "auto_reply_enabled": false,
      "created_at": "2024-01-01T12:00:00Z",
      "updated_at": "2024-01-01T12:30:00Z"
    }
  }
  ```

- **Delete Chat Participant**: `DELETE /chat-participants/{phone}` - Delete a chat participant

### Database Structure
- **Sender Stores**: `db/user_<phone>.db` - Individual sender WhatsApp sessions (SQLite)
- **Sender Tracking**: `db/store.db` - Maps phone numbers to device IDs and tracks authentication status (SQLite)
- **Chat Participants**: MSSQL database with `chat_participants` table for auto-reply settings
- **Message Storage**: MSSQL database with `whatsapp_messages` table

#### Database Separation
- **SQLite**: Used only for WhatsApp session storage and sender tracking
- **MSSQL**: Used for message storage and chat participant management

#### Chat Participants Table Schema (MSSQL)
```sql
CREATE TABLE chat_participants (
    id INT IDENTITY(1,1) PRIMARY KEY,
    phone NVARCHAR(20) UNIQUE NOT NULL,
    name NVARCHAR(100),
    auto_reply_enabled BIT NOT NULL DEFAULT 1,
    created_at DATETIMEOFFSET DEFAULT GETDATE(),
    updated_at DATETIMEOFFSET DEFAULT GETDATE()
);
```

### File Management
- **File Sharing**: Files to be shared are stored in the configured `share_folder` (default: `./files`)
- **Media Downloads**: Received media files are automatically downloaded to the configured `receive_folder` (default: `./received`)
- **Supported Types**: Any document type supported by WhatsApp (PDF, DOC, XLS, etc.)
- **Media Types**: Images, videos, audio, documents, stickers
- **Usage**: Place files in the share folder and reference them by filename in the API
- **Cross-Platform**: Paths work correctly on Windows, macOS, and Linux

### AI Auto-Reply with Ollama
- **Automatic Responses**: Automatically replies to received text messages using AI
- **Ollama Integration**: Uses local Ollama server for AI responses
- **Conditional Activation**: Only enabled when both Ollama URL and model are configured
- **Connection Resilience**: Automatically disables feature if Ollama server is unavailable
- **Response Cleaning**: Automatically removes internal AI thinking patterns from responses
- **Text-Only**: Only responds to text messages, not media files
- **Real-time**: Processes messages as they are received

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
3. **Create file folders** (optional):
   ```bash
   mkdir files
   mkdir received
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
2. Install Microsoft SQL Server (required for message storage and chat participant management)
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

5. **Delete a Sender**:
   ```bash
   curl -X DELETE "http://localhost:8080/senders/911234567890"
   ```
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

7. **Manage Chat Participants**:
   ```bash
   # Get all chat participants
   curl "http://localhost:8080/chat-participants"
   
   # Create a new chat participant
   curl -X POST http://localhost:8080/chat-participants \
     -H "Content-Type: application/json" \
     -d '{
       "phone": "919876543210",
       "name": "John Doe",
       "auto_reply_enabled": true
     }'
   
   # Update a chat participant
   curl -X PUT http://localhost:8080/chat-participants/919876543210 \
     -H "Content-Type: application/json" \
     -d '{
       "name": "John Smith",
       "auto_reply_enabled": false
     }'
   
   # Toggle auto-reply for a participant
   curl -X POST http://localhost:8080/chat-participants/919876543210/auto-reply \
     -H "Content-Type: application/json" \
     -d '{
       "auto_reply_enabled": false
     }'
   
   # Delete a chat participant
   curl -X DELETE "http://localhost:8080/chat-participants/919876543210"
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
- **`database`**: Manages SQLite operations for sender mappings and GORM for MSSQL message/participant storage
- **`store`**: Handles WhatsApp session storage for senders
- **`whatsapp`**: Manages WhatsApp client operations and QR code sessions
- **`api`**: Handles HTTP requests for the REST API
- **`server`**: Manages the HTTP server lifecycle

### Database Architecture

The application uses a hybrid database approach:
- **SQLite**: Lightweight, file-based storage for WhatsApp session data and sender tracking
- **MSSQL**: Enterprise-grade database for message storage and chat participant management
- **GORM**: ORM layer for MSSQL operations with automatic migrations
- **Dual Database**: Separate concerns between session management (SQLite) and business data (MSSQL)

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
# Required: MSSQL Database (for messages and chat participants)
export MSSQL_SERVER="localhost"
export MSSQL_DATABASE="whatsapp_automation"
export MSSQL_USERNAME="sa"
export MSSQL_PASSWORD="YourPassword123!"

# API Configuration
export API_PORT=":8080"
export FILE_SHARE_FOLDER="./files"
export RECEIVE_FOLDER="./received"

# Optional: Ollama AI integration
export OLLAMA_URL="http://localhost:11434"
export OLLAMA_MODEL="llama2"
```

#### **2. config.ini File** (Recommended for development):
```ini
[database]
# Required: MSSQL Database for message storage and chat participant management
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
receive_folder = ./received

[ollama]
# Optional: AI auto-reply feature
url = http://localhost:11434
model = llama2
```

#### **3. Default Values** (fallback):
- **API Server**: `:8080`
- **QR Code Expiry**: 10 minutes
- **Connection Check Interval**: 1 minute
- **Database Files**: SQLite files in the `db/` directory (WhatsApp sessions only)
- **MSSQL Database**: Required for message storage and chat participant management
- **File Sharing**: `./files` directory
- **Build Output**: Binary files in the `build/` directory

### **Configuration Priority**:
1. **`config.ini`** (highest priority)
2. **Environment variables** (fallback)
3. **Default values** (lowest priority)

## Ollama AI Integration

### Overview
The WhatsApp automation app includes optional AI auto-reply functionality using Ollama. When configured, the app will automatically respond to received text messages using a local AI model.

### Setup

#### 1. Install Ollama
First, install Ollama on your system:
```bash
# macOS/Linux
curl -fsSL https://ollama.ai/install.sh | sh

# Windows
# Download from https://ollama.ai/download
```

#### 2. Pull a Model
Download an AI model to use:
```bash
# Popular models
ollama pull llama2
ollama pull qwen2.5:7b
ollama pull mistral:7b
ollama pull codellama:7b
```

#### 3. Configure the App
Add Ollama configuration to your `config.ini`:
```ini
[ollama]
# Ollama server URL (default: http://localhost:11434)
url = http://localhost:11434

# AI model name (must match the model you pulled)
model = llama2
```

Or use environment variables:
```bash
export OLLAMA_URL="http://localhost:11434"
export OLLAMA_MODEL="llama2"
```

#### 4. Start Ollama Server
```bash
# Start Ollama server
ollama serve

# In another terminal, test the model
ollama run llama2 "Hello, how are you?"
```

### How It Works

1. **Message Reception**: When a text message is received by any authenticated sender
2. **Chat Participant Check**: The system checks if the sender is in the chat participants table
   - If not found, automatically creates a new entry with auto-reply enabled by default
   - If found, checks the `auto_reply_enabled` setting
3. **Auto-Reply Decision**: Only proceeds if auto-reply is enabled for that participant
4. **AI Processing**: The message is sent to the configured Ollama model
5. **Response Generation**: Ollama generates an AI response
6. **Response Cleaning**: Internal thinking patterns are automatically removed
7. **Auto-Reply**: The cleaned response is automatically sent back to the original sender

### Features

#### **Conditional Activation**
- Only activates when both `OLLAMA_URL` and `OLLAMA_MODEL` are configured
- Automatically disables if Ollama server is unavailable
- Graceful fallback - doesn't affect other WhatsApp functionality

#### **Automatic Chat Participant Management**
- New chat participants are automatically created when they send their first message
- Auto-reply is enabled by default for new participants
- Use the chat participant APIs to manage auto-reply settings per participant
- Disable auto-reply for specific participants without affecting others
- Chat participant data is stored in MSSQL database for persistence and scalability

#### **Response Cleaning**
Automatically removes common AI thinking patterns:
- `<think>...</think>` blocks
- `<thinking>...</thinking>` blocks
- `<reasoning>...</reasoning>` blocks
- `<internal>...</internal>` blocks
- `<process>...</process>` blocks

#### **Logging**
The app provides comprehensive logging for the AI feature:
```
✅ Ollama connection successful, auto-reply feature enabled
DEBUG: Generating AI response for message from 919148155203: Hi
DEBUG: Original AI response: <think>This is a greeting...</think>Hello! How can I help you?
DEBUG: Cleaned AI response: Hello! How can I help you?
✅ Auto-reply sent from 918088584128 to 919148155203: Hello! How can I help you?
```

### Troubleshooting

#### **Feature Not Working**
1. **Check Configuration**: Ensure both `url` and `model` are set in config
2. **Verify Ollama Server**: Make sure `ollama serve` is running
3. **Test Model**: Try `ollama run <model> "test"` to verify the model works
4. **Check Logs**: Look for Ollama-related log messages in the app output

#### **Connection Issues**
```
WARNING: Ollama connection failed, auto-reply feature will be disabled
```
- Verify Ollama server is running on the configured URL
- Check if the model name matches exactly
- Ensure network connectivity to the Ollama server

#### **Response Quality Issues**
- Try different models for better responses
- Some models may require specific prompts or system messages
- Consider using larger models for better quality (e.g., `llama2:70b` vs `llama2:7b`)

### Supported Models
Any model available in Ollama can be used:
- **Llama 2**: `llama2`, `llama2:7b`, `llama2:13b`, `llama2:70b`
- **Qwen**: `qwen2.5:7b`, `qwen2.5:14b`, `qwen2.5:32b`
- **Mistral**: `mistral:7b`, `mistral:7b-instruct`
- **Code Llama**: `codellama:7b`, `codellama:13b`
- **Custom Models**: Any model you've created or pulled

### Security Considerations
- **Local Processing**: All AI processing happens locally on your Ollama server
- **No External APIs**: No data is sent to external AI services
- **Privacy**: Messages are only processed by your local AI model
- **Network**: Ensure Ollama server is properly secured if exposed to network

## Dependencies

- `go.mau.fi/whatsmeow` - WhatsApp client library
- `modernc.org/sqlite` - Pure Go SQLite driver
- `github.com/skip2/go-qrcode` - QR code generation
- `google.golang.org/protobuf` - Protocol buffers
- `gorm.io/gorm` - GORM ORM
- `gorm.io/driver/sqlserver` - MSSQL driver for GORM

## License

This project is licensed under the MIT License.
