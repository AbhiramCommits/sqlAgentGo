.PHONY: up down seed test fmt vet

up:
	docker compose up -d --wait

down:
	docker compose down

seed:
	go run ./cmd/sqlagent seed

test:
	go test ./...

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...
