package repository

import (
	"context"
	"database/sql"
	"errors"
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

func TestApplyEventPersistsOwnerProjection(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	serverID := uuid.New()
	ownerID := uuid.New()
	eventID := uuid.New()
	occurredAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO metric_events").
		WithArgs(eventID, "owner_updated", &serverID, (*uuid.UUID)(nil), occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO metric_owners").
		WithArgs(&serverID, &ownerID, occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := New(db).ApplyEvent(context.Background(), Event{
		ID: eventID, Type: "owner_updated", ServerID: &serverID, OwnerID: &ownerID, OccurredAt: occurredAt,
	}); err != nil {
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

func TestProjectionHelpers(t *testing.T) {
	if !contains([]string{"VIEW_CHANNELS", "ADMINISTRATOR"}, "ADMINISTRATOR") {
		t.Fatal("contains did not find an existing value")
	}
	if contains([]string{"VIEW_CHANNELS"}, "ADMINISTRATOR") {
		t.Fatal("contains found a missing value")
	}

	ids := []uuid.UUID{uuid.New(), uuid.New()}
	values := uuidStrings(ids)
	if len(values) != len(ids) || values[0] != ids[0].String() || values[1] != ids[1].String() {
		t.Fatalf("uuidStrings = %#v, want UUID strings", values)
	}
}

func TestActiveStaffIDsReturnsRows(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	mock.ExpectQuery("SELECT user_id FROM staff_members").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("staff-1").AddRow("staff-2"))

	ids, err := New(db).ActiveStaffIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "staff-1" || ids[1] != "staff-2" {
		t.Fatalf("ids = %#v, want staff-1 and staff-2", ids)
	}
}

func TestSummaryAndGlobalSetActivityFlags(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	serverID := uuid.NewString()

	mock.ExpectQuery("COUNT\\(DISTINCT user_id\\) FILTER").
		WithArgs(serverID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"active_members", "messages_sent", "new_members"}).AddRow(2, 3, 1))
	summary, err := New(db).Summary(context.Background(), serverID, from, to)
	if err != nil || !summary.HasActivity || summary.ActiveMembers != 2 {
		t.Fatalf("summary = %+v, err = %v", summary, err)
	}

	mock.ExpectQuery("COUNT\\(DISTINCT user_id\\) FILTER").
		WithArgs(from, to).
		WillReturnRows(sqlmock.NewRows([]string{"active_users", "messages_sent", "servers_created"}).AddRow(4, 5, 1))
	global, err := New(db).Global(context.Background(), from, to)
	if err != nil || !global.HasActivity || global.ActiveUsers != 4 {
		t.Fatalf("global = %+v, err = %v", global, err)
	}
}

func TestResolveIncidentReportsMissingIncident(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	mock.ExpectExec("UPDATE incidents SET status").
		WithArgs("incident-id").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := New(db).ResolveIncident(context.Background(), "incident-id"); !errors.Is(err, ErrIncidentNotFound) {
		t.Fatalf("ResolveIncident error = %v, want ErrIncidentNotFound", err)
	}
}

func TestIncidentsSupportsAllStatuses(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT id,title,description,affected_services,platform_status,status,created_at,resolved_at").
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "description", "affected_services", "platform_status", "status", "created_at", "resolved_at"}).
			AddRow("id", "title", "description", "{api,db}", "maintenance", "resolved", now, now))

	incidents, err := New(db).Incidents(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(incidents) != 1 || incidents[0].Status != "resolved" || len(incidents[0].AffectedServices) != 2 {
		t.Fatalf("incidents = %#v", incidents)
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
