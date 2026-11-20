package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// Migration es un script "up" numerado de la carpeta migrations/.
type Migration struct {
	Version int64
	Name    string
	SQL     string
}

var migrationFileName = regexp.MustCompile(`^(\d+)_(.+)\.up\.sql$`)

// migrationLockKey identifica el advisory lock de las migraciones de este
// servicio; evita que dos instancias apliquen el mismo script a la vez.
const migrationLockKey int64 = 0x6d657472696373 // "metrics"

// LoadMigrations lee los NNNNNN_nombre.up.sql de fsys ordenados por versión.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var migrations []Migration
	seen := map[int64]string{}
	for _, entry := range entries {
		match := migrationFileName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			continue
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migración %s: %w", entry.Name(), err)
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("migraciones %s y %s tienen la misma versión", previous, entry.Name())
		}
		seen[version] = entry.Name()
		content, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, Migration{Version: version, Name: entry.Name(), SQL: string(content)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

// Migrate aplica las migraciones pendientes y devuelve las versiones aplicadas.
//
// Usa la misma tabla schema_migrations (version, dirty) que golang-migrate, así
// que convive con `make migrate` y con el contenedor migrate de docker compose.
// Cada script corre en su propia transacción con un advisory lock de
// transacción: funciona detrás de un pooler en modo transacción, donde un lock
// de sesión podría quedar tomado en otro backend.
func Migrate(ctx context.Context, db *sql.DB, migrations []Migration) ([]int64, error) {
	var applied []int64
	for _, migration := range migrations {
		ok, err := applyMigration(ctx, db, migration)
		if err != nil {
			return applied, fmt.Errorf("migración %s: %w", migration.Name, err)
		}
		if ok {
			applied = append(applied, migration.Version)
		}
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration Migration) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL)`); err != nil {
		return false, err
	}
	var current int64
	var dirty bool
	err = tx.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&current, &dirty)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		current = 0
	case err != nil:
		return false, err
	case dirty:
		return false, fmt.Errorf("schema_migrations quedó sucia en la versión %d: revisá la base y corregí la fila a mano", current)
	}
	if current >= migration.Version {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations`); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`, migration.Version); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
