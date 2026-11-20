import { useState, type ReactNode } from "react";
import type { PostingPay, Stage } from "./api";
import { fitBand, money, moneyK } from "./format";

export function Fit({ fit, title }: { fit: number | null; title?: string }) {
  return (
    <span className={`fit fit-${fitBand(fit)}`} title={title ?? "Fit score"}>
      {fit ?? "–"}
    </span>
  );
}

const stageNames: Record<Stage, string> = { active: "Active", waiting: "Waiting", completed: "Completed" };

export function StageBadge({ stage }: { stage: Stage }) {
  return <span className={`badge stage-${stage}`}>{stageNames[stage]}</span>;
}

export function Badge({ tone = "neutral", children }: { tone?: "neutral" | "warn" | "good" | "bad" | "accent"; children: ReactNode }) {
  return <span className={`badge tone-${tone}`}>{children}</span>;
}

export function ErrorNote({ message }: { message: string }) {
  if (!message) return null;
  return (
    <p className="error" role="alert">
      {message}
    </p>
  );
}

export function Panel({ title, actions, children }: { title: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="panel">
      <header className="panel-head">
        <h2>{title}</h2>
        {actions}
      </header>
      {children}
    </section>
  );
}

/** Posting links open in a new tab and never send the dashboard's address. */
export function External({ href, children }: { href: string; children: ReactNode }) {
  if (!/^https?:\/\//i.test(href)) return <>{children}</>;
  return (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

export function Progress({ done, total }: { done: number; total: number }) {
  return (
    <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={total} aria-valuenow={done}>
      <div className="progress-fill" style={{ width: `${total ? (done / total) * 100 : 0}%` }} />
    </div>
  );
}

/** Copies text to the clipboard (the dashboard is served from localhost, which browsers allow). */
export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  return (
    <button
      className="btn small"
      type="button"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setState("copied");
        } catch {
          setState("failed");
        }
        setTimeout(() => setState("idle"), 1500);
      }}
    >
      {state === "copied" ? "Copied" : state === "failed" ? "Couldn't copy" : label}
    </button>
  );
}

function range([lo, hi]: [number, number]): string {
  return lo === hi ? money(lo, true) : `${money(lo, true)} to ${money(hi, true)}`;
}

/** The posting's listed pay (usually base only) against what you make now, at a glance; the details on hover. */
export function PayVerdict({ pay }: { pay: PostingPay | null }) {
  if (!pay) return null;
  const total = moneyK(pay.now.total);
  const detail =
    `Listed pay is usually base only. Against your base (${money(pay.now.base)}): ${range(pay.vsBase)}. ` +
    `Against your total pay (${money(pay.now.total)}): ${range(pay.vsTotal)}.`;
  switch (pay.verdict) {
    case "above":
      return (
        <span className="pay pay-above" title={detail}>
          ▲ Beats your {total} total
        </span>
      );
    case "overlaps":
      return (
        <span className="pay pay-overlaps" title={detail}>
          ≈ Up to {moneyK(pay.vsTotal[1])} over your {total} total
        </span>
      );
    default:
      return (
        <span className="pay pay-below" title={detail}>
          ▼ {moneyK(-pay.vsTotal[1])}+ under your {total} total
        </span>
      );
  }
}

/** The full read for one posting: base against base, and against total pay. */
export function PayDetail({ pay }: { pay: PostingPay | null }) {
  if (!pay) return null;
  return (
    <p className="small">
      <PayVerdict pay={pay} />
      <br />
      <span className="muted">
        Listed pay is usually base only. Against your base of {money(pay.now.base)}: {range(pay.vsBase)}. Against your total pay of{" "}
        {money(pay.now.total)} (bonus, 401(k) match, stock): {range(pay.vsTotal)}. Paste an offer on the pursuit page for the full picture.
      </span>
    </p>
  );
}
