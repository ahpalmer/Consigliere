import { createContext, useContext } from "react";

export interface Session {
  signInRequired: boolean;
  /** Call when the server says the session is gone; shows the sign-in screen. */
  signedOut: () => void;
}

export const SessionContext = createContext<Session>({ signInRequired: false, signedOut: () => {} });

export const useSession = () => useContext(SessionContext);
