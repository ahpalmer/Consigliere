// Command consigliere is a personal agent: Telegram in front, Claude for judgment, deterministic pipelines behind.
//
//	consigliere                      run the agent (from the repo root)
//	consigliere auth gmail           one-time consent for the dedicated mailbox
//	consigliere secrets set KEY VAL  store a secret (e.g. Telegram:BotToken) outside the repo
//	consigliere secrets list         show which secrets are set (values hidden)
package main

import (
	"context"
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
	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/config"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/mail"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/router"
	"github.com/ahpalmer/consigliere/internal/scheduler"
	"github.com/ahpalmer/consigliere/internal/store"
	"github.com/ahpalmer/consigliere/internal/telegram"
)

func main() {
	configPath := flag.String("config", "config/consigliere.json", "settings file (no secrets)")
	secretsPath := flag.String("secrets", config.DefaultSecretsPath(), "secrets file (flat \"Section:Key\" JSON), kept outside the repo")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args := flag.Args(); {
	case len(args) == 0 || args[0] == "run":
		err = run(ctx, *configPath, *secretsPath)
	case len(args) == 2 && args[0] == "auth" && args[1] == "gmail":
		err = authGmail(ctx, *configPath, *secretsPath)
	case len(args) == 4 && args[0] == "secrets" && args[1] == "set":
		if err = config.SetSecret(*secretsPath, args[2], args[3]); err == nil {
			fmt.Printf("Saved %s to %s\n", args[2], *secretsPath)
		}
	case len(args) == 2 && args[0] == "secrets" && args[1] == "list":
		err = listSecrets(*secretsPath)
	default:
		err = errors.New("usage: consigliere [run | auth gmail | secrets set KEY VALUE | secrets list]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath, secretsPath string) error {
	cfg, err := config.Load(configPath, secretsPath, os.Environ())
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
	fetcher := radar.NewHTTPFetcher()
	scanner := &radar.Scanner{DB: db, Config: radarCfg, Clock: clk, FetchConcurrency: 6, Sources: []radar.Source{
		radar.Greenhouse{HTTP: fetcher}, radar.Lever{HTTP: fetcher}, radar.Ashby{HTTP: fetcher},
		&mail.Source{DB: db, Mailbox: mail.NewGmail(cfg.Mail), Extractor: mail.LLMExtractor{LLM: llmClient}, Clock: clk,
			Query: cfg.Mail.Query, MaxMessages: cfg.Mail.MaxMessagesPerScan, AllowedDomains: cfg.Mail.AllowedSenderDomains},
	}}
	alerts := &radar.Alerts{DB: db, Ranking: ranking, Channel: sender, Clock: clk, MinScore: cfg.Radar.AlertMinScore}
	scoring := &radar.ScoringRunner{DB: db, Ranking: ranking, Alerts: alerts, State: state, Clock: clk, Tasks: tasks,
		BatchSize: cfg.Radar.ScoreBatchSize, Concurrency: cfg.Radar.ScoreConcurrency,
		Scorer: radar.LLMScorer{LLM: llmClient, Model: scoreModel, Owner: cfg.Agent.OwnerName, Pack: pack, Config: radarCfg, Clock: clk}}
	digest := &radar.Digest{DB: db, Ranking: ranking, Clock: clk, MaxItems: cfg.Radar.DigestMaxItems, MinScore: cfg.Radar.DigestMinScore}
	feedback := &radar.Feedback{DB: db, Clock: clk}
	radarCommands := &radar.Commands{DB: db, Scanner: scanner, Scoring: scoring, Digest: digest, Feedback: feedback, Config: radarCfg, Clock: clk,
		Tasks: tasks, Channel: sender}

	chat := llm.NewChat(llmClient, llm.SystemPrompt(cfg.Agent.OwnerName))
	receiver := &telegram.Receiver{API: tg, Sender: sender, Router: router.New(state, chat, usage, radarCommands),
		Callbacks: []channel.CallbackHandler{feedback}, Allowed: cfg.Telegram.AllowedChatIds}
	sched := &scheduler.Scheduler{State: state, Channel: sender, Location: location, PingURL: cfg.Scheduler.HealthchecksPingUrl,
		Jobs: []scheduler.Job{
			&radar.ScanJob{Scanner: scanner, Scoring: scoring, Cron: cfg.Radar.ScanCron},
			&radar.ScoreJob{Scoring: scoring, Cron: cfg.Radar.ScoreCron},
			&radar.DigestJob{Digest: digest, Channel: sender, Cron: cfg.Radar.DigestCron},
		}}

	// Run until Ctrl+C / SIGTERM, then stop in order: polling and scheduling first, then background work, and only
	// then (deferred) close the database, so nothing writes to a closed DB.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- receiver.Run(runCtx) }()
	go func() { errs <- sched.Run(runCtx) }()

	var firstErr error
	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case firstErr = <-errs:
	}
	cancel()
	deadline := time.After(15 * time.Second)
	for remaining := 2 - boolToInt(firstErr != nil); remaining > 0; remaining-- {
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

func listSecrets(path string) error {
	flat, err := config.ReadSecrets(path)
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
