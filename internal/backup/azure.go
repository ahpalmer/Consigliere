package backup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BlobContainer uploads to an Azure Storage container with a bearer token. No account keys: the storage account
// has them turned off.
type BlobContainer struct {
	URL    string // https://<account>.blob.core.windows.net/<container>
	Tokens TokenSource
	HTTP   *http.Client
}

// TokenSource hands out access tokens; azure.ManagedIdentity is the real one.
type TokenSource interface {
	Token(ctx context.Context, scope string) (string, error)
}

const storageScope = "https://storage.azure.com/.default"

func (b *BlobContainer) Put(ctx context.Context, name string, body io.Reader, size int64) error {
	token, err := b.Tokens.Token(ctx, storageScope)
	if err != nil {
		return err
	}
	// One Put Blob request: fine up to 5000 MiB, far beyond this database.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(b.URL, "/")+"/"+url.PathEscape(name), body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-ms-version", "2023-11-03")
	req.Header.Set("x-ms-blob-type", "BlockBlob")
	req.Header.Set("Content-Type", "application/vnd.sqlite3")
	resp, err := b.client().Do(req)
	if err != nil {
		return fmt.Errorf("upload %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("upload %s: %s: %s", name, resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

func (b *BlobContainer) client() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}
