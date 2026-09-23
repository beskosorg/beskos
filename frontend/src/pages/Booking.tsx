import "../webmail/webmail.css";
import { LanguageSelect } from "../webmail/LanguageSelect";
import "./booking.css";
import { useEffect, useMemo, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { apiUrl } from "../apiBase";
import { hueFor, initials } from "../webmail/util";
import { applyBrandColor } from "../webmail/branding";
import { useBrandingCapability } from "../useEdition";

// PUBLIC scheduling page (/b/:org/:user -- Calendly-style): whoever has the link picks a free day
// and time slot and fills in their details; the backend creates the event and the Meet room,
// sends a confirmation email and notifies the team member. Reuses the theme tokens via .wm, with
// no session and no shell.
interface Info { org: string; name: string; avatar?: string; title: string; duration_min: number; phone_field: string; company_field: string; weekdays: number[]; brand?: { color?: string; logo?: string; logo_white_dark?: boolean } }

const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

export function BookingPage() {
  return <div className="wm"><div className="bkp"><BookingFlow /></div></div>;
}

function BookingFlow() {
  const { t, i18n } = useTranslation();
  const { org = "", user = "" } = useParams();
  const base = apiUrl(`/api/public/booking/${encodeURIComponent(org)}/${encodeURIComponent(user)}`);

  const [info, setInfo] = useState<Info | null>(null);
  const [err, setErr] = useState(false);
  const [day, setDay] = useState<Date | null>(null);
  const [slots, setSlots] = useState<string[] | null>(null);
  const [slot, setSlot] = useState("");
  const [form, setForm] = useState({ name: "", email: "", phone: "", company: "", note: "" });
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<{ starts_at: string; meet_url?: string } | null>(null);
  const [formErr, setFormErr] = useState("");

  useEffect(() => {
    fetch(base).then((r) => r.ok ? r.json() : Promise.reject()).then(setInfo).catch(() => setErr(true));
  }, [base]);
  // Organization branding: the public page wears the customer's brand color.
  useEffect(() => { if (info?.brand?.color) applyBrandColor(info.brand.color); return () => applyBrandColor(""); }, [info?.brand?.color]);

  // Next 21 days within the window (the backend decides real availability per day).
  const days = useMemo(() => {
    if (!info) return [];
    const out: Date[] = [];
    for (let i = 0; i < 21 && out.length < 14; i++) {
      const d = new Date(); d.setDate(d.getDate() + i); d.setHours(0, 0, 0, 0);
      if (info.weekdays.includes(d.getDay())) out.push(d);
    }
    return out;
  }, [info]);

  useEffect(() => {
    if (!day) return;
    setSlots(null); setSlot("");
    fetch(`${base}/slots?date=${iso(day)}`).then((r) => r.ok ? r.json() : Promise.reject())
      .then((r) => setSlots(r.slots ?? [])).catch(() => setSlots([]));
  }, [day, base]);

  const submit = () => {
    setFormErr("");
    if (!form.name.trim()) { setFormErr(t("booking.nameRequired")); return; }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(form.email.trim())) { setFormErr(t("booking.emailInvalid")); return; }
    if (info?.phone_field === "required" && !form.phone.trim()) { setFormErr(t("booking.phoneRequired")); return; }
    if (info?.company_field === "required" && !form.company.trim()) { setFormErr(t("booking.companyRequired")); return; }
    setBusy(true);
    fetch(`${base}/book`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ...form, starts_at: slot }),
    }).then(async (r) => {
      if (r.status === 409) { setFormErr(t("booking.slotTaken")); setSlot(""); setSlots(null); if (day) setDay(new Date(day)); return; }
      if (!r.ok) { setFormErr(t("booking.failed")); return; }
      setDone(await r.json());
    }).catch(() => setFormErr(t("booking.failed"))).finally(() => setBusy(false));
  };

  if (err) return <div className="bkp-card bkp-center"><h2>{t("booking.notFound")}</h2></div>;
  if (!info) return <div className="bkp-card bkp-center"><span className="desc">…</span></div>;

  const fmtDay = (d: Date) => d.toLocaleDateString(i18n.language, { weekday: "short", day: "2-digit", month: "short" });
  const fmtTime = (s: string) => new Date(s).toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" });

  if (done) {
    return (
      <div className="bkp-card bkp-center">
        <div className="bkp-ok">✓</div>
        <h2>{t("booking.confirmed")}</h2>
        <p className="bkp-when">{new Date(done.starts_at).toLocaleDateString(i18n.language, { weekday: "long", day: "2-digit", month: "long" })} · {fmtTime(done.starts_at)} · {info.duration_min} min</p>
        <p className="desc">{t("booking.confirmedHint", { email: form.email.trim() })}</p>
        {done.meet_url && <a className="add" href={done.meet_url}>{t("booking.joinMeet")}</a>}
        <PoweredBy />
      </div>
    );
  }

  return (
    <div className="bkp-card">
      <LangSwitch />
      {info.brand?.logo && <img className={"bkp-logo" + (info.brand.logo_white_dark ? " white-dark" : "")} src={info.brand.logo} alt={info.org} />}
      <div className="bkp-who">
        {info.avatar
          ? <img src={info.avatar} alt="" />
          : <span className="av" style={{ background: hueFor(info.name || org) }}>{initials(info.name || org)}</span>}
        <div>
          <b>{info.name}</b>
          <i>{info.org}</i>
        </div>
        <span className="bkp-dur">{info.duration_min} min</span>
      </div>
      {info.title && <h2 className="bkp-title">{info.title}</h2>}

      <div className="rlabel" style={{ margin: "14px 0 7px", padding: 0 }}>{t("booking.pickDay")}</div>
      <div className="bkp-days">
        {days.map((d) => (
          <button key={d.toISOString()} className={day && iso(day) === iso(d) ? "on" : ""} onClick={() => setDay(d)}>{fmtDay(d)}</button>
        ))}
        {days.length === 0 && <p className="desc">{t("booking.noAvailability")}</p>}
      </div>

      {day && (
        <>
          <div className="rlabel" style={{ margin: "14px 0 7px", padding: 0 }}>{t("booking.pickTime")}</div>
          {slots === null && <span className="desc">…</span>}
          {slots !== null && slots.length === 0 && <p className="desc">{t("booking.dayFull")}</p>}
          <div className="bkp-slots">
            {(slots ?? []).map((s) => (
              <button key={s} className={slot === s ? "on" : ""} onClick={() => setSlot(s)}>{fmtTime(s)}</button>
            ))}
          </div>
        </>
      )}

      {slot && (
        <div className="bkp-form">
          <div className="field"><label>{t("booking.yourName")} *</label>
            <input value={form.name} maxLength={120} onChange={(e) => setForm({ ...form, name: e.target.value })} /></div>
          <div className="field"><label>{t("booking.yourEmail")} *</label>
            <input type="email" value={form.email} maxLength={254} onChange={(e) => setForm({ ...form, email: e.target.value })} /></div>
          {info.phone_field !== "off" && (
            <div className="field"><label>{t("booking.yourPhone")}{info.phone_field === "required" ? " *" : ""}</label>
              <input value={form.phone} maxLength={40} onChange={(e) => setForm({ ...form, phone: e.target.value })} /></div>
          )}
          {info.company_field !== "off" && (
            <div className="field"><label>{t("booking.yourCompany")}{info.company_field === "required" ? " *" : ""}</label>
              <input value={form.company} maxLength={120} onChange={(e) => setForm({ ...form, company: e.target.value })} /></div>
          )}
          <div className="field"><label>{t("booking.yourNote")}</label>
            <textarea rows={2} value={form.note} maxLength={500} onChange={(e) => setForm({ ...form, note: e.target.value })} /></div>
          {formErr && <p className="bkp-err">{formErr}</p>}
          <button className="add" disabled={busy} onClick={submit}>
            {busy ? "…" : t("booking.confirm", { time: fmtTime(slot) })}
          </button>
        </div>
      )}
      <PoweredBy />
    </div>
  );
}

// Language switch on the public page: browser detection is automatic (LanguageDetector);
// this gives a manual override to whoever received the link.
function LangSwitch() {
  const { i18n } = useTranslation();
  return (
    <LanguageSelect className="bkp-lang" value={i18n.resolvedLanguage ?? i18n.language}
      onChange={(lng) => void i18n.changeLanguage(lng)} />
  );
}

// Product signature on the public page -- hidden when the instance has the Pro branding capability.
function PoweredBy() {
  if (useBrandingCapability()) return null;
  return <a className="bkp-powered" href="https://beskos.com" target="_blank" rel="noreferrer"><Trans i18nKey="public.poweredBy" values={{ brand: "Beskos" }} components={{ b: <b /> }} /></a>;
}
