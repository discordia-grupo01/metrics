package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

const (
	platformExchange   = "discordia.events"
	platformRoutingKey = "platform.status_changed"
)

type Publisher interface {
	PublishStatusChanged(context.Context, string, time.Time) error
	Close()
}

type AMQPPublisher struct {
	url     string
	mu      sync.Mutex
	conn    *amqp091.Connection
	channel *amqp091.Channel
}

func NewAMQPPublisher(url string) *AMQPPublisher {
	return &AMQPPublisher{url: url}
}

func (p *AMQPPublisher) PublishStatusChanged(ctx context.Context, status string, occurredAt time.Time) error {
	body, err := json.Marshal(map[string]any{
		"event_id":    uuid.NewString(),
		"occurred_at": occurredAt.UTC(),
		"status":      status,
	})
	if err != nil {
		return fmt.Errorf("marshal platform status event: %w", err)
	}
	channel, err := p.getChannel()
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	message := amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		Body:         body,
	}
	if err := channel.PublishWithContext(ctx, platformExchange, platformRoutingKey, false, false, message); err != nil {
		p.reset()
		channel, err = p.getChannel()
		if err != nil {
			return fmt.Errorf("reconnect after publish failure: %w", err)
		}
		if err := channel.PublishWithContext(ctx, platformExchange, platformRoutingKey, false, false, message); err != nil {
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
