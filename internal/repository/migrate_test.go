package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/discordia-grupo01/metrics/migrations"
)

func TestLoadMigrationsSortsAndSkipsOtherFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"000002_b.up.sql":   {Data: []byte("B")},
		"000001_a.up.sql":   {Data: []byte("A")},
		"000001_a.down.sql": {Data: []byte("down")},
		"embed.go":          {Data: []byte("package migrations")},
	}
	got, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Version != 1 || got[0].SQL != "A" || got[1].Version != 2 {
		t.Fatalf("unexpected migrations: %+v", got)
	}
}

func TestLoadMigrationsRejectsDuplicateVersion(t *testing.T) {
	_, err := LoadMigrations(fstest.MapFS{
		"000001_a.up.sql": {Data: []byte("A")},
		"1_b.up.sql":      {Data: []byte("B")},
	})
	if err == nil || !strings.Contains(err.Error(), "misma versión") {
		t.Fatalf("expected duplicate version error, got %v", err)
	}
}

// Las migraciones embebidas son las que se despliegan: si falta alguna, la
// base de producción queda sin columnas que el código ya consulta.
func TestEmbeddedMigrationsIncludeIncidentAuthors(t *testing.T) {
	got, err := LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	for index, migration := range got {
		if migration.Version != int64(index+1) {
			t.Fatalf("migration versions must be consecutive from 1: %+v", got)
		}
	}
	last := got[len(got)-1]
	if last.Version < 5 || !strings.Contains(strings.Join(sqlOf(got), "\n"), "resolved_by") {
		t.Fatalf("embedded migrations miss incident authors: last=%s", last.Name)
	}
}

func sqlOf(list []Migration) []string {
	out := make([]string, 0, len(list))
	for _, migration := range list {
		out = append(out, migration.SQL)
	}
	return out
}

func expectMigrationPrelude(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock($1)`)).
		WithArgs(migrationLockKey).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestMigrateAppliesOnlyPendingMigrations(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()

	list := []Migration{
		{Version: 4, Name: "000004_old.up.sql", SQL: "ALTER TABLE old"},
		{Version: 5, Name: "000005_incident_authors.up.sql", SQL: "ALTER TABLE incidents ADD COLUMN created_by UUID"},
	}
	expectMigrationPrelude(mock)
	mock.ExpectQuery(`SELECT version, dirty FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "dirty"}).AddRow(4, false))
	mock.ExpectRollback()

	expectMigrationPrelude(mock)
	mock.ExpectQuery(`SELECT version, dirty FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "dirty"}).AddRow(4, false))
	mock.ExpectExec(regexp.QuoteMeta(list[1].SQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO schema_migrations`).WithArgs(int64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, err := Migrate(context.Background(), db, list)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != 5 {
		t.Fatalf("expected only version 5 applied, got %v", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateOnEmptyDatabaseStartsFromFirst(t *testing.T) {
	db, mock := newRepositoryMock(t)
	defer db.Close()

	expectMigrationPrelude(mock)
	mock.ExpectQuery(`SELECT version, dirty FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "dirty"}))
	mock.ExpectExec(`CREATE TABLE metric_events`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO schema_migrations`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, err := Migrate(context.Background(), db, []Migration{{Version: 1, Name: "000001_init.up.sql", SQL: "CREATE TABLE metric_events ()"}})
	if err != nil || len(applied) != 1 {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateStopsOnDirtyStateAndFailedScript(t *testing.T) {
	t.Run("dirty", func(t *testing.T) {
		db, mock := newRepositoryMock(t)
		defer db.Close()
		expectMigrationPrelude(mock)
		mock.ExpectQuery(`SELECT version, dirty FROM schema_migrations`).
			WillReturnRows(sqlmock.NewRows([]string{"version", "dirty"}).AddRow(3, true))
		mock.ExpectRollback()

		_, err := Migrate(context.Background(), db, []Migration{{Version: 4, Name: "000004_x.up.sql", SQL: "X"}})
		if err == nil || !strings.Contains(err.Error(), "sucia") {
			t.Fatalf("expected dirty error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("script error rolls back", func(t *testing.T) {
		db, mock := newRepositoryMock(t)
		defer db.Close()
		expectMigrationPrelude(mock)
		mock.ExpectQuery(`SELECT version, dirty FROM schema_migrations`).
			WillReturnRows(sqlmock.NewRows([]string{"version", "dirty"}).AddRow(4, false))
		mock.ExpectExec(`BROKEN`).WillReturnError(errors.New(`column "created_by" already exists`))
		mock.ExpectRollback()

		applied, err := Migrate(context.Background(), db, []Migration{{Version: 5, Name: "000005_x.up.sql", SQL: "BROKEN"}})
		if err == nil || !strings.Contains(err.Error(), "000005_x.up.sql") || len(applied) != 0 {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
