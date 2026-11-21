import { Fragment, type ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { api, type CodePlan as Plan } from "../api";
import { Badge, ErrorNote, Panel } from "../components";
import { dateTime } from "../format";
import { useLoad } from "../hooks";

/** Just enough Markdown for a roadmap line: **bold**, `code`, and [links](...) shown as their text. */
export function inline(text: string): ReactNode[] {
  return text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g).map((part, i) => {
    if (part.startsWith("**") && part.endsWith("**") && part.length > 4) return <strong key={i}>{part.slice(2, -2)}</strong>;
    if (part.startsWith("`") && part.endsWith("`") && part.length > 2) return <code key={i}>{part.slice(1, -1)}</code>;
    const link = /^\[([^\]]+)\]\([^)]+\)$/.exec(part);
    if (link) return <Fragment key={i}>{link[1]}</Fragment>;
    return <Fragment key={i}>{part}</Fragment>;
  });
}

function tone(status: string): "good" | "warn" | "accent" | "neutral" | "bad" {
  const s = status.toLowerCase();
  if (s.startsWith("done")) return "good";
  if (s.startsWith("blocked")) return "bad";
  if (s.startsWith("built") || s.includes("in progress") || s.includes("phase")) return "accent";
  if (s.startsWith("planned") || s.startsWith("not started")) return "neutral";
  return "warn";
}

/** The repo's plan (M20): open milestones first with their details, done ones folded away. */
export function CodePlan() {
  const repo = useParams().repo ?? "";
  const plan = useLoad(() => api.codePlan(repo), [repo]);
  const p = plan.data;

  return (
    <>
      <Link to="/code" className="back">
        ← Code
      </Link>
      <div className="page-head">
        <div>
          <p className="eyebrow">{repo}</p>
          <h1>Plan</h1>
          {p && (
            <p className="muted small">
              {p.path} on {p.branch}
              {p.asOf && `, as of ${dateTime(p.asOf)}`}
              {p.branches.length > 0 && ` · also ${p.branches.join(", ")}`}
            </p>
          )}
        </div>
      </div>
      <ErrorNote message={plan.error} />
      {!p && !plan.error && <p className="muted">Reading it from the dev box…</p>}
      {p?.stale && <p className="banner warn">The dev box is asleep, so this is the copy read {dateTime(p.readAt)}.</p>}
      {p && !p.found && <p className="empty">This repo has no {p.path}. Add one, or point `plan:` at its plan in code/repos.yaml.</p>}
      {p?.found && (p.milestones.length > 0 ? <Milestones plan={p} /> : <Panel title={p.path}>{<p className="prewrap">{p.text}</p>}</Panel>)}
    </>
  );
}

function Milestones({ plan }: { plan: Plan }) {
  const open = plan.milestones.filter((m) => m.open);
  const done = plan.milestones.filter((m) => !m.open);
  return (
    <div className="stack">
      {open.length === 0 && <p className="empty">Everything in the plan is done.</p>}
      {open.map((m) => (
        <section key={m.id} className="panel milestone">
          <div className="row wrap">
            <h2 className="grow">
              <span className="muted">{m.id}</span> {inline(m.title)}
            </h2>
            <Badge tone={tone(m.status)}>{inline(m.status)}</Badge>
          </div>
          {m.onBranches.map((b) => (
            <p key={b.branch} className="muted small">
              {b.new ? "Only on " : "On "}
              <code>{b.branch}</code>: {inline(b.status)}
            </p>
          ))}
          {m.details && <p className="prewrap">{inline(m.details)}</p>}
        </section>
      ))}
      {done.length > 0 && (
        <details className="panel">
          <summary>
            {done.length} done
          </summary>
          <ul className="bullets">
            {done.map((m) => (
              <li key={m.id}>
                <strong>{m.id}</strong> {inline(m.title)} <span className="muted">({inline(m.status)})</span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
