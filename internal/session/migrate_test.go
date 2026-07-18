package session

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrate_CreatesTables(t *testing.T) {
	db := openTestDB(t)

	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"sessions", "messages", "tool_calls"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not created: %v", table, err)
		}
	}

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("user_version = %d, want 1", version)
	}
}

func TestMigrate_IdempotentOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db1, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if migrateErr := migrate(context.Background(), db1); migrateErr != nil {
		t.Fatalf("migrate (first open): %v", migrateErr)
	}
	_ = db1.Close()

	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open (second): %v", err)
	}
	defer func() { _ = db2.Close() }()

	// A second migrate on the same file must not error or re-run 0001
	// (which would fail on CREATE TABLE for an already-existing table).
	if migrateErr := migrate(context.Background(), db2); migrateErr != nil {
		t.Fatalf("migrate (second open): %v", migrateErr)
	}

	var version int
	if err := db2.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("user_version after reopen = %d, want 1", version)
	}
}

func TestLoadMigrations_SortedByVersion(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("loadMigrations() returned none")
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version <= migrations[i-1].version {
			t.Errorf("migrations not strictly increasing: %v", migrations)
		}
	}
}
