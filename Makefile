.PHONY: up down seed test fmt vet report check-tags build-minimal

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

check-tags:
	go build -tags snowflake ./...
	go build -tags noduckdb ./...
	go build -tags "snowflake noduckdb" ./...

build-minimal:
	docker build --target sqlagent-minimal -t sqlagent-minimal .

fmt:
	gofmt -w cmd internal scripts

vet:
	go vet ./...
