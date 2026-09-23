import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { login, verifyMfa, type MfaChallenge } from "./auth";
import { backend, ApiError, type Branding } from "./backend";
import { applyBrandColor, cachedBrand, BESKOS_MARK } from "./branding";
import { ForgotPassword } from "./PasswordRecovery";
import { apiBase, setServer } from "../apiBase";
import { isTauri } from "./tauriNotify";

// Webmail login: authenticates the END USER against Stalwart (Basic behind the proxy, §D7).
// Distinct from the panel login (our API). Here the credential is the mailbox's own.
export function WebmailLogin() {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [pwd, setPwd] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  // Second factor: when login returns a challenge, the code step is shown instead of email/password.
  const [mfa, setMfa] = useState<MfaChallenge | null>(null);
  const [code, setCode] = useState("");
  // Locked out: a separate screen rather than a modal, because somebody who cannot get in has
  // one thing to do and should not be looking at the form that just refused them.
  const [forgot, setForgot] = useState(false);
  // Brand on the login: starts from the last saved one (shows up immediately on return) and resolves by
  // the organization of the typed email (Google-style), once the email looks complete.
  const [brand, setBrand] = useState<Branding | null>(() => cachedBrand());
  useEffect(() => { applyBrandColor(brand?.color ?? ""); }, [brand?.color]);
  useEffect(() => {
    if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)) return;
    const id = setTimeout(() => { backend.brandingResolve(email).then((b) => { if (b.color || b.logo || b.name) setBrand(b); }).catch(() => {}); }, 400);
    return () => clearTimeout(id);
  }, [email]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(""); setBusy(true);
    try {
      const challenge = await login(email, pwd);
      if (challenge) { setMfa(challenge); setCode(""); } // account has a second factor → ask for the code
    } catch (e) {
      // 401 = wrong credential; any other failure = server unreachable (don't conflate
      // "wrong password" with "backend is down", law §4.4). Classified by the ApiError's STATUS,
      // not by regex on the message: the message comes back translated, and a translated 401 doesn't contain "401".
      const status = (e as ApiError)?.status;
      setErr(status === 401 ? t("webmail.login.failed") : t("webmail.login.unreachable"));
    } finally {
      setBusy(false);
    }
  }

  async function submitCode(e: React.FormEvent) {
    e.preventDefault();
    setErr(""); setBusy(true);
    try {
      await verifyMfa(email, mfa!.challenge, code.trim());
    } catch (e) {
      const status = (e as ApiError)?.status;
      setErr(status === 401 ? t("webmail.login.badCode") : t("webmail.login.unreachable"));
    } finally {
      setBusy(false);
    }
  }

  // The split screen. Left is the organisation — their logo, their name, their colour, at a size
  // that means something. Right is the act of signing in, and nothing else.
  //
  // WHY A PANEL AND NOT A BIGGER LOGO: this screen is white-labelled. A 26px mark above a grey card
  // told a customer's staff nothing about whose system they were entering; half the screen in their
  // own colour does. On a phone the panel collapses to a header, because there is no half to give.
  const panel = (
    <aside className="lg-panel">
      <div className="lg-panel-in">
        <img className={"lg-mark" + (brand?.logo && brand?.logo_white_dark ? " white-dark" : "")}
          src={brand?.logo || BESKOS_MARK} alt={brand?.name || "Beskos"} />
        <h2>{brand?.name || "Beskos"}</h2>
        {/* Only ours: putting words in a customer's mouth on their own sign-in screen is not our place. */}
        {!brand?.name && <p>{t("webmail.login.tagline")}</p>}
      </div>
    </aside>
  );

  if (forgot) {
    return (
      <div className="wm">
        <div className="lg-split">
          {panel}
          <main className="lg-main"><ForgotPassword email={email} onBack={() => setForgot(false)} /></main>
        </div>
      </div>
    );
  }

  return (
    <div className="wm">
      <div className="lg-split">
        {panel}
        <main className="lg-main">
          {!mfa ? (
            <form className="lg-card" onSubmit={submit}>
              <h1 className="lg-h">{t("webmail.login.signInTitle")}</h1>
              <p className="lg-sub">{t("webmail.login.subtitle")}</p>

              <label className="lg-field">
                <span>{t("webmail.login.emailLabel")}</span>
                <input type="email" value={email} autoComplete="username" required autoFocus
                  placeholder={t("webmail.login.email")}
                  onChange={(e) => setEmail(e.target.value)} />
              </label>
              <label className="lg-field">
                <span>{t("webmail.login.passwordLabel")}</span>
                <input type="password" value={pwd} autoComplete="current-password" required
                  onChange={(e) => setPwd(e.target.value)} />
              </label>

              {err && <p className="lg-err">{err}</p>}
              <button className="lg-btn" disabled={busy}>{busy ? "…" : t("webmail.login.submit")}</button>
              {/* Getting the address wrong puts every password on this screen through to nothing,
                  and the failure is indistinguishable from a wrong password. This is the way back. */}
              {isTauri() && (
                <button type="button" className="lg-link" onClick={() => setServer("")}>
                  {t("serverSetup.change", { server: apiBase().replace(/^https?:\/\//, "") })}
                </button>
              )}
              <button type="button" className="lg-link" onClick={() => setForgot(true)}>
                {t("login.forgotLink")}
              </button>
            </form>
          ) : (
            <form className="lg-card" onSubmit={submitCode}>
              <h1 className="lg-h">{t("webmail.login.mfaTitle")}</h1>
              <p className="lg-sub">{t("webmail.login.mfaPrompt")}</p>
              <label className="lg-field">
                <span>{t("webmail.login.codeLabel")}</span>
                <input inputMode="numeric" autoComplete="one-time-code" autoFocus value={code}
                  placeholder={t("webmail.login.codePlaceholder")}
                  onChange={(e) => setCode(e.target.value)} required />
              </label>
              {err && <p className="lg-err">{err}</p>}
              <button className="lg-btn" disabled={busy || !code.trim()}>{busy ? "…" : t("webmail.login.verify")}</button>
              <button type="button" className="lg-link" onClick={() => { setMfa(null); setErr(""); }}>
                {t("webmail.login.back")}
              </button>
            </form>
          )}
        </main>
      </div>
    </div>
  );
}
