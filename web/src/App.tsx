import { useCallback, useEffect, useState } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
import { api } from "./api";
import { SessionContext } from "./session";
import { Board } from "./pages/Board";
import { ItemPage } from "./pages/ItemPage";
import { Login } from "./pages/Login";
import { Options } from "./pages/Options";
import { PursuitPage } from "./pages/PursuitPage";
import { StatusPage } from "./pages/StatusPage";

type Gate = "checking" | "signed-in" | "signed-out" | "offline";

export function App() {
  const [gate, setGate] = useState<Gate>("checking");
  const [signInRequired, setSignInRequired] = useState(false);

  const check = useCallback(async () => {
    try {
      const s = await api.session();
      setSignInRequired(s.signInRequired);
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
  if (gate === "signed-out") return <Login onSignedIn={() => setGate("signed-in")} />;

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
          <nav className="nav">
            <NavLink to="/" end>
              Pipeline
            </NavLink>
            <NavLink to="/options">Options</NavLink>
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
          <Route path="/options" element={<Options />} />
          <Route path="/items/:id" element={<ItemPage />} />
          <Route path="/status" element={<StatusPage />} />
          <Route path="*" element={<p className="muted">Nothing here.</p>} />
        </Routes>
      </main>
    </SessionContext.Provider>
  );
}
