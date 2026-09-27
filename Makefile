.PHONY: up down seed test fmt vet report

up:
	docker compose up -d --wait

down:
	docker compose down

seed:
	go run ./cmd/sqlagent seed

test:
	go test ./...

report:
	go run scripts/make_report.go results.json -out report.md

fmt:
	gofmt -w cmd internal scripts

vet:
	go vet ./...
