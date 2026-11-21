import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, type Adventure, type AdventureInput, type Cost, type Idea, type IdeaInput } from "../api";
import { Badge, ErrorNote, Panel } from "../components";
import { useAction, useLoad } from "../hooks";

// The Adventures section (M13): Upcoming, Ideas, Generate and History. Reminders and the ask for a rating afterward
// go out on Telegram; everything can also be done here.

/** "Today", "Tomorrow" or "Sat, Dec 5", plus the time when there is one. */
export function when(date: string, time: string, today: string): string {
  const d = new Date(`${date}T12:00:00`);
  const t = new Date(`${today}T12:00:00`);
  const days = Math.round((d.getTime() - t.getTime()) / 86_400_000);
  let out =
    days === 0
      ? "Today"
      : days === 1
        ? "Tomorrow"
        : d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric", year: d.getFullYear() === t.getFullYear() ? undefined : "numeric" });
  if (time) {
    const [h = 0, m = 0] = time.split(":").map(Number);
    out += ` at ${h % 12 || 12}${m ? `:${String(m).padStart(2, "0")}` : ""}${h < 12 ? "am" : "pm"}`;
  }
  return out;
}

function Stars({ value, onPick, disabled }: { value: number | null; onPick?: (n: number) => void; disabled?: boolean }) {
  return (
    <span className="stars" role={onPick ? "radiogroup" : undefined} aria-label="Rating">
      {[1, 2, 3, 4, 5].map((n) =>
        onPick ? (
          <button key={n} type="button" className={value !== null && n <= value ? "on" : ""} disabled={disabled} aria-label={`${n} stars`} onClick={() => onPick(n)}>
            ★
          </button>
        ) : (
          <span key={n} className={value !== null && n <= value ? "on" : ""}>
            ★
          </span>
        ),
      )}
    </span>
  );
}

const blankPlan: AdventureInput = { title: "", place: "", notes: "", date: "", time: "", prepNote: "", prepDue: "" };

/** Add or change a planned adventure. From an idea, blank title, place and notes are filled from it. */
function PlanForm({ initial, id, onDone, onCancel }: { initial: AdventureInput; id?: number; onDone: () => void; onCancel: () => void }) {
  const [a, setA] = useState(initial);
  const { run, busy, error } = useAction();
  const set = (k: keyof AdventureInput) => (e: { target: { value: string } }) => setA({ ...a, [k]: e.target.value });
  return (
    <form
      className="subform"
      onSubmit={async (e) => {
        e.preventDefault();
        if (await run(() => (id ? api.updateAdventure(id, a) : api.planAdventure(a)))) onDone();
      }}
    >
      <div className="form-grid">
        <label className="field wide">
          What
          <input value={a.title} placeholder={a.ideaId ? "(from the idea)" : "Dinner at Poole's"} onChange={set("title")} />
        </label>
        <label className="field">
          Day
          <input type="date" required value={a.date} onChange={set("date")} />
        </label>
        <label className="field">
          Time (optional)
          <input type="time" value={a.time} onChange={set("time")} />
        </label>
        <label className="field wide">
          Where
          <input value={a.place} onChange={set("place")} />
        </label>
        <label className="field wide">
          Notes
          <textarea rows={2} value={a.notes} onChange={set("notes")} />
        </label>
        <label className="field">
          Prep (optional)
          <input value={a.prepNote} placeholder="Book a table" onChange={set("prepNote")} />
        </label>
        <label className="field">
          Prep by
          <input type="date" value={a.prepDue} max={a.date || undefined} onChange={set("prepDue")} />
        </label>
      </div>
      <p className="muted small">Telegram reminds you of the prep on its day, the day before and the day of, then asks how it went.</p>
      <div className="row">
        <button className="btn primary" disabled={busy}>
          {id ? "Save" : "Plan it"}
        </button>
        <button type="button" className="btn ghost" onClick={onCancel}>
          Cancel
        </button>
      </div>
      <ErrorNote message={error} />
    </form>
  );
}

// --- Upcoming ---

export function AdventuresUpcoming() {
  const list = useLoad(() => api.adventures(), []);
  const [params, setParams] = useSearchParams();
  const fromIdea = Number(params.get("idea")) || null;
  const [planning, setPlanning] = useState(false);
  const data = list.data;

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Upcoming</h1>
          <p className="muted">What's planned, soonest first. Past ones wait here for a rating.</p>
        </div>
        {!planning && !fromIdea && (
          <button className="btn primary" onClick={() => setPlanning(true)}>
            Plan an adventure
          </button>
        )}
      </div>
      <ErrorNote message={list.error} />
      {(planning || fromIdea) && (
        <Panel title={fromIdea ? "Plan this idea" : "Plan an adventure"}>
          <PlanForm
            initial={{ ...blankPlan, ideaId: fromIdea }}
            onCancel={() => {
              setPlanning(false);
              setParams({});
            }}
            onDone={() => {
              setPlanning(false);
              setParams({});
              void list.reload();
            }}
          />
        </Panel>
      )}
      {data && data.upcoming.length === 0 && !planning && !fromIdea && <p className="empty">Nothing planned. Pick something from Ideas, or generate a few.</p>}
      <div className="stack">
        {data?.upcoming.map((a) => <UpcomingCard key={a.id} a={a} today={data.today} onChange={list.reload} />)}
      </div>
    </>
  );
}

function UpcomingCard({ a, today, onChange }: { a: Adventure; today: string; onChange: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [review, setReview] = useState("");
  const { run, busy, error } = useAction();
  const past = a.date < today;
  const prepDue = a.prepDue && a.prepDue <= today && !past;

  if (editing) {
    return (
      <Panel title={`Change: ${a.title}`}>
        <PlanForm initial={a} id={a.id} onCancel={() => setEditing(false)} onDone={() => void onChange().then(() => setEditing(false))} />
      </Panel>
    );
  }
  return (
    <section className="panel adventure">
      <div className="row wrap">
        <h2 className="grow">{a.title}</h2>
        <Badge tone={past ? "warn" : a.date === today ? "accent" : "neutral"}>{when(a.date, a.time, today)}</Badge>
      </div>
      {a.place && <p className="muted">{a.place}</p>}
      {a.notes && <p className="prewrap">{a.notes}</p>}
      {a.prepNote && (
        <p className={prepDue ? "banner warn" : "muted small"}>
          Prep: {a.prepNote}
          {a.prepDue && ` (by ${when(a.prepDue, "", today)})`}
        </p>
      )}
      {past ? (
        <div className="subform">
          <strong>How was it?</strong>
          <input value={review} placeholder="A line about how it went (optional)" onChange={(e) => setReview(e.target.value)} />
          <div className="row wrap">
            <Stars value={null} disabled={busy} onPick={async (n) => (await run(() => api.rateAdventure(a.id, n, review))) && onChange()} />
            <button className="btn small ghost" disabled={busy} onClick={async () => (await run(() => api.skipAdventure(a.id))) && onChange()}>
              Didn't happen
            </button>
          </div>
        </div>
      ) : (
        <div className="row wrap">
          <button className="btn small" onClick={() => setEditing(true)}>
            Change
          </button>
          <button
            className="btn small ghost"
            disabled={busy}
            onClick={async () => confirm("Call it off?") && (await run(() => api.skipAdventure(a.id))) && onChange()}
          >
            Call off
          </button>
        </div>
      )}
      <ErrorNote message={error} />
    </section>
  );
}

// --- Ideas ---

const costs: Cost[] = ["", "$", "$$", "$$$", "$$$$"];
const blankIdea: IdeaInput = { title: "", description: "", place: "", cost: "", duration: "", tags: [] };

function IdeaForm({ initial, id, onDone, onCancel }: { initial: IdeaInput; id?: number; onDone: () => void; onCancel: () => void }) {
  const [i, setI] = useState(initial);
  const [tags, setTags] = useState(initial.tags.join(", "));
  const { run, busy, error } = useAction();
  const set = (k: keyof IdeaInput) => (e: { target: { value: string } }) => setI({ ...i, [k]: e.target.value });
  return (
    <form
      className="subform"
      onSubmit={async (e) => {
        e.preventDefault();
        const body = { ...i, tags: tags.split(",") };
        if (await run(() => (id ? api.updateIdea(id, body) : api.addIdea(body)))) onDone();
      }}
    >
      <div className="form-grid">
        <label className="field wide">
          Idea
          <input value={i.title} required placeholder="Kayak the Neuse" onChange={set("title")} />
        </label>
        <label className="field wide">
          Details
          <textarea rows={2} value={i.description} onChange={set("description")} />
        </label>
        <label className="field">
          Where
          <input value={i.place} onChange={set("place")} />
        </label>
        <label className="field">
          Cost
          <select value={i.cost} onChange={set("cost")}>
            {costs.map((c) => (
              <option key={c} value={c}>
                {c || "—"}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          How long
          <input value={i.duration} placeholder="an evening" onChange={set("duration")} />
        </label>
        <label className="field">
          Tags
          <input value={tags} placeholder="outdoors, food" onChange={(e) => setTags(e.target.value)} />
        </label>
      </div>
      <div className="row">
        <button className="btn primary" disabled={busy}>
          {id ? "Save" : "Add idea"}
        </button>
        <button type="button" className="btn ghost" onClick={onCancel}>
          Cancel
        </button>
      </div>
      <ErrorNote message={error} />
    </form>
  );
}

export function AdventureIdeas() {
  const [q, setQ] = useState("");
  const [favorites, setFavorites] = useState(false);
  const [adding, setAdding] = useState(false);
  const list = useLoad(() => api.ideas(q, favorites), [q, favorites]);

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Ideas</h1>
          <p className="muted">The idea bank: favorites first, then what you've rated best.</p>
        </div>
        {!adding && (
          <button className="btn primary" onClick={() => setAdding(true)}>
            Add an idea
          </button>
        )}
      </div>
      {adding && (
        <Panel title="Add an idea">
          <IdeaForm
            initial={blankIdea}
            onCancel={() => setAdding(false)}
            onDone={() => {
              setAdding(false);
              void list.reload();
            }}
          />
        </Panel>
      )}
      <div className="filters">
        <input type="search" placeholder="Search ideas" value={q} onChange={(e) => setQ(e.target.value)} />
        <label className="inline-field">
          <input type="checkbox" checked={favorites} onChange={(e) => setFavorites(e.target.checked)} /> Favorites only
        </label>
      </div>
      <ErrorNote message={list.error} />
      {list.data?.ideas.length === 0 && <p className="empty">{q || favorites ? "No ideas match." : "No ideas yet. Add one, or generate a few."}</p>}
      <div className="idea-grid">
        {list.data?.ideas.map((i) => <IdeaCard key={i.id} idea={i} onChange={list.reload} />)}
      </div>
    </>
  );
}

function IdeaFacts({ idea }: { idea: IdeaInput }) {
  const facts = [idea.cost, idea.duration, idea.place].filter(Boolean);
  return (
    <>
      {facts.length > 0 && <p className="muted small">{facts.join(" · ")}</p>}
      {idea.description && <p>{idea.description}</p>}
      {idea.tags.length > 0 && (
        <div className="row wrap">
          {idea.tags.map((t) => (
            <Badge key={t}>{t}</Badge>
          ))}
        </div>
      )}
    </>
  );
}

function IdeaCard({ idea, onChange }: { idea: Idea; onChange: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const { run, busy, error } = useAction();
  const navigate = useNavigate();
  if (editing) {
    return (
      <section className="panel">
        <IdeaForm initial={idea} id={idea.id} onCancel={() => setEditing(false)} onDone={() => void onChange().then(() => setEditing(false))} />
      </section>
    );
  }
  return (
    <section className="panel idea">
      <div className="row">
        <h2 className="grow">{idea.title}</h2>
        <button
          className={idea.favorite ? "icon-btn fav on" : "icon-btn fav"}
          aria-label={idea.favorite ? "Unfavorite" : "Favorite"}
          aria-pressed={idea.favorite}
          disabled={busy}
          onClick={async () => (await run(() => api.favoriteIdea(idea.id, !idea.favorite))) && onChange()}
        >
          {idea.favorite ? "♥" : "♡"}
        </button>
      </div>
      <IdeaFacts idea={idea} />
      {idea.done > 0 && (
        <p className="muted small">
          Done {idea.done === 1 ? "once" : `${idea.done} times`}
          {idea.avgRating > 0 && (
            <>
              {" · "}
              <Stars value={Math.round(idea.avgRating)} />
            </>
          )}
        </p>
      )}
      <div className="row wrap">
        <button className="btn small primary" onClick={() => navigate(`/adventures?idea=${idea.id}`)}>
          Plan it
        </button>
        <button className="btn small" onClick={() => setEditing(true)}>
          Edit
        </button>
        <button
          className="btn small ghost danger"
          disabled={busy}
          onClick={async () => confirm("Remove this idea?") && (await run(() => api.archiveIdea(idea.id))) && onChange()}
        >
          Remove
        </button>
      </div>
      <ErrorNote message={error} />
    </section>
  );
}

// --- Generate ---

export function AdventureGenerate() {
  const [constraints, setConstraints] = useState("");
  const [ideas, setIdeas] = useState<IdeaInput[]>([]);
  const [saved, setSaved] = useState<Record<number, number>>({}); // index → saved idea id
  const { run, busy, error } = useAction();
  const save = useAction();
  const navigate = useNavigate();

  const keep = async (i: number) => {
    if (saved[i]) return saved[i];
    const picked = ideas[i];
    if (!picked) return undefined;
    const idea = await save.run(() => api.addIdea({ ...picked, source: "generated" }));
    if (idea) setSaved((s) => ({ ...s, [i]: idea.id }));
    return idea?.id;
  };

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Generate</h1>
          <p className="muted">Say what you're after; you get 5 ideas shaped by what you've rated and favorited.</p>
        </div>
      </div>
      <Panel title="What are you looking for?">
        <form
          className="subform"
          onSubmit={async (e) => {
            e.preventDefault();
            const out = await run(() => api.generateIdeas(constraints.trim()));
            if (out) {
              setIdeas(out.ideas);
              setSaved({});
            }
          }}
        >
          <textarea
            rows={3}
            value={constraints}
            placeholder="A rainy Saturday, under $50 for two, nothing more than an hour's drive"
            onChange={(e) => setConstraints(e.target.value)}
          />
          <button className="btn primary" disabled={busy}>
            {busy ? "Thinking…" : ideas.length ? "Generate 5 more" : "Generate 5 ideas"}
          </button>
          <ErrorNote message={error} />
        </form>
      </Panel>
      <ErrorNote message={save.error} />
      <div className="idea-grid">
        {ideas.map((idea, i) => (
          <section key={`${idea.title}-${i}`} className="panel idea">
            <h2>{idea.title}</h2>
            <IdeaFacts idea={idea} />
            <div className="row wrap">
              <button
                className="btn small primary"
                disabled={save.busy}
                onClick={async () => {
                  const id = await keep(i);
                  if (id) navigate(`/adventures?idea=${id}`);
                }}
              >
                Plan it
              </button>
              <button className="btn small" disabled={save.busy || !!saved[i]} onClick={() => void keep(i)}>
                {saved[i] ? "Saved ✓" : "Save to ideas"}
              </button>
            </div>
          </section>
        ))}
      </div>
    </>
  );
}

// --- History ---

export function AdventureHistory() {
  const list = useLoad(() => api.adventures(), []);
  const data = list.data;
  return (
    <>
      <div className="page-head">
        <div>
          <h1>History</h1>
          <p className="muted">Everything you've done, newest first. Ratings shape the ideas you're given.</p>
        </div>
      </div>
      <ErrorNote message={list.error} />
      {data?.history.length === 0 && <p className="empty">Nothing yet. Rated adventures show up here.</p>}
      <ul className="history">{data?.history.map((a) => <HistoryRow key={a.id} a={a} today={data.today} onChange={list.reload} />)}</ul>
    </>
  );
}

function HistoryRow({ a, today, onChange }: { a: Adventure; today: string; onChange: () => Promise<void> }) {
  const [review, setReview] = useState(a.review);
  const [editing, setEditing] = useState(false);
  const { run, busy, error } = useAction();
  return (
    <li className="history-row">
      <div className="row wrap">
        <strong className="grow">{a.title}</strong>
        <span className="muted small">{when(a.date, "", today)}</span>
      </div>
      {a.status === "skipped" ? (
        <span className="muted small">Didn't happen</span>
      ) : editing ? (
        <div className="subform">
          <input value={review} placeholder="How it went" onChange={(e) => setReview(e.target.value)} />
          <Stars value={a.rating} disabled={busy} onPick={async (n) => (await run(() => api.rateAdventure(a.id, n, review))) && (await onChange(), setEditing(false))} />
        </div>
      ) : (
        <div className="row wrap">
          <Stars value={a.rating} />
          {a.review && <span className="muted">{a.review}</span>}
          <button className="btn small ghost" onClick={() => setEditing(true)}>
            Edit
          </button>
        </div>
      )}
      {a.place && <span className="muted small">{a.place}</span>}
      <ErrorNote message={error} />
    </li>
  );
}
