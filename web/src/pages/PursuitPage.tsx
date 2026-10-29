import { useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { api, type Draft, type DraftKind, type Pursuit, type PursuitDetail } from "../api";
import { Badge, ErrorNote, External, Fit, Panel, StageBadge } from "../components";
import { completion, dateTime, eventText, nextWorkdayMorning, outcomeLabels, relativeDay, shortDate, toLocalInput } from "../format";
import { useAction, useLoad } from "../hooks";

export function PursuitPage() {
  const id = Number(useParams().id);
  const location = useLocation();
  const warning = (location.state as { warning?: string } | null)?.warning ?? "";
  // While a draft is being written, check back every few seconds.
  const detail = useLoad(() => api.pursuit(id), [id], { poll: (d) => d?.drafts.some((x) => x.status === "pending") ?? false });

  if (!detail.data) {
    if (!detail.error) return <p className="muted">Loading…</p>;
    return (
      <>
        <Link to="/" className="back">
          ← Pipeline
        </Link>
        <ErrorNote message={detail.error === "not found" ? "That job isn't here (it may have been removed)." : detail.error} />
      </>
    );
  }
  const { pursuit: p } = detail.data;
  const reload = detail.reload;

  return (
    <>
      <Link to="/" className="back">
        ← Pipeline
      </Link>
      <div className="page-head">
        <div>
          <p className="eyebrow">
            {p.company} · {p.ref}
          </p>
          <h1>{p.title}</h1>
          <div className="row wrap facts">
            <StageBadge stage={p.stage} />
            {p.fit !== null && (
              <span>
                Fit <Fit fit={p.fit} />
              </span>
            )}
            {p.tier && <span>{p.tier}</span>}
            <span>Active since {shortDate(p.activatedAt)}</span>
            {p.appliedAt && <span>Applied {relativeDay(p.appliedAt)}</span>}
            {p.stage === "completed" && <Badge>{completion(p.outcome, p.stageReached)}</Badge>}
          </div>
        </div>
        <div className="row">
          {p.itemId !== null && (
            <Link className="btn ghost" to={`/items/${p.itemId}`}>
              Posting details
            </Link>
          )}
          {p.url && (
            <External href={p.url}>
              <span className="btn">Open posting ↗</span>
            </External>
          )}
        </div>
      </div>
      {warning && <p className="banner warn">{warning}</p>}

      <div className="detail-grid">
        <div className="stack">
          {p.stage !== "completed" && <Checklist p={p} onChange={reload} />}
          <Actions detail={detail.data} onChange={reload} />
          <Notes p={p} onChange={reload} />
        </div>
        <div className="stack">
          <Drafts p={p} drafts={detail.data.drafts} onChange={reload} />
          <Timeline detail={detail.data} />
        </div>
      </div>
    </>
  );
}

function Checklist({ p, onChange }: { p: Pursuit; onChange: () => Promise<void> }) {
  const { run, busy, error } = useAction();
  return (
    <Panel title="Checklist">
      <ul className="checklist">
        {p.steps.map((s) => (
          <li key={s.key} className={s.doneAt ? "done" : p.nextStep === s.label ? "next" : ""}>
            <label>
              <input
                type="checkbox"
                checked={!!s.doneAt}
                disabled={busy}
                onChange={async (e) => {
                  if (await run(() => api.setStep(p.id, s.key, e.target.checked))) await onChange();
                }}
              />
              <span>{s.label}</span>
            </label>
            {s.doneAt ? <span className="muted small">{shortDate(s.doneAt)}</span> : p.nextStep === s.label && <Badge tone="accent">Next</Badge>}
          </li>
        ))}
      </ul>
      <ErrorNote message={error} />
    </Panel>
  );
}

function Actions({ detail, onChange }: { detail: PursuitDetail; onChange: () => Promise<void> }) {
  const p = detail.pursuit;
  const { run, busy, error } = useAction();
  const [mode, setMode] = useState<"" | "interview" | "complete">("");
  const act = async (f: () => Promise<unknown>) => {
    if (await run(f)) {
      setMode("");
      await onChange();
    }
  };

  if (p.stage === "completed") {
    return (
      <Panel title="Done">
        <p>
          {completion(p.outcome, p.stageReached)}
          {p.completedAt && ` on ${shortDate(p.completedAt)}`}.
        </p>
        <button className="btn" disabled={busy} onClick={() => act(() => api.reopen(p.id))}>
          Reopen
        </button>
        <ErrorNote message={error} />
      </Panel>
    );
  }

  return (
    <Panel title="Move it along">
      {p.interviewAt && (
        <p>
          🎤 Interview <b>{dateTime(p.interviewAt)}</b>
        </p>
      )}
      {p.stage === "waiting" && p.followUpAt && (
        <p className={p.followUpDue ? "due" : "muted"}>
          📬 Follow up {p.followUpDue ? "now" : `on ${shortDate(p.followUpAt)}`}
        </p>
      )}
      {p.ghostCheckDue && (
        <div className="banner warn">
          No reply since you applied. Ghosted?
          <div className="row">
            <button className="btn small" disabled={busy} onClick={() => act(() => api.complete(p.id, "ghosted", ""))}>
              Yes, mark ghosted
            </button>
            <button className="btn ghost small" disabled={busy} onClick={() => act(() => api.keepWaiting(p.id))}>
              Keep waiting
            </button>
          </div>
        </div>
      )}
      <div className="row wrap">
        {/* An interview moves a job back to Active; it was applied for already. */}
        {p.stage === "active" && !p.appliedAt && (
          <button className="btn primary" disabled={busy} onClick={() => act(() => api.applied(p.id))}>
            📨 I applied
          </button>
        )}
        {p.stage === "waiting" && (
          <button className="btn" disabled={busy} onClick={() => act(() => api.followedUp(p.id))}>
            📬 I followed up
          </button>
        )}
        <button className={mode === "interview" ? "btn active" : "btn"} onClick={() => setMode(mode === "interview" ? "" : "interview")}>
          🎤 {p.interviewAt ? "New interview" : "Schedule interview"}
        </button>
        <button className={mode === "complete" ? "btn active" : "btn"} onClick={() => setMode(mode === "complete" ? "" : "complete")}>
          🗂 Complete…
        </button>
      </div>
      {mode === "interview" && <InterviewForm busy={busy} onSubmit={(at, note) => act(() => api.interview(p.id, at, note))} />}
      {mode === "complete" && (
        <CompleteForm outcomes={detail.outcomes} busy={busy} onSubmit={(outcome, note) => act(() => api.complete(p.id, outcome, note))} />
      )}
      <ErrorNote message={error} />
    </Panel>
  );
}

function InterviewForm({ busy, onSubmit }: { busy: boolean; onSubmit: (at: string, note: string) => void }) {
  const [at, setAt] = useState(toLocalInput(nextWorkdayMorning()));
  const [note, setNote] = useState("");
  return (
    <form
      className="subform"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(at, note.trim());
      }}
    >
      <div className="row wrap">
        <label className="field">
          <span>When</span>
          <input type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} required />
        </label>
        <label className="field grow">
          <span>Notes (who, format)</span>
          <input value={note} placeholder="phone screen with Dana" onChange={(e) => setNote(e.target.value)} />
        </label>
      </div>
      <button className="btn primary" disabled={busy}>
        Save interview
      </button>
    </form>
  );
}

function CompleteForm({ outcomes, busy, onSubmit }: { outcomes: string[]; busy: boolean; onSubmit: (outcome: string, note: string) => void }) {
  const [outcome, setOutcome] = useState("");
  const [note, setNote] = useState("");
  return (
    <form
      className="subform"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(outcome, note.trim());
      }}
    >
      <div className="segmented" role="radiogroup" aria-label="Outcome">
        {outcomes.map((o) => (
          <button type="button" key={o} role="radio" aria-checked={outcome === o} className={outcome === o ? "on" : ""} onClick={() => setOutcome(o)}>
            {outcomeLabels[o] ?? o}
          </button>
        ))}
      </div>
      <label className="field">
        <span>What happened (optional, for your history)</span>
        <input value={note} onChange={(e) => setNote(e.target.value)} />
      </label>
      <button className="btn primary" disabled={busy || !outcome}>
        Complete
      </button>
    </form>
  );
}

function Notes({ p, onChange }: { p: Pursuit; onChange: () => Promise<void> }) {
  const [text, setText] = useState("");
  const { run, busy, error } = useAction();
  return (
    <Panel title="Notes">
      {p.notes ? <p className="prewrap">{p.notes}</p> : <p className="muted">No notes yet.</p>}
      <form
        className="row"
        onSubmit={async (e) => {
          e.preventDefault();
          if (await run(() => api.addNote(p.id, text.trim()))) {
            setText("");
            await onChange();
          }
        }}
      >
        <input className="grow" value={text} placeholder="Recruiter is Dana; wants a call Friday" onChange={(e) => setText(e.target.value)} />
        <button className="btn" disabled={busy || !text.trim()}>
          Add
        </button>
      </form>
      <ErrorNote message={error} />
    </Panel>
  );
}

const draftKinds: { kind: DraftKind; label: string; hint: string; needsNotes?: boolean; placeholder?: string }[] = [
  { kind: "kit", label: "✍️ Apply kit", hint: "Tailored one-page resume, people you know there, search links, outreach drafts." },
  { kind: "prep", label: "🎤 Prep pack", hint: "Likely questions, which stories to tell, questions to ask.", placeholder: "Anything about the interview (optional)" },
  { kind: "thanks", label: "✉️ Thank-you", hint: "A short note to send after an interview.", needsNotes: true, placeholder: "Who you met, what you talked about" },
];

function Drafts({ p, drafts, onChange }: { p: Pursuit; drafts: Draft[]; onChange: () => Promise<void> }) {
  const [kind, setKind] = useState<DraftKind | "">("");
  const [notes, setNotes] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const { run, busy, error } = useAction();
  const chosen = draftKinds.find((k) => k.kind === kind);

  return (
    <Panel title="Drafts">
      <div className="row wrap">
        {draftKinds.map((k) => (
          <button key={k.kind} className={kind === k.kind ? "btn active" : "btn"} onClick={() => setKind(kind === k.kind ? "" : k.kind)}>
            {k.label}
          </button>
        ))}
      </div>
      {chosen && (
        <form
          className="subform"
          onSubmit={async (e) => {
            e.preventDefault();
            const d = await run(() => api.startDraft(p.id, chosen.kind, notes.trim()));
            if (d) {
              setKind("");
              setNotes("");
              setOpen(d.id);
              await onChange();
            }
          }}
        >
          <p className="muted small">{chosen.hint} Takes about a minute; nothing is sent anywhere.</p>
          {chosen.placeholder && (
            <textarea rows={2} value={notes} placeholder={chosen.placeholder} onChange={(e) => setNotes(e.target.value)} required={chosen.needsNotes} />
          )}
          <button className="btn primary" disabled={busy}>
            Write it
          </button>
        </form>
      )}
      <ErrorNote message={error} />
      {drafts.length === 0 ? (
        <p className="muted">Nothing written yet.</p>
      ) : (
        <ul className="drafts">
          {drafts.map((d) => (
            <DraftRow key={d.id} d={d} open={open === d.id} onToggle={() => setOpen(open === d.id ? null : d.id)} />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function DraftRow({ d, open, onToggle }: { d: Draft; open: boolean; onToggle: () => void }) {
  // The pursuit lists drafts without their text; fetch it when opened (and again once a pending one is ready).
  const full = useLoad(async () => (open && d.status === "ready" ? api.draft(d.id) : undefined), [open, d.status, d.id]);
  const [copied, setCopied] = useState(false);
  return (
    <li className="draft">
      <button className="draft-head" onClick={onToggle} aria-expanded={open}>
        <span>{d.title}</span>
        <span className="row">
          {d.status === "pending" && <Badge tone="accent">Writing…</Badge>}
          {d.status === "failed" && <Badge tone="bad">Failed</Badge>}
          <span className="muted small">{relativeDay(d.createdAt)}</span>
        </span>
      </button>
      {open && d.status === "failed" && <p className="error">{d.error}</p>}
      {open && d.status === "ready" && (
        <div className="draft-body">
          <div className="row">
            {d.fileName && (
              <a className="btn small" href={api.draftFileUrl(d.id)} download={d.fileName}>
                ⬇ {d.fileName}
              </a>
            )}
            <button
              className="btn ghost small"
              disabled={!full.data}
              onClick={async () => {
                if (!full.data) return;
                await navigator.clipboard.writeText(full.data.text);
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              }}
            >
              {copied ? "Copied" : "Copy text"}
            </button>
          </div>
          {full.data ? <pre className="draft-text">{full.data.text}</pre> : <p className="muted">Loading…</p>}
        </div>
      )}
    </li>
  );
}

function Timeline({ detail }: { detail: PursuitDetail }) {
  return (
    <Panel title="History">
      <ol className="timeline">
        {[...detail.events].reverse().map((e, i) => (
          <li key={i}>
            <span className="muted small">{shortDate(e.at)}</span>
            <span>{eventText(e.kind, e.detail)}</span>
          </li>
        ))}
      </ol>
    </Panel>
  );
}
