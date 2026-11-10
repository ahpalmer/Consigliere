// Package azure signs in as the VM's managed identity and reads Key Vault secrets, with plain HTTP calls (no SDK).
package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TokenSource hands out access tokens for an OAuth scope, e.g. "https://storage.azure.com/.default".
type TokenSource interface {
	Token(ctx context.Context, scope string) (string, error)
}

// ManagedIdentity signs in through the VM's instance metadata service: no secret is involved, the token is only
// handed to processes on that VM.
type ManagedIdentity struct {
	ClientID string // the user-assigned identity; empty takes the VM's only one
	Endpoint string // defaults to the metadata service
	HTTP     *http.Client
}

const imdsEndpoint = "http://169.254.169.254/metadata/identity/oauth2/token"

// ManagedIdentityFromEnv takes the identity's client ID from AZURE_CLIENT_ID (set by the systemd unit).
func ManagedIdentityFromEnv() *ManagedIdentity {
	return &ManagedIdentity{ClientID: os.Getenv("AZURE_CLIENT_ID")}
}

func (m *ManagedIdentity) Token(ctx context.Context, scope string) (string, error) {
	// The metadata service takes a resource rather than a scope.
	q := url.Values{"api-version": {"2018-02-01"}, "resource": {strings.TrimSuffix(scope, "/.default")}}
	if m.ClientID != "" {
		q.Set("client_id", m.ClientID)
	}
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = imdsEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata", "true")
	client := m.HTTP
	if client == nil {
		// Never through a proxy: the metadata service is only on the VM's own link.
		client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sign in as the VM's identity: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("sign in as the VM's identity: %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("sign in as the VM's identity: %s: %s %s", resp.Status, out.Error, out.Description)
	}
	return out.AccessToken, nil
}

// KeyVault reads secrets from one vault.
type KeyVault struct {
	Name    string
	Tokens  TokenSource
	BaseURL string // defaults to https://<Name>.vault.azure.net
	HTTP    *http.Client
}

const vaultScope = "https://vault.azure.net/.default"

// Secret returns the current version of a secret.
func (v *KeyVault) Secret(ctx context.Context, name string) (string, error) {
	token, err := v.Tokens.Token(ctx, vaultScope)
	if err != nil {
		return "", err
	}
	base := v.BaseURL
	if base == "" {
		base = "https://" + v.Name + ".vault.azure.net"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/secrets/"+url.PathEscape(name)+"?api-version=7.4", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := v.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read %s from Key Vault %s: %w", name, v.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The error body names the problem (not found, forbidden) and never holds the value.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("read %s from Key Vault %s: %s: %s", name, v.Name, resp.Status, strings.TrimSpace(string(detail)))
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("read %s from Key Vault %s: %w", name, v.Name, err)
	}
	return out.Value, nil
}

// ParseSecretRef splits "keyvault://<vault>/<secret>", the form --secrets takes on the VM.
func ParseSecretRef(s string) (vault, secret string, ok bool) {
	rest, found := strings.CutPrefix(s, "keyvault://")
	if !found {
		return "", "", false
	}
	vault, secret, found = strings.Cut(rest, "/")
	if !found || vault == "" || secret == "" || strings.Contains(secret, "/") {
		return "", "", false
	}
	return vault, secret, true
}
