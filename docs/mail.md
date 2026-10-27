# Mail ingestion

Radar can read job-alert emails (LinkedIn, Indeed, Glassdoor, ...) from a **dedicated mailbox** that only ever
receives forwarded alerts. The agent never touches the owner's real inbox.

```
real inbox ──(filter: from:jobalerts-noreply@linkedin.com → forward)──► dedicated Gmail ──(gmail.readonly)──► EmailSource
```

## How it works

- `EmailSource` is a Radar source (`ats: email`). Each scan lists recent messages (`Mail:Query`), skips ones already
  in `ProcessedMessages`, and ignores senders outside `Mail:AllowedSenderDomains`, so they never reach the LLM.
- A cheap model extracts listings as structured JSON (`Prompts/extract.md`). Per-sender HTML parsers would break
  whenever a template changes; this works across senders.
- **Guardrail:** a listing is kept only if its URL appears in the email itself. Email text is treated as untrusted data.
- LinkedIn URLs collapse to `/jobs/view/{id}/`, so the same job from several alerts is one item.
- Email is an *aggregator* source: when the same company + title exists on a company board, the board item (with its
  full description) wins and the email copy is marked as a duplicate, whichever arrived first.

## Setup

1. **Dedicated Gmail account.** In the *real* inbox: Settings → Forwarding and POP/IMAP → add the dedicated address
   (confirm the code it receives). Keep "forward all mail" off. Create a filter `from:(jobalerts-noreply@linkedin.com)`
   → *Forward it to* the dedicated address.
2. **Google Cloud OAuth client** (signed in as the dedicated account): create a project, enable the **Gmail API**,
   configure the OAuth consent screen (External) and **Publish app** to *In production* (in *Testing*, refresh tokens
   expire after 7 days). Create credentials → OAuth client ID → **Desktop app**.
3. **Secrets:**
   ```bash
   dotnet user-secrets --project src/Consigliere.Host set "Mail:ClientId" "<client id>"
   dotnet user-secrets --project src/Consigliere.Host set "Mail:ClientSecret" "<client secret>"
   dotnet run --project src/Consigliere.Host -- auth gmail   # browser consent; saves Mail:RefreshToken
   ```
   Google warns the app is unverified; that's expected for a personal app (Advanced → continue).
4. **Watchlist:** add `- { name: Alert emails, ats: email, board: gmail }` to `radar/watchlist.yaml`.

To revoke access: myaccount.google.com/permissions (dedicated account), or delete the forwarding filter.
