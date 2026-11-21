// Command consigliere-mcp serves one Consigliere bot over MCP (stdio), for Claude Desktop, Claude Code and other MCP
// clients. It calls the bot's dashboard API with the dashboard token and never touches the database, so it can do only
// what the dashboard can.
//
//	CONSIGLIERE_URL=http://consigliere:8080 CONSIGLIERE_TOKEN=... consigliere-mcp
//
// CONSIGLIERE_URL defaults to http://127.0.0.1:8080. The token comes from CONSIGLIERE_TOKEN, or from a file named by
// --token-file, never from a flag (flags show in process lists). Logs go to stderr; stdout carries the protocol.
//
//	consigliere-mcp --http 127.0.0.1:8091
//
// serves streamable HTTP instead. On a bot's VM the release's own binary does that (`consigliere mcp-http`, in
// deploy/vm/consigliere-mcp.service). It holds no token: each caller
// sends the dashboard token as its bearer token, and it's checked with the dashboard on every request.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/ahpalmer/consigliere/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "consigliere-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	base := flag.String("url", envOr("CONSIGLIERE_URL", "http://127.0.0.1:8080"), "the bot's dashboard address")
	tokenFile := flag.String("token-file", "", "read the dashboard token from this file instead of CONSIGLIERE_TOKEN")
	listen := flag.String("http", "", "serve streamable HTTP on this address instead of stdio; callers bring their own token")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	token := os.Getenv("CONSIGLIERE_TOKEN")
	if *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(b))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *listen != "" {
		return (&mcpserver.HTTP{Base: *base, Version: version()}).Serve(ctx, *listen)
	}
	s := &mcpserver.Server{API: &mcpserver.API{Base: *base, Token: token}}
	return s.MCP(version()).Run(ctx, &mcp.StdioTransport{})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "dev"
}
