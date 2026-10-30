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

func TestHandleRejectsInvalidAuthorizationEvent(t *testing.T) {
	err := handle(context.Background(), nil, "servers.role_updated", []byte(`{
		"event_id":"00000000-0000-0000-0000-000000000001",
		"occurred_at":"2026-10-30T12:00:00Z",
		"server_id":"00000000-0000-0000-0000-000000000002",
		"role_id":"not-a-uuid"
	}`))
	if err == nil || !isInvalid(err) {
		t.Fatalf("error = %v, want invalid event", err)
	}
}

func TestHandleRejectsIncompleteProjectionEvents(t *testing.T) {
	tests := []struct {
		name       string
		routingKey string
		body       string
	}{
		{
			name:       "missing server",
			routingKey: "servers.owner_updated",
			body:       `{"event_id":"00000000-0000-0000-0000-000000000001","occurred_at":"2026-10-30T12:00:00Z","owner_id":"00000000-0000-0000-0000-000000000003"}`,
		},
		{
			name:       "invalid owner",
			routingKey: "servers.owner_updated",
			body:       `{"event_id":"00000000-0000-0000-0000-000000000001","occurred_at":"2026-10-30T12:00:00Z","server_id":"00000000-0000-0000-0000-000000000002","owner_id":"invalid"}`,
		},
		{
			name:       "invalid assigned role",
			routingKey: "servers.member_roles_updated",
			body:       `{"event_id":"00000000-0000-0000-0000-000000000001","occurred_at":"2026-10-30T12:00:00Z","server_id":"00000000-0000-0000-0000-000000000002","user_id":"00000000-0000-0000-0000-000000000003","role_ids":["invalid"]}`,
		},
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

func TestParseUUIDsAcceptsEmptyRoleAssignment(t *testing.T) {
	ids, err := parseUUIDs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids == nil || len(ids) != 0 {
		t.Fatalf("ids = %#v, want empty non-nil slice", ids)
	}
}
