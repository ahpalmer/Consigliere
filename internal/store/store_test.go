package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestFreshDatabaseGetsSchemaAndMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db", "test.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM "RadarItems"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptsDatabaseCreatedByEFCore(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "ef.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	script, _ := migrations.ReadFile("migrations/001_initial.sql")
	mustExec(t, db, string(script))
	mustExec(t, db, `CREATE TABLE "__EFMigrationsHistory" ("MigrationId" TEXT NOT NULL PRIMARY KEY, "ProductVersion" TEXT NOT NULL)`)
	mustExec(t, db, `INSERT INTO "__EFMigrationsHistory" VALUES ('20261027023428_MailIngestion', '10.0.14')`)
	mustExec(t, db, `INSERT INTO "SourceRuns" ("Board","Fetched","FinishedAt","New","Passed","Source","StartedAt") VALUES ('b',1,'2026-10-26 21:55:29.7107',0,0,'greenhouse','2026-10-26 21:55:29.5')`)

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var started string
	if err := db.QueryRow(`SELECT "StartedAt" FROM "SourceRuns"`).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTime(started); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesHalfMigratedEFDatabase(t *testing.T) {
	db, _ := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "old.db")))
	defer db.Close()
	mustExec(t, db, `CREATE TABLE "__EFMigrationsHistory" ("MigrationId" TEXT NOT NULL PRIMARY KEY, "ProductVersion" TEXT NOT NULL)`)
	mustExec(t, db, `INSERT INTO "__EFMigrationsHistory" VALUES ('20261026183511_Initial', '10.0.14')`)
	if err := Migrate(context.Background(), db); err == nil {
		t.Fatal("expected an error for an incompletely migrated EF database")
	}
}

func TestTimesRoundTripInEFFormat(t *testing.T) {
	in := time.Date(2026, 10, 26, 21, 55, 29, 710700000, time.FixedZone("EDT", -4*3600))
	s := FormatTime(in)
	if s != "2026-10-27 01:55:29.7107" {
		t.Fatalf("got %q", s)
	}
	out, err := ParseTime(s)
	if err != nil || !out.Equal(in) {
		t.Fatalf("%v %v", out, err)
	}
	// The .NET migration wrote strftime("%f") values with millisecond padding.
	if _, err := ParseTime("2026-08-12 19:02:22.000"); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}
