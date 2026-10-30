// Package backup copies the database to blob storage on a schedule, so losing the disk loses at most a day.
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ahpalmer/consigliere/internal/clock"
)

// Uploader stores one file. BlobContainer is the real one.
type Uploader interface {
	Put(ctx context.Context, name string, body io.Reader, size int64) error
}

// Job is the scheduled backup. Old copies are pruned by the storage account's lifecycle rule, not here.
type Job struct {
	DB     *sql.DB
	Blobs  Uploader
	Clock  clock.Clock
	Cron   string
	TmpDir string // where the snapshot is written before upload; defaults to os.TempDir()
}

func (j *Job) Name() string     { return "backup" }
func (j *Job) Schedule() string { return j.Cron }

func (j *Job) Run(ctx context.Context) error {
	dir := j.TmpDir
	if dir == "" {
		dir = os.TempDir()
	}
	name := "consigliere-" + j.Clock.Now().Format("2006-01-02T1504Z") + ".db"
	path := filepath.Join(dir, name)
	defer os.Remove(path)
	// VACUUM INTO writes a consistent, compacted copy while everything else keeps using the database.
	if _, err := j.DB.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := j.Blobs.Put(ctx, name, f, info.Size()); err != nil {
		return err
	}
	slog.Info("backed up database", "blob", name, "bytes", info.Size())
	return nil
}
