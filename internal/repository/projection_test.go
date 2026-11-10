package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestApplyEventPersistsServerProjectionAtomically(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	serverID := uuid.New()
	eventID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "server_updated", &serverID, (*uuid.UUID)(nil), occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO metric_servers").
		WithArgs(&serverID, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := New(db).ApplyEvent(context.Background(), Event{
		ID: eventID, Type: "server_updated", ServerID: &serverID, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEventDoesNotReapplyDuplicateEvent(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	eventID := uuid.New()
	serverID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "server_deleted", &serverID, (*uuid.UUID)(nil), occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := New(db).ApplyEvent(context.Background(), Event{
		ID: eventID, Type: "server_deleted", ServerID: &serverID, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEventPersistsStaffProjection(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	userID := uuid.New()
	eventID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "staff_member_added", (*uuid.UUID)(nil), &userID, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO staff_members").
		WithArgs(&userID, true, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := New(db).ApplyEvent(context.Background(), Event{
		ID: eventID, Type: "staff_member_added", UserID: &userID,
		Active: true, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIsStaffReturnsFalseWhenProjectionHasNoUser(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	mock.ExpectQuery("SELECT active FROM staff_members").
		WithArgs("staff-user").
		WillReturnError(sql.ErrNoRows)

	ok, err := New(db).IsStaff(context.Background(), "staff-user")
	if err != nil || ok {
		t.Fatalf("IsStaff = %v, %v; want false, nil", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEventGuardsRoleProjectionAgainstOlderEvents(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	serverID := uuid.New()
	roleID := uuid.New()
	eventID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "role_updated", &serverID, (*uuid.UUID)(nil), occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("WHERE metric_roles.occurred_at <= EXCLUDED.occurred_at")).
		WithArgs(&serverID, &roleID, true, false, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := New(db).ApplyEvent(context.Background(), Event{
		ID: eventID, Type: "role_updated", ServerID: &serverID, RoleID: &roleID,
		Permissions: []string{"ADMINISTRATOR"}, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newRepositoryMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}
