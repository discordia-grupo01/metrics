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
	EventID     string    `json:"event_id"`
	OccurredAt  time.Time `json:"occurred_at"`
	ServerID    string    `json:"server_id"`
	UserID      string    `json:"user_id"`
	AuthorID    string    `json:"author_id"`
	RoleID      string    `json:"role_id"`
	RoleIDs     []string  `json:"role_ids"`
	Permissions []string  `json:"permissions"`
	IsEveryone  bool      `json:"is_everyone"`
	OwnerID     string    `json:"owner_id"`
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
	for _, key := range []string{
		"servers.member_joined", "servers.member_left", "servers.server_updated",
		"servers.server_deleted", "servers.role_updated", "servers.role_deleted",
		"servers.member_roles_updated", "servers.owner_updated",
		"messaging.message_created",
	} {
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
	projection := repository.Event{
		ID: eventID, ServerID: serverID, UserID: userID, OccurredAt: occurredAt,
		Permissions: event.Permissions, IsEveryone: event.IsEveryone,
	}
	switch routingKey {
	case "servers.member_joined":
		projection.Type = "member_joined"
	case "servers.member_left":
		projection.Type = "member_left"
	case "servers.server_updated":
		projection.Type = "server_updated"
	case "servers.server_deleted":
		projection.Type = "server_deleted"
	case "servers.role_updated":
		projection.Type = "role_updated"
		projection.RoleID, err = parseRequiredUUID(event.RoleID, "role_id")
		if err != nil {
			return err
		}
	case "servers.role_deleted":
		projection.Type = "role_deleted"
		projection.RoleID, err = parseRequiredUUID(event.RoleID, "role_id")
		if err != nil {
			return err
		}
	case "servers.member_roles_updated":
		projection.Type = "member_roles_updated"
		projection.RoleIDs, err = parseUUIDs(event.RoleIDs)
		if err != nil {
			return err
		}
		if projection.UserID == nil {
			return &invalidEvent{"user_id is required"}
		}
	case "servers.owner_updated":
		projection.Type = "owner_updated"
		projection.OwnerID, err = parseRequiredUUID(event.OwnerID, "owner_id")
		if err != nil {
			return err
		}
	case "messaging.message_created":
		projection.Type = "message"
	default:
		return &invalidEvent{"unsupported routing key"}
	}
	if serverID == nil && projection.Type != "message" {
		return &invalidEvent{"server_id is required"}
	}
	return repo.ApplyEvent(ctx, projection)
}

func parseUUID(value string) *uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &id
}

func parseRequiredUUID(value, field string) (*uuid.UUID, error) {
	id := parseUUID(value)
	if id == nil {
		return nil, &invalidEvent{field + " must be a UUID"}
	}
	return id, nil
}

func parseUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id := parseUUID(value)
		if id == nil {
			return nil, &invalidEvent{"role_ids must contain UUIDs"}
		}
		result = append(result, *id)
	}
	return result, nil
}

type invalidEvent struct{ message string }

var _ error = (*invalidEvent)(nil)

func (e *invalidEvent) Error() string { return e.message }

func isInvalid(err error) bool {
	var target *invalidEvent
	return errors.As(err, &target)
}
