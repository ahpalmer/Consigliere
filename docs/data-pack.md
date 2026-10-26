# The data pack

Everything personal the agent knows lives in a **data pack**: a folder outside this repository,
versioned in its own private git repo. The host reads it from `DataPack:Path`.

```
<data-pack>/
  profile/    long-form background, tagged bullet bank, wins log, story bank
  radar/      preferences.yaml (filters, tiers), watchlist.yaml (sources to poll)
  prompts/    system.md and task prompts
  db/         SQLite database (gitignored in the pack; backed up to blob storage)
```

Why it's separate:

- **No accidental commits.** The pack isn't inside this working tree, so `git add -A` here can't pick it up.
  `.gitignore` also blocks `*.db` and any `data-pack` folder other than `samples/`.
- **Prompts are data.** The system and task prompts carry personal context, so they live in the pack
  and the public code stays generic.
- **Hot edits.** Prompts are re-read on use, so you can edit them without redeploying.

All reads go through `DataPack.Resolve`, which rejects paths that escape the pack root.
