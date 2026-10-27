# The data pack

The data pack is the small set of things that are genuinely personal: who the owner is and what they're
looking for. It's a folder outside this repository, versioned in its own private git repo. The host reads it
from `DataPack:Path`.

```
<data-pack>/
  profile/    long-form background, tagged bullet bank, wins log, story bank
  radar/      preferences.yaml (filters, tiers), watchlist.yaml (sources to poll)
  db/         SQLite database (gitignored in the pack; backed up to blob storage)
```

Everything else, including prompts, lives in this repo:

- **Prompts** are embedded in the binary (`internal/*/prompts/*.md`), versioned and reviewed like code.
  Personal details come in through placeholders, e.g. `{{owner}}` ← `Agent:OwnerName` (secrets file / Key Vault).
- **Settings** (schedules, models, limits) are in `config/consigliere.json`. Secrets are in the secrets file / Key Vault.

Why the pack is separate:

- **No accidental commits.** The pack isn't inside this working tree, so `git add -A` here can't pick it up.
  `.gitignore` also blocks `*.db` and any `data-pack` folder other than `samples/`.
- **Hot edits.** Pack files are re-read on use, so you can edit preferences without redeploying.

All reads go through `datapack.Pack.Resolve`, which rejects paths that escape the pack root.
