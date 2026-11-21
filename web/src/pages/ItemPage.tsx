import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, type Draft } from "../api";
import { ErrorNote, External, Fit, Panel, PayDetail } from "../components";
import { fitTitle, relativeDay, shortDate } from "../format";
import { useAction, useLoad } from "../hooks";
import { ItemActions } from "./Options";
import { coverHint, coverPlaceholder, DraftRow, Research } from "./PursuitPage";

export function ItemPage() {
  const id = Number(useParams().id);
  // While a letter is being written, check back every few seconds.
  const item = useLoad(() => api.item(id), [id], { poll: (d) => d?.drafts.some((x) => x.status === "pending") ?? false });
  const [full, setFull] = useState(false);

  if (!item.data) {
    if (!item.error) return <p className="muted">Loading…</p>;
    return (
      <>
        <Link to="/options" className="back">
          ← Options
        </Link>
        <ErrorNote message={item.error === "not found" ? "That job isn't here (it may have been removed)." : item.error} />
      </>
    );
  }
  const { item: o, description, pastedAt } = item.data;
  const long = description.length > 1800;
  // Alert emails carry only a snippet, so the score is a rough one until the full posting is pasted.
  const snippetOnly = o.source === "email" && !pastedAt;

  return (
    <>
      <Link to="/options" className="back">
        ← Options
      </Link>
      <div className="page-head">
        <div>
          <p className="eyebrow">
            {o.priority === "high" && "★ "}
            {o.company} · #{o.id}
          </p>
          <h1>
            <External href={o.url}>{o.title}</External>
          </h1>
          <div className="row wrap facts">
            <span>
              Fit <Fit fit={o.rankScore} title={fitTitle(o)} /> <span className="muted small">{fitTitle(o).replace(/^Fit \d+: /, "")}</span>
            </span>
            {o.locations && <span>{o.locations}</span>}
            {o.comp && <span>{o.comp}</span>}
            <span>{o.postedAt ? `Posted ${shortDate(o.postedAt)} (${relativeDay(o.postedAt)})` : `First seen ${shortDate(o.firstSeenAt)}`}</span>
            {[o.tier, o.category].filter(Boolean).length > 0 && <span>{[o.tier, o.category].filter(Boolean).join(" · ")}</span>}
          </div>
        </div>
        <ItemActions o={o} onChange={item.reload} />
      </div>

      <div className="detail-grid">
        <div className="stack">
          {o.pay && (
            <Panel title="Pay">
              <PayDetail pay={o.pay} />
            </Panel>
          )}
          <Panel title="Why it scored this way">
            {o.summary && <p>{o.summary}</p>}
            {o.reasons.length > 0 && (
              <>
                <h3>Fits</h3>
                <ul className="bullets">
                  {o.reasons.map((r) => (
                    <li key={r}>{r}</li>
                  ))}
                </ul>
              </>
            )}
            {o.gaps.length > 0 && (
              <>
                <h3>Gaps</h3>
                <ul className="bullets gaps">
                  {o.gaps.map((g) => (
                    <li key={g}>{g}</li>
                  ))}
                </ul>
              </>
            )}
          </Panel>
          <CoverLetter itemId={o.id} drafts={item.data.drafts.filter((d) => d.kind === "cover")} onChange={item.reload} />
          <Research links={item.data.research} />
        </div>
        <div className="stack">
          <Panel title="Posting" actions={pastedAt && <span className="muted small">pasted {shortDate(pastedAt)}</span>}>
            <PastePosting itemId={o.id} snippetOnly={snippetOnly} onChange={item.reload} />
            {description ? (
              <>
                <p className="prewrap posting">{long && !full ? description.slice(0, 1800) + "…" : description}</p>
                {long && (
                  <button className="btn ghost small" onClick={() => setFull(!full)}>
                    {full ? "Show less" : "Show all"}
                  </button>
                )}
              </>
            ) : (
              <p className="muted">No posting text on file.</p>
            )}
          </Panel>
        </div>
      </div>
    </>
  );
}

function PastePosting({ itemId, snippetOnly, onChange }: { itemId: number; snippetOnly: boolean; onChange: () => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const { run, busy, error } = useAction();
  if (!open) {
    return snippetOnly ? (
      <div className="banner warn">
        This came from an alert email, which only has a snippet, so the score is a rough one. Paste the full posting to re-score it.
        <div className="row">
          <button className="btn small primary" onClick={() => setOpen(true)}>
            Paste full posting
          </button>
        </div>
      </div>
    ) : (
      <button className="btn ghost small" onClick={() => setOpen(true)}>
        Paste full posting
      </button>
    );
  }
  return (
    <form
      className="subform"
      onSubmit={async (e) => {
        e.preventDefault();
        if (await run(() => api.pastePosting(itemId, text.trim()))) {
          setText("");
          setOpen(false);
          await onChange();
        }
      }}
    >
      <p className="muted small">
        Open the job, copy the whole description, and paste it here. It replaces the text below, the pay range is taken from it when
        listed, and the job is re-scored.
      </p>
      <textarea rows={8} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="row">
        <button className="btn primary" disabled={busy || !text.trim()}>
          {busy ? "Scoring…" : "Re-score"}
        </button>
        <button type="button" className="btn ghost" onClick={() => setOpen(false)}>
          Cancel
        </button>
      </div>
      <ErrorNote message={error} />
    </form>
  );
}

function CoverLetter({ itemId, drafts, onChange }: { itemId: number; drafts: Draft[]; onChange: () => Promise<void> }) {
  const [instructions, setInstructions] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const { run, busy, error } = useAction();
  return (
    <Panel title="Cover letter">
      <form
        className="subform"
        onSubmit={async (e) => {
          e.preventDefault();
          const d = await run(() => api.coverLetter(itemId, instructions.trim()));
          if (d) {
            setOpen(d.id);
            await onChange();
          }
        }}
      >
        <p className="muted small">{coverHint} Takes about a minute; nothing is sent anywhere.</p>
        <textarea rows={6} value={instructions} placeholder={coverPlaceholder} onChange={(e) => setInstructions(e.target.value)} />
        <button className="btn primary" disabled={busy}>
          📝 Write cover letter
        </button>
      </form>
      <ErrorNote message={error} />
      {drafts.length > 0 && (
        <ul className="drafts">
          {drafts.map((d) => (
            <DraftRow key={d.id} d={d} open={open === d.id} onToggle={() => setOpen(open === d.id ? null : d.id)} />
          ))}
        </ul>
      )}
    </Panel>
  );
}
