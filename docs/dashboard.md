# The dashboard

A React + TypeScript app (`web/`) served by the agent itself. `npm run build` writes it into `internal/web/dist`, and
`go build` embeds that folder, so the binary (and the container) carry the app. There's no separate web server.

| Page | What it's for |
|---|---|
| Pipeline | To-dos (interviews, thank-yous, follow-ups, ghost checks, next steps), then Active / Waiting / Completed. Add a job the radar didn't find. |
| Pursuit | Checklist (tick and untick), applied / followed up / interview / complete / reopen, notes, history, and drafts: apply kit (with the DOCX), prep pack, thank-you. |
| Options | Every scored job, ranked, with search and tier, category and score filters. Browsing doesn't mark anything as sent, so the Telegram digest is unaffected. |
| Item | One posting: why it scored the way it did, the posting text, 👍/👎, Make active. |
| Status | Kill switch, scoring backlog, Claude usage since start, last run of every source. |

Telegram and the dashboard share everything: a draft started from either one shows up in the dashboard, and a step
ticked in one is ticked in the other.

## Run it

```bash
cd web && npm ci && npm run build && cd ..   # once, and after changing anything in web/
go run ./cmd/consigliere                      # dashboard at http://127.0.0.1:8080
```

For hot reload while working on the app, keep the agent running and start `npm run dev` in `web/`; Vite serves the app
at http://localhost:5173 and forwards `/api` to the agent.

## Settings

| Setting | Default | |
|---|---|---|
| `Web:Listen` | `127.0.0.1:8080` | Empty turns the dashboard off. |
| `Web:Token` | (none) | Sign-in secret. Required whenever `Web:Listen` isn't loopback; the agent refuses to start otherwise. |

```bash
go run ./cmd/consigliere secrets set "Web:Token" "$(openssl rand -base64 32)"
```

## Security

The dashboard can do everything the owner can (except send anything: there is still no submit or send tool), so it's
built to be reachable only privately:

- **Network.** Loopback by default. In AKS there's no ingress: the manifest sets `Web:Listen` to `0.0.0.0:8080` inside the
  pod, `Web:Token` comes from Key Vault, and you reach it with `kubectl port-forward` ([deploy.md](deploy.md)).
- **Sign-in.** With a token set, every API call needs the session cookie (HttpOnly, SameSite=Strict, an HMAC derived from
  the token, so rotating the token signs everyone out) or `Authorization: Bearer <token>`. Wrong tokens cost a second.
- **No token, loopback only.** Without a token the API also checks the `Host` header, so a web page that rebinds its
  DNS to 127.0.0.1 can't use it.
- **Cross-site requests.** Writes must be `application/json`, which a cross-site form can't send.
- **Headers.** A strict Content-Security-Policy (`default-src 'self'`, no framing), `nosniff`, and `no-referrer`, so
  opening a posting never tells the employer's site where you came from.
