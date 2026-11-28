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
| **M13** | Dashboard sections, starting with **Adventures**: idea bank, planning, reminders, ratings ([#6](https://github.com/ahpalmer/Consigliere/pull/6)) | Merged; turning it on is [on your list](#waiting-on-you) |
| **M14** | Fit the second bot's data pack and sources to its owner's field; move owner-specific bits into data | Blocked: waiting on you ([below](#waiting-on-you)) |
| **M15** | MCP server as its own program, calling the bot's API | Planned: you're building it by hand |
| **M16** | Dev box: a VM on the tailnet with Tailscale SSH and the toolchains, no Azure rights ([dev-box.md](dev-box.md)) | Done |
| **M17** | Repos on the dev box: synced from a list, pushes with one scoped, expiring GitHub token | Done |
| **M18** | A **Code** page: each repo active or not, Continue Coding / New Session open a Remote Control session, waking the VM | Done |
| **M19** | More on the Code page: branch and changes per repo, stop a session, `/code` in Telegram, sleep when idle ([#8](https://github.com/ahpalmer/Consigliere/pull/8)) | Merged; deploying it is [on your list](#waiting-on-you) |
| **M20** | The plan on the Code page: open a repo to read its roadmap, open milestones first ([#11](https://github.com/ahpalmer/Consigliere/pull/11)) | Built, waiting on you |
| **M21** | Adventures: a weekend calendar from both Google calendars, weekdays dimmed, free weekends easy to count | Planned |
| **M22** | Adventures: a to-do list on each idea (find a teacher, buy used gear, rent the beach house) | Planned |
| **M23** | Adventures: the bot works through those to-dos with you (research, drafts, dates on free weekends, reminders) | Planned |
| **M24** | Voice coding: plan talking to a Claude Code session hands-free, from the Code page | Planned |
| **M25** | Voice coding: pick speech-to-text and text-to-speech | Planned |
| **M26** | Voice coding: send what you say into a running Claude Code session | Planned |
| **M27** | Voice coding: read Claude's reply back aloud, with the session left open | Planned |

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
afterward. **Merged** (#6, `internal/adventures`): a bot gets the
section when its data pack has `adventures/settings.yaml`; reminders go out at 9am (`Adventures:ReminderCron`) with
⭐1-5 buttons the day after, and the generator uses past ratings, reviews, favorites and `likes`. No data pack has that
file yet, so the section is still off everywhere: turning it on is on [your list](#waiting-on-you).

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
  `gh` (read only), and the bot deallocates the box after `sleep_after_hours` (default 3) with nothing going on.
  **Merged** (#8); the infra redeploy and agent setup are on [your list](#waiting-on-you). The dev box stays separate from the bot VMs so coding
  sessions can never reach a bot's secrets.
- **M20:** the plan, on the Code page. Clicking a repo opens its roadmap, so "what's the plan?" never needs asking.
  The dev box agent reads the plan file from the repo's synced clone (`docs/roadmap.md` by default, or a `plan:` path
  per repo in `code/repos.yaml`), from the default branch plus any open feature branch's version, and the bot serves it
  to the dashboard. The page shows the open milestones first (not done, in progress, built but not merged) with their
  details, and the done ones folded away. A repo with no plan file just says so. While the dev box sleeps, the last
  copy the bot saw is shown with its date, rather than waking the VM just to read a file. **Built:** #9 was merged
  into M19's branch after #8 had already gone in, so it never reached `main`; #11 is the same branch, aimed at `main`.

**M21-M23: Adventures, continued.**
- **M21: the weekend calendar.** A month calendar at the top of Adventures, filled from your Google calendar and
  Josephine's. Weekdays are dimmed; Saturdays and Sundays are big and marked busy or free, with a count of free
  weekends in the next few months up top. Only weekend events are kept (title and time; nothing stored past the
  weekend). Each calendar is read with its **secret iCal address** (Google Calendar → Settings → the calendar →
  Integrate calendar), kept in Key Vault: read only, nothing to sign in to, and it can be reset any time to cut the bot
  off. (The Google Calendar API with OAuth is the fallback if that's not enough.) Planned adventures show on their
  weekend too.
- **M22: to-dos on each idea.** Opening an idea shows its own to-do list, like a pursuit's checklist: add, tick,
  reorder, delete, with an optional due date (for example "rent a house at the beach for two weeks" by May). Ideas with
  open to-dos show how many on the Ideas page, and the list carries over when the idea becomes a plan.
- **M23: knocking them out.** The bot helps with each to-do on request or on its own: suggest steps for a new idea,
  research (links to look at, never buying or booking), draft a message (to a teacher, a host), pick a free weekend
  from M21 for one that needs a date, and nudge in Telegram when a due date gets close. Drafts only, as everywhere.

**M24-M27: Voice coding.** The Claude app reads replies aloud in chat, but not in a coding session. These milestones
build that on the Code page, so a repo can be worked on from the car: talk, it's typed into the running session,
and Claude's reply is spoken back, with the session left open.
- **M24: the plan.** Settle how it fits together and write it up (`docs/voice.md`) before building. The likely shape:
  the Code page gets a **Voice** button on a session; the phone listens and turns speech into text; the bot passes it
  to the dev box agent, which types it into that tmux session; when Claude finishes (a Claude Code `Stop` hook on the
  box), the agent reads the last reply from the session's transcript and the page speaks it. Questions to settle:
  how a turn starts and ends hands-free (tap, wake word, silence), what's read aloud (the summary, not diffs or tool
  output), approvals and questions from Claude, the phone's screen locking in the car (a page loses the mic when
  it's in the background), and cost.
- **M25: speech engines.** Compare the browser's own speech (free, built into Safari and Chrome, uneven quality)
  with hosted ones (Azure AI Speech, since the bots are on Azure, and others) for both directions: accuracy with code
  words, delay, voice quality and cost per hour. Pick one each, with a fallback.
- **M26: speaking into a session.** The dev box agent gets "send this text to session X" (`tmux send-keys`, only into
  sessions it opened), and the Code page gets a push-to-talk view per session that shows what was heard before
  sending, so a bad transcription can be fixed or dropped.
- **M27: hearing the reply.** A `Stop` hook on the dev box marks each finished turn; the agent picks out Claude's last
  message from the transcript, trims it for listening (no code blocks or file lists), and the page speaks it. The
  session keeps running in tmux and Remote Control the whole time, so it can be picked up on screen later.

## Waiting on you

Things only you can do. Each one unblocks or finishes a milestone.

- [ ] **M14:** talk through the second bot's owner's field with them (target roles, where they'd move, pay, which job
  sites and alerts they use), then tell me. Until then their data pack and sources stay as they are.
- [ ] **M13:** turn Adventures on: add `adventures/settings.yaml` to the data pack (copy the sample in
  `samples/data-pack/adventures/`: `cadence_days`, `home`, `with`, `likes`). It's merged but off until then.
- [ ] **M19, M20:** merge #11 (M20 again; see its details). Then redeploy the dev box's infra (`az deployment sub
  create -n consigliere-dev -l eastus2 -f infra/dev.bicep -p infra/dev.bicepparam`) so the bot may put the box to
  sleep, and run `bash deploy/dev.sh setup` to install the new agent (repo details, Stop, idle time, plans).
- [ ] **M21:** when it's built, copy the secret iCal address of your calendar and Josephine's (with her OK) into the
  bot's Key Vault.
- [ ] **M9:** when a real offer comes in, paste it on the pursuit page and check the numbers it reads.
- [ ] **M11:** paste a few full postings on alert-email jobs and see whether the new scores feel right.
