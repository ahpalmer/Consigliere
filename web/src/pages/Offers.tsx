import { useState } from "react";
import { api, type CompColumn, type Comparison, type Offer } from "../api";
import { ErrorNote, Panel } from "../components";
import { money, shortDate } from "../format";
import { useAction, useLoad } from "../hooks";

/** Offers on a pursuit (M9): paste one in, see it next to the current package year by year, and fix what was read. */
export function Offers({ pursuitId }: { pursuitId: number }) {
  const list = useLoad(() => api.offers(pursuitId), [pursuitId]);
  const [text, setText] = useState("");
  const [adding, setAdding] = useState(false);
  const { run, busy, error } = useAction();
  const offers = list.data?.offers ?? [];
  const showForm = adding || (list.data !== undefined && offers.length === 0);

  return (
    <Panel
      title="Offers"
      actions={
        offers.length > 0 &&
        !adding && (
          <button className="btn small" onClick={() => setAdding(true)}>
            Paste another
          </button>
        )
      }
    >
      <ErrorNote message={list.error} />
      {list.data && !list.data.current && <p className="banner warn">{list.data.currentError}</p>}
      {showForm && (
        <form
          className="subform"
          onSubmit={async (e) => {
            e.preventDefault();
            if (await run(() => api.addOffer(pursuitId, text.trim()))) {
              setText("");
              setAdding(false);
              await list.reload();
            }
          }}
        >
          <p className="muted small">
            Paste the offer letter, the recruiter's email, or your notes from the call. A model copies the numbers out (nothing is
            sent anywhere); the math is plain code.
          </p>
          <textarea rows={6} value={text} placeholder="Base $150,000, 10% target bonus, $40K RSUs over 4 years…" onChange={(e) => setText(e.target.value)} />
          <div className="row">
            <button className="btn primary" disabled={busy || !text.trim()}>
              {busy ? "Reading…" : "Compare"}
            </button>
            {adding && (
              <button type="button" className="btn ghost" onClick={() => setAdding(false)}>
                Cancel
              </button>
            )}
          </div>
          <ErrorNote message={error} />
        </form>
      )}
      {offers.map((o) => (
        <OfferView key={o.id} offer={o} onChange={list.reload} />
      ))}
    </Panel>
  );
}

function OfferView({ offer, onChange }: { offer: Offer; onChange: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [yaml, setYaml] = useState(offer.package);
  const [showText, setShowText] = useState(false);
  const { run, busy, error } = useAction();
  const c = offer.comparison;

  return (
    <section className="offer">
      <div className="row wrap offer-head">
        <h3>{offer.company || "Offer"}</h3>
        <span className="muted small">pasted {shortDate(offer.createdAt)}</span>
        <span className="row grow end">
          <button
            className="btn small"
            onClick={() => {
              setYaml(offer.package);
              setEditing(!editing);
            }}
          >
            {editing ? "Close" : "Fix numbers"}
          </button>
          <button className="btn small ghost" onClick={() => setShowText(!showText)}>
            {showText ? "Hide text" : "Pasted text"}
          </button>
          <button
            className="btn small ghost danger"
            disabled={busy}
            onClick={async () => {
              if (confirm("Delete this offer?") && (await run(() => api.deleteOffer(offer.id)))) await onChange();
            }}
          >
            Delete
          </button>
        </span>
      </div>
      {showText && <p className="prewrap small posting">{offer.text}</p>}
      {editing && (
        <form
          className="subform"
          onSubmit={async (e) => {
            e.preventDefault();
            if (await run(() => api.updateOffer(offer.id, yaml))) {
              setEditing(false);
              await onChange();
            }
          }}
        >
          <p className="muted small">
            The package as read, in the same shape as comp/current.yaml in your data pack. Fix anything misread, or add what you learn;
            list sections that don't exist under <code>none</code> to stop the questions about them.
          </p>
          <textarea className="mono" rows={16} value={yaml} spellCheck={false} onChange={(e) => setYaml(e.target.value)} />
          <button className="btn primary" disabled={busy}>
            Save
          </button>
        </form>
      )}
      <ErrorNote message={error} />
      <ErrorNote message={offer.error} />
      {c && <ComparisonView c={c} />}
    </section>
  );
}

function ComparisonView({ c }: { c: Comparison }) {
  const years = Array.from({ length: c.years }, (_, i) => i);
  const forfeited = c.forfeited.filter((f) => !f.private);
  const privateForfeits = c.forfeited.filter((f) => f.private);
  const privateLines = c.incoming.private;

  return (
    <div className="stack">
      <p className="offer-verdict">
        Over {c.years} years from {c.start}, the offer pays{" "}
        <strong className={c.differenceTotal >= 0 ? "good-text" : "error-text"}>
          {money(Math.abs(c.differenceTotal))} {c.differenceTotal >= 0 ? "more" : "less"}
        </strong>{" "}
        in guaranteed pay than staying.
      </p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th />
              {years.map((i) => (
                <th key={i} className="num">
                  Year {i + 1}
                </th>
              ))}
              <th className="num">Total</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>Staying</td>
              {c.current.totals.map((v, i) => (
                <td key={i} className="num">
                  {money(v)}
                </td>
              ))}
              <td className="num">{money(c.current.total)}</td>
            </tr>
            <tr>
              <td>The offer</td>
              {c.incoming.totals.map((v, i) => (
                <td key={i} className="num">
                  {money(v)}
                </td>
              ))}
              <td className="num">{money(c.incoming.total)}</td>
            </tr>
            <tr className="total-row">
              <td>Difference</td>
              {c.difference.map((v, i) => (
                <td key={i} className={v < 0 ? "num error-text" : "num"}>
                  {money(v, true)}
                </td>
              ))}
              <td className={c.differenceTotal < 0 ? "num error-text" : "num"}>{money(c.differenceTotal, true)}</td>
            </tr>
          </tbody>
        </table>
      </div>

      {privateLines.length > 0 && (
        <div className="banner warn">
          <strong>Private equity, not counted above.</strong> It's paper value until there's a sale or listing, and may be worth nothing:{" "}
          {privateLines.map((l) => `${l.label.replace("Equity (paper value): ", "")} ${money(l.total)} over ${c.years} years`).join("; ")}.
        </div>
      )}

      <details>
        <summary>Line by line</summary>
        <LineTable title="Staying" col={c.current} years={years} />
        <LineTable title="Taking the offer" col={c.incoming} years={years} />
      </details>

      {(forfeited.length > 0 || privateForfeits.length > 0) && (
        <div>
          <h4>What leaving gives up</h4>
          <ul className="bullets">
            {forfeited.map((f) => (
              <li key={f.label}>
                {f.label}: <strong>{money(f.amount)}</strong>
                {f.note && <span className="muted"> ({f.note})</span>}
              </li>
            ))}
            {privateForfeits.map((f) => (
              <li key={f.label}>
                {f.label}: {money(f.amount)} <span className="muted">paper value{f.note && `, ${f.note}`}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Bullets title="Ask them" items={c.questions} />
      <Bullets title="Also in the offer" items={c.notes} />
      <Bullets title="Fix in comp/current.yaml" items={c.warnings} tone="gaps" />
      <Bullets title="Assumptions" items={c.assumptions} muted />
    </div>
  );
}

function LineTable({ title, col, years }: { title: string; col: CompColumn; years: number[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{title}</th>
            {years.map((i) => (
              <th key={i} className="num">
                Year {i + 1}
              </th>
            ))}
            <th className="num">Total</th>
          </tr>
        </thead>
        <tbody>
          {[...col.lines, ...col.private].map((l) => (
            <tr key={l.label} className={col.private.includes(l) ? "muted" : undefined}>
              <td title={l.note}>
                {l.label}
                {l.note && <span className="muted small"> · {l.note}</span>}
              </td>
              {l.amounts.map((v, i) => (
                <td key={i} className="num">
                  {v === 0 ? "–" : money(v)}
                </td>
              ))}
              <td className="num">{money(l.total)}</td>
            </tr>
          ))}
          <tr className="total-row">
            <td>Total</td>
            {col.totals.map((v, i) => (
              <td key={i} className="num">
                {money(v)}
              </td>
            ))}
            <td className="num">{money(col.total)}</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}

function Bullets({ title, items, tone, muted }: { title: string; items: string[]; tone?: "gaps"; muted?: boolean }) {
  if (items.length === 0) return null;
  return (
    <div className={muted ? "muted small" : undefined}>
      <h4>{title}</h4>
      <ul className={tone ? `bullets ${tone}` : "bullets"}>
        {items.map((q) => (
          <li key={q}>{q}</li>
        ))}
      </ul>
    </div>
  );
}
