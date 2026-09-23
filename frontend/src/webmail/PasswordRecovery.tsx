// Getting back in: asking for help, and setting the new password.
//
// A MAIL PROVIDER CANNOT SEND YOU A LINK. Everywhere else this screen says "check your e-mail" —
// here the e-mail is the thing you cannot open. So the honest wording is about who was told, not
// about an inbox to go and look in.
//
// THE ANSWER NEVER DEPENDS ON THE ADDRESS. Same message whether the account exists or not, because
// a form that says "no such user" is a way to find out who works at a company. The screen says so
// plainly rather than pretending to have sent something.

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { req } from "./backend";

export function ForgotPassword({ email, onBack }: { email: string; onBack: () => void }) {
  const { t } = useTranslation();
  const [value, setValue] = useState(email);
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    // Failure is treated as success on purpose: the server answers the same either way, and an
    // error here would be a signal the server deliberately refuses to give.
    try {
      await req("/api/public/password/forgot", { method: "POST", body: JSON.stringify({ email: value.trim() }) });
    } catch {
      /* same outcome */
    }
    setBusy(false);
    setSent(true);
  }

  if (sent) {
    return (
      <div className="lg-card">
        <h1 className="lg-h">{t("login.forgotSentTitle")}</h1>
        <p className="lg-sub">{t("login.forgotSent")}</p>
        <button className="lg-btn" onClick={onBack}>{t("login.backToSignIn")}</button>
      </div>
    );
  }

  return (
    <form className="lg-card" onSubmit={submit}>
      <h1 className="lg-h">{t("login.forgotTitle")}</h1>
      <p className="lg-sub">{t("login.forgotBlurb")}</p>
      <label className="lg-field">
        <span>{t("webmail.login.email")}</span>
        <input type="email" value={value} onChange={(e) => setValue(e.target.value)} required autoFocus />
      </label>
      <button className="lg-btn" disabled={busy || !value.trim()}>
        {busy ? "…" : t("login.forgotSubmit")}
      </button>
      <button type="button" className="lg-link" onClick={onBack}>{t("login.backToSignIn")}</button>
    </form>
  );
}

export function ResetPassword({ token }: { token: string }) {
  const { t } = useTranslation();
  const [pwd, setPwd] = useState("");
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  // Shown only after the server says the second factor is missing: asking everybody for a code they
  // may not have is how a recovery screen becomes another thing to be locked out of.
  const [needsCode, setNeedsCode] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      await req("/api/public/password/reset", {
        method: "POST",
        body: JSON.stringify({ token, password: pwd, code: code.trim() }),
      });
      setDone(true);
    } catch (e) {
      const msg = (e as Error).message;
      if (msg.includes("mfa") || msg.includes("factor") || msg.includes("fator")) {
        setNeedsCode(true);
        setErr(needsCode ? t("login.resetBadCode") : "");
      } else {
        setErr(msg);
      }
    } finally {
      setBusy(false);
    }
  }

  if (done) {
    return (
      <div className="lg-card">
        <h1 className="lg-h">{t("login.resetDoneTitle")}</h1>
        <p className="lg-sub">{t("login.resetDone")}</p>
        <a className="lg-btn" href="/mail">{t("login.backToSignIn")}</a>
      </div>
    );
  }

  return (
    <form className="lg-card" onSubmit={submit}>
      <h1 className="lg-h">{t("login.resetTitle")}</h1>
      <p className="lg-sub">{t("login.resetBlurb")}</p>
      <label className="lg-field">
        <span>{t("login.newPassword")}</span>
        <input type="password" value={pwd} onChange={(e) => setPwd(e.target.value)} required autoFocus minLength={10} />
      </label>
      {needsCode && (
        <label className="lg-field">
          <span>{t("login.secondFactor")}</span>
          <input inputMode="numeric" autoComplete="one-time-code" value={code}
            onChange={(e) => setCode(e.target.value)} required autoFocus />
          <small>{t("login.secondFactorHint")}</small>
        </label>
      )}
      {err && <p className="lg-err">{err}</p>}
      <button className="lg-btn" disabled={busy || pwd.length < 10}>{busy ? "…" : t("login.resetSubmit")}</button>
    </form>
  );
}
