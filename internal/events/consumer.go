package events

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

type envelope struct {
	EventID    string    `json:"event_id"`
	OccurredAt time.Time `json:"occurred_at"`
	ServerID   string    `json:"server_id"`
	UserID     string    `json:"user_id"`
	AuthorID   string    `json:"author_id"`
}

func Consume(ctx context.Context, url, exchange, queue string, repo *repository.Repository) error {
	connection, err := amqp091.Dial(url)
	if err != nil {
		return err
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err := channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		return err
	}
	for _, key := range []string{"servers.member_joined", "servers.member_left", "servers.server_updated", "messaging.message_created"} {
		if err := channel.QueueBind(queue, key, exchange, false, nil); err != nil {
			return err
		}
	}
	deliveries, err := channel.Consume(queue, "metrics", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}
			if err := handle(ctx, repo, delivery.RoutingKey, delivery.Body); err != nil {
				log.Printf("metrics event %s failed: %v", delivery.RoutingKey, err)
				invalid := isInvalid(err)
				_ = delivery.Nack(false, !invalid)
				continue
			}
			_ = delivery.Ack(false)
		}
	}
}

func handle(ctx context.Context, repo *repository.Repository, routingKey string, body []byte) error {
	var event envelope
	if err := json.Unmarshal(body, &event); err != nil {
		return &invalidEvent{err.Error()}
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return &invalidEvent{"event_id is not a UUID"}
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		return &invalidEvent{"occurred_at is required"}
	}
	serverID := parseUUID(event.ServerID)
	userValue := event.UserID
	if routingKey == "messaging.message_created" {
		userValue = event.AuthorID
	}
	userID := parseUUID(userValue)
	var eventType string
	switch routingKey {
	case "servers.member_joined":
		eventType = "member_joined"
	case "servers.member_left":
		eventType = "member_left"
	case "servers.server_updated":
		eventType = "server_updated"
	case "messaging.message_created":
		eventType = "message"
	default:
		return &invalidEvent{"unsupported routing key"}
	}
	return repo.RecordEvent(ctx, eventID, eventType, serverID, userID, occurredAt)
}

func parseUUID(value string) *uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &id
}

type invalidEvent struct{ message string }

var _ error = (*invalidEvent)(nil)

func (e *invalidEvent) Error() string { return e.message }

func isInvalid(err error) bool {
	var target *invalidEvent
	return errors.As(err, &target)
}
