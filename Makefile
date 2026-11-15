.PHONY: run test mutation build migrate openapi

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

# Genera openapi.yaml desde las anotaciones swag del codigo (mismo flujo que
# servers) y valida la sintaxis del resultado. --requiredByDefault: todos los
# campos de las respuestas se devuelven siempre salvo los marcados omitempty.
openapi:
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g cmd/api/main.go -o ./.openapi-tmp --parseInternal --requiredByDefault -q
	npx --yes swagger2openapi@7 ./.openapi-tmp/swagger.yaml -o openapi.yaml --patch
	npx --yes swagger-cli@4.0.4 validate openapi.yaml

migrate:
	migrate -path migrations -database "$$DATABASE_URL" up
