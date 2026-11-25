package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"

	"github.com/discordia-grupo01/events-contract/broker"
	eventenvelope "github.com/discordia-grupo01/events-contract/envelope"
	"github.com/discordia-grupo01/events-contract/platform"
	"github.com/discordia-grupo01/events-contract/validate"

	"github.com/discordia-grupo01/metrics/internal/logging"
	"github.com/discordia-grupo01/metrics/internal/telemetry"
)

const (
	platformExchange   = broker.ExchangeName
	platformRoutingKey = platform.RoutingKeyStatusChanged
	healthRoutingKey   = platform.RoutingKeyHealthChanged
)

type Publisher interface {
	PublishStatusChanged(context.Context, string, time.Time) error
	PublishHealthChanged(context.Context, string, string, string, int, []string, time.Time) error
	Close()
}

type AMQPPublisher struct {
	url     string
	mu      sync.Mutex
	conn    *amqp091.Connection
	channel *amqp091.Channel
}

func (p *AMQPPublisher) PublishHealthChanged(ctx context.Context, service, previousStatus, status string, failures int, audience []string, occurredAt time.Time) error {
	if audience == nil {
		audience = []string{}
	}
	body, err := json.Marshal(platform.HealthChanged{
		Meta:                eventenvelope.Meta{EventID: uuid.NewString(), OccurredAt: occurredAt.UTC()},
		Service:             service,
		PreviousStatus:      previousStatus,
		Status:              status,
		ConsecutiveFailures: failures,
		AudienceUserIDs:     audience,
	})
	if err != nil {
		return fmt.Errorf("marshal health change event: %w", err)
	}
	return p.publish(ctx, healthRoutingKey, body)
}

func NewAMQPPublisher(url string) *AMQPPublisher {
	return &AMQPPublisher{url: url}
}

func (p *AMQPPublisher) PublishStatusChanged(ctx context.Context, status string, occurredAt time.Time) error {
	body, err := json.Marshal(platform.StatusChanged{
		Meta:   eventenvelope.Meta{EventID: uuid.NewString(), OccurredAt: occurredAt.UTC()},
		Status: status,
	})
	if err != nil {
		return fmt.Errorf("marshal platform status event: %w", err)
	}
	return p.publish(ctx, platformRoutingKey, body)
}

func (p *AMQPPublisher) publish(ctx context.Context, routingKey string, body []byte) error {
	if err := validate.Validate(routingKey, body); err != nil {
		logging.Warnf("metrics event %s does not meet the event contract: %v", routingKey, err)
	}
	channel, err := p.getChannel()
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	message := amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		Body:         body,
		Headers:      telemetry.InjectAMQP(ctx, nil),
	}
	if err := publishConfirmed(ctx, channel, platformExchange, routingKey, message); err != nil {
		p.reset()
		channel, err = p.getChannel()
		if err != nil {
			return fmt.Errorf("reconnect after publish failure: %w", err)
		}
		if err := publishConfirmed(ctx, channel, platformExchange, routingKey, message); err != nil {
			return fmt.Errorf("publish after reconnect: %w", err)
		}
	}
	return nil
}

func (p *AMQPPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.channel, p.conn = nil, nil
}

func (p *AMQPPublisher) getChannel() (*amqp091.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil && !p.channel.IsClosed() {
		return p.channel, nil
	}
	p.closeLocked()
	conn, err := amqp091.DialConfig(p.url, amqp091.Config{Dial: amqp091.DefaultDial(3 * time.Second)})
	if err != nil {
		return nil, err
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := channel.ExchangeDeclare(platformExchange, "topic", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	p.conn, p.channel = conn, channel
	return channel, nil
}

func (p *AMQPPublisher) closeLocked() {
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.channel, p.conn = nil, nil
}

func (p *AMQPPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeLocked()
}

// confirmTimeout acota cuánto se espera la confirmación del broker.
var confirmTimeout = 5 * time.Second

// publishConfirmed publica y espera el ack del broker: sin él, un nil solo
// significa que el mensaje salió por el socket, no que RabbitMQ lo guardó, y
// el outbox marcaría como publicado un evento que se pudo perder.
func publishConfirmed(ctx context.Context, ch *amqp091.Channel, exchange, routingKey string, msg amqp091.Publishing) error {
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, msg)
	if err != nil {
		return err
	}
	return waitConfirm(ctx, confirm)
}

// deferredConfirm es lo que usamos de *DeferredConfirmation; existe para poder
// probar la espera sin un RabbitMQ real.
type deferredConfirm interface {
	WaitContext(ctx context.Context) (bool, error)
}

func waitConfirm(ctx context.Context, confirm deferredConfirm) error {
	ctx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	acked, err := confirm.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("esperando la confirmación del broker: %w", err)
	}
	if !acked {
		return errors.New("el broker rechazó el mensaje")
	}
	return nil
}
