// Package store owns the SQLite database: opening it, migrating it, and the shared encodings for its columns.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so the binary cross-compiles for the container
)

//go:embed migrations/*.sql
var migrations embed.FS

// efFinalMigration is the last EF Core migration whose schema 001_initial.sql reproduces.
const efFinalMigration = "20261027023428_MailIngestion"

// Open opens (creating if needed) the database at path and applies pending migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// WAL + busy timeout: the scheduler, chat commands, and background scoring all touch the DB concurrently.
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies embedded migrations in order. A database created by the .NET version (EF Core) is adopted
// as-is when its EF history shows the same final schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS "SchemaMigrations" ("Id" TEXT NOT NULL PRIMARY KEY, "AppliedAt" TEXT NOT NULL)`); err != nil {
		return err
	}
	applied := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT "Id" FROM "SchemaMigrations"`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		applied[id] = true
	}
	rows.Close()

	if !applied["001_initial"] {
		adopted, err := adoptEFDatabase(ctx, db)
		if err != nil {
			return err
		}
		if adopted {
			if err := markApplied(ctx, db, "001_initial"); err != nil {
				return err
			}
			applied["001_initial"] = true
		}
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		id := strings.TrimSuffix(name, ".sql")
		if applied[id] {
			continue
		}
		script, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "SchemaMigrations" ("Id", "AppliedAt") VALUES (?, ?)`, id, FormatTime(time.Now())); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// adoptEFDatabase reports whether db already has the full schema from the .NET version.
func adoptEFDatabase(ctx context.Context, db *sql.DB) (bool, error) {
	var hasHistory int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = '__EFMigrationsHistory'`).Scan(&hasHistory); err != nil {
		return false, err
	}
	if hasHistory == 0 {
		return false, nil
	}
	var final int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "__EFMigrationsHistory" WHERE "MigrationId" = ?`, efFinalMigration).Scan(&final); err != nil {
		return false, err
	}
	if final == 0 {
		return false, fmt.Errorf("database was created by an older .NET build; run the .NET version once to finish its migrations (need %s)", efFinalMigration)
	}
	return true, nil
}

func markApplied(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO "SchemaMigrations" ("Id", "AppliedAt") VALUES (?, ?)`, id, FormatTime(time.Now()))
	return err
}

// timeLayout matches EF Core's SQLite DateTime text ("yyyy-MM-dd HH:mm:ss.FFFFFFF"): sortable and readable.
const timeLayout = "2006-01-02 15:04:05.9999999"

// FormatTime encodes t as UTC text.
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// FormatTimePtr encodes an optional time; nil stays NULL.
func FormatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return FormatTime(*t)
}

// ParseTime decodes UTC text written by either implementation (any number of fractional digits).
func ParseTime(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad timestamp %q: %w", s, err)
	}
	return t, nil
}

// ParseTimePtr decodes a nullable timestamp column.
func ParseTimePtr(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := ParseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Placeholders returns "?, ?, ?" for n values, for IN clauses.
func Placeholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// Args converts a typed slice for use as variadic query arguments.
func Args[T any](values []T) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
