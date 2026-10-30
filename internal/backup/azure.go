package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

// WorkloadIdentity signs in as the pod's Azure identity: AKS projects a Kubernetes service account token into the
// pod, and Entra ID exchanges it for an access token (a federated credential on the identity trusts that account).
type WorkloadIdentity struct {
	TenantID, ClientID string
	TokenFile          string // the projected service account token; re-read each time because it rotates
	AuthorityHost      string
	HTTP               *http.Client
}

// WorkloadIdentityFromEnv reads the variables the AKS workload identity webhook injects into labeled pods.
func WorkloadIdentityFromEnv() (*WorkloadIdentity, error) {
	w := &WorkloadIdentity{
		TenantID:      os.Getenv("AZURE_TENANT_ID"),
		ClientID:      os.Getenv("AZURE_CLIENT_ID"),
		TokenFile:     os.Getenv("AZURE_FEDERATED_TOKEN_FILE"),
		AuthorityHost: os.Getenv("AZURE_AUTHORITY_HOST"),
	}
	if w.AuthorityHost == "" {
		w.AuthorityHost = "https://login.microsoftonline.com/"
	}
	if w.TenantID == "" || w.ClientID == "" || w.TokenFile == "" {
		return nil, errors.New("workload identity is not set up: AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_FEDERATED_TOKEN_FILE are required")
	}
	return w, nil
}

func (w *WorkloadIdentity) Token(ctx context.Context, scope string) (string, error) {
	assertion, err := os.ReadFile(w.TokenFile)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"client_id":             {w.ClientID},
		"scope":                 {scope},
		"grant_type":            {"client_credentials"},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
		"client_assertion":      {strings.TrimSpace(string(assertion))},
	}
	endpoint := strings.TrimRight(w.AuthorityHost, "/") + "/" + url.PathEscape(w.TenantID) + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := w.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sign in to Azure: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("sign in to Azure: %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("sign in to Azure: %s: %s %s", resp.Status, out.Error, out.Description)
	}
	return out.AccessToken, nil
}
