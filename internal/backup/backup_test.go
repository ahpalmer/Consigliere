package backup

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/azure"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

type memoryUploads map[string][]byte

func (m memoryUploads) Put(_ context.Context, name string, body io.Reader, size int64) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return io.ErrShortWrite
	}
	m[name] = b
	return nil
}

func TestJobUploadsAWorkingCopyOfTheDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE "Marker" ("Value" TEXT); INSERT INTO "Marker" VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}

	uploads := memoryUploads{}
	job := &Job{DB: db, Blobs: uploads, Clock: clock.NewFake(time.Date(2026, 10, 29, 3, 30, 0, 0, time.UTC)), TmpDir: dir}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}

	copied, ok := uploads["consigliere-2026-10-29T0330Z.db"]
	if !ok {
		t.Fatalf("uploads = %v", uploads)
	}
	restored := filepath.Join(dir, "restored.db")
	if err := os.WriteFile(restored, copied, 0o600); err != nil {
		t.Fatal(err)
	}
	copyDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(restored))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var value string
	if err := copyDB.QueryRowContext(ctx, `SELECT "Value" FROM "Marker"`).Scan(&value); err != nil || value != "kept" {
		t.Fatalf("restored copy: value=%q err=%v", value, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "consigliere-2026-10-29T0330Z.db")); !os.IsNotExist(err) {
		t.Fatalf("snapshot left behind: %v", err)
	}
}

func TestBlobContainerSignsInWithTheVMIdentity(t *testing.T) {
	var got *http.Request
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata/identity/oauth2/token":
			if r.URL.Query().Get("resource") != "https://storage.azure.com" || r.URL.Query().Get("client_id") != "client-1" {
				http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"access_token":"vm-token"}`))
		case "/backups/consigliere.db":
			got = r
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	blobs := &BlobContainer{URL: server.URL + "/backups/",
		Tokens: &azure.ManagedIdentity{ClientID: "client-1", Endpoint: server.URL + "/metadata/identity/oauth2/token"}}
	if err := blobs.Put(context.Background(), "consigliere.db", bytes.NewReader([]byte("db")), 2); err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Authorization") != "Bearer vm-token" || got.Header.Get("x-ms-blob-type") != "BlockBlob" || string(gotBody) != "db" {
		t.Fatalf("upload: headers=%v body=%q", got.Header, gotBody)
	}
}
