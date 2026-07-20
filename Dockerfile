# syntax=docker/dockerfile:1
# Build: docker build -t mock-server .
# Run:  docker run --rm -p 8000:8000 -v /path/to/routes.json:/app/routes.json:ro mock-server
FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
# COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mock-server ./

FROM alpine:3.22

WORKDIR /app

RUN adduser -D -u 1000 appuser

COPY --from=build /out/mock-server /usr/local/bin/mock-server
COPY routes.json ./routes.json

RUN chown -R appuser:appuser /app
USER appuser

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=3s --start-period=8s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8000/__mock/health >/dev/null || exit 1

CMD ["mock-server"]
