package session

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var migrationNamePattern = regexp.MustCompile(`^(\d+)_.+\.sql$`)

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("session: read migrations dir: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		m := migrationNamePattern.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("session: migration file %q doesn't match NNNN_name.sql", name)
		}

		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("session: migration file %q: %w", name, err)
		}

		data, err := migrationsFS.ReadFile(path.Join("migrations", name))
		if err != nil {
			return nil, fmt.Errorf("session: read migration %q: %w", name, err)
		}

		migrations = append(migrations, migration{version: version, name: name, sql: string(data)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// migrate applies every migration whose version is greater than the
// database's current schema version, in order, each in its own
// transaction. Schema version is tracked via PRAGMA user_version rather
// than a separate table -- simpler, and idiomatic for SQLite (AGENTS.md:
// "add a schema_version pragma/table from day one"). Forward-only:
// migrations already applied are never re-run, and an applied migration
// file must never be edited afterward.
func migrate(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	var current int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("session: read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session: begin migration %s: %w", m.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed; the error path already reports the real failure

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("session: apply migration %s: %w", m.name, err)
	}

	// PRAGMA user_version doesn't accept bound parameters; m.version comes
	// from our own embedded filenames, not external input.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return fmt.Errorf("session: set schema version after %s: %w", m.name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: commit migration %s: %w", m.name, err)
	}
	return nil
}
