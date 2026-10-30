// Package config loads settings in layers, later layers winning:
//
//  1. config/consigliere.json (committed, no secrets)
//  2. a secrets file outside the repo (flat "Section:Key" entries, the same format as .NET user-secrets)
//  3. environment variables, "Section__Key" (e.g. Telegram__BotToken), as Kubernetes/Key Vault inject them
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Config struct {
	LogLevel  string
	DataPack  DataPack
	Anthropic Anthropic
	Agent     Agent
	Llm       Llm
	Telegram  Telegram
	Radar     Radar
	Pipeline  Pipeline
	Scheduler Scheduler
	Mail      Mail
	Web       Web
	Backup    Backup
}

type DataPack struct {
	// Root of the private data pack. Relative paths resolve against the working directory.
	Path string
}

type Anthropic struct{ ApiKey string }

type Agent struct {
	// Substituted for {{owner}} in prompts. Keep it in secrets, not the committed config.
	OwnerName string
}

type Llm struct {
	// Model per task (chat, draft, score, extract), so each route can be tuned independently.
	Models          map[string]string
	MaxHistoryTurns int
}

type Telegram struct {
	BotToken string
	// Chat IDs allowed to talk to the agent. Everyone else is ignored (and logged, so you can find your own ID).
	AllowedChatIds []int64
	// Where proactive notifications go. Defaults to the first allowed chat.
	OwnerChatId int64
}

type Radar struct {
	ScanCron, ScoreCron, DigestCron  string
	DigestMaxItems                   int
	DigestMinScore, AlertMinScore    int
	ScoreBatchSize, ScoreConcurrency int
	// Location of the SQLite database inside the data pack.
	DatabasePath string
}

type Pipeline struct {
	// Warn when activating beyond this many active pursuits (a soft cap: it never blocks).
	ActiveSoftCap int
	// Days after applying before follow-up reminders (sent twice, this many days apart).
	FollowUpDays int
	// Days of silence after applying before proposing "ghosted".
	GhostDays int
}

type Scheduler struct {
	TimeZone string
	// Optional healthchecks.io ping base, e.g. "https://hc-ping.com/<ping-key>". Keep it in secrets.
	HealthchecksPingUrl string
}

// Web is the dashboard.
type Web struct {
	// Address to listen on; empty turns the dashboard off. Anything but loopback requires Token.
	Listen string
	// Sign-in secret for the dashboard. Keep it in secrets.
	Token string
}

// Backup copies the database to blob storage. Off unless ContainerUrl is set (in AKS, where the pod's workload
// identity can write to the container).
type Backup struct {
	Cron         string
	ContainerUrl string // e.g. https://<account>.blob.core.windows.net/backups
}

type Mail struct {
	ClientId, ClientSecret string
	// Written by `consigliere auth gmail`.
	RefreshToken         string
	Query                string
	MaxMessagesPerScan   int
	AllowedSenderDomains []string
}

func (m Mail) MissingSettings() []string {
	var missing []string
	for name, v := range map[string]string{"Mail:ClientId": m.ClientId, "Mail:ClientSecret": m.ClientSecret, "Mail:RefreshToken": m.RefreshToken} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// ModelFor returns the configured model for a task.
func (l Llm) ModelFor(task string) (string, error) {
	if m, ok := l.Models[task]; ok && m != "" {
		return m, nil
	}
	return "", fmt.Errorf("no model configured for task %q", task)
}

// OwnerChat is where proactive notifications go.
func (t Telegram) OwnerChat() int64 {
	if t.OwnerChatId != 0 {
		return t.OwnerChatId
	}
	if len(t.AllowedChatIds) > 0 {
		return t.AllowedChatIds[0]
	}
	return 0
}

func defaults() map[string]any {
	return map[string]any{
		"LogLevel": "info",
		"DataPack": map[string]any{"Path": "samples/data-pack"},
		"Agent":    map[string]any{"OwnerName": "the owner"},
		"Llm": map[string]any{
			"Models":          map[string]any{"chat": "claude-opus-5-5", "draft": "claude-opus-5-5", "score": "claude-haiku-5-5", "extract": "claude-haiku-5-5"},
			"MaxHistoryTurns": 20,
		},
		"Radar": map[string]any{
			"ScanCron": "0 * * * *", "ScoreCron": "*/15 * * * *", "DigestCron": "0 8 * * *",
			"DigestMaxItems": 25, "DigestMinScore": 50, "AlertMinScore": 75, "ScoreBatchSize": 200, "ScoreConcurrency": 8,
			"DatabasePath": "db/consigliere.db",
		},
		"Pipeline":  map[string]any{"ActiveSoftCap": 10, "FollowUpDays": 7, "GhostDays": 30},
		"Scheduler": map[string]any{"TimeZone": "America/New_York"},
		"Web":       map[string]any{"Listen": "127.0.0.1:8080"},
		"Backup":    map[string]any{"Cron": "30 3 * * *"},
		"Mail": map[string]any{
			"Query": "newer_than:14d", "MaxMessagesPerScan": 50,
			"AllowedSenderDomains": []any{"linkedin.com", "indeed.com", "glassdoor.com", "wellfound.com", "ziprecruiter.com", "dice.com", "builtin.com"},
		},
	}
}

// Load merges defaults, the JSON file (if it exists), the secrets file (if it exists), and the environment.
func Load(configPath, secretsPath string, environ []string) (*Config, error) {
	tree := defaults()

	if raw, err := os.ReadFile(configPath); err == nil {
		var file map[string]any
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("parse %s: %w", configPath, err)
		}
		merge(tree, file)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if secretsPath != "" {
		flat, err := ReadSecrets(secretsPath)
		if err != nil {
			return nil, err
		}
		merge(tree, unflatten(flat, ":"))
	}

	envFlat := map[string]string{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.Contains(k, "__") {
			envFlat[k] = v
		}
	}
	merge(tree, unflatten(envFlat, "__"))

	// Round-trip through JSON into the typed struct. Numbers that arrived as strings (secrets, env) are coerced.
	raw, err := json.Marshal(coerceNumbers(tree))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &cfg, nil
}

// DefaultSecretsPath picks the secrets file: $CONSIGLIERE_SECRETS, else <user config dir>/Consigliere/secrets.json.
// If that doesn't exist yet but the .NET version's user-secrets file does, that file is used, so existing
// secrets carry over to the Go version untouched.
func DefaultSecretsPath() string {
	if p := os.Getenv("CONSIGLIERE_SECRETS"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	native := filepath.Join(dir, "Consigliere", "secrets.json")
	if _, err := os.Stat(native); err == nil {
		return native
	}
	legacy := filepath.Join(dir, "Microsoft", "UserSecrets", "dotnet-Consigliere.Host-141f7b7f-9bfd-4e1b-979b-27c15113afee", "secrets.json")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return native
}

// ReadSecrets reads a flat {"Section:Key": "value"} file. A missing file is empty, not an error.
func ReadSecrets(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	raw = []byte(strings.TrimPrefix(string(raw), "\xef\xbb\xbf")) // .NET writes a UTF-8 BOM
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("parse secrets %s: %w", path, err)
	}
	return flat, nil
}

// SetSecret writes one key to the secrets file, creating it if needed.
func SetSecret(path, key, value string) error {
	flat, err := ReadSecrets(path)
	if err != nil {
		return err
	}
	flat[key] = value
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(flat, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		key := matchKey(dst, k)
		if sub, ok := v.(map[string]any); ok {
			if existing, ok := dst[key].(map[string]any); ok {
				merge(existing, sub)
				continue
			}
		}
		dst[key] = v
	}
}

// matchKey makes keys case-insensitive like .NET configuration: "telegram:bottoken" overrides "Telegram.BotToken".
func matchKey(m map[string]any, k string) string {
	for existing := range m {
		if strings.EqualFold(existing, k) {
			return existing
		}
	}
	return k
}

// unflatten turns {"Telegram:AllowedChatIds:0": "1"} into nested maps; all-numeric child keys become arrays.
func unflatten(flat map[string]string, sep string) map[string]any {
	root := map[string]any{}
	for key, value := range flat {
		parts := strings.Split(key, sep)
		node := root
		for i, p := range parts {
			p = matchKey(node, p)
			if i == len(parts)-1 {
				node[p] = value
				break
			}
			child, ok := node[p].(map[string]any)
			if !ok {
				child = map[string]any{}
				node[p] = child
			}
			node = child
		}
	}
	return arrayify(root).(map[string]any)
}

func arrayify(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for k, child := range m {
		m[k] = arrayify(child)
	}
	if len(m) == 0 {
		return m
	}
	indexes := make([]int, 0, len(m))
	for k := range m {
		n, err := strconv.Atoi(k)
		if err != nil {
			return m
		}
		indexes = append(indexes, n)
	}
	sort.Ints(indexes)
	arr := make([]any, len(indexes))
	for i, n := range indexes {
		arr[i] = m[strconv.Itoa(n)]
	}
	return arr
}

// coerceNumbers converts numeric-looking strings to numbers so `"20"` from an env var decodes into an int field.
// Values that must stay strings (tokens, keys) are never purely numeric in practice; chat IDs are meant to be numbers.
func coerceNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			t[k] = coerceNumbers(child)
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = coerceNumbers(child)
		}
		return t
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n
		}
		return t
	default:
		return v
	}
}
