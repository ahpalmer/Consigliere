import type { ReactNode } from "react";
import type { Stage } from "./api";
import { fitBand } from "./format";

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
