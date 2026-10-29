import { api } from "../api";
import { Badge, ErrorNote, Panel } from "../components";
import { dateTime, tokens } from "../format";
import { useAction, useLoad } from "../hooks";

export function StatusPage() {
  const status = useLoad(() => api.status(), [], { poll: true, pollMs: 15_000 });
  const { run, busy, error } = useAction();

  if (!status.data) return status.error ? <ErrorNote message={status.error} /> : <p className="muted">Loading…</p>;
  const s = status.data;

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Status</h1>
          <p className="muted">Running since {dateTime(s.startedAt)}</p>
        </div>
      </div>
      <div className="stack">
        <Panel title="Kill switch">
          <div className="row wrap">
            {s.paused ? <Badge tone="warn">Paused</Badge> : <Badge tone="good">Running</Badge>}
            <span className="muted">
              {s.paused ? "No scans, scoring, drafts or chat replies from Claude." : "Scans hourly, scores what's new, writes drafts when asked."}
            </span>
            <button
              className={s.paused ? "btn primary" : "btn danger"}
              disabled={busy}
              onClick={async () => (await run(() => (s.paused ? api.resume() : api.pause()))) && status.reload()}
            >
              {s.paused ? "Resume" : "Pause everything"}
            </button>
          </div>
          <ErrorNote message={error} />
        </Panel>

        <Panel title="Scoring">
          <p>
            {s.scoring.running ? "Scoring now. " : ""}
            {s.scoring.pending > 0 ? `${s.scoring.pending} job(s) waiting for a score.` : "Everything is scored."}
          </p>
        </Panel>

        <Panel title="Claude usage since start">
          {s.usage.length === 0 ? (
            <p className="muted">No calls yet.</p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Model</th>
                    <th className="num">Calls</th>
                    <th className="num">Input</th>
                    <th className="num">Cached</th>
                    <th className="num">Output</th>
                  </tr>
                </thead>
                <tbody>
                  {s.usage.map((u) => (
                    <tr key={u.model}>
                      <td>{u.model}</td>
                      <td className="num">{u.calls}</td>
                      <td className="num">{tokens(u.inputTokens)}</td>
                      <td className="num">{tokens(u.cachedTokens)}</td>
                      <td className="num">{tokens(u.outputTokens)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel title="Sources">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Source</th>
                  <th className="hide-narrow">Board</th>
                  <th>Last run</th>
                  <th className="num">Jobs</th>
                  <th className="num">New</th>
                </tr>
              </thead>
              <tbody>
                {s.sources.map((src) => (
                  <tr key={`${src.ats}/${src.board}`} className={src.error ? "failed" : ""}>
                    <td>
                      {src.name}
                      {src.priority && src.priority !== "normal" && <span className="muted small"> · {src.priority}</span>}
                    </td>
                    <td className="muted hide-narrow">
                      {src.ats}/{src.board}
                    </td>
                    <td>{src.error ? <span className="error-text" title={src.error}>Failed: {src.error}</span> : src.lastRun ? dateTime(src.lastRun) : "never"}</td>
                    <td className="num">{src.fetched}</td>
                    <td className="num">{src.new}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      </div>
    </>
  );
}
