# The data pack

The data pack is the small set of things that are genuinely personal: who the owner is and what they're
looking for. It's a folder outside this repository, versioned in its own private git repo. The host reads it
from `DataPack:Path`.

```
<data-pack>/
  profile/    who the owner is (see below)
  radar/      preferences.yaml (filters, tiers), watchlist.yaml (sources to poll)
  db/         SQLite database (gitignored in the pack; backed up to blob storage)
```

| `profile/` file | Used by | What it is |
|---|---|---|
| `summary.md`, `resume.master.md` | scoring, kit, prep | Short summary and the full resume as text |
| `bullets.yaml` | `/kit` | The resume as data: header, skills, sections with min/max, tagged bullets by id. The kit picks and orders bullets; it never writes new ones. `draft: true` bullets are never used. |
| `stories.md` | `/prep` | STAR stories with themes |
| `wins.md` | the owner | Raw log of wins; source material for bullets and stories |
| `networks.yaml` | `/kit` | Veteran groups per company, plus general programs |
| `connections.csv` | `/kit` | LinkedIn's Connections export, unmodified. Read locally to list people you know at a company; never sent to the model. |

Scoring reads only `summary.md` and `resume.master.md` (or `about.md`), so editing stories or the bullet bank
doesn't make every score stale.

Everything else, including prompts, lives in this repo:

- **Prompts** are embedded in the binary (`internal/*/prompts/*.md`), versioned and reviewed like code.
  Personal details come in through placeholders, e.g. `{{owner}}` ← `Agent:OwnerName` (secrets file / Key Vault).
- **Settings** (schedules, models, limits) are in `config/consigliere.json`. Secrets are in the secrets file / Key Vault.

Why the pack is separate:

- **No accidental commits.** The pack isn't inside this working tree, so `git add -A` here can't pick it up.
  `.gitignore` also blocks `*.db` and any `data-pack` folder other than `samples/`.
- **Hot edits.** Pack files are re-read on use, so you can edit preferences without redeploying.

All reads go through `datapack.Pack.Resolve`, which rejects paths that escape the pack root.
