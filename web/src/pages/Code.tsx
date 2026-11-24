import { useEffect, useState } from "react";
import { api, type CodeRepo, type DevBoxState } from "../api";
import { Badge, ErrorNote, External, Panel } from "../components";
import { dateTime } from "../format";
import { useAction, useLoad } from "../hooks";

type Mode = "continue" | "new";

const devBox: Record<DevBoxState, { tone: "good" | "warn" | "bad" | "neutral"; label: string; note: string }> = {
  running: { tone: "good", label: "Running", note: "Sessions open in a few seconds." },
  booting: { tone: "warn", label: "Booting", note: "Almost up." },
  starting: { tone: "warn", label: "Waking", note: "About a minute." },
  asleep: { tone: "neutral", label: "Asleep", note: "Either button wakes it first (about a minute)." },
  stopping: { tone: "warn", label: "Stopping", note: "Wait until it's asleep, then wake it." },
  deallocating: { tone: "warn", label: "Stopping", note: "Wait until it's asleep, then wake it." },
  unknown: { tone: "bad", label: "Unknown", note: "" },
};

/** Claude Code on the dev box: a terminal per session, each reachable from the Claude app (docs/dev-box.md). */
export function Code() {
  // Poll every few seconds while something is changing (the VM waking, a session waiting for it), else every 15.
  const [changing, setChanging] = useState(false);
  const status = useLoad(() => api.code(), [], { poll: true, pollMs: changing ? 4_000 : 15_000 });
  const data = status.data;
  useEffect(
    () => setChanging(!!data && (!["running", "asleep"].includes(data.vm) || data.repos.some((r) => r.waiting && !r.error))),
    [data],
  );

  if (!data) return status.error ? <ErrorNote message={status.error} /> : <p className="muted">Loading…</p>;
  const box = devBox[data.vm] ?? devBox.unknown;

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Code</h1>
          <p className="muted">Claude Code on the dev box. Open a session here, then work in it from the Claude app → Code.</p>
        </div>
      </div>
      <div className="stack">
        <Panel title="Dev box">
          <div className="row wrap">
            <Badge tone={box.tone}>{box.label}</Badge>
            <span className="muted">{box.note}</span>
          </div>
          <ErrorNote message={data.vmError} />
        </Panel>
        <Panel title="Repos">
          <ul className="repo-list">
            {data.repos.map((repo) => (
              <RepoRow key={repo.name} repo={repo} onChange={() => void status.reload()} />
            ))}
          </ul>
        </Panel>
      </div>
    </>
  );
}

/** Branch, uncommitted changes, commits to push or pull, the last commit, and open PRs. */
function RepoFacts({ repo }: { repo: CodeRepo }) {
  const facts = [repo.branch];
  if (repo.changes > 0) facts.push(`${repo.changes} changed`);
  if (repo.ahead) facts.push(`↑${repo.ahead} to push`);
  if (repo.behind) facts.push(`↓${repo.behind} behind`);
  return (
    <>
      <span className="repo-note muted">
        {facts.join(" · ")}
        {repo.lastCommit && ` · last commit ${dateTime(repo.lastCommit.at)}: ${repo.lastCommit.subject}`}
      </span>
      {repo.prs && repo.prs.length > 0 && (
        <span className="repo-note row wrap">
          {repo.prs.map((p) => (
            <External key={p.number} href={p.url}>
              #{p.number} {p.title}
              {p.draft && " (draft)"}
            </External>
          ))}
        </span>
      )}
    </>
  );
}

function RepoRow({ repo, onChange }: { repo: CodeRepo; onChange: () => void }) {
  const { run, busy, error } = useAction();
  const [note, setNote] = useState("");
  const active = repo.sessions.length > 0;

  const start = async (mode: Mode) => {
    setNote("");
    const res = await run(() => api.startCodeSession(repo.name, mode));
    if (res) {
      setNote(
        res.waking ? "Waking the dev box. The session opens in about a minute." : `Opened ${res.session}. It's in the Claude app → Code.`,
      );
    }
    onChange();
  };

  return (
    <li>
      <Badge tone={active ? "good" : "bad"}>● {active ? "Active" : "Inactive"}</Badge>
      <span className="repo-name">{repo.name}</span>
      <span className="row">
        <button className="btn small primary" disabled={busy || !!(repo.waiting && !repo.error)} onClick={() => void start("continue")}>
          Continue Coding
        </button>
        <button className="btn small" disabled={busy || !!(repo.waiting && !repo.error)} onClick={() => void start("new")}>
          New Session
        </button>
      </span>
      {repo.known && <RepoFacts repo={repo} />}
      {repo.sessions.map((s) => (
        <span key={s.name} className="repo-note row">
          <span className="muted">
            {s.name} (since {dateTime(s.startedAt)})
          </span>
          <button
            className="btn small ghost"
            disabled={busy}
            onClick={async () => {
              if (confirm(`Stop ${s.name}? Its conversation is kept for the next Continue Coding.`) && (await run(() => api.stopCodeSession(s.name)))) {
                setNote(`Stopped ${s.name}.`);
                onChange();
              }
            }}
          >
            Stop
          </button>
        </span>
      ))}
      {repo.waiting && !repo.error && (
        <span className="repo-note muted">Waiting for the dev box, then {repo.waiting === "new" ? "a new session" : "continuing"}…</span>
      )}
      {repo.error && <span className="repo-note error-text">{repo.error}</span>}
      {note && !repo.waiting && <span className="repo-note muted">{note}</span>}
      {error && <span className="repo-note error-text">{error}</span>}
    </li>
  );
}
