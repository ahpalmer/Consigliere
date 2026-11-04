import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, type ApplicantField, type Suggestion } from "../api";
import { CopyButton, ErrorNote, External, Fit, Panel } from "../components";
import { relativeDay } from "../format";
import { useAction, useLoad } from "../hooks";

export function Suggested() {
  // While an application is being prepared, check back every few seconds.
  const page = useLoad(api.suggestions, [], { poll: (d) => d?.medium.some((s) => s.kit?.status === "pending") ?? false });
  const data = page.data;

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Suggested</h1>
          <p className="muted">
            Jobs from Options that aren't in your pipeline yet. Strong matches are yours to work on; for the rest, Consigliere
            prepares the application and you send it.
          </p>
        </div>
      </div>
      <ErrorNote message={page.error} />
      {!data && !page.error && <p className="muted">Loading…</p>}
      {data && (
        <div className="stack">
          <Panel title={`Make these active (${data.high.length})`}>
            <p className="muted small">Score {data.highMinScore}+ (priority companies count their bonus). Worth your own time: tailor, find people, reach out.</p>
            {data.high.length === 0 ? (
              <p className="empty">No strong matches right now.</p>
            ) : (
              <ul className="options">
                {data.high.map((s) => (
                  <HighRow key={s.id} s={s} onChange={page.reload} />
                ))}
              </ul>
            )}
          </Panel>

          <Panel title={`Ready to apply (${data.medium.length})`}>
            <p className="muted small">
              Score {data.mediumMinScore}–{data.highMinScore - 1}. Each gets a tailored resume and drafted answers (the best few every morning, or
              press Prepare). Application forms can't be filled in from a link, so open the form, then paste from here; your browser's autofill
              handles name, email and phone.
            </p>
            <YourDetails fields={data.applicant} />
            {data.medium.length === 0 ? (
              <p className="empty">Nothing in this range right now.</p>
            ) : (
              <ul className="apply-list">
                {data.medium.map((s) => (
                  <ApplyCard key={s.id} s={s} onChange={page.reload} />
                ))}
              </ul>
            )}
          </Panel>
        </div>
      )}
    </>
  );
}

function Summary({ s }: { s: Suggestion }) {
  return (
    <>
      <Fit fit={s.rankScore} title={s.rankScore !== s.fit ? `Fit ${s.fit}, plus ${s.rankScore - (s.fit ?? 0)} for a priority company` : undefined} />
      <div className="option-main">
        <Link to={`/items/${s.id}`} className="option-title">
          {s.title}
        </Link>
        <div className="option-meta">
          <b>
            {s.priority === "high" && <span title="Priority company">★ </span>}
            {s.company}
          </b>
          {s.locations && <span>{s.locations}</span>}
          {s.comp && <span>{s.comp}</span>}
          <span>{relativeDay(s.postedAt ?? s.firstSeenAt)}</span>
        </div>
        {s.summary && <p className="option-summary">{s.summary}</p>}
      </div>
    </>
  );
}

/** Moves the job into Active and opens its pursuit. */
function ActivateButton({ id, label, primary, run, busy }: { id: number; label: string; primary?: boolean; run: ReturnType<typeof useAction>["run"]; busy: boolean }) {
  const navigate = useNavigate();
  return (
    <button
      className={primary ? "btn primary small" : "btn small"}
      disabled={busy}
      onClick={async () => {
        const res = await run(() => api.activateItem(id));
        if (res) navigate(`/pursuits/${res.pursuit.id}`, { state: { warning: res.warning } });
      }}
    >
      🎯 {label}
    </button>
  );
}

/** The job's page has the cover letter writer (instructions box, Word download). */
function CoverLink({ id }: { id: number }) {
  return (
    <Link className="btn small" to={`/items/${id}`}>
      📝 Cover letter
    </Link>
  );
}

function HighRow({ s, onChange }: { s: Suggestion; onChange: () => Promise<void> }) {
  const { run, busy, error } = useAction();
  return (
    <li className="option">
      <Summary s={s} />
      <div className="item-actions">
        <ActivateButton id={s.id} label="Make active" primary run={run} busy={busy} />
        <CoverLink id={s.id} />
        <div className="row">
          <button className="btn ghost small" disabled={busy} onClick={async () => (await run(() => api.feedback(s.id, -1))) && onChange()}>
            Not for me
          </button>
          <External href={s.url}>
            <span className="icon-btn" title="Open posting">
              ↗
            </span>
          </External>
        </div>
        <ErrorNote message={error} />
      </div>
    </li>
  );
}

function ApplyCard({ s, onChange }: { s: Suggestion; onChange: () => Promise<void> }) {
  const { run, busy, error } = useAction();
  const [open, setOpen] = useState(false);
  const [applied, setApplied] = useState("");
  const kit = s.kit;
  // The suggestion list carries the kit without its long text; the answers are all the card needs.
  const prepare = async () => (await run(() => api.prepareKit(s.id))) && onChange();

  return (
    <li className="apply-card">
      <div className="option">
        <Summary s={s} />
        <div className="item-actions">
          {!kit && (
            <button className="btn primary small" disabled={busy} onClick={prepare}>
              Prepare application
            </button>
          )}
          {kit?.status === "pending" && <span className="muted small">Preparing… about a minute</span>}
          {kit?.status === "failed" && (
            <button className="btn small" disabled={busy} onClick={prepare} title={kit.error}>
              Failed: try again
            </button>
          )}
          <ActivateButton id={s.id} label="Add to active" run={run} busy={busy} />
          <CoverLink id={s.id} />
          <button className="btn ghost small" disabled={busy} onClick={async () => (await run(() => api.feedback(s.id, -1))) && onChange()}>
            Not for me
          </button>
          <ErrorNote message={error} />
        </div>
      </div>

      {kit?.status === "ready" && (
        <div className="apply-steps">
          <ol className="steps">
            <li>
              {kit.fileName ? (
                <a className="btn small" href={api.draftFileUrl(kit.id)} download={kit.fileName}>
                  ⬇ Download tailored resume
                </a>
              ) : (
                <span className="muted small">No resume: add profile/bullets.yaml to the data pack.</span>
              )}
            </li>
            <li>
              <External href={s.applyUrl}>
                <span className="btn primary small">Open the application ↗</span>
              </External>
            </li>
            <li>
              <button className="btn small" onClick={() => setOpen(!open)} aria-expanded={open}>
                {open ? "Hide answers" : `Answers (${kit.answers.length})`}
              </button>
            </li>
            <li>
              {applied ? (
                <span className="muted small">📨 {applied}</span>
              ) : (
                <button
                  className="btn small"
                  disabled={busy}
                  onClick={async () => {
                    const p = await run(() => api.appliedItem(s.id));
                    if (p) {
                      setApplied(`In Waiting as ${p.ref}. Follow-ups are scheduled.`);
                      setTimeout(() => void onChange(), 2500);
                    }
                  }}
                >
                  📨 I applied
                </button>
              )}
            </li>
          </ol>
          {open && (
            <ul className="answers">
              {kit.answers.length === 0 && <li className="muted small">No drafted answers in this kit. Prepare it again for some.</li>}
              {kit.answers.map((a) => (
                <li key={a.label}>
                  <div className="row">
                    <b className="grow">{a.label}</b>
                    <CopyButton text={a.text} />
                  </div>
                  <p className="prewrap">{a.text}</p>
                </li>
              ))}
              <li>
                <FullKit id={kit.id} />
              </li>
            </ul>
          )}
        </div>
      )}
    </li>
  );
}

/** The whole kit (focus, gaps, people you know there, outreach drafts), fetched when opened. */
function FullKit({ id }: { id: number }) {
  const [open, setOpen] = useState(false);
  const full = useLoad(async () => (open ? api.draft(id) : undefined), [open, id]);
  return (
    <>
      <button className="btn ghost small" onClick={() => setOpen(!open)} aria-expanded={open}>
        {open ? "Hide the full kit" : "Show the full kit: focus, gaps, people you know there, outreach drafts"}
      </button>
      {open && (full.data ? <pre className="draft-text">{full.data.text}</pre> : <ErrorNote message={full.error} />)}
    </>
  );
}

/** The answers every form asks for, once for all applications. */
function YourDetails({ fields }: { fields: ApplicantField[] }) {
  if (fields.length === 0) {
    return <p className="muted small">Add profile/application.yaml to the data pack for copyable standard answers.</p>;
  }
  return (
    <details className="your-details">
      <summary>Your details ({fields.length}): name, contact and standard answers to copy</summary>
      <dl className="field-grid">
        {fields.map((f) => (
          <div key={f.label} className="field-row">
            <dt>{f.label}</dt>
            <dd>{f.value}</dd>
            <CopyButton text={f.value} />
          </div>
        ))}
      </dl>
    </details>
  );
}
