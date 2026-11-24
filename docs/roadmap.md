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
| **M9** | Package comparison: an incoming package vs the owner's current one, year by year, in plain code | Done |
| **M11** | A high-priority source with no public feed: alert emails (job sites and company career sites) plus pasted details, re-score on demand | Done |
| **M12** | One small VM per bot: systemd, secrets from Key Vault into memory, Tailscale, per-branch deploys | Done |
| **M13** | Dashboard sections, starting with **Adventures**: idea bank, planning, reminders, ratings ([#6](https://github.com/ahpalmer/Consigliere/pull/6)) | Built, waiting on you |
| **M14** | Fit the second bot's data pack and sources to its owner's field; move owner-specific bits into data | Blocked: waiting on you ([below](#waiting-on-you)) |
| **M15** | MCP server as its own program, calling the bot's API | Planned: you're building it by hand |
| **M16** | Dev box: a VM on the tailnet with Tailscale SSH and the toolchains, no Azure rights ([dev-box.md](dev-box.md)) | Done |
| **M17** | Repos on the dev box: synced from a list, pushes with one scoped, expiring GitHub token | Done |
| **M18** | A **Code** page: each repo active or not, Continue Coding / New Session open a Remote Control session, waking the VM | Done |
| **M19** | More on the Code page: branch and changes per repo, stop a session, `/code` in Telegram, sleep when idle ([#8](https://github.com/ahpalmer/Consigliere/pull/8)) | Built, waiting on you |
| **M20** | The plan on the Code page: open a repo to read its roadmap, open milestones first ([#9](https://github.com/ahpalmer/Consigliere/pull/9), after #8) | Built, waiting on you |

## Details for open milestones

**M9: Package comparison.** The current package goes in the data pack (base pay, bonus, stock vests, retirement match,
health, perks), each value dated. An incoming package is pasted in; a cheap model extracts it. All math is plain,
unit-tested code: totals for years 1-4, what's forfeited by leaving, and questions for anything not stated. Private
equity is shown apart from guaranteed pay, with a warning. **Built** (`internal/comp`, the pursuit page's Offers panel):
the retirement match is worked out on what the owner actually puts in, on both sides, and stock can be a steady yearly
amount instead of grants. Every posting that lists pay (most do, under pay-transparency laws) shows it against total
pay now on Options, Suggested and the posting page, and Options filters on it. Fit is now three parts: the model
scores only the resume match, and code blends in location (by tier) and pay (against pay now), weighted in
`preferences.yaml` (`fit:`); jobs sort by that fit instead of tier and category first. The owner's
`comp/current.yaml` is in the data pack. **Done** (merged in #4). Trying it on a real offer is on [your list](#waiting-on-you).

**M11: A source with no public feed.** Its site's terms forbid automated access, so the feed is alert emails (its own,
or from a third-party site that lists its items), forwarded to the dedicated mailbox. **Done:** those alerts dedupe by
the third-party site's own id, and every item and pursuit page links to that site's company research page (a link the
owner opens; nothing is fetched). **Built:** a company's own career-site alerts (apple.com) are read once its domain is
listed under `senders:` on the watchlist's email entry. On the item page the owner pastes the full posting; it replaces
the alert's snippet, takes the pay range from it, and the job is re-scored at once (a later copy of the alert never
overwrites it). The "top picks" view was dropped: Options already sorts by fit. **Done** (merged in #5).

**M13: Adventures.** A section switcher in the dashboard (each bot turns sections on in config). Adventures has these pages:
Upcoming, Ideas (add, generate, favorite), Generate (constraints in, 5 ideas out, using past ratings) and History.
Reminders go through Telegram: a cadence nudge, prep due dates, the day before and the day of, then a rating
afterward. Before merging, it's tested with a separate test bot. **Built** (#6, `internal/adventures`): a bot gets the
section when its data pack has `adventures/settings.yaml`; reminders go out at 9am (`Adventures:ReminderCron`) with
⭐1-5 buttons the day after, and the generator uses past ratings, reviews, favorites and `likes`. **Next:** on your
list.

**M14: Second bot's field.** **Blocked:** waiting on you (see [below](#waiting-on-you)). Talk it through with that bot's owner, then rewrite their data pack (profile, preferences, sources) and
add sources that carry their kind of work (alert emails, documented public APIs). Also make the shared code
neutral: search links and groups come from data, prompts use field-neutral wording, and per-bot settings move out of
the shared config.

**M15: MCP server.** An MCP server as its own program that calls a bot's dashboard API with its token and never
opens the database. **Planned:** you're building this one by hand, to learn how MCP works and how to set it up.
#7 has a full version to compare against when you want it.
1. Read-only tools over stdio.
2. Resources and prompts.
3. Write tools with annotations, plus progress reporting for long tasks.
4. Streamable HTTP next to each bot, behind Tailscale.
5. Optional central service: OAuth, one bot per user. Costs isolation and a third VM.

**M16-M19: Coding from the phone.** The owner works on their repositories from the Claude app with the PC off.
- **M16:** a separate dev box VM, reachable only over Tailscale SSH: `infra/dev.bicep`, `deploy/dev.sh`,
  `deploy/dev/setup.sh`. Deployed.
- **M17:** a timer clones and fetches the repos listed in the data pack, never touching uncommitted work. Pushes use a
  fine-grained, expiring GitHub token read from Key Vault on each push. `main` stays behind PRs and CI, and data
  repos (live on push, unprotectable on the free plan) refuse pushes to `main` from the box (`deploy/dev/box/`).
  Deployed.
- **M18:** the dashboard's Code page ([dev-box.md](dev-box.md#the-code-page)). Each repo shows active or inactive;
  Continue Coding (`claude --continue`) and New Session (`claude`) open a tmux session with Remote Control on, so it
  shows up in the Claude app, any number at once. An agent on the dev box opens them, answering only the bot's tailnet
  machine; when the box is asleep, the bot starts the VM first (a custom role: read and start that one VM).
  **Done:** deployed, and sessions open from the phone (this roadmap was updated from one).
- **M19:** the page grows: branch, uncommitted changes and open PRs per repo, stopping a session, `/code` in
  Telegram, and letting the VM sleep after a few idle hours. **Built** (#8): the agent reads each repo with git and
  `gh` (read only), and the bot deallocates the box after `sleep_after_hours` (default 3) with nothing going on. The dev box stays separate from the bot VMs so coding
  sessions can never reach a bot's secrets.
- **M20:** the plan, on the Code page. Clicking a repo opens its roadmap, so "what's the plan?" never needs asking.
  The dev box agent reads the plan file from the repo's synced clone (`docs/roadmap.md` by default, or a `plan:` path
  per repo in `code/repos.yaml`), from the default branch plus any open feature branch's version, and the bot serves it
  to the dashboard. The page shows the open milestones first (not done, in progress, built but not merged) with their
  details, and the done ones folded away. A repo with no plan file just says so. While the dev box sleeps, the last
  copy the bot saw is shown with its date, rather than waking the VM just to read a file. **Built** (#9, on top of #8).

## Waiting on you

Things only you can do. Each one unblocks or finishes a milestone.

- [ ] **M14:** talk through the second bot's owner's field with them (target roles, where they'd move, pay, which job
  sites and alerts they use), then tell me. Until then their data pack and sources stay as they are.
- [ ] **M13:** choose which bot gets Adventures and add `adventures/settings.yaml` to its data pack (copy the sample
  in `samples/data-pack/adventures/`: `cadence_days`, `home`, `with`, `likes`). Try it on a test bot first, as planned,
  then merge #6.
- [ ] **M19, M20:** merge #8, then #9. Then redeploy the dev box's infra (`az deployment sub create -n consigliere-dev
  -l eastus2 -f infra/dev.bicep -p infra/dev.bicepparam`) so the bot may put the box to sleep, and run
  `bash deploy/dev.sh setup` to install the new agent (repo details, Stop, idle time, plans).
- [ ] **This roadmap:** merge this PR after the others, so the statuses match what's on `main`.
- [ ] **M9:** when a real offer comes in, paste it on the pursuit page and check the numbers it reads.
- [ ] **M11:** paste a few full postings on alert-email jobs and see whether the new scores feel right.
