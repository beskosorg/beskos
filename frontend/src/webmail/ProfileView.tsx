import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { PasswordField } from "./PasswordField";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { backend } from "./backend";
import { toast } from "./toast";
import { api as panelApi } from "../api";
import { TotpDrawer, SmsDrawer } from "../panel/SecuritySection";
import { MigrationSection } from "../panel/MigrationSection";
import { IcUsers, IcKey, IcShield, IcDownload } from "../panel/icons";

type Section = "profile" | "password" | "security" | "migration";
const SECTIONS: Section[] = ["profile", "password", "security", "migration"];

// "My account" page (avatar menu): SAME pattern as Admin — section rail on the left,
// content on the right (mobile: list -> detail). 2FA respects the organization's policy:
// when required -> it can't be disabled and only the allowed method appears.
export function ProfileView({ section, onSection }: { section?: string; onSection: (s: string) => void }) {
  const { t } = useTranslation();
  const current: Section | null = SECTIONS.includes(section as Section) ? (section as Section) : null;
  const shown: Section = current ?? "profile";

  const items: { id: Section; label: string; icon: JSX.Element }[] = [
    { id: "profile", label: t("profile.sectionProfile"), icon: <IcUsers size={17} /> },
    { id: "password", label: t("profile.passwordTitle"), icon: <IcKey size={17} /> },
    { id: "security", label: t("profile.mfaTitle"), icon: <IcShield size={17} /> },
    // Moving in from the old provider. It lives here, not in the admin console, because everyone
    // migrates their OWN mailbox -- and a migration gated behind an administrator is one that never
    // happens for the eight people who are not one.
    { id: "migration", label: t("migration.title"), icon: <IcDownload size={17} /> },
  ];

  return (
    <div className={"adm" + (current ? " has-section" : "")}>
      <aside className="adm-rail">
        <div className="adm-title">{t("profile.title")}</div>
        {items.map((i) => (
          <button key={i.id} className={shown === i.id ? "on" : ""} onClick={() => onSection(i.id)}>
            {i.icon} <span>{i.label}</span>
          </button>
        ))}
      </aside>
      <section className="adm-main">
        <button className="adm-back" onClick={() => onSection("")}>‹ {t("profile.title")}</button>
        {shown === "profile" && <ProfileSection />}
        {shown === "password" && <PasswordSection />}
        {shown === "security" && <MFASection />}
        {shown === "migration" && <MigrationSection />}
      </section>
    </div>
  );
}

// ---------- Profile ----------
function ProfileSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["my-profile"], queryFn: () => backend.myProfile() });

  const [name, setName] = useState("");
  const [jobTitle, setJobTitle] = useState("");
  const [phone, setPhone] = useState("");
  const [avatar, setAvatar] = useState("");
  useEffect(() => {
    if (q.data) {
      setName(q.data.name); setJobTitle(q.data.job_title);
      setPhone(q.data.phone); setAvatar(q.data.avatar);
    }
  }, [q.data]);

  const save = useMutation({
    mutationFn: () => backend.updateMyProfile({ name, job_title: jobTitle, phone, avatar }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["my-profile"] });
      qc.invalidateQueries({ queryKey: ["me-presence"] });
      toast(t("profile.saved"));
    },
    onError: (e) => toast((e as Error).message),
  });

  const fileRef = useRef<HTMLInputElement>(null);
  function pickAvatar(f: File) {
    const img = new Image();
    img.onload = () => {
      const size = 128;
      const canvas = document.createElement("canvas");
      canvas.width = size; canvas.height = size;
      const ctx = canvas.getContext("2d")!;
      const m = Math.min(img.width, img.height);
      ctx.drawImage(img, (img.width - m) / 2, (img.height - m) / 2, m, m, 0, 0, size, size);
      setAvatar(canvas.toDataURL("image/jpeg", 0.85));
      URL.revokeObjectURL(img.src);
    };
    img.src = URL.createObjectURL(f);
  }

  const initials = (name || q.data?.email || "?").slice(0, 2).toUpperCase();

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("profile.sectionProfile")}</h1><div className="desc">{t("profile.subtitle")}</div></div>
      </div>
      <div className="pnl-content">
        <div className="pnl-card"><div className="pnl-card-b">
          <div className="profile-id">
            {avatar
              ? <img className="profile-avatar" src={avatar} alt="" />
              : <div className="profile-avatar profile-avatar-fallback">{initials}</div>}
            <div className="profile-id-actions">
              <button className="pnl-btn" onClick={() => fileRef.current?.click()}>{t("profile.changePhoto")}</button>
              {avatar && <button className="pnl-btn ghost" onClick={() => setAvatar("")}>{t("profile.removePhoto")}</button>}
              <input ref={fileRef} type="file" accept="image/*" hidden
                onChange={(e) => { const f = e.target.files?.[0]; if (f) pickAvatar(f); e.target.value = ""; }} />
              <div className="desc">{t("profile.photoHint")}</div>
            </div>
          </div>
          <div className="profile-grid">
            <div className="pnl-field"><label>{t("profile.name")}</label>
              <input className="pnl-input" value={name} onChange={(e) => setName(e.target.value)} /></div>
            <div className="pnl-field"><label>{t("profile.jobTitle")}</label>
              <input className="pnl-input" placeholder={t("profile.jobTitlePh")} value={jobTitle} onChange={(e) => setJobTitle(e.target.value)} /></div>
            <div className="pnl-field"><label>{t("profile.phone")}</label>
              <input className="pnl-input" placeholder="+351 912 345 678" value={phone} onChange={(e) => setPhone(e.target.value)} /></div>
            <div className="pnl-field"><label>{t("profile.email")}</label>
              <input className="pnl-input" value={q.data?.email ?? ""} disabled /></div>
          </div>
          <div className="profile-actions">
            <button className="pnl-btn primary" disabled={save.isPending} onClick={() => save.mutate()}>
              {t("common.save")}
            </button>
          </div>
        </div></div>
      </div>
    </>
  );
}

// ---------- Password ----------
function PasswordSection() {
  const { t } = useTranslation();
  const [curPw, setCurPw] = useState("");
  const [newPw, setNewPw] = useState("");
  const [pwErr, setPwErr] = useState("");
  // The meter decides whether the password passes: the mail server rejects weak ones, and it's
  // better to say so right at the field than after submitting.
  const pwOk = useRef(false);
  const meQ = useQuery({ queryKey: ["me-profile"], queryFn: () => backend.me(), retry: false });
  const email = meQ.data?.email ?? "";
  const changePw = useMutation({
    mutationFn: () => backend.changeMyPassword(curPw, newPw),
    onSuccess: () => { setCurPw(""); setNewPw(""); setPwErr(""); toast(t("profile.passwordChanged")); },
    onError: (e) => setPwErr((e as Error).message),
  });

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("profile.passwordTitle")}</h1><div className="desc">{t("profile.passwordHint")}</div></div>
      </div>
      <div className="pnl-content">
        <div className="pnl-card"><div className="pnl-card-b">
          <div className="profile-grid">
            <div className="pnl-field"><label>{t("profile.currentPassword")}</label>
              <input className="pnl-input" type="password" autoComplete="current-password" value={curPw} onChange={(e) => setCurPw(e.target.value)} /></div>
            <PasswordField label={t("profile.newPassword")} value={newPw} userInputs={[email]}
              onChange={setNewPw} onStrength={(s) => { pwOk.current = s.ok; }} />
          </div>
          {pwErr && <p className="pnl-err">{pwErr}</p>}
          <div className="profile-actions">
            <button className="pnl-btn primary" disabled={!curPw || !pwOk.current || changePw.isPending}
              onClick={() => changePw.mutate()}>{t("profile.changePassword")}</button>
          </div>
        </div></div>
      </div>
    </>
  );
}

// ---------- 2FA (aware of the organization's policy) ----------
function MFASection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => panelApi.me(), retry: false });

  const [mfaDrawer, setMfaDrawer] = useState<"totp" | "sms" | null>(null);
  const [recovery, setRecovery] = useState<string[]>([]);
  const [disablePw, setDisablePw] = useState("");
  const [disableOpen, setDisableOpen] = useState(false);
  const [err, setErr] = useState("");

  const totpOn = meQ.data?.mfa?.totp, smsOn = meQ.data?.mfa?.sms;
  const orgRequired = meQ.data?.org_mfa_required === true;
  const method = meQ.data?.mfa_required_method ?? "any";

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["me"] });
    qc.invalidateQueries({ queryKey: ["me-presence"] });
  };
  const mfaDone = (codes: string[]) => {
    setRecovery(codes); setMfaDrawer(null); refresh(); toast(t("mfa.enabled"));
  };
  const disable = useMutation({
    mutationFn: () => panelApi.mfaDisable(disablePw),
    onSuccess: () => { setDisableOpen(false); setDisablePw(""); setErr(""); setRecovery([]); refresh(); toast(t("mfa.disabled")); },
    onError: (e) => setErr((e as Error).message),
  });

  // Policy fixes the method: only the allowed button appears.
  const canTotp = !totpOn && (!orgRequired || method !== "sms");
  const canSms = !smsOn && (!orgRequired || method !== "totp");

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("profile.mfaTitle")}</h1><div className="desc">{t("profile.mfaDesc")}</div></div>
      </div>
      <div className="pnl-content">
        <div className="pnl-card">
          <div className="pnl-card-h">
            <IcShield size={16} /><h3>{t("mfa.title")}</h3>
            <div className="grow" />
            <span className={"pnl-pill " + (totpOn ? "ok" : "muted")}>TOTP {totpOn ? "✓" : "—"}</span>
            <span className={"pnl-pill " + (smsOn ? "ok" : "muted")}>SMS {smsOn ? "✓" : "—"}</span>
          </div>
          <div className="pnl-card-b">
            {orgRequired && (
              <div className="warnbox" style={{ marginTop: 0, marginBottom: 12 }}>
                {t("profile.orgRequired_" + method)}
              </div>
            )}
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
              {canTotp && <button className="pnl-btn" onClick={() => setMfaDrawer("totp")}>{t("mfa.enableTotp")}</button>}
              {canSms && <button className="pnl-btn" onClick={() => setMfaDrawer("sms")}>{t("mfa.enableSms")}</button>}
              {/* Disable: NEVER when the organization requires 2FA. */}
              {!orgRequired && (totpOn || smsOn) && !disableOpen && (
                <button className="pnl-btn danger" onClick={() => setDisableOpen(true)}>{t("mfa.disable")}</button>
              )}
            </div>
            {disableOpen && (
              <div style={{ marginTop: 12, maxWidth: 360 }}>
                <div className="pnl-field"><label>{t("profile.confirmWithPassword")}</label>
                  <input className="pnl-input" type="password" autoComplete="current-password" autoFocus
                    value={disablePw} onChange={(e) => { setDisablePw(e.target.value); setErr(""); }} /></div>
                {err && <p className="pnl-err">{err}</p>}
                <div style={{ display: "flex", gap: 8 }}>
                  <button className="pnl-btn ghost" onClick={() => { setDisableOpen(false); setDisablePw(""); setErr(""); }}>{t("common.cancel")}</button>
                  <button className="pnl-btn danger" disabled={!disablePw || disable.isPending}
                    onClick={() => disable.mutate()}>{t("mfa.disable")}</button>
                </div>
              </div>
            )}
            {recovery.length > 0 && (
              <div style={{ marginTop: 12 }}>
                <p style={{ fontWeight: 600, margin: "4px 0 2px" }}>{t("mfa.recoveryTitle")}</p>
                <p className="desc" style={{ marginTop: 0 }}>{t("mfa.recoveryHint")}</p>
                <div className="pnl-code-grid">{recovery.map((c) => <span key={c}>{c}</span>)}</div>
              </div>
            )}
          </div>
        </div>
      </div>

      {mfaDrawer === "totp" && <TotpDrawer onClose={() => setMfaDrawer(null)} onDone={mfaDone} />}
      {mfaDrawer === "sms" && <SmsDrawer onClose={() => setMfaDrawer(null)} onDone={mfaDone} />}
    </>
  );
}
