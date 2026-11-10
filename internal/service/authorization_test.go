package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/discordia-grupo01/metrics/internal/repository"
)

func TestProjectionAuthorizerChecksStaffMembership(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	authorizer := NewProjectionAuthorizer(repository.New(db), []string{" staff-user ", ""})

	mock.ExpectQuery("SELECT active FROM staff_members").
		WithArgs("staff-user").
		WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(true))
	if !authorizer.IsStaff(context.Background(), "staff-user") {
		t.Fatal("expected configured staff user to be authorized")
	}

	mock.ExpectQuery("SELECT active FROM staff_members").
		WithArgs("other-user").
		WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(false))
	if authorizer.IsStaff(context.Background(), "other-user") {
		t.Fatal("expected unconfigured user not to be staff")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionAuthorizerUsesStaffProjectionBeforeFallback(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	authorizer := NewProjectionAuthorizer(repository.New(db), []string{"staff-user"})

	mock.ExpectQuery("SELECT active FROM staff_members").
		WithArgs("staff-user").
		WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(false))

	if !authorizer.IsStaff(context.Background(), "staff-user") {
		t.Fatal("expected configured fallback staff user to be authorized")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionAuthorizerAuthorizesProjectedStaff(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	authorizer := NewProjectionAuthorizer(repository.New(db), nil)

	mock.ExpectQuery("SELECT active FROM staff_members").
		WithArgs("staff-user").
		WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(true))

	if !authorizer.IsStaff(context.Background(), "staff-user") {
		t.Fatal("expected projected staff user to be authorized")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRejectsNonStaffServerSummary(t *testing.T) {
	db, _ := newMockDB(t)
	defer db.Close()
	svc := New(repository.New(db), staticAuthorizer{staff: false})

	_, err := svc.ServerSummary(context.Background(), "00000000-0000-0000-0000-000000000001", "user-1", testFrom(), testTo())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}

func TestServiceRejectsNonStaffServerGrowth(t *testing.T) {
	db, _ := newMockDB(t)
	defer db.Close()
	svc := New(repository.New(db), staticAuthorizer{staff: false})

	_, err := svc.ServerGrowth(context.Background(), "00000000-0000-0000-0000-000000000001", "user-1", testFrom(), testTo())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}

func TestServiceLetsStaffReadServerGrowth(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()
	svc := New(repository.New(db), staticAuthorizer{staff: true})
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"date", "members"}))

	if _, err := svc.ServerGrowth(context.Background(), "00000000-0000-0000-0000-000000000001", "staff-user", testFrom(), testTo()); errors.Is(err, ErrForbidden) {
		t.Fatal("expected staff user to be authorized")
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
	staff bool
}

func (a staticAuthorizer) IsStaff(context.Context, string) bool { return a.staff }

func testFrom() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
func testTo() time.Time   { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
