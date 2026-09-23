import { useState } from "react";
import { useTranslation } from "react-i18next";
import { defaultServer, setServer } from "../apiBase";
import { BESKOS_MARK } from "./branding";
import "./onboarding.css";

// First launch of the packaged application: which server does it talk to.
//
// The web build never sees this — there the app is served by the same origin it calls. The
// packaged one has no origin of its own worth calling, so until this is answered nothing can be
// reached, including the login screen. That is why it gates the router rather than living in
// settings: a login form pointed at nowhere fails in a way that looks like wrong credentials.
//
// The address is checked before it is kept. Somebody typing their mail domain instead of the
// application's, or an instance that is not up, otherwise lands on a login screen that refuses
// every password with no explanation.

export function ServerSetup({ onDone }: { onDone?: () => void }) {
  const { t } = useTranslation();
  const [url, setUrl] = useState(defaultServer());
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const normalise = (raw: string) => {
    const v = raw.trim().replace(/\/$/, "");
    if (!v) return "";
    return /^https?:\/\//i.test(v) ? v : `https://${v}`;
  };

  async function submit() {
    const target = normalise(url);
    if (!target) return;
    setBusy(true);
    setErr("");
    try {
      // GET /api/edition needs no session and every instance answers it, which makes it the one
      // call that can tell "this is a Beskos server" apart from "this host exists".
      const res = await fetch(`${target}/api/edition`, { headers: { Accept: "application/json" } });
      if (!res.ok) throw new Error(String(res.status));
      await res.json();
      onDone?.();
      setServer(target); // persists and reloads
    } catch {
      setErr(t("serverSetup.unreachable"));
      setBusy(false);
    }
  }

  return (
    <div className="wm">
      <div className="ob">
        <div className="ob-card">
          <header className="ob-head">
            <img src={BESKOS_MARK} alt="" />
          </header>
          <h1>{t("serverSetup.title")}</h1>
          <p className="ob-sub">{t("serverSetup.blurb")}</p>
          <label className="ob-fld">
            <span>{t("serverSetup.label")}</span>
            <input
              type="url"
              inputMode="url"
              autoFocus
              placeholder="https://mail.example.org"
              value={url}
              onChange={(e) => { setErr(""); setUrl(e.target.value); }}
              onKeyDown={(e) => { if (e.key === "Enter") void submit(); }}
            />
          </label>
          {err && <p className="ob-err">{err}</p>}
          <button className="ob-cta" disabled={busy || !url.trim()} onClick={() => void submit()}>
            {busy ? "…" : t("serverSetup.continue")}
          </button>
          <p className="ob-sub" style={{ marginTop: 14, marginBottom: 0 }}>{t("serverSetup.hint")}</p>
        </div>
      </div>
    </div>
  );
}
