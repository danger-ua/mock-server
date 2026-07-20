.PHONY: reload run test build docker-build docker-up docker-down

HOST ?= localhost
PORT ?= 18000
RELOAD_URL := http://$(HOST):$(PORT)/__mock/reload

reload:
	@echo "POST $(RELOAD_URL)"
	@curl -fsS -X POST -w "\nHTTP %{http_code}\n" $(RELOAD_URL)

run:
	go run .

test:
	go test ./...

build:
	go build -o bin/mock-server .

docker-build:
	docker build -t mock-server .

docker-up:
	docker compose up -d

docker-down:
	docker compose down
