package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayersFileSecretsAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "consigliere.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	os.WriteFile(cfgPath, []byte(`{"Radar": {"DigestMaxItems": 10}, "Telegram": {"AllowedChatIds": []}}`), 0o644)
	// Same flat shape as .NET user-secrets, including a BOM and an array index.
	os.WriteFile(secretsPath, []byte("\xef\xbb\xbf"+`{"Telegram:BotToken": "123:abc", "Telegram:AllowedChatIds:0": "8642072876", "agent:ownername": "Vito"}`), 0o600)

	cfg, err := Load(cfgPath, secretsPath, []string{"Radar__DigestMaxItems=7", "PATH=/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.BotToken != "123:abc" || cfg.Agent.OwnerName != "Vito" {
		t.Fatalf("secrets not applied: %+v", cfg)
	}
	if len(cfg.Telegram.AllowedChatIds) != 1 || cfg.Telegram.AllowedChatIds[0] != 8642072876 {
		t.Fatalf("chat ids: %v", cfg.Telegram.AllowedChatIds)
	}
	if cfg.Radar.DigestMaxItems != 7 {
		t.Fatalf("env should win, got %d", cfg.Radar.DigestMaxItems)
	}
	if cfg.Radar.ScanCron != "0 * * * *" {
		t.Fatalf("defaults lost: %q", cfg.Radar.ScanCron)
	}
}

func TestSetSecretRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "secrets.json")
	if err := SetSecret(path, "Mail:RefreshToken", "rt"); err != nil {
		t.Fatal(err)
	}
	flat, err := ReadSecrets(path)
	if err != nil || flat["Mail:RefreshToken"] != "rt" {
		t.Fatalf("%v %v", flat, err)
	}
}
