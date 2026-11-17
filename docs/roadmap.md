# Roadmap

Consigliere is a personal agent: a Go service that watches sources, scores what it finds against a private profile,
and works with its owner over Telegram and a private dashboard. The code is task-agnostic; everything personal
(profile, preferences, prompts, contacts) lives in a separate private data pack. Every milestone below has a label
so it can be referred to by number. Numbers are never reused, so a dropped milestone leaves a gap (M10).

**Guardrails for every milestone:** drafts only, never a submit or send tool. No scraping of sites whose terms forbid
it. The bot reads only a dedicated, forwarded mailbox, never the owner's real inbox. Secrets live in Key Vault or a
local secrets file, never in the repo. Only one copy of a bot runs at a time.

| # | Milestone | Status |
|---|---|---|
| **M0** | Skeleton: repo, data-pack boundary, sample data pack, secret hygiene (gitleaks, .gitignore) | Done |
| **M1** | Telegram channel with a chat-ID allowlist, Claude chat, `/status`, `/pause` kill switch | Done |
| **M2** | Radar ingestion from public feeds, SQLite, dedupe, rule filter, in-process scheduler | Done |
| **M3** | Scoring with structured output and caching, tiered alerts, feedback buttons, dead-man's switch | Done |
| **M4** | Read-only ingestion from a dedicated, forwarded mailbox | Done |
| **M5** | Cloud on AKS (Bicep, Key Vault, OIDC deploys, backups) | Done, replaced by M12 |
| **M6** | Pipeline: Options → Active → Waiting → Completed, per-item checklist, drafting kit, morning briefing | Done |
| **M7** | Updates from email move items through the pipeline (confirmations, scheduling, outcomes) | Done |
| **M8** | React + TypeScript dashboard, reachable only over the tailnet | Done |
| **M9** | Package comparison: an incoming package vs the owner's current one, year by year, in plain code | Planned |
| **M11** | A high-priority source with no public feed: alert emails plus pasted details, re-score on demand, a top-4 view | In progress |
| **M12** | One small VM per bot: systemd, secrets from Key Vault into memory, Tailscale, per-branch deploys | Done |
| **M13** | Dashboard sections, starting with **Dates**: idea bank, planning, reminders, ratings (branch `feature/m13-dates`) | Not started |
| **M14** | Fit the second bot's data pack and sources to its owner's field; move owner-specific bits into data | Planned |
| **M15** | MCP server as its own program, calling the bot's API (branch `feature/m15-mcp`) | Phase 1 done |
| **M16** | Dev box: a VM on the tailnet with Tailscale SSH and the toolchains, no Azure rights ([dev-box.md](dev-box.md)) | In progress |
| **M17** | Repos on the dev box: synced from a list, pushes with one scoped, expiring GitHub token | In progress |
| **M18** | Claude Code Remote Control servers per repo, so sessions are driven from the Claude app | Planned |
| **M19** | A **Code** section in the dashboard and `/code` in Telegram: repo status, start/stop sessions, wake/sleep the VM | Planned |

## Details for open milestones

**M9: Package comparison.** The current package goes in the data pack (base pay, bonus, stock vests, retirement match,
health, perks), each value dated. An incoming package is pasted in; a cheap model extracts it. All math is plain,
unit-tested code: totals for years 1-4, what's forfeited by leaving, and questions for anything not stated. Private
equity is shown apart from guaranteed pay, with a warning.

**M11: A source with no public feed.** Its site's terms forbid automated access, so the feed is alert emails (its own,
or from a third-party site that lists its items), forwarded to the dedicated mailbox. **Done:** those alerts dedupe by
the third-party site's own id, and every item and pursuit page links to that site's company research page (a link the
owner opens; nothing is fetched). **Next:** for the shortlist, the owner pastes the full text on the item page to
re-score it. A "top picks" view shows the best 4 from that source, plus a weekly Telegram line.

**M13: Dates.** A section switcher in the dashboard (each bot turns sections on in config). Dates has these pages:
Upcoming, Ideas (add, generate, favorite), Generate (constraints in, 5 ideas out, using past ratings) and History.
Reminders go through Telegram: a cadence nudge, prep due dates, the day before and the day of, then a rating
afterward. Before merging, it's tested with a separate test bot.

**M14: Second bot's field.** Talk it through with that bot's owner, then rewrite their data pack (profile, preferences, sources) and
add sources that carry their kind of work (alert emails, documented public APIs). Also make the shared code
neutral: search links and groups come from data, prompts use field-neutral wording, and per-bot settings move out of
the shared config.

**M15: MCP server.** `consigliere-mcp` uses the official Go SDK and calls a bot's dashboard API with its token.
It never opens the database.
1. Read-only tools over stdio. **Done:** search, get item, list and get pursuits, suggestions, status.
2. Resources and prompts.
3. Write tools with annotations, plus progress reporting for long drafts.
4. Streamable HTTP next to each bot, behind Tailscale.
5. Optional central service: OAuth, one bot per user, stateless and scalable. Costs isolation and a third VM.

See [mcp.md](mcp.md).

**M16-M19: Coding from the phone.** The owner works on their repositories from the Claude app with the PC off.
- **M16:** a separate dev box VM, reachable only over Tailscale SSH. **Built:** `infra/dev.bicep`, `deploy/dev.sh`,
  `deploy/dev/setup.sh`. **Next:** the owner deploys it and signs in.
- **M17:** a timer clones and fetches the repos listed in the data pack, never touching uncommitted work. Pushes use a
  fine-grained, expiring GitHub token read from Key Vault on each push. `main` stays behind PRs and CI, and data
  repos (live on push, unprotectable on the free plan) refuse pushes to `main` from the box. **Built** with M16's
  setup (`deploy/dev/box/`); **next:** the owner deploys both.
- **M18:** one `claude remote-control --spawn worktree` server per repo, run by systemd inside tmux, so each repo
  shows up in the Claude app and every new session gets its own worktree. `claude --continue` sessions connect too.
- **M19:** the bot fetches each repo's state from the dev box over the tailnet, shows it on the dashboard, and starts
  or stops the repo's server. It wakes the VM on demand and lets it sleep when idle. The dev box stays separate from
  the bot VMs so coding sessions can never reach a bot's secrets.
