import { useState } from "react";
import { useTranslation } from "react-i18next";
import { QRCodeSVG } from "qrcode.react";
import { api } from "../api";
import { Drawer } from "./ui";

// 2FA (TOTP/SMS) enrollment drawers, reused by the profile page and by MFAGate. (The old
// SecuritySection component was replaced by OrgSecuritySection in admin and by the profile's own
// security section.)
export function TotpDrawer({ onClose, onDone }: { onClose: () => void; onDone: (codes: string[]) => void }) {
  const { t } = useTranslation();
  const [data, setData] = useState<{ secret: string; otpauth_uri: string } | null>(null);
  const [code, setCode] = useState(""), [err, setErr] = useState("");
  if (!data) { api.totpStart().then(setData).catch((e) => setErr((e as Error).message)); }
  async function enable() {
    setErr("");
    try { const r = await api.totpEnable(code); onDone(r.recovery_codes); } catch (e) { setErr((e as Error).message); }
  }
  return (
    <Drawer title={t("mfa.enableTotp")} onClose={onClose}>
      {!data ? <div className="pnl-empty">…</div> : (
        <>
          <p className="desc" style={{ marginTop: 0 }}>{t("mfa.scanHint")}</p>
          {/* otpauth:// QR code: fixed white background for contrast even in dark theme. */}
          <div style={{ display: "flex", justifyContent: "center", background: "#fff", borderRadius: 12, padding: 14, margin: "4px 0 12px" }}>
            <QRCodeSVG value={data.otpauth_uri} size={172} marginSize={1} />
          </div>
          <div className="pnl-field"><label>{t("mfa.secret")}</label><div className="pnl-code-grid" style={{ gridTemplateColumns: "1fr" }}>{data.secret}</div></div>
          <div className="pnl-field" style={{ marginTop: 14 }}><label>{t("mfa.enterCode")}</label>
            <input className="pnl-input" placeholder="000000" value={code} onChange={(e) => setCode(e.target.value)} /></div>
          {err && <p className="pnl-err">{err}</p>}
          <button className="pnl-btn primary" style={{ marginTop: 8 }} onClick={enable}>{t("mfa.confirm")}</button>
        </>
      )}
    </Drawer>
  );
}

export function SmsDrawer({ onClose, onDone }: { onClose: () => void; onDone: (codes: string[]) => void }) {
  const { t } = useTranslation();
  const [phone, setPhone] = useState(""), [challenge, setChallenge] = useState(""), [code, setCode] = useState(""), [err, setErr] = useState("");
  async function start() { setErr(""); try { const r = await api.smsStart(phone); setChallenge(r.challenge); } catch (e) { setErr((e as Error).message); } }
  async function enable() { setErr(""); try { const r = await api.smsEnable(challenge, code); onDone(r.recovery_codes); } catch (e) { setErr((e as Error).message); } }
  return (
    <Drawer title={t("mfa.enableSms")} onClose={onClose}>
      {!challenge ? (
        <>
          <div className="pnl-field"><label>{t("mfa.phone")}</label><input className="pnl-input" placeholder="+55 11 99999-9999" value={phone} onChange={(e) => setPhone(e.target.value)} autoFocus /></div>
          {err && <p className="pnl-err">{err}</p>}
          <button className="pnl-btn primary" onClick={start}>{t("mfa.sendCode")}</button>
        </>
      ) : (
        <>
          <div className="pnl-field"><label>{t("mfa.enterCode")}</label><input className="pnl-input" placeholder="000000" value={code} onChange={(e) => setCode(e.target.value)} autoFocus /></div>
          {err && <p className="pnl-err">{err}</p>}
          <button className="pnl-btn primary" onClick={enable}>{t("mfa.confirm")}</button>
        </>
      )}
    </Drawer>
  );
}
