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
| The VM's setup: Tailscale SSH, the `dev` user, git, tmux, gh, Go, Node, Claude Code | `deploy/dev/setup.sh` | you, with `deploy/dev.sh setup` (safe to rerun) |

## How it fits together

- **No public ports.** The network security group has no allow rules, like the bots'. Outbound is open, so Claude,
  GitHub, package registries and the web all work. Tailscale connects out too, and `tailscaled` answers SSH on the
  tailnet only.
- **Tailscale SSH, as `dev`.** You sign in with your tailnet identity: no SSH keys, no passwords. The tailnet policy
  decides who may sign in. `dev` has no sudo, so sessions can't change the machine. Admin goes through
  `deploy/dev.sh setup`, which runs as root through `az vm run-command`.
- **Azure rights: its own vault only.** The VM's identity, `id-consigliere-dev`, can read the dev box's Key Vault and
  nothing else: no bot's resources, no subscription rights. The Azure CLI isn't installed.
- **Tools.** Everything is pinned to a release at least two weeks old with its published SHA-256. Go and Node live in
  `/usr/local`; `go install` and `npm -g` go to the user's home. Claude Code uses Anthropic's installer on the stable
  channel and updates itself.
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

4. **Set up the VM** (about 5 minutes; rerun any time to update the tools):
   ```bash
   bash deploy/dev.sh setup
   ```
   It ends with `ready: ssh dev@consigliere-dev.<tailnet>.ts.net`.

5. **Sign in.** With Tailscale on:
   - **PC:** `ssh dev@consigliere-dev` (Windows' built-in OpenSSH works; so does `tailscale ssh dev@consigliere-dev`).
   - **Phone:** an SSH app such as Termius. Host `consigliere-dev`, user `dev`, no password or key.

   Then check the tools: `go version`, `node --version`, `gh --version`, `claude --version`.

## Everyday commands

```bash
bash deploy/dev.sh start   # start it (about a minute)
bash deploy/dev.sh stop    # stop and deallocate: compute stops billing, the disk stays
bash deploy/dev.sh setup   # reapply the setup, e.g. after bumping a pinned version
```
