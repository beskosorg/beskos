import { useCallback, useMemo, useSyncExternalStore } from "react";
import { JmapClient } from "../jmap/client";
import { MailService } from "../jmap/mail";
import { apiBase, getSessionToken, setSessionToken, setUnauthorizedHandler } from "../apiBase";
import { backend } from "./backend";

// Webmail auth (credential brokering): the mailbox credential no longer lives in the browser.
// Login authenticates against our API (session: HttpOnly cookie on the web, opaque token in the
// native app) and the backend stores the encrypted JMAP credential; the proxy injects it
// server-to-server. Here we only persist NON-secret data (email + accountId) to rehydrate the UI
// between visits — never the password.
const KEY = "mailin.wm.auth";

interface Stored {
  email: string;
  accountId: string;
}

function read(): Stored | null {
  const raw = localStorage.getItem(KEY);
  return raw ? (JSON.parse(raw) as Stored) : null;
}

// Minimal store so useSyncExternalStore reacts to login/logout.
const listeners = new Set<() => void>();
function emit() { listeners.forEach((l) => l()); }
function subscribe(l: () => void) { listeners.add(l); return () => listeners.delete(l); }

// An invalid session on ANY call clears local state; useSyncExternalStore then re-renders into
// the login screen. (Registered at module scope: runs once, without depending on a mounted component.)
setUnauthorizedHandler(() => {
  if (!localStorage.getItem(KEY)) return; // already logged out — avoids loops
  setSessionToken("");
  localStorage.removeItem(KEY);
  emit();
});

export interface WebmailSession {
  email: string;
  service: MailService;
  logout: () => void;
}

export function useWebmail(): { session: WebmailSession | null; login: typeof login } {
  const stored = useSyncExternalStore(subscribe, () => localStorage.getItem(KEY));

  const logout = useCallback(() => {
    void backend.logout();
    setSessionToken("");
    localStorage.removeItem(KEY);
    emit();
  }, []);

  // Memoized per account: the MailService (and the JmapClient) keep a stable identity across
  // renders, essential so the SSE push (useJmapPush) does not reconnect on every render. JMAP
  // authenticates via the session: same-origin cookie on the web, Bearer token in the native app
  // (getSessionToken).
  const session = useMemo<WebmailSession | null>(() => {
    if (!stored) return null;
    const parsed = JSON.parse(stored) as Stored;
    return {
      email: parsed.email,
      service: new MailService(new JmapClient(apiBase(), { bearer: getSessionToken() }), parsed.accountId),
      logout,
    };
  }, [stored, logout]);

  return { session, login };
}

// MfaChallenge: when the user has a second factor, login returns this instead of a session. The
// UI asks for the code and calls verifyMfa to complete it.
export interface MfaChallenge { challenge: string; methods: string[] }

// establishSession finishes login with the issued token: stores it (memory/localStorage),
// validates the JMAP session through our proxy, and persists the account. Shared by both the
// no-MFA and post-MFA paths.
async function establishSession(email: string, token: string): Promise<void> {
  setSessionToken(token); // memory (web) / localStorage (native app) — opaque token, not the password
  const client = new JmapClient(apiBase(), { bearer: getSessionToken() });
  const session = await client.session(); // authenticated via the session; the proxy injects the mailbox's Basic auth
  const stored: Stored = { email, accountId: session.accountId };
  localStorage.setItem(KEY, JSON.stringify(stored));
  emit();
}

// login opens the session on our API (which validates the credential against Stalwart and stores
// the encrypted JMAP credential server-side). If the account has a second factor, there is NO
// session yet: it returns the challenge for the UI to ask for the code — returning early here
// matters, since an empty token would otherwise make the JMAP session fail with a 401. Without
// MFA, it signs in right away.
export async function login(email: string, password: string): Promise<MfaChallenge | null> {
  const resp = await backend.login(email, password); // 401 → throws and doesn't persist
  if (resp.mfa_required) return { challenge: resp.challenge ?? "", methods: resp.methods ?? [] };
  await establishSession(email, resp.token ?? "");
  return null;
}

// verifyMfa exchanges the challenge + code for the token and completes login.
export async function verifyMfa(email: string, challenge: string, code: string): Promise<void> {
  const resp = await backend.mfaVerify(challenge, code);
  await establishSession(email, resp.token);
}

export { read as readWebmailAuth };
