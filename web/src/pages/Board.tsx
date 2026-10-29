import { Children, useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, type Pursuit, type Summary, type Todo } from "../api";
import { Badge, ErrorNote, Fit, Progress } from "../components";
import { completion, dateTime, relativeDay } from "../format";
import { useAction, useLoad } from "../hooks";

export function Board() {
  const board = useLoad(
    async () => {
      const [summary, active, waiting, completed] = await Promise.all([
        api.summary(),
        api.pursuits("active"),
        api.pursuits("waiting"),
        api.pursuits("completed"),
      ]);
      return { summary, active, waiting, completed };
    },
    [],
    { poll: true, pollMs: 30_000 },
  );
  const [showAll, setShowAll] = useState(false);
  const [adding, setAdding] = useState(false);

  if (!board.data) return board.error ? <ErrorNote message={board.error} /> : <p className="muted">Loading…</p>;
  const { summary, active, waiting, completed } = board.data;
  const recent = showAll ? completed : completed.slice(0, 8);

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Pipeline</h1>
          <Funnel summary={summary} />
        </div>
        <button className="btn primary" onClick={() => setAdding((a) => !a)}>
          {adding ? "Cancel" : "Add a job"}
        </button>
      </div>
      {summary.paused && (
        <p className="banner warn">
          Paused: no scans, scoring or drafts until you resume. <Link to="/status">Status</Link>
        </p>
      )}
      {adding && <AddJob onDone={() => setAdding(false)} />}
      <Today todos={summary.todos} />

      <div className="columns">
        <Column
          title="Active"
          count={summary.softCap ? `${active.length} / ${summary.softCap}` : String(active.length)}
          over={summary.softCap > 0 && active.length > summary.softCap}
          empty={
            <>
              Nothing active. Pick something from <Link to="/options">Options</Link>.
            </>
          }
        >
          {active.map((p) => (
            <PursuitCard key={p.id} p={p} />
          ))}
        </Column>
        <Column title="Waiting" count={String(waiting.length)} empty="Nothing waiting on a reply.">
          {waiting.map((p) => (
            <PursuitCard key={p.id} p={p} />
          ))}
        </Column>
        <Column title="Completed" count={String(completed.length)} empty="Nothing completed yet.">
          {recent.map((p) => (
            <PursuitCard key={p.id} p={p} />
          ))}
          {completed.length > recent.length && (
            <button className="btn ghost small" onClick={() => setShowAll(true)}>
              Show {completed.length - recent.length} more
            </button>
          )}
        </Column>
      </div>
    </>
  );
}

function Funnel({ summary }: { summary: Summary }) {
  const { applied, interviewed, offers } = summary.funnel;
  return (
    <p className="funnel">
      <span>
        <b>{applied}</b> applied
      </span>
      <span aria-hidden="true">→</span>
      <span>
        <b>{interviewed}</b> interviewed
      </span>
      <span aria-hidden="true">→</span>
      <span>
        <b>{offers}</b> {offers === 1 ? "offer" : "offers"}
      </span>
    </p>
  );
}

const todoText: Record<Todo["kind"], (t: Todo) => string> = {
  interview: (t) => `Interview ${dateTime(t.pursuit.interviewAt!)}${t.prepDone ? "" : " · prep not done"}`,
  thanks: () => "Send a thank-you note",
  followup: (t) => `Follow up${t.pursuit.appliedAt ? ` · applied ${relativeDay(t.pursuit.appliedAt)}` : ""}`,
  ghost: () => "No reply in a while. Ghosted?",
  next: (t) => t.label,
};

const todoIcon: Record<Todo["kind"], string> = { interview: "🎤", thanks: "✉️", followup: "📬", ghost: "👻", next: "→" };

function Today({ todos }: { todos: Todo[] }) {
  if (todos.length === 0) return null;
  return (
    <section className="today">
      <h2>To do</h2>
      <ul>
        {todos.map((t) => (
          <li key={`${t.kind}-${t.pursuit.id}`} className={`todo todo-${t.kind}`}>
            <span className="todo-icon" aria-hidden="true">
              {todoIcon[t.kind]}
            </span>
            <Link to={`/pursuits/${t.pursuit.id}`}>
              <b>{t.pursuit.company}</b> · {t.pursuit.title}
            </Link>
            <span className="todo-what">{todoText[t.kind](t)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

function Column(props: { title: string; count: string; over?: boolean; empty: ReactNode; children: ReactNode }) {
  const items = Children.toArray(props.children);
  return (
    <section className="column">
      <h2>
        {props.title}{" "}
        <span className={props.over ? "count over" : "count"} title={props.over ? "Over the soft cap" : undefined}>
          {props.count}
        </span>
      </h2>
      {items.length ? <div className="stack">{items}</div> : <p className="empty">{props.empty}</p>}
    </section>
  );
}

function PursuitCard({ p }: { p: Pursuit }) {
  const done = p.steps.filter((s) => s.doneAt).length;
  return (
    <Link to={`/pursuits/${p.id}`} className={`pcard stage-${p.stage}`}>
      <div className="pcard-top">
        <span className="pcard-company">{p.company}</span>
        {p.fit !== null && <Fit fit={p.fit} />}
      </div>
      <div className="pcard-title">{p.title}</div>
      {p.stage === "active" && (
        <>
          <Progress done={done} total={p.steps.length} />
          <div className="pcard-meta">
            {p.interviewAt ? <span>🎤 {dateTime(p.interviewAt)}</span> : p.nextStep ? <span>Next: {p.nextStep}</span> : <span>All steps done</span>}
          </div>
        </>
      )}
      {p.stage === "waiting" && (
        <div className="pcard-meta">
          {p.appliedAt && <span>Applied {relativeDay(p.appliedAt)}</span>}
          {p.ghostCheckDue ? <Badge tone="warn">Ghosted?</Badge> : p.followUpDue && <Badge tone="accent">Follow up</Badge>}
        </div>
      )}
      {p.stage === "completed" && (
        <div className="pcard-meta">
          <Badge tone={p.outcome === "offer" ? "good" : "neutral"}>{completion(p.outcome, p.stageReached)}</Badge>
          {p.completedAt && <span>{relativeDay(p.completedAt)}</span>}
        </div>
      )}
      <span className="pcard-ref">{p.ref}</span>
    </Link>
  );
}

function AddJob({ onDone }: { onDone: () => void }) {
  const [url, setUrl] = useState("");
  const [company, setCompany] = useState("");
  const [title, setTitle] = useState("");
  const { run, busy, error } = useAction();
  const navigate = useNavigate();

  return (
    <form
      className="card add-job"
      onSubmit={async (e) => {
        e.preventDefault();
        const res = await run(() => api.activateManual(url.trim(), company.trim(), title.trim()));
        if (res) {
          onDone();
          navigate(`/pursuits/${res.pursuit.id}`, { state: { warning: res.warning } });
        }
      }}
    >
      <p className="muted small">For a job the radar didn't find. Jobs it found have a "Make active" button in Options.</p>
      <div className="row wrap">
        <label className="field grow">
          <span>Company</span>
          <input value={company} onChange={(e) => setCompany(e.target.value)} required />
        </label>
        <label className="field grow">
          <span>Title</span>
          <input value={title} onChange={(e) => setTitle(e.target.value)} required />
        </label>
      </div>
      <label className="field">
        <span>Posting link (optional)</span>
        <input type="url" placeholder="https://" value={url} onChange={(e) => setUrl(e.target.value)} />
      </label>
      <ErrorNote message={error} />
      <button className="btn primary" disabled={busy}>
        Make active
      </button>
    </form>
  );
}
