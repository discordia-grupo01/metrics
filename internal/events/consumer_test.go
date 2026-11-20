package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/discordia-grupo01/metrics/internal/repository"
	"github.com/google/uuid"
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

func TestHandleRejectsStaffEventWithInvalidSignature(t *testing.T) {
	body := `{"event_id":"00000000-0000-0000-0000-000000000001","occurred_at":"2026-10-30T12:00:00Z","user_id":"00000000-0000-0000-0000-000000000003","signature":"00"}`

	err := handle(context.Background(), nil, "admin.staff_member_added", []byte(body), "secret")
	if err == nil || !isInvalid(err) || err.Error() != "invalid staff event signature" {
		t.Fatalf("error = %v, want invalid signature event", err)
	}
}

func TestVerifyStaffEventAcceptsSignatureWithoutServerID(t *testing.T) {
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 123456000, time.UTC)
	eventID := "00000000-0000-0000-0000-000000000001"
	userID := "00000000-0000-0000-0000-000000000003"
	secret := "secret"
	signature := staffSignature(secret, "admin.staff_member_added", eventID, userID, occurredAt)
	if !verifyStaffEvent(secret, "admin.staff_member_added", eventID, userID, occurredAt, signature) {
		t.Fatal("expected valid staff event signature")
	}
}

func TestVerifyStaffEventRejectsInvalidInputs(t *testing.T) {
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		secret    string
		signature string
	}{
		{name: "missing secret", secret: ""},
		{name: "missing signature", secret: "secret"},
		{name: "invalid hex", secret: "secret", signature: "not-hex"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if verifyStaffEvent(test.secret, "admin.staff_member_added", uuid.NewString(), uuid.NewString(), occurredAt, test.signature) {
				t.Fatal("verifyStaffEvent accepted invalid input")
			}
		})
	}
}

func TestParseUUIDHelpers(t *testing.T) {
	id := uuid.New()
	if parsed := parseUUID(id.String()); parsed == nil || *parsed != id {
		t.Fatalf("parseUUID = %v, want %s", parsed, id)
	}
	if parseUUID("invalid") != nil {
		t.Fatal("parseUUID accepted invalid UUID")
	}
	if _, err := parseRequiredUUID("invalid", "role_id"); err == nil || !isInvalid(err) {
		t.Fatalf("parseRequiredUUID error = %v, want invalid event", err)
	}
	if _, err := parseUUIDs([]string{id.String(), "invalid"}); err == nil || !isInvalid(err) {
		t.Fatalf("parseUUIDs error = %v, want invalid event", err)
	}
}

func TestHandlePersistsMessageProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repository.New(db)
	eventID := uuid.New()
	serverID := uuid.New()
	authorID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "message", &serverID, &authorID, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	body := `{"event_id":"` + eventID.String() + `","occurred_at":"` + occurredAt.Format(time.RFC3339) +
		`","server_id":"` + serverID.String() + `","author_id":"` + authorID.String() + `"}`
	if err := handle(context.Background(), repo, "messaging.message_created", []byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHandlePersistsStaffProjectionWithValidSignature(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repository.New(db)
	eventID := uuid.New()
	userID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	secret := "secret"
	routingKey := "admin.staff_member_added"
	signature := staffSignature(secret, routingKey, eventID.String(), userID.String(), occurredAt)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "staff_member_added", (*uuid.UUID)(nil), &userID, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO staff_members").
		WithArgs(&userID, true, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	body := `{"event_id":"` + eventID.String() + `","occurred_at":"` + occurredAt.Format(time.RFC3339) +
		`","user_id":"` + userID.String() + `","signature":"` + signature + `"}`
	if err := handle(context.Background(), repo, routingKey, []byte(body), secret); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func staffSignature(secret, routingKey, eventID, userID string, occurredAt time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{
		routingKey, eventID, userID, occurredAt.UTC().Format(time.RFC3339Nano),
	}, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}
