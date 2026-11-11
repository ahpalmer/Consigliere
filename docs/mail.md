# Mail ingestion

Radar reads a **dedicated mailbox** that only receives job mail the owner forwards to it: job alerts (LinkedIn,
Indeed, Glassdoor, ...) become items, and emails about the owner's own applications (confirmations, interview
requests, rejections, offers) update the pipeline. The agent never touches the owner's real inbox.

```
real inbox ──(filters: job mail only, never security mail → forward/redirect)──► dedicated Gmail ──(gmail.readonly)──► EmailSource
                  ◄─(gmail.insert)─── apply kits (placed in the inbox, never sent)
```

## How it works

- `mail.Source` is a Radar source (`ats: email`). Each scan lists recent messages (`Mail:Query`, Spam included with
  `Mail:IncludeSpam`, since forwarded mail sometimes lands there), skips ones already in `ProcessedMessages`, and reads only
  allowed senders, so nothing else reaches the LLM:
  - job sites (`Mail:AllowedSenderDomains`),
  - application and scheduling systems (`Mail:UpdateSenderDomains`: Greenhouse, Lever, Ashby, Workday, Calendly, ...),
  - companies with an open pursuit: a sender domain part equal to the company's name (`careers.google.com` for Google,
    `redhat.com` for Red Hat).

  `Mail:BlockedSenders` (account-security senders: sign-in codes, resets) are never read, whatever the domain. A message
  skipped as "sender not allowed" is checked again on later scans, so a recruiter's email is read once their company
  becomes a pursuit.
- A cheap model reads each allowed message into structured JSON (`internal/mail/prompts/read.md`): the job listings it
  advertises, and what it says about one of the owner's applications. Per-sender parsers would break whenever a
  template changes; this works across senders. The model has no tools; its only output is that JSON.
- **Guardrails:** a listing or link is kept only if its URL appears in the email itself. Email text is treated as
  untrusted data. Nothing is ever replied to, and nothing closes a pursuit without the owner's tap.
- LinkedIn URLs collapse to `/jobs/view/{id}/`, and Glassdoor links are keyed by their listing id (`jl=` or
  `jobListingId=`), so the same job from several alerts is one item.
- Email is an *aggregator* source: when the same company + title exists on a company board, the board item (with its
  full description) wins and the email copy is marked as a duplicate, whichever arrived first.

## Application updates

Each update is matched to an open pursuit by company (name, or the sender's domain) and the closest title, recorded in
its history, and then:

| The email says | What happens |
|---|---|
| Application received | Active → Waiting (follow-ups scheduled). An untracked job is added as a pursuit first. |
| Interview request, scheduling | Back to Active. With a stated time, the interview is set and a Prep button comes with the alert; without one, the alert says to record it with `/interview`. |
| Assessment | Back to Active, alert with the deadline and the link. |
| Offer | Alert with a "Complete as offer" button. |
| Rejection | No alert: the next morning briefing asks whether to file it as rejected. |
| Recruiter outreach | Alert with an `/activate` line to start tracking it. Nothing is created. |
| Anything else | History only. |

**Backfill.** The first time this runs, it reads older mail once (`Mail:BackfillQuery`, 180 days) for updates only:
confirmations move pursuits to Waiting (or add them), everything else just goes into history, and one Telegram
message says what it found. Old interview requests don't reopen anything.

## Setup

1. **Dedicated Gmail account.** In the *real* inbox: Settings → Forwarding and POP/IMAP → add the dedicated address
   (confirm the code it receives). Keep "forward all mail" off: a real inbox holds password resets and sign-in codes,
   and a bot-readable copy of those is an account-takeover risk. Instead, filters forward job mail only:
   - job sites and application systems by sender (`from:linkedin.com`, `from:greenhouse.io`, ...);
   - recruiters at any company by words (`recruiter`, `interview`, `"your application"`, `"this role"`, ...);
   - and on every filter, exclusions for security mail: senders such as `accounts.google.com` and
     `accountprotection.microsoft.com`, and subjects with `password`, `reset`, `verify`, `sign in`, `code`.

   Gmail: *Forward it to* keeps the original sender. Outlook: use **Forward**, not Redirect. Redirected mail fails
   Gmail's sender checks (it claims to be from LinkedIn but comes from Outlook's servers), so it lands in Spam or, for
   senders with strict policies, is rejected outright. A forward comes from your own address, so tell the agent which
   addresses forward to it and it reads each message as from the original sender in the forwarded header:
   ```bash
   go run ./cmd/consigliere secrets set Mail:ForwardedBy:0 you@outlook.com
   ```
   In the dedicated Gmail, a `from:you@outlook.com` filter set to *Never send it to Spam* keeps forwards out of Spam
   (the agent reads Spam too, `Mail:IncludeSpam`, but the inbox is easier to check by eye).
2. **Google Cloud OAuth client** (signed in as the dedicated account): create a project, enable the **Gmail API**,
   configure the OAuth consent screen (External) and **Publish app** to *In production* (in *Testing*, refresh tokens
   expire after 7 days). Create credentials → OAuth client ID → **Desktop app**.
3. **Secrets:**
   ```bash
   go run ./cmd/consigliere secrets set "Mail:ClientId" "<client id>"
   go run ./cmd/consigliere secrets set "Mail:ClientSecret" "<client secret>"
   go run ./cmd/consigliere auth gmail   # browser consent (loopback + PKCE); saves Mail:RefreshToken
   ```
   Google warns the app is unverified; that's expected for a personal app (Advanced → continue).
4. **Watchlist:** add `- { name: Alert emails, ats: email, board: gmail }` to `radar/watchlist.yaml`.

## Alerts for companies with no public board

Some companies (Apple, for one) don't post on Greenhouse, Lever or Ashby, and their careers sites forbid automated
access. Their jobs still reach the agent through alert emails from a job site that lists them:

1. On Glassdoor (or LinkedIn), search jobs for that company with the keywords you want, then turn on a **job alert**
   for the search, sent by email, daily. One alert per search (for example, one for platform and SRE roles and one for
   computer vision).
2. Make sure the real inbox's forwarding filter covers the alert's sender (`from:glassdoor.com`). `glassdoor.com` is
   already in `Mail:AllowedSenderDomains`.
3. Put the company under `company_priority: high` in `radar/preferences.yaml` if its jobs should rank first.

Alerts carry a title, location and a line or two, not the full posting, so their scores are rough. Open the ones worth
it and judge from the posting itself. The agent never visits Glassdoor; the dashboard only links to it (each job and
pursuit page has a **Company research** link to the company's reviews, pay reports and interview questions).

## Apply kits in the inbox

A kit asked for in chat (`/kit`, or the ✍️ Kit button) also lands in the dedicated inbox, from "Consigliere", with the
tailored resume attached. It's *inserted* (`gmail.insert`), not sent: that permission can only add messages to this one
mailbox, so a kit can't reach anyone else. The messages carry an `X-Consigliere` header, and the email source skips
them. If delivery fails, chat says why; the kit itself still arrives in chat.

A sign-in saved before this existed can only read. To add the permission, run `go run ./cmd/consigliere auth gmail`
again, then push the secrets to Key Vault and restart the agent (docs/deploy.md, "Change a secret").

To revoke access: myaccount.google.com/permissions (dedicated account), or delete the forwarding filter.
