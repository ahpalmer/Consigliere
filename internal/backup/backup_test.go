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

func TestBlobContainerSignsInWithTheWorkloadIdentity(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("k8s-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got *http.Request
	var gotBody []byte
	azure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tenant-1/oauth2/v2.0/token":
			r.ParseForm()
			if r.Form.Get("client_assertion") != "k8s-token" || r.Form.Get("client_id") != "client-1" || r.Form.Get("scope") != storageScope {
				http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"access_token":"entra-token"}`))
		case "/backups/consigliere.db":
			got = r
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer azure.Close()

	blobs := &BlobContainer{URL: azure.URL + "/backups/",
		Tokens: &WorkloadIdentity{TenantID: "tenant-1", ClientID: "client-1", TokenFile: tokenFile, AuthorityHost: azure.URL + "/"}}
	if err := blobs.Put(context.Background(), "consigliere.db", bytes.NewReader([]byte("db")), 2); err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Authorization") != "Bearer entra-token" || got.Header.Get("x-ms-blob-type") != "BlockBlob" || string(gotBody) != "db" {
		t.Fatalf("upload: headers=%v body=%q", got.Header, gotBody)
	}
}

func TestWorkloadIdentityFromEnvNeedsTheWebhookVariables(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_FEDERATED_TOKEN_FILE", "")
	if _, err := WorkloadIdentityFromEnv(); err == nil {
		t.Fatal("want an error without the workload identity variables")
	}
}
