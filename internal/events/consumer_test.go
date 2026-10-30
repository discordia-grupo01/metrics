package events

import (
	"context"
	"testing"
)

func TestHandleRejectsMalformedEvents(t *testing.T) {
	tests := []struct {
		name       string
		routingKey string
		body       string
	}{
		{name: "invalid json", routingKey: "servers.member_joined", body: "{"},
		{name: "invalid event id", routingKey: "servers.member_joined", body: `{"event_id":"not-a-uuid","occurred_at":"2026-10-30T12:00:00Z"}`},
		{name: "missing occurred at", routingKey: "servers.member_joined", body: `{"event_id":"00000000-0000-0000-0000-000000000001"}`},
		{name: "unsupported routing key", routingKey: "unknown.event", body: `{"event_id":"00000000-0000-0000-0000-000000000001","occurred_at":"2026-10-30T12:00:00Z"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := handle(context.Background(), nil, test.routingKey, []byte(test.body))
			if err == nil || !isInvalid(err) {
				t.Fatalf("error = %v, want invalid event", err)
			}
		})
	}
}
