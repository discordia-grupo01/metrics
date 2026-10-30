.PHONY: run test build migrate

run:
	go run ./cmd/api

test:
	go test ./... -cover

build:
	go build -o bin/api ./cmd/api

migrate:
	migrate -path migrations -database "$$DATABASE_URL" up
