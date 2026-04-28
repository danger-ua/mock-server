# Mock Server

Configuration-driven mock HTTP API written in Go. Routes are loaded from `routes.json`.

## Commands

```bash
go test ./...
go run ./cmd/mock-server
```

```bash
docker build -t mock-server .
docker run --rm -p 8000:8000 mock-server
```

## Custom Manifest

```bash
docker run --rm -p 8000:8000 -v /host/path/routes.json:/app/routes.json:ro mock-server
```

```bash
docker run --rm -p 8000:8000 \
  -e MOCK_SERVER_ROUTES_PATH=/app/routes.json \
  -e MOCK_SERVER_WATCH=1 \
  -v /host/routes.json:/app/routes.json:ro \
  mock-server
```

Set `MOCK_SERVER_WATCH=1` to reload the mounted routes file when it changes. You can also reload manually with `POST /__mock/reload`.