package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/discordia-grupo01/events-contract/validate"
	"github.com/discordia-grupo01/metrics/internal/events/deadletter"
	"github.com/discordia-grupo01/metrics/internal/logging"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/discordia-grupo01/metrics/internal/telemetry"
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
	Signature   string    `json:"signature"`
}

func Consume(ctx context.Context, url, exchange, queue, staffEventSecret string, repo *repository.Repository) error {
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
		"admin.staff_member_added", "admin.staff_member_removed",
	} {
		if err := channel.QueueBind(queue, key, exchange, false, nil); err != nil {
			return err
		}
	}
	retrier, err := deadletter.New(connection, queue, deadletter.DefaultPolicy())
	if err != nil {
		return err
	}
	defer retrier.Close()

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
			spanCtx, end := telemetry.StartConsumer(ctx, queue, delivery)
			if err := handle(spanCtx, repo, delivery.RoutingKey, delivery.Body, staffEventSecret); err != nil {
				logging.Errorf("metrics event %s failed: %v", delivery.RoutingKey, err)
				if isInvalid(err) {
					_ = delivery.Nack(false, false)
					end(true)
					continue
				}
				retrier.Settle(spanCtx, delivery, true)
				end(true)
				continue
			}
			retrier.Settle(spanCtx, delivery, false)
			end(false)
		}
	}
}

func handle(ctx context.Context, repo *repository.Repository, routingKey string, body []byte, secrets ...string) error {
	staffEventSecret := ""
	if len(secrets) > 0 {
		staffEventSecret = secrets[0]
	}
	var event envelope
	if err := json.Unmarshal(body, &event); err != nil {
		return &invalidEvent{err.Error()}
	}
	if err := validate.Validate(routingKey, body); err != nil && !errors.Is(err, validate.ErrUnknownEvent) {
		logging.Warnf("metrics event %s does not meet the event contract: %v", routingKey, err)
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
	case "admin.staff_member_added", "admin.staff_member_removed":
		if userID == nil {
			return &invalidEvent{"user_id must be a UUID"}
		}
		if !verifyStaffEvent(staffEventSecret, routingKey, event.EventID, event.UserID, occurredAt, event.Signature) {
			return &invalidEvent{"invalid staff event signature"}
		}
		projection.Type = strings.TrimPrefix(routingKey, "admin.")
		projection.Active = routingKey == "admin.staff_member_added"
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
	if serverID == nil && projection.Type != "message" &&
		projection.Type != "staff_member_added" && projection.Type != "staff_member_removed" {
		return &invalidEvent{"server_id is required"}
	}
	return repo.ApplyEvent(ctx, projection)
}

func verifyStaffEvent(secret, routingKey, eventID, userID string, occurredAt time.Time, got string) bool {
	if secret == "" || got == "" {
		return false
	}
	raw, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{
		routingKey, eventID, userID, occurredAt.UTC().Format(time.RFC3339Nano),
	}, "\n")))
	return hmac.Equal(raw, mac.Sum(nil))
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
