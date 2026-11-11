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
