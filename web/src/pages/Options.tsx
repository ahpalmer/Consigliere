import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api, type Option } from "../api";
import { ErrorNote, External, Fit, StageBadge } from "../components";
import { relativeDay } from "../format";
import { useAction, useLoad } from "../hooks";

export function Options() {
  // Filters live in the URL, so a filtered view survives reloads and the back button.
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const tier = params.get("tier") ?? "";
  const category = params.get("category") ?? "";
  const minScore = params.get("minScore");
  const [search, setSearch] = useState(q);

  const page = useLoad(() => api.options({ q, tier, category, minScore: minScore === null ? undefined : Number(minScore) }), [
    q,
    tier,
    category,
    minScore,
  ]);

  // Search as you type, a beat after the last keystroke.
  useEffect(() => {
    const timer = setTimeout(() => update("q", search.trim()), 250);
    return () => clearTimeout(timer);
  }, [search]);

  function update(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    if (next.toString() !== params.toString()) setParams(next, { replace: true });
  }

  const data = page.data;
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Options</h1>
          <p className="muted">
            Scored jobs, best first: priority companies, then location tier, category and fit. Browsing here doesn't use up the Telegram digest.
          </p>
        </div>
      </div>
      <div className="filters">
        <input type="search" placeholder="Search title, company, location" value={search} onChange={(e) => setSearch(e.target.value)} />
        <select value={tier} onChange={(e) => update("tier", e.target.value)} aria-label="Location tier">
          <option value="">All tiers</option>
          {data?.tiers.map((t) => (
            <option key={t}>{t}</option>
          ))}
        </select>
        <select value={category} onChange={(e) => update("category", e.target.value)} aria-label="Category">
          <option value="">All categories</option>
          {data?.categories.map((c) => (
            <option key={c}>{c}</option>
          ))}
          <option>other</option>
        </select>
        <label className="inline-field">
          Min score
          <input
            type="number"
            min={0}
            max={115}
            step={5}
            value={minScore ?? data?.defaultMinScore ?? 50}
            onChange={(e) => update("minScore", e.target.value)}
          />
        </label>
      </div>
      <ErrorNote message={page.error} />
      {data && (
        <p className="muted small">
          {data.options.length < data.total ? `Top ${data.options.length} of ${data.total}` : `${data.total} jobs`}
          {data.unscored > 0 && ` · ${data.unscored} still being scored`}
        </p>
      )}
      {!data && !page.error && <p className="muted">Loading…</p>}
      <ul className="options">
        {data?.options.map((o) => (
          <OptionRow key={o.id} o={o} onChange={page.reload} />
        ))}
      </ul>
      {data && data.options.length === 0 && <p className="empty">Nothing matches. Try a lower minimum score.</p>}
    </>
  );
}

export function OptionRow({ o, onChange }: { o: Option; onChange: () => Promise<void> }) {
  return (
    <li className="option">
      {/* The ranked score, so the badges match the sort order and the "Min score" filter. */}
      <Fit fit={o.rankScore} title={o.rankScore !== o.fit ? `Fit ${o.fit}, plus ${o.rankScore - (o.fit ?? 0)} for a priority company` : undefined} />
      <div className="option-main">
        <Link to={`/items/${o.id}`} className="option-title">
          {o.title}
        </Link>
        <div className="option-meta">
          <b>
            {o.priority === "high" && <span title="Priority company">★ </span>}
            {o.company}
          </b>
          {o.locations && <span>{o.locations}</span>}
          {o.comp && <span>{o.comp}</span>}
          <span>{relativeDay(o.postedAt ?? o.firstSeenAt)}</span>
          {[o.tier, o.category].filter(Boolean).length > 0 && <span>{[o.tier, o.category].filter(Boolean).join(" · ")}</span>}
        </div>
        {o.summary && <p className="option-summary">{o.summary}</p>}
      </div>
      <ItemActions o={o} onChange={onChange} />
    </li>
  );
}

export function ItemActions({ o, onChange }: { o: Option; onChange: () => Promise<void> }) {
  const { run, busy, error } = useAction();
  const navigate = useNavigate();
  return (
    <div className="item-actions">
      {o.pursuit ? (
        <Link to={`/pursuits/${o.pursuit.id}`} className="pursuit-link" title="In your pipeline">
          {o.pursuit.ref} <StageBadge stage={o.pursuit.stage} />
        </Link>
      ) : (
        <button
          className="btn primary small"
          disabled={busy}
          onClick={async () => {
            const res = await run(() => api.activateItem(o.id));
            if (res) navigate(`/pursuits/${res.pursuit.id}`, { state: { warning: res.warning } });
          }}
        >
          🎯 Make active
        </button>
      )}
      <div className="row">
        <button
          className={o.feedback === 1 ? "icon-btn on" : "icon-btn"}
          title="Good find"
          aria-pressed={o.feedback === 1}
          disabled={busy}
          onClick={async () => (await run(() => api.feedback(o.id, 1))) && onChange()}
        >
          👍
        </button>
        <button
          className={o.feedback === -1 ? "icon-btn on" : "icon-btn"}
          title="A miss"
          aria-pressed={o.feedback === -1}
          disabled={busy}
          onClick={async () => (await run(() => api.feedback(o.id, -1))) && onChange()}
        >
          👎
        </button>
        <External href={o.url}>
          <span className="icon-btn" title="Open posting">
            ↗
          </span>
        </External>
      </div>
      <ErrorNote message={error} />
    </div>
  );
}
