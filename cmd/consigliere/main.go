// Command consigliere is a personal agent: Telegram in front, Claude for judgment, deterministic pipelines behind.
//
//	consigliere                      run the agent (from the repo root)
//	consigliere auth gmail           one-time consent for the dedicated mailbox
//	consigliere secrets set KEY VAL  store a secret (e.g. Telegram:BotToken) outside the repo
//	consigliere secrets list         show which secrets are set (values hidden)
//	consigliere keyvault get VAULT NAME  print one Key Vault secret, signed in as the VM's identity (for scripts)
//
// On the VM, --secrets is keyvault://<vault>/<secret>: the secrets JSON is read from Key Vault into memory.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata" // IANA zones work on Windows and in minimal containers

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/azure"
	"github.com/ahpalmer/consigliere/internal/backup"
	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/config"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/kit"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/mail"
	"github.com/ahpalmer/consigliere/internal/pipeline"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/router"
	"github.com/ahpalmer/consigliere/internal/scheduler"
	"github.com/ahpalmer/consigliere/internal/store"
	"github.com/ahpalmer/consigliere/internal/telegram"
	"github.com/ahpalmer/consigliere/internal/web"
)

func main() {
	configPath := flag.String("config", "config/consigliere.json", "settings file (no secrets)")
	secretsPath := flag.String("secrets", config.DefaultSecretsPath(), "secrets file (flat \"Section:Key\" JSON) kept outside the repo, or keyvault://<vault>/<secret>")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args := flag.Args(); {
	case len(args) == 0 || args[0] == "run":
		err = run(ctx, *configPath, *secretsPath)
	case len(args) == 2 && args[0] == "auth" && args[1] == "gmail":
		if err = writableSecrets(*secretsPath); err == nil {
			err = authGmail(ctx, *configPath, *secretsPath)
		}
	case len(args) == 4 && args[0] == "secrets" && args[1] == "set":
		if err = writableSecrets(*secretsPath); err == nil {
			err = config.SetSecret(*secretsPath, args[2], args[3])
		}
		if err == nil {
			fmt.Printf("Saved %s to %s\n", args[2], *secretsPath)
		}
	case len(args) == 2 && args[0] == "secrets" && args[1] == "list":
		err = listSecrets(ctx, *secretsPath)
	case len(args) == 4 && args[0] == "keyvault" && args[1] == "get":
		err = printVaultSecret(ctx, args[2], args[3])
	default:
		err = errors.New("usage: consigliere [run | auth gmail | secrets set KEY VALUE | secrets list | keyvault get VAULT NAME]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath, secretsPath string) error {
	secrets, err := readSecrets(ctx, secretsPath)
	if err != nil {
		return err
	}
	cfg, err := config.LoadWithSecrets(configPath, secrets, os.Environ())
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)})))
	switch {
	case cfg.Anthropic.ApiKey == "":
		return errors.New("Anthropic:ApiKey is not configured")
	case cfg.Telegram.BotToken == "":
		return errors.New("Telegram:BotToken is not configured")
	}

	// Fail fast on a bad data pack path rather than on the first message.
	pack, err := datapack.Open(cfg.DataPack.Path)
	if err != nil {
		return err
	}
	slog.Info("starting", "data_pack", pack.Root, "secrets", secretsPath)
	dbPath, err := pack.Resolve(cfg.Radar.DatabasePath)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	location, err := time.LoadLocation(cfg.Scheduler.TimeZone)
	if err != nil {
		return err
	}

	clk := clock.Real{}
	state := agent.NewState(clk.Now())
	tasks := agent.NewTasks(ctx)
	usage := llm.NewUsageTracker()
	llmClient := llm.NewClient(cfg.Anthropic.ApiKey, cfg.Llm, usage)
	tg := telegram.NewAPI(cfg.Telegram.BotToken)
	sender := &telegram.Sender{API: tg, Owner: cfg.Telegram.OwnerChat()}

	// Radar.
	radarCfg := radar.ConfigLoader{Pack: pack}
	ranking := radar.RankingFactory{Config: radarCfg}
	scoreModel, err := cfg.Llm.ModelFor(llm.TaskScore)
	if err != nil {
		return err
	}
	pipe := &pipeline.Pipeline{DB: db, Clock: clk, Location: location,
		SoftCap: cfg.Pipeline.ActiveSoftCap, FollowUpDays: cfg.Pipeline.FollowUpDays, GhostDays: cfg.Pipeline.GhostDays}
	fetcher := radar.NewHTTPFetcher()
	gmailBox := mail.NewGmail(cfg.Mail)
	// Emails about the owner's applications update the pipeline (M7): received → waiting, interview → active, ...
	mailUpdates := &pipeline.MailUpdates{Pipeline: pipe, Channel: sender}
	mailSource := &mail.Source{DB: db, Mailbox: gmailBox, Reader: mail.LLMReader{LLM: llmClient}, Clock: clk,
		Query: cfg.Mail.Query, MaxMessages: cfg.Mail.MaxMessagesPerScan, AllowedDomains: cfg.Mail.AllowedSenderDomains,
		UpdateDomains: cfg.Mail.UpdateSenderDomains, BlockedSenders: cfg.Mail.BlockedSenders, ForwardedBy: cfg.Mail.ForwardedBy, Companies: mailUpdates.Companies, Updates: mailUpdates}
	scanner := &radar.Scanner{DB: db, Config: radarCfg, Clock: clk, FetchConcurrency: 6, Sources: []radar.Source{
		radar.Greenhouse{HTTP: fetcher}, radar.Lever{HTTP: fetcher}, radar.Ashby{HTTP: fetcher}, radar.HackerNews{HTTP: fetcher},
		mailSource,
	}}
	// Every item shown on its own gets 👍/👎 plus "🎯 Make active", which starts a pursuit.
	itemButtons := func(id int64) []channel.Button { return append(radar.FeedbackButtons(id), pipeline.ActivateButton(id)) }
	alerts := &radar.Alerts{DB: db, Ranking: ranking, Channel: sender, Clock: clk, MinScore: cfg.Radar.AlertMinScore, Buttons: itemButtons}
	scoring := &radar.ScoringRunner{DB: db, Ranking: ranking, Alerts: alerts, State: state, Clock: clk, Tasks: tasks,
		BatchSize: cfg.Radar.ScoreBatchSize, Concurrency: cfg.Radar.ScoreConcurrency,
		Scorer: radar.LLMScorer{LLM: llmClient, Model: scoreModel, Owner: cfg.Agent.OwnerName, Pack: pack, Config: radarCfg, Clock: clk}}
	digest := &radar.Digest{DB: db, Ranking: ranking, Clock: clk, MaxItems: cfg.Radar.DigestMaxItems, MinScore: cfg.Radar.DigestMinScore}
	feedback := &radar.Feedback{DB: db, Clock: clk}
	catalog := &radar.Catalog{DB: db, Ranking: ranking, Config: radarCfg, Clock: clk, MinScore: cfg.Radar.DigestMinScore}
	radarCommands := &radar.Commands{DB: db, Scanner: scanner, Scoring: scoring, Digest: digest, Feedback: feedback, Catalog: catalog, Config: radarCfg, Clock: clk,
		Tasks: tasks, Channel: sender, ItemButtons: itemButtons}

	// Pipeline: active → waiting → completed, with the apply kit.
	drafts := &pipeline.Drafts{Pipeline: pipe, Kit: &kit.Kit{Pack: pack, LLM: llmClient, Owner: cfg.Agent.OwnerName}, Tasks: tasks, State: state}
	if err := drafts.Recover(ctx); err != nil {
		return err
	}
	suggestions := &pipeline.Suggestions{Catalog: catalog, Pipeline: pipe, Drafts: drafts,
		HighMinScore: cfg.Pipeline.SuggestHighScore, MediumMinScore: cfg.Pipeline.SuggestMediumScore,
		MaxHigh: cfg.Pipeline.SuggestMaxHigh, MaxMedium: cfg.Pipeline.SuggestMaxMedium, PerCompany: cfg.Pipeline.SuggestPerCompany,
		AutoPrepare: cfg.Pipeline.AutoPrepareKits, Cron: cfg.Pipeline.PrepareCron}
	pipelineCommands := &pipeline.Commands{Pipeline: pipe, Drafts: drafts, Channel: sender, Files: sender, Tasks: tasks, State: state}
	if len(cfg.Mail.MissingSettings()) == 0 {
		pipelineCommands.Inbox = gmailBox // kits also land in the dedicated mailbox
	}

	chat := llm.NewChat(llmClient, llm.SystemPrompt(cfg.Agent.OwnerName))
	receiver := &telegram.Receiver{API: tg, Sender: sender, Router: router.New(state, chat, usage, radarCommands, pipelineCommands),
		Callbacks: []channel.CallbackHandler{feedback, pipelineCommands}, Allowed: cfg.Telegram.AllowedChatIds}
	sched := &scheduler.Scheduler{State: state, Channel: sender, Location: location, PingURL: cfg.Scheduler.HealthchecksPingUrl,
		Jobs: []scheduler.Job{
			&radar.ScanJob{Scanner: scanner, Scoring: scoring, Cron: cfg.Radar.ScanCron},
			&radar.ScoreJob{Scoring: scoring, Cron: cfg.Radar.ScoreCron},
			&pipeline.Briefing{Pipeline: pipe, Digest: digest, Suggestions: suggestions, Channel: sender, Cron: cfg.Radar.DigestCron},
		}}
	if cfg.Pipeline.AutoPrepareKits > 0 {
		sched.Jobs = append(sched.Jobs, suggestions)
	}
	sched.Jobs = append(sched.Jobs,
		&radar.TuningSummary{DB: db, Channel: sender, Clock: clk, Cron: cfg.Radar.TuningCron},
		&pipeline.MonthlySummary{Pipeline: pipe, Channel: sender, Cron: cfg.Pipeline.SummaryCron})
	// Backups run where there's somewhere to put them: on the VM, signed in as its managed identity.
	if cfg.Backup.ContainerUrl != "" {
		sched.Jobs = append(sched.Jobs, &backup.Job{DB: db,
			Blobs: &backup.BlobContainer{URL: cfg.Backup.ContainerUrl, Tokens: azure.ManagedIdentityFromEnv()},
			Clock: clk, Cron: cfg.Backup.Cron})
	}

	// The dashboard is optional: an empty Web:Listen turns it off.
	var dashboard *web.Server
	if cfg.Web.Listen != "" {
		if err := web.CheckAddr(cfg.Web.Listen, cfg.Web.Token); err != nil {
			return err
		}
		dashboard = &web.Server{Addr: cfg.Web.Listen, Token: cfg.Web.Token, Pipeline: pipe, Drafts: drafts, Suggestions: suggestions, Feedback: feedback,
			Catalog: catalog, Scoring: scoring, State: state, Usage: usage, Clock: clk}
	}

	// Run until Ctrl+C / SIGTERM, then stop in order: polling, scheduling and the dashboard first, then background
	// work, and only then (deferred) close the database, so nothing writes to a closed DB.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	services := []func(context.Context) error{receiver.Run, sched.Run}
	if dashboard != nil {
		services = append(services, dashboard.Run)
	}
	errs := make(chan error, len(services))
	for _, run := range services {
		go func() { errs <- run(runCtx) }()
	}
	if len(cfg.Mail.MissingSettings()) == 0 {
		tasks.Go("mail-backfill", func(ctx context.Context) error { return backfillMail(ctx, db, mailSource, cfg.Mail, sender) })
	}

	var firstErr error
	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case firstErr = <-errs:
	}
	cancel()
	deadline := time.After(15 * time.Second)
	for remaining := len(services) - boolToInt(firstErr != nil); remaining > 0; remaining-- {
		select {
		case <-errs:
		case <-deadline:
			slog.Warn("timed out waiting for shutdown")
			remaining = 0
		}
	}
	if !tasks.Wait(10 * time.Second) {
		slog.Warn("background work still running at exit")
	}
	return firstErr
}

// backfillMail reads older mail once for application updates (the mailbox may hold confirmations and interview
// requests from before this version), then says what it found. A failure is retried on the next start.
func backfillMail(ctx context.Context, db *sql.DB, src *mail.Source, cfg config.Mail, ch channel.Channel) error {
	const key = "mail_backfill_v1"
	if _, done, err := store.GetState(ctx, db, key); err != nil || done {
		return err
	}
	notes, err := src.Backfill(ctx, cfg.BackfillQuery, cfg.BackfillMax)
	if err != nil {
		slog.Warn("mail backfill failed; will retry on the next start", "err", err)
		return nil
	}
	if err := store.SetState(ctx, db, key, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	slog.Info("mail backfill done", "updates", len(notes))
	if len(notes) == 0 {
		return nil
	}
	return ch.Notify(ctx, fmt.Sprintf("📬 Read older mail for application updates: %d found (%s). Each is in its pursuit's history; /waiting and /active show where things stand.",
		len(notes), strings.Join(notes, ", ")))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func authGmail(ctx context.Context, configPath, secretsPath string) error {
	cfg, err := config.Load(configPath, secretsPath, os.Environ())
	if err != nil {
		return err
	}
	token, err := mail.Authorize(ctx, cfg.Mail, func(f string, a ...any) { fmt.Printf(f, a...) })
	if err != nil {
		return err
	}
	if err := config.SetSecret(secretsPath, "Mail:RefreshToken", token); err != nil {
		return err
	}
	fmt.Printf("Gmail authorized. Refresh token saved to %s as Mail:RefreshToken.\n", secretsPath)
	return nil
}

// readSecrets reads the secrets file, or on the VM the same JSON from Key Vault (keyvault://<vault>/<secret>).
func readSecrets(ctx context.Context, path string) (map[string]string, error) {
	vault, name, ok := azure.ParseSecretRef(path)
	if !ok {
		if path == "" {
			return map[string]string{}, nil
		}
		return config.ReadSecrets(path)
	}
	raw, err := (&azure.KeyVault{Name: vault, Tokens: azure.ManagedIdentityFromEnv()}).Secret(ctx, name)
	if err != nil {
		return nil, err
	}
	flat, err := config.ParseSecrets([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parse secrets %s: %w", path, err)
	}
	return flat, nil
}

// writableSecrets refuses Key Vault for commands that save a secret: the vault is changed with deploy/push-secrets.sh.
func writableSecrets(path string) error {
	if _, _, ok := azure.ParseSecretRef(path); ok {
		return errors.New("secrets in Key Vault are changed with deploy/push-secrets.sh, not from here")
	}
	return nil
}

// printVaultSecret writes a secret's value to stdout exactly (no newline added), so a script can pipe it into a file
// in memory, e.g. the data pack's deploy key.
func printVaultSecret(ctx context.Context, vault, name string) error {
	value, err := (&azure.KeyVault{Name: vault, Tokens: azure.ManagedIdentityFromEnv()}).Secret(ctx, name)
	if err != nil {
		return err
	}
	_, err = fmt.Print(value)
	return err
}

func listSecrets(ctx context.Context, path string) error {
	flat, err := readSecrets(ctx, path)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println(path)
	for _, k := range keys {
		fmt.Printf("  %s = (set)\n", k)
	}
	return nil
}

func logLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
