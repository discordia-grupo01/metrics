.PHONY: run test mutation build migrate

run:
	go run ./cmd/api

test:
	go test ./... -cover

# Mutation testing de internal/service e internal/health con gremlins (ver
# docs/mutation-testing). Tarda menos de un minuto; no corre en el CI.
mutation:
	go clean -testcache
	gremlins unleash ./internal/service --timeout-coefficient 30 --workers 4 \
		--invert-assignments --invert-logical --invert-loopctrl --remove-self-assignments \
		--invert-bitwise --invert-bwassign --output docs/mutation-testing/service-after.json
	gremlins unleash ./internal/health --timeout-coefficient 30 --workers 4 \
		--invert-assignments --invert-logical --invert-loopctrl --remove-self-assignments \
		--invert-bitwise --invert-bwassign --output docs/mutation-testing/health-after.json

build:
	go build -o bin/api ./cmd/api

migrate:
	migrate -path migrations -database "$$DATABASE_URL" up
