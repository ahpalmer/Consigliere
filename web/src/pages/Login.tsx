import { useState } from "react";
import { api, ApiError } from "../api";
import { ErrorNote } from "../components";

export function Login({ onSignedIn }: { onSignedIn: () => void }) {
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  return (
    <main className="center-screen">
      <form
        className="card narrow"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            await api.login(token);
            onSignedIn();
          } catch (err) {
            setError(err instanceof ApiError && err.status === 401 ? "That token isn't right." : String(err));
          } finally {
            setBusy(false);
          }
        }}
      >
        <h1 className="brand-title">Consigliere</h1>
        <label className="field">
          <span>Dashboard token</span>
          <input
            type="password"
            autoComplete="current-password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            autoFocus
            required
          />
        </label>
        <ErrorNote message={error} />
        <button className="btn primary" disabled={busy || !token}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
        <p className="muted small">The token is the Web:Token secret.</p>
      </form>
    </main>
  );
}
