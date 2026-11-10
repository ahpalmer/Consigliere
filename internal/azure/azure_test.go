package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeyVaultSecretSignsInAsTheManagedIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata/identity/oauth2/token":
			q := r.URL.Query()
			if r.Header.Get("Metadata") != "true" || q.Get("resource") != "https://vault.azure.net" || q.Get("client_id") != "client-1" {
				http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"access_token":"vm-token"}`))
		case "/secrets/consigliere-secrets":
			if r.Header.Get("Authorization") != "Bearer vm-token" {
				http.Error(w, `{"error":{"code":"Unauthorized"}}`, http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"value":"{\"Web:Token\":\"t\"}","id":"https://kv/secrets/consigliere-secrets/1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	identity := &ManagedIdentity{ClientID: "client-1", Endpoint: server.URL + "/metadata/identity/oauth2/token"}
	vault := &KeyVault{Name: "kv-test", Tokens: identity, BaseURL: server.URL}
	got, err := vault.Secret(context.Background(), "consigliere-secrets")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"Web:Token":"t"}` {
		t.Fatalf("secret = %q", got)
	}
	if _, err := vault.Secret(context.Background(), "missing"); err == nil {
		t.Fatal("want an error for a missing secret")
	}
}

func TestParseSecretRef(t *testing.T) {
	for in, want := range map[string][2]string{
		"keyvault://kv-consigliere-abc/consigliere-secrets": {"kv-consigliere-abc", "consigliere-secrets"},
		"keyvault://kv/":    {},
		"keyvault:///name":  {},
		"keyvault://kv/a/b": {},
		"C:/secrets.json":   {},
	} {
		vault, secret, ok := ParseSecretRef(in)
		if ok != (want[0] != "") || vault != want[0] || secret != want[1] {
			t.Errorf("ParseSecretRef(%q) = %q, %q, %v", in, vault, secret, ok)
		}
	}
}
