// Typed client for the Go API (internal/web/api.go). Shapes mirror the JSON there.

export type Stage = "active" | "waiting" | "completed";

export interface Step {
  key: string;
  label: string;
  doneAt: string | null;
}

export interface Pursuit {
  id: number;
  ref: string;
  itemId: number | null;
  company: string;
  title: string;
  url: string;
  stage: Stage;
  outcome: string;
  stageReached: string;
  notes: string;
  activatedAt: string;
  appliedAt: string | null;
  interviewAt: string | null;
  followUpAt: string | null;
  ghostCheckAt: string | null;
  completedAt: string | null;
  fit: number | null;
  tier: string;
  steps: Step[];
  nextStep: string;
  followUpDue: boolean;
  ghostCheckDue: boolean;
}

export interface Todo {
  kind: "interview" | "thanks" | "followup" | "ghost" | "next";
  pursuit: Pursuit;
  label: string;
  prepDone: boolean;
}

export interface Summary {
  counts: Record<Stage, number>;
  softCap: number;
  paused: boolean;
  todos: Todo[];
  funnel: { applied: number; interviewed: number; offers: number };
  outcomes: Record<string, number>;
}

export type DraftKind = "kit" | "prep" | "thanks";

export interface Draft {
  id: number;
  pursuitId: number | null;
  itemId: number | null;
  kind: DraftKind;
  title: string;
  status: "pending" | "ready" | "failed";
  text: string;
  error: string;
  fileName: string;
  createdAt: string;
  finishedAt: string | null;
}

export interface PursuitEvent {
  at: string;
  kind: string;
  detail: string;
}

export interface PursuitDetail {
  pursuit: Pursuit;
  events: PursuitEvent[];
  drafts: Draft[];
  outcomes: string[];
}

export interface Option {
  id: number;
  company: string;
  title: string;
  locations: string;
  url: string;
  source: string;
  comp: string;
  postedAt: string | null;
  firstSeenAt: string;
  tier: string;
  category: string;
  fit: number | null;
  rankScore: number;
  priority: "high" | "normal" | "low";
  summary: string;
  reasons: string[];
  gaps: string[];
  feedback: number | null;
  sent: boolean;
  pursuit: { id: number; ref: string; stage: Stage } | null;
}

export interface OptionsPage {
  options: Option[];
  total: number;
  tiers: string[];
  categories: string[];
  unscored: number;
  defaultMinScore: number;
}

export interface Status {
  paused: boolean;
  startedAt: string;
  scoring: { running: boolean; pending: number };
  usage: { model: string; calls: number; inputTokens: number; outputTokens: number; cachedTokens: number }[];
  sources: {
    name: string;
    ats: string;
    board: string;
    priority: string;
    lastRun: string | null;
    fetched: number;
    new: number;
    error: string;
  }[];
}

export class SignInRequired extends Error {
  constructor() {
    super("Sign in required");
  }
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: {} as Record<string, string>, credentials: "same-origin" };
  // Every write sends JSON, even without a body: the server refuses anything else (no cross-site forms).
  if (method !== "GET") {
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
    init.body = JSON.stringify(body ?? {});
  }
  const res = await fetch(path, init);
  if (res.status === 401 && path !== "/api/login") {
    throw new SignInRequired();
  }
  const data = (await res.json().catch(() => ({}))) as { error?: string };
  if (!res.ok) {
    throw new ApiError(data.error ?? `Request failed (${res.status})`, res.status);
  }
  return data as T;
}

const get = <T>(path: string) => request<T>("GET", path);
const send = <T>(method: string, path: string, body?: unknown) => request<T>(method, path, body);

export const api = {
  session: () => get<{ signInRequired: boolean; signedIn: boolean }>("/api/session"),
  login: (token: string) => send<{ signedIn: boolean }>("POST", "/api/login", { token }),
  logout: () => send<{ signedIn: boolean }>("POST", "/api/logout"),

  summary: () => get<Summary>("/api/summary"),
  status: () => get<Status>("/api/status"),
  pause: () => send<{ paused: boolean }>("POST", "/api/pause"),
  resume: () => send<{ paused: boolean }>("POST", "/api/resume"),

  pursuits: (stage: Stage) => get<Pursuit[]>(`/api/pursuits?stage=${stage}`),
  pursuit: (id: number) => get<PursuitDetail>(`/api/pursuits/${id}`),
  activateItem: (itemId: number) =>
    send<{ pursuit: Pursuit; created: boolean; warning: string }>("POST", "/api/pursuits", { itemId }),
  activateManual: (url: string, company: string, title: string) =>
    send<{ pursuit: Pursuit; created: boolean; warning: string }>("POST", "/api/pursuits", { url, company, title }),
  setStep: (id: number, step: string, done: boolean) =>
    send<Pursuit>(done ? "PUT" : "DELETE", `/api/pursuits/${id}/steps/${encodeURIComponent(step)}`),
  applied: (id: number) => send<Pursuit>("POST", `/api/pursuits/${id}/applied`),
  followedUp: (id: number) => send<Pursuit>("POST", `/api/pursuits/${id}/followed-up`),
  keepWaiting: (id: number) => send<Pursuit>("POST", `/api/pursuits/${id}/keep-waiting`),
  reopen: (id: number) => send<Pursuit>("POST", `/api/pursuits/${id}/reopen`),
  interview: (id: number, at: string, note: string) =>
    send<Pursuit>("POST", `/api/pursuits/${id}/interview`, { at, note }),
  complete: (id: number, outcome: string, note: string) =>
    send<Pursuit>("POST", `/api/pursuits/${id}/complete`, { outcome, note }),
  addNote: (id: number, text: string) => send<Pursuit>("POST", `/api/pursuits/${id}/notes`, { text }),
  startDraft: (id: number, kind: DraftKind, notes: string) =>
    send<Draft>("POST", `/api/pursuits/${id}/drafts`, { kind, notes }),

  draft: (id: number) => get<Draft>(`/api/drafts/${id}`),
  draftFileUrl: (id: number) => `/api/drafts/${id}/file`,

  options: (q: { q?: string; tier?: string; category?: string; minScore?: number }) => {
    const params = new URLSearchParams();
    if (q.q) params.set("q", q.q);
    if (q.tier) params.set("tier", q.tier);
    if (q.category) params.set("category", q.category);
    if (q.minScore !== undefined) params.set("minScore", String(q.minScore));
    return get<OptionsPage>(`/api/options?${params}`);
  },
  item: (id: number) => get<{ item: Option; description: string }>(`/api/items/${id}`),
  feedback: (id: number, value: 1 | -1) => send<{ feedback: number }>("PUT", `/api/items/${id}/feedback`, { value }),
};
