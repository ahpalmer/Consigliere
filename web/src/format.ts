// Display helpers. Dates arrive as RFC 3339 (UTC) and show in the browser's time zone.

const DAY = 24 * 60 * 60 * 1000;

/** "today", "yesterday", "3 days ago", "in 2 days". */
export function relativeDay(iso: string, now: Date = new Date()): string {
  const then = new Date(iso);
  const days = Math.round((startOfDay(then) - startOfDay(now)) / DAY);
  if (days === 0) return "today";
  if (days === -1) return "yesterday";
  if (days === 1) return "tomorrow";
  return days < 0 ? `${-days} days ago` : `in ${days} days`;
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

export function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Value for <input type="datetime-local">: local time, minutes precision. */
export function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** The next weekday at 10:00, a reasonable default for scheduling an interview. */
export function nextWorkdayMorning(now: Date = new Date()): Date {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 10, 0);
  while (d.getDay() === 0 || d.getDay() === 6) d.setDate(d.getDate() + 1);
  return d;
}

export const outcomeLabels: Record<string, string> = {
  offer: "Offer",
  rejected: "Rejected",
  ghosted: "Ghosted",
  withdrawn: "Withdrew",
  dropped: "Dropped",
};

const reachedLabels: Record<string, string> = {
  active: "before applying",
  applied: "after applying",
  interview: "after interviewing",
  offer: "",
};

/** "Rejected after interviewing". */
export function completion(outcome: string, stageReached: string): string {
  const label = outcomeLabels[outcome] ?? outcome;
  const reached = outcome === "ghosted" && stageReached === "applied" ? "" : (reachedLabels[stageReached] ?? "");
  return reached ? `${label} ${reached}` : label;
}

const eventLabels: Record<string, string> = {
  activated: "Made active",
  step: "Done",
  step_undone: "Undone",
  applied: "Applied",
  followed_up: "Followed up",
  interview: "Interview scheduled",
  keep_waiting: "Kept waiting",
  note: "Note",
  completed: "Completed",
  reopened: "Reopened",
};

const stepLabels: Record<string, string> = {
  resume: "Tailor resume",
  people: "Find people",
  outreach: "Send outreach",
  submit: "Submit application",
  followup: "Follow up",
  prep: "Prep for interview",
  interview: "Interview",
  thanks: "Send thank-you note",
};

export function eventText(kind: string, detail: string): string {
  const label = eventLabels[kind] ?? kind;
  if (kind === "step" || kind === "step_undone") return `${label}: ${stepLabels[detail] ?? detail}`;
  if (kind === "completed") {
    const [outcome = "", ...rest] = detail.split(" ");
    return [`${label}: ${outcomeLabels[outcome] ?? outcome}`, rest.join(" ")].filter(Boolean).join(" · ");
  }
  return detail ? `${label}: ${detail}` : label;
}

/** Fit band for coloring: strong ≥ 75 (the alert bar), fair ≥ 50 (the digest bar), else weak. */
export function fitBand(fit: number | null): "strong" | "fair" | "weak" | "none" {
  if (fit === null) return "none";
  if (fit >= 75) return "strong";
  if (fit >= 50) return "fair";
  return "weak";
}

export function tokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}
