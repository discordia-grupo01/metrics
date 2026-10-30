FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api

FROM alpine:3.19
# tailscale + socat: permiten llegar al RabbitMQ privado desde Render
RUN apk add --no-cache tailscale socat ca-certificates \
    && adduser -D appuser
COPY --from=builder /bin/api /bin/api
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]