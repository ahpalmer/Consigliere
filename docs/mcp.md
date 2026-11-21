# MCP server

`consigliere-mcp` puts one bot in front of MCP clients such as Claude Desktop and Claude Code, so a conversation with
Claude can search options, read pursuits, tick steps, and have drafts written. It's a separate program
(`cmd/consigliere-mcp`, `internal/mcpserver`) built on the official Go SDK, and it only calls the bot's dashboard API
with the dashboard token. It never opens the database or the data pack, so it can do exactly what the dashboard can and
nothing more.

## Set up

Build it on the machine the MCP client runs on:

```sh
go build -o ~/bin/consigliere-mcp ./cmd/consigliere-mcp
```

It needs the dashboard's address and its token (`Web:Token`):

| Setting | Default | |
|---|---|---|
| `CONSIGLIERE_URL` or `--url` | `http://127.0.0.1:8080` | The dashboard. From your own devices: `https://<bot>.<tailnet>.ts.net`. |
| `CONSIGLIERE_TOKEN` or `--token-file` | (none) | The dashboard token. Never a flag, which would show in process lists. |

**Claude Code:**

```sh
claude mcp add consigliere --env CONSIGLIERE_URL=https://<bot>.<tailnet>.ts.net --env CONSIGLIERE_TOKEN=<token> -- ~/bin/consigliere-mcp
```

**Claude Desktop** (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "consigliere": {
      "command": "/home/you/bin/consigliere-mcp",
      "env": { "CONSIGLIERE_URL": "https://<bot>.<tailnet>.ts.net", "CONSIGLIERE_TOKEN": "<token>" }
    }
  }
}
```

The machine has to be on the tailnet, as for the dashboard. With two bots, add two servers, each with its own URL and
token.

### Over HTTP, with nothing to install

Each bot's VM also serves MCP over streamable HTTP at `https://<bot>.<tailnet>.ts.net:8443/mcp`
(`deploy/vm/consigliere-mcp.service`, published by `tailscale serve` like the dashboard). It's the release's own binary
(`consigliere mcp-http`) in a process of its own, with no secrets and no database: each caller sends the dashboard
token as `Authorization: Bearer <token>`, which it checks with the dashboard on every request and passes through.
Only tailnet devices can reach it.

**Claude Code:**

```sh
claude mcp add --transport http consigliere https://<bot>.<tailnet>.ts.net:8443/mcp --header "Authorization: Bearer <token>"
```

Any client that can send a header to a remote MCP server works the same way. claude.ai's custom connectors can't reach
it: they connect from Anthropic's cloud, which isn't on your tailnet.

## Tools

Read only (`readOnlyHint`):

| Tool | |
|---|---|
| `get_summary` | Pursuits by stage, what needs doing today, the funnel |
| `get_status` | Paused or not, each source's last run, scoring progress |
| `search_options` | Scored jobs, best fit first; by words, tier, category, minimum fit, and pay against pay now |
| `get_item` | One job: score and reasons, pay, posting text, drafts, research links |
| `list_pursuits`, `get_pursuit` | Pursuits in a stage; one pursuit with checklist, notes, history, drafts, people |
| `get_suggestions` | What to apply to next |
| `get_draft` | One draft's text or status |
| `list_offers` | Offers on a pursuit, compared with current pay |

Changes. Each carries hints: `destructiveHint` when it replaces or closes something, `idempotentHint` when calling it
again does nothing more, and `openWorldHint: false` on all of them, since they touch only the bot.

| Tool | Hints | |
|---|---|---|
| `make_active` | idempotent | Start a pursuit for a job (from options, or by company and title) |
| `rate_job` | idempotent | 👍/👎 |
| `paste_posting` | destructive, idempotent | Replace an alert's snippet with the full posting and re-score |
| `set_step` | idempotent | Tick or untick a checklist step |
| `mark_applied` | idempotent | The owner applied: to Waiting, follow-ups scheduled |
| `record_follow_up`, `keep_waiting`, `reopen_pursuit` | | The same buttons as the pursuit page |
| `schedule_interview` | idempotent | Record an interview's time |
| `complete_pursuit` | destructive | Close with an outcome (`reopen_pursuit` undoes it) |
| `add_note` | | A dated note on a pursuit |
| `write_draft` | | Apply kit, cover letter, interview prep or thank-you note; waits for it, reporting progress |
| `compare_offer` | | Read an offer and compare it with current pay |
| `pause_bot`, `resume_bot` | idempotent | The kill switch |

Drafts are never sent anywhere, and nothing here submits an application: the guardrails are the dashboard's. Tools
that spend model calls (`paste_posting`, `write_draft`, `compare_offer`) are refused while the bot is paused.

`write_draft` checks the draft every 3 seconds and sends a progress notification each time, if the client asked for
progress. A draft still being written after 4 minutes comes back with its id, for `get_draft` later.

## Resources and prompts

The read tools are also resources, for clients that attach context rather than call tools: `consigliere://summary`,
`consigliere://status`, `consigliere://suggestions`, and the templates `consigliere://pursuits/{stage}`,
`consigliere://pursuit/{id}`, `consigliere://item/{id}` and `consigliere://draft/{id}`.

Prompts start common conversations: `triage_options` (verdicts on the top jobs), `prep_interview` (for a pursuit),
`weekly_review` (every open pursuit and its next step) and `weigh_offer` (an offer against staying). Each tells Claude
which tools to read and to ask before changing anything.

## Tests

`internal/mcpserver/server_test.go` runs the MCP server against the real dashboard API over a temp database, so a
path or body that drifts from the API fails the build.
