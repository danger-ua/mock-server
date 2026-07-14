# Mock Server

A lightweight, configuration-driven mock HTTP API server written in Go. Perfect for development, testing, and integration scenarios where you need to simulate backend services without deploying actual endpoints.

## Features

- **JSON Configuration**: Define all endpoints, responses, and behaviors in a simple `routes.json` file
- **Dynamic Reloading**: Hot-reload configuration changes without restarting the server
- **HTTP Methods Support**: GET, POST, PUT, DELETE, PATCH, and more
- **Custom Status Codes**: Return any HTTP status code (2xx, 4xx, 5xx)
- **Response Delays**: Simulate network latency with configurable delays
- **JSON Response Bodies**: Full control over response payloads
- **Docker Support**: Pre-built Docker image for easy deployment
- **Watch Mode**: Automatic reload when configuration file changes

## How It Works

Routes are loaded from `routes.json`. Each route defines:
- Request path and HTTP method
- Response status code
- JSON response body
- Optional delay in seconds

## Commands

```bash
go test ./...
go run ./cmd/mock-server
```

```bash
docker build -t mock-server .
docker run --rm -p 8000:8000 mock-server
```

## Quick Start

1. **Start the server** with default routes:
```bash
go run ./cmd/mock-server
```

2. **Test the mock endpoints**:
```bash
# Health check
curl http://localhost:8000/health

# Get users
curl http://localhost:8000/users

# Upload data
curl -X POST http://localhost:8000/upload -H "Content-Type: application/json" -d '{"data": "test"}'
```

3. **Check the response**:
```json
{
  "users": [{"id": 1, "name": "Admin"}]
}
```

## Configuration Examples

### Basic Endpoint
```json
{
  "path": "/api/users",
  "method": "GET",
  "status": 200,
  "response": {
    "users": [
      {"id": 1, "name": "Alice"},
      {"id": 2, "name": "Bob"}
    ]
  }
}
```

### With Simulated Delay
```json
{
  "path": "/api/slow-endpoint",
  "method": "GET",
  "status": 200,
  "response": {"data": "processed"},
  "delay": 2.5
}
```

### Error Response
```json
{
  "path": "/api/unauthorized",
  "method": "GET",
  "status": 401,
  "response": {
    "error": "Unauthorized",
    "message": "Authentication required"
  }
}
```

### Complex POST Response (e.g., OpenAI-like API)
```json
{
  "path": "/v1/chat/completions",
  "method": "POST",
  "status": 200,
  "response": {
    "id": "chatcmpl-mock-1",
    "object": "chat.completion",
    "choices": [
      {
        "index": 0,
        "message": {
          "role": "assistant",
          "content": "This is a mock response"
        }
      }
    ]
  }
}
```

## Usage Examples

### Running Locally

```bash
# Run with default routes.json
make run

# Run tests
make test

# Build binary
make build
./bin/mock-server
```

### Using Custom Configuration

Mount a custom routes file:
```bash
docker run --rm -p 8000:8000 \
  -v /path/to/routes.json:/app/routes.json:ro \
  mock-server
```

With environment variables for path and watch mode:
```bash
docker run --rm -p 8000:8000 \
  -e MOCK_SERVER_ROUTES_PATH=/app/routes.json \
  -e MOCK_SERVER_WATCH=1 \
  -v /path/to/routes.json:/app/routes.json:ro \
  mock-server
```

### Example Requests and Responses

**GET Request:**
```bash
curl -X GET http://localhost:8000/health
```

**POST Request:**
```bash
curl -X POST http://localhost:8000/upload \
  -H "Content-Type: application/json" \
  -d '{"file": "data.txt"}'
```

**Response with Delay (waits 2.5 seconds):**
```bash
curl http://localhost:8000/delay/slow
```

**Error Status Code:**
```bash
curl http://localhost:8000/unauthorized
# Returns 401 with error response
```

### Docker Compose

Start with compose:
```bash
make docker-up
```

View logs:
```bash
docker compose logs -f mock-server
```

Stop services:
```bash
make docker-down
```

### Hot-Reload Configuration

Set `MOCK_SERVER_WATCH=1` to automatically reload routes when the config file changes, or manually reload:

```bash
make reload

# With custom host/port:
make reload HOST=127.0.0.1 PORT=8080

# Using curl:
curl -X POST http://localhost:8000/__mock/reload
```

## Custom Manifest Configuration

Custom route manifests allow you to define mock endpoints for your specific use case. Edit `routes.json` to add or modify endpoints, then reload the configuration.

### Route Schema

Each route in `routes.json` must follow this structure:

```json
{
  "endpoints": [
    {
      "path": "/api/endpoint",           // Request path
      "method": "GET",                   // HTTP method
      "status": 200,                     // HTTP status code
      "response": {},                    // JSON response body
      "delay": 0.5                       // (Optional) delay in seconds
    }
  ]
}
```

**Field Descriptions:**
- `path`: The URL path for this endpoint
- `method`: HTTP method (GET, POST, PUT, DELETE, PATCH, etc.)
- `status`: HTTP status code to return (200, 201, 400, 401, 404, 500, etc.)
- `response`: JSON object to return as the response body
- `delay`: (Optional) Artificial delay in seconds before responding

## Makefile

```bash
make reload                       # POST http://localhost:8000/__mock/reload
make reload HOST=mock PORT=8080   # override target host/port
make run                          # go run ./cmd/mock-server
make test                         # go test ./...
make build                        # build ./bin/mock-server
make docker-build                 # docker build -t mock-server .
make docker-up                    # docker compose up -d
make docker-down                  # docker compose down
```