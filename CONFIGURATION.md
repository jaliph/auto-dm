# Configuration Guide

## Environment Variables

The application can be configured using the following environment variables:

### Database Configuration

```bash
# MSSQL Database Settings
export MSSQL_SERVER="localhost:1433"
export MSSQL_DATABASE="whatsapp_automation"
export MSSQL_USERNAME="sa"
export MSSQL_PASSWORD="YourStrong@Passw0rd"
```

### API Configuration

```bash
# API Server Port
export API_PORT=":8080"
```

## Database Setup

### MSSQL Database

1. Install Microsoft SQL Server
2. Create a database named `whatsapp_automation`
3. The application will automatically create the required tables

### SQLite Databases (Automatic)

The application automatically creates these SQLite databases:
- `admin_store.db` - Admin's WhatsApp session
- `user_<phone>.db` - Individual user WhatsApp sessions
- `store.db` - Phone number to device ID mappings

## Running the Application

1. Set up environment variables (optional, defaults will be used)
2. Run the application:
   ```bash
   go run main.go
   ```
3. Scan the QR code with your WhatsApp app for admin authentication
4. Send commands to your own WhatsApp number:
   - `/register <phone>` - Register a new user
   - `/reconnect <phone>` - Reconnect a user
   - `/unregister <phone>` - Unregister a user

## API Endpoints

- `POST /send` - Send messages with JSON body:
  ```json
  {
    "sender": "1234567890",
    "recipient": "9876543210",
    "message": "Hello World"
  }
  ```
- `GET /messages?phone=<phone>&limit=<limit>` - Get messages for a phone
- `GET /messages?limit=<limit>` - Get recent messages
- `GET /stats` - Get message statistics

## Message Storage

All WhatsApp messages received by any registered client are automatically stored in the MSSQL database with the following information:
- Sender and recipient phone numbers
- Message type (text, image, video, etc.)
- Message content and media URLs
- Timestamps and chat IDs
