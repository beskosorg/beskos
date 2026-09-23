// End user session (login, MFA, logout). Extracted from backend.ts (law 1.4 -- 600 lines).
import { req } from "./backend";

export const authApi = {
  // End user session on our API (validates the credential against Stalwart). With a 2nd factor,
  // returns mfa_required + challenge (no token) -- the 2nd step is mfaVerify.
  login: (email: string, password: string) =>
    req<{ email?: string; name?: string; token?: string; mfa_required?: boolean; challenge?: string; methods?: string[] }>("/api/webmail/login", {
      method: "POST", body: JSON.stringify({ email, password }),
    }),
  // 2nd login step: exchanges the challenge + code (TOTP/SMS/recovery) for a session.
  mfaVerify: (challenge: string, code: string) =>
    req<{ token: string }>("/api/auth/mfa/verify", {
      method: "POST", body: JSON.stringify({ challenge, code }),
    }),
  logout: () => req<{ status: string }>("/api/webmail/logout", { method: "POST" }).catch(() => {}),
};
