package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/discordia-grupo01/metrics/internal/repository"
)

func TestProjectionAuthorizerChecksStaffMembership(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	authorizer := NewProjectionAuthorizer(repository.New(db), []string{" staff-user ", ""})

	if !authorizer.IsStaff("staff-user") {
		t.Fatal("expected configured staff user to be authorized")
	}
	if authorizer.IsStaff("other-user") {
		t.Fatal("expected unconfigured user not to be staff")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionAuthorizerUsesLocalProjection(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	repo := repository.New(db)
	authorizer := NewProjectionAuthorizer(repo, nil)

	query := regexp.QuoteMeta(`SELECT EXISTS (
		SELECT 1
		FROM metric_servers s
		WHERE s.server_id = $1 AND s.deleted_at IS NULL
		AND (
			EXISTS (SELECT 1 FROM metric_owners o WHERE o.server_id = s.server_id AND o.owner_id = $2)
			OR EXISTS (
				SELECT 1
				FROM metric_member_roles mr
				JOIN LATERAL unnest(mr.role_ids) assigned(role_id) ON true
				JOIN metric_roles r ON r.server_id = mr.server_id AND r.role_id = assigned.role_id
				WHERE mr.server_id = s.server_id AND mr.user_id = $2
					AND r.is_admin AND NOT r.deleted
			)
		)
	)`)
	mock.ExpectQuery(query).WithArgs("server-1", "user-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	ok, err := authorizer.CanAdministerServer(context.Background(), "server-1", "user-1")
	if err != nil || !ok {
		t.Fatalf("authorization = %v, %v; want true, nil", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionAuthorizerReturnsRepositoryErrors(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	authorizer := NewProjectionAuthorizer(repository.New(db), nil)
	expected := errors.New("database unavailable")
	mock.ExpectQuery("SELECT EXISTS").WillReturnError(expected)

	_, err := authorizer.CanAdministerServer(context.Background(), "server-1", "user-1")
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
}

func TestServiceRejectsUnauthorizedServer(t *testing.T) {
	db, _ := newMockDB(t)
	defer db.Close()
	svc := New(repository.New(db), staticAuthorizer{allowed: false})

	_, err := svc.ServerSummary(context.Background(), "00000000-0000-0000-0000-000000000001", "user-1", testFrom(), testTo())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}

func TestServicePropagatesAuthorizationError(t *testing.T) {
	db, _ := newMockDB(t)
	defer db.Close()
	expected := errors.New("projection unavailable")
	svc := New(repository.New(db), staticAuthorizer{err: expected})

	_, err := svc.ServerGrowth(context.Background(), "00000000-0000-0000-0000-000000000001", "user-1", testFrom(), testTo())
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
}

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

type staticAuthorizer struct {
	allowed bool
	err     error
}

func (a staticAuthorizer) CanAdministerServer(context.Context, string, string) (bool, error) {
	return a.allowed, a.err
}

func (staticAuthorizer) IsStaff(string) bool { return false }

func testFrom() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
func testTo() time.Time   { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
