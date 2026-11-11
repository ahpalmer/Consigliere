import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, type Draft } from "../api";
import { ErrorNote, External, Fit, Panel } from "../components";
import { relativeDay, shortDate } from "../format";
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
  const { item: o, description } = item.data;
  const long = description.length > 1800;

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
              Fit <Fit fit={o.fit} />
              {o.rankScore !== o.fit && <span className="muted"> (ranked {o.rankScore}, priority company)</span>}
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
          <Panel title="Posting">
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
