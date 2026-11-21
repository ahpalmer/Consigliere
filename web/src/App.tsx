import { useCallback, useEffect, useState } from "react";
import { NavLink, Route, Routes, useLocation } from "react-router-dom";
import { api } from "./api";
import { SessionContext } from "./session";
import { AdventureGenerate, AdventureHistory, AdventureIdeas, AdventuresUpcoming } from "./pages/Adventures";
import { Board } from "./pages/Board";
import { Code } from "./pages/Code";
import { ItemPage } from "./pages/ItemPage";
import { Login } from "./pages/Login";
import { Options } from "./pages/Options";
import { PursuitPage } from "./pages/PursuitPage";
import { StatusPage } from "./pages/StatusPage";
import { Suggested } from "./pages/Suggested";

type Gate = "checking" | "signed-in" | "signed-out" | "offline";

export function App() {
  const [gate, setGate] = useState<Gate>("checking");
  const [signInRequired, setSignInRequired] = useState(false);
  const [code, setCode] = useState(false);
  const [adventures, setAdventures] = useState(false);
  // Sections (M13): Jobs always, Adventures when the bot's data pack turns it on. The nav shows the current one's pages.
  const section = useLocation().pathname.startsWith("/adventures") ? "adventures" : "jobs";

  const check = useCallback(async () => {
    try {
      const s = await api.session();
      setSignInRequired(s.signInRequired);
      setCode(s.code ?? false);
      setAdventures(s.adventures ?? false);
      setGate(s.signedIn ? "signed-in" : "signed-out");
    } catch {
      setGate("offline");
    }
  }, []);

  useEffect(() => {
    void check();
  }, [check]);

  const signedOut = useCallback(() => setGate("signed-out"), []);

  if (gate === "checking") return null;
  if (gate === "offline") {
    return (
      <main className="center-screen">
        <div className="card narrow">
          <h1 className="brand-title">Consigliere</h1>
          <p>Can't reach the agent. Is it running?</p>
          <button className="btn" onClick={() => void check()}>
            Try again
          </button>
        </div>
      </main>
    );
  }
  if (gate === "signed-out") return <Login onSignedIn={() => void check()} />;

  return (
    <SessionContext.Provider value={{ signInRequired, signedOut }}>
      <header className="topbar">
        <div className="topbar-inner">
          <NavLink to="/" className="brand" end>
            <span className="brand-mark" aria-hidden="true">
              C
            </span>
            Consigliere
          </NavLink>
          {adventures && (
            <nav className="sections" aria-label="Section">
              <NavLink to="/" className={section === "jobs" ? "active" : ""}>
                Jobs
              </NavLink>
              <NavLink to="/adventures" className={section === "adventures" ? "active" : ""}>
                Adventures
              </NavLink>
            </nav>
          )}
          <nav className="nav">
            {section === "adventures" ? (
              <>
                <NavLink to="/adventures" end>
                  Upcoming
                </NavLink>
                <NavLink to="/adventures/ideas">Ideas</NavLink>
                <NavLink to="/adventures/generate">Generate</NavLink>
                <NavLink to="/adventures/history">History</NavLink>
              </>
            ) : (
              <>
                <NavLink to="/" end>
                  Pipeline
                </NavLink>
                <NavLink to="/suggested">Suggested</NavLink>
                <NavLink to="/options">Options</NavLink>
              </>
            )}
            {code && <NavLink to="/code">Code</NavLink>}
            <NavLink to="/status">Status</NavLink>
          </nav>
          {signInRequired && (
            <button
              className="btn ghost small"
              onClick={async () => {
                await api.logout();
                signedOut();
              }}
            >
              Sign out
            </button>
          )}
        </div>
      </header>
      <main className="page">
        <Routes>
          <Route path="/" element={<Board />} />
          <Route path="/pursuits/:id" element={<PursuitPage />} />
          <Route path="/suggested" element={<Suggested />} />
          <Route path="/options" element={<Options />} />
          <Route path="/items/:id" element={<ItemPage />} />
          <Route path="/status" element={<StatusPage />} />
          {code && <Route path="/code" element={<Code />} />}
          {adventures && (
            <>
              <Route path="/adventures" element={<AdventuresUpcoming />} />
              <Route path="/adventures/ideas" element={<AdventureIdeas />} />
              <Route path="/adventures/generate" element={<AdventureGenerate />} />
              <Route path="/adventures/history" element={<AdventureHistory />} />
            </>
          )}
          <Route path="*" element={<p className="muted">Nothing here.</p>} />
        </Routes>
      </main>
    </SessionContext.Provider>
  );
}
