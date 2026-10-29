import { useCallback, useEffect, useRef, useState } from "react";
import { SignInRequired } from "./api";
import { useSession } from "./session";

export interface Loaded<T> {
  data: T | undefined;
  error: string;
  loading: boolean;
  reload: () => Promise<void>;
}

/**
 * Loads data and reloads it when deps change, and every `pollMs` while `poll` is (or returns) true.
 * A 401 anywhere sends the app back to the sign-in screen.
 */
export function useLoad<T>(
  load: () => Promise<T>,
  deps: unknown[],
  opts: { pollMs?: number; poll?: boolean | ((data: T | undefined) => boolean) } = {},
): Loaded<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const { signedOut } = useSession();
  const loadRef = useRef(load);
  loadRef.current = load;

  const reload = useCallback(async () => {
    try {
      const value = await loadRef.current();
      setData(value);
      setError("");
    } catch (e) {
      if (e instanceof SignInRequired) signedOut();
      else setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [signedOut]);

  useEffect(() => {
    setLoading(true);
    void reload();
  }, deps);

  const { pollMs = 3000, poll = false } = opts;
  const polling = typeof poll === "function" ? poll(data) : poll;
  useEffect(() => {
    if (!polling) return;
    const timer = setInterval(() => void reload(), pollMs);
    return () => clearInterval(timer);
  }, [polling, pollMs, reload]);

  return { data, error, loading, reload };
}

/**
 * Wraps a change (a button press): tracks busy state and the error message, and handles sign-out.
 * Returns the action's result, or undefined when it failed.
 */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { signedOut } = useSession();

  const run = useCallback(
    async <T>(action: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true);
      setError("");
      try {
        return await action();
      } catch (e) {
        if (e instanceof SignInRequired) signedOut();
        else setError(e instanceof Error ? e.message : String(e));
        return undefined;
      } finally {
        setBusy(false);
      }
    },
    [signedOut],
  );

  return { run, busy, error, clearError: () => setError("") };
}
