# The dev box

A VM where Claude Code works on your repositories while your own computer is off (M16-M19 in the
[roadmap](roadmap.md)). You reach it two ways: Tailscale SSH for a shell, and later (M18) the Claude app, through Claude
Code's Remote Control.

It's kept apart from the bots on purpose: its own resource group, network, Key Vault and identity. Claude Code
sessions run commands and read outside text (web pages, issues, packages), so a session that gets talked into
something must not be able to reach a bot's secrets. The bots' VMs also stay small and always on, while this one can
sleep.

| What | Where | Applied by |
|---|---|---|
| Azure resources: VM, network, Key Vault, identity, nightly shutdown, budget | `infra/dev.bicep`, `infra/dev.bicepparam` | you, with `az deployment sub create` |
| The VM's setup: Tailscale SSH, the `dev` user, git, tmux, gh, Go, Node, .NET, PowerShell, Claude Code | `deploy/dev/setup.sh` | you, with `deploy/dev.sh setup` (safe to rerun) |
| What runs on it: repo sync, the GitHub token helpers, the push guard | `deploy/dev/box/` | copied in by `deploy/dev.sh setup` |
| Which repos | `code/repos.yaml` in your data pack | you, by pushing a change to it |

## How it fits together

- **No public ports.** The network security group has no allow rules, like the bots'. Outbound is open, so Claude,
  GitHub, package registries and the web all work. Tailscale connects out too, and `tailscaled` answers SSH on the
  tailnet only.
- **Tailscale SSH, as `dev`.** You sign in with your tailnet identity: no SSH keys, no passwords. The tailnet policy
  decides who may sign in. `dev` has no sudo, so sessions can't change the machine. Admin goes through
  `deploy/dev.sh setup`, which runs as root through `az vm run-command`.
- **Azure rights: its own vault only.** The VM's identity, `id-consigliere-dev`, can read the dev box's Key Vault and
  nothing else: no bot's resources, no subscription rights. The Azure CLI isn't installed.
- **Tools.** Everything is pinned to a release at least two weeks old with its published hash. Go, Node, the .NET 10
  SDK (plus the .NET 8 runtime) and PowerShell 7 live in `/usr/local`; `go install`, `npm -g` and `dotnet tool` go to
  the user's home. Claude Code uses Anthropic's installer on the stable channel and updates itself. Windows-only
  projects (`net*-windows`) can be edited and compiled here, but run only on Windows.
- **Repos.** `dev-sync.timer` reads `code/repos.yaml` from GitHub's copy of your data pack every hour (and at boot), clones
  missing repos into `~/src/<name>` and fetches the rest. It fast-forwards a repo's default branch only when it's
  checked out with no local changes, and never resets, switches branches or deletes anything.
- **GitHub access: one token.** A fine-grained token for the listed repos only, with read/write on contents and pull
  requests and nothing else. Without the Workflows permission, sessions can't change `.github/workflows`, which deploy
  and reach Azure. It lives in the dev box's Key Vault; git (through `git-credential-keyvault`) and `gh` read it on
  each use, so it's never on disk. Anything running as `dev` can read it too, so its scope and expiry are the limits.
  Commits use your name and your GitHub noreply address.
- **Data repos can't be pushed to `main`.** A bot syncs its data pack's `main` every few minutes, and GitHub's free plan
  can't protect a private repo's branches, so a push there would go live with no review. Repos marked `protect_main`
  get a pre-push hook that refuses `main`: sessions push a branch, and you merge it on GitHub. Code repos rely on
  GitHub's rulesets (pull request + CI) instead.
- **Sleep.** The VM shuts down (deallocated) every night at 03:00 Eastern, so a forgotten one doesn't bill all month.
  The disk and everything on it stay. Start it from the Azure app (Virtual machines → consigliere-dev → Start) or
  `deploy/dev.sh start`. M19 makes the bot wake it on demand and stop it when idle.

## Cost

Pay-as-you-go in East US 2, per month:

| | Always on | Stopped |
|---|---|---|
| VM (`Standard_B2ps_v2`: 2 vCPU, 8 GiB, Arm64, $0.067/hour) | ~$49 | $0 |
| OS disk (64 GiB Standard SSD) | ~$5 | ~$5 |
| Public IP (outbound only) | ~$3.70 | ~$3.70 |
| **Total** | **~$58** | **~$9** |

Four hours a day comes to about $17. The budget emails you at 80% of $60 and when the month's forecast passes it.

## One-time setup

Run these from the repo root in Git Bash, signed in to your **personal** Azure account (`az account show`).

1. **Tailscale policy.** In the admin console → Access controls, add a tag for the dev box and let your devices
   sign in to it as `dev`:
   ```jsonc
   "tagOwners": {
     "tag:dev": ["autogroup:admin"],
   },
   "ssh": [
     // keep any rules already here
     {"action": "accept", "src": ["autogroup:member"], "dst": ["tag:dev"], "users": ["dev"]},
   ],
   ```
   If the policy doesn't allow all traffic between your devices, also allow `tcp:22` from `autogroup:member` to
   `tag:dev`. Use `"action": "check"` instead of `"accept"` to re-confirm in the browser every 12 hours.

2. **Azure resources** (a few minutes):
   ```bash
   export CONSIGLIERE_OWNER_OBJECT_ID=$(az ad signed-in-user show --query id -o tsv)
   export CONSIGLIERE_BUDGET_EMAIL=<where budget alerts go>
   az deployment sub what-if -n consigliere-dev -l eastus2 -f infra/dev.bicep -p infra/dev.bicepparam   # preview
   az deployment sub create  -n consigliere-dev -l eastus2 -f infra/dev.bicep -p infra/dev.bicepparam -o none
   ```

3. **Tailscale auth key.** Admin console → Settings → Keys → *Generate auth key*: not reusable, pre-approved, tagged
   `tag:dev` (a tagged device's key never expires). Save it without echoing it:
   ```bash
   bash deploy/dev.sh tailscale-auth-key
   ```

4. **GitHub token.** GitHub → Settings → Developer settings → Personal access tokens → *Fine-grained tokens* →
   *Generate new token*:
   - Name `consigliere-dev`, expiration 90 days, resource owner you.
   - Repository access: *Only select repositories*, every repo in `code/repos.yaml`.
   - Repository permissions: **Contents** and **Pull requests** read and write. Leave everything else at *No access*
     (Metadata turns on by itself).

   Save it (GitHub checks it and reads its expiry first):
   ```bash
   bash deploy/dev.sh github-token
   ```
   To renew, make a new one the same way and run this again; the dev box uses it on the next push.

5. **Repo list.** Put `code/repos.yaml` in your data pack and push it:
   ```yaml
   repos:
     - { name: Consigliere, url: https://github.com/<you>/Consigliere.git }
     - { name: Consigliere-data, url: https://github.com/<you>/Consigliere-data.git, protect_main: true }
   ```

6. **Set up the VM** (about 10 minutes the first time; rerun any time to update the tools):
   ```bash
   export DEV_PACK_REPO=https://github.com/<you>/<data pack>.git
   export GIT_USER_NAME=<your name>
   export GIT_USER_EMAIL=<id>+<you>@users.noreply.github.com   # GitHub → Settings → Emails
   bash deploy/dev.sh setup
   ```
   It ends with the repos it has and `ready: ssh dev@consigliere-dev.<tailnet>.ts.net`.

7. **Sign in.** With Tailscale on:
   - **PC:** `ssh dev@consigliere-dev` (Windows' built-in OpenSSH works; so does `tailscale ssh dev@consigliere-dev`).
   - **Phone:** an SSH app such as Termius. Host `consigliere-dev`, user `dev`, no password or key.

   Then check: `ls ~/src`, `go version`, `node --version`, `dotnet --info`, `pwsh --version`, `gh auth status`,
   `claude --version`.

## Everyday commands

```bash
bash deploy/dev.sh start   # start it (about a minute)
bash deploy/dev.sh stop    # stop and deallocate: compute stops billing, the disk stays
bash deploy/dev.sh setup   # reapply the setup, e.g. after bumping a pinned version
```

On the dev box, run `dev-sync` to sync right away and see what it does.
