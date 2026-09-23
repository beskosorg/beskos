import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { backend, settingsApi, type UserSettings } from "./backend";
import { BESKOS_MARK } from "./branding";
import { PasswordField } from "./PasswordField";
import { BRAND_COLORS } from "./branding";
import { IcCheck, IcUsers, IcStar, IcPlus, IcX } from "../panel/icons";
import { useBrandingCapability } from "../useEdition";
import "./onboarding.css";

// Owner's first access: set the password, dress up the house (logo and color), and invite the
// team. Kept OUTSIDE the signup deliberately — before the card, every extra field is someone giving up.
//
// Only runs once: the completed flag lives in the user's settings (general.onboarded),
// so it doesn't need a new column or a migration.

type Step = 0 | 1 | 2;

export function Onboarding({ email, onDone }: { email: string; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [step, setStep] = useState<Step>(0);
  const [err, setErr] = useState("");
  // Branding is Pro (PATCH /api/branding answers 403 without it), so a Community install skips
  // that step instead of offering a logo and colour it cannot save.
  const branded = useBrandingCapability();
  const steps: Step[] = branded ? [0, 1, 2] : [0, 2];
  // Every step change clears the error: one step's failure is not the next step's problem, and a
  // leftover "requires a Pro licence" sitting above the invite form says something untrue.
  const go = (s: Step) => { setErr(""); setStep(s); };

  // Step 1 — password
  const [pw, setPw] = useState("");
  const [pw2, setPw2] = useState("");
  const strength = useRef(false); // password acceptable? (the meter decides)
  // Step 2 — brand
  const [color, setColor] = useState(BRAND_COLORS[0].hex);
  const [logo, setLogo] = useState("");
  // Step 3 — team
  const [invites, setInvites] = useState<string[]>([]);
  const [invite, setInvite] = useState("");

  const domains = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const domain = domains.data?.[0];

  const finish = useMutation({
    mutationFn: async () => {
      // The whole section, not just the flag: the server merge is shallow, so a partial patch of
      // `general` replaces it and drops whatever else was in there. See the same note in App.tsx.
      const prev = qc.getQueryData<UserSettings>(["settings"]);
      await settingsApi.patch({ general: { ...(prev?.general ?? {}), onboarded: true } });
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["settings"] }); onDone(); },
  });

  const savePassword = useMutation({
    mutationFn: () => api.resetUserPassword(email, pw),
    onSuccess: () => go(branded ? 1 : 2),
    onError: (e) => setErr((e as Error).message),
  });
  const saveBrand = useMutation({
    mutationFn: () => backend.brandingUpdate({ color, logo }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["branding"] }); go(2); },
    onError: (e) => setErr((e as Error).message),
  });
  // Invites: each address becomes an email invite (the person picks a password upon accepting).
  const sendInvites = useMutation({
    mutationFn: async () => { for (const e of invites) await api.createInvite(e, "member"); },
    onSuccess: () => finish.mutate(),
    onError: (e) => setErr((e as Error).message),
  });

  const onLogo = (f: File | undefined) => {
    if (!f) return;
    // Data URL: that's how the brand is stored (the backend receives the string). Small file.
    if (f.size > 512 * 1024) { setErr(t("onboarding.logoTooBig")); return; }
    const rd = new FileReader();
    rd.onload = () => { setErr(""); setLogo(String(rd.result || "")); };
    rd.readAsDataURL(f);
  };

  const addInvite = () => {
    const e = invite.trim().toLowerCase();
    if (!e.includes("@") || invites.includes(e)) return;
    setInvites((v) => [...v, e]);
    setInvite("");
  };

  return (
    <div className="ob">
      <div className="ob-card">
        <header className="ob-head">
          <img src={BESKOS_MARK} alt="" />
          <div className="ob-dots">{steps.map((s) => <i key={s} className={s === step ? "on" : s < step ? "done" : ""} />)}</div>
        </header>

        {step === 0 && (
          <>
            <h1>{t("onboarding.h0")}</h1>
            <p className="ob-sub">{t("onboarding.p0", { email })}</p>
            <PasswordField label={t("onboarding.password")} value={pw} autoFocus userInputs={[email]}
              onChange={(v) => { setErr(""); setPw(v); }} onStrength={(s) => { strength.current = s.ok; }} />
            <label className="ob-fld"><span>{t("onboarding.password2")}</span>
              <input type="password" value={pw2} onChange={(e) => { setErr(""); setPw2(e.target.value); }} />
            </label>
            {err && <p className="ob-err">{err}</p>}
            {/* Blocked while the password doesn't pass: the mail server rejects it either
                way, and it's better to say so here than after submitting. */}
            <button className="ob-cta" disabled={savePassword.isPending || !strength.current || pw !== pw2}
              onClick={() => {
                if (!strength.current) { setErr(t("webmail.pwHintWeak")); return; }
                if (pw !== pw2) { setErr(t("onboarding.errPwMatch")); return; }
                savePassword.mutate();
              }}>{t("onboarding.continue")}</button>
          </>
        )}

        {step === 1 && (
          <>
            <h1>{t("onboarding.h1")}</h1>
            <p className="ob-sub">{t("onboarding.p1")}</p>
            <div className="ob-logo">
              <div className="ob-prev" style={{ background: color }}>
                {logo ? <img src={logo} alt="" /> : <IcStar size={22} />}
              </div>
              <label className="ob-upload">
                {t("onboarding.pickLogo")}
                <input type="file" accept="image/png,image/jpeg,image/svg+xml,image/webp" hidden
                  onChange={(e) => onLogo(e.target.files?.[0])} />
              </label>
              {logo && <button className="ob-link" onClick={() => setLogo("")}>{t("onboarding.removeLogo")}</button>}
            </div>
            <div className="ob-colors">
              {BRAND_COLORS.map((c) => (
                <button key={c.id} className={"ob-color" + (color === c.hex ? " on" : "")} style={{ background: c.hex }}
                  title={t(c.name)} onClick={() => setColor(c.hex)}>{color === c.hex && <IcCheck size={14} />}</button>
              ))}
            </div>
            {err && <p className="ob-err">{err}</p>}
            <button className="ob-cta" disabled={saveBrand.isPending} onClick={() => saveBrand.mutate()}>{t("onboarding.continue")}</button>
            <button className="ob-skip" onClick={() => go(2)}>{t("onboarding.skip")}</button>
          </>
        )}

        {step === 2 && (
          <>
            <h1>{t("onboarding.h2")}</h1>
            <p className="ob-sub">{t("onboarding.p2", { domain: domain?.domain ?? "" })}</p>
            <div className="ob-invite">
              <input type="email" placeholder={`colega@${domain?.domain ?? "acme.com"}`} value={invite}
                onChange={(e) => setInvite(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addInvite(); } }} />
              <button onClick={addInvite}><IcPlus size={16} /></button>
            </div>
            {invites.length > 0 && (
              <ul className="ob-list">
                {invites.map((e) => (
                  <li key={e}><IcUsers size={14} /> {e}
                    <button onClick={() => setInvites((v) => v.filter((x) => x !== e))}><IcX size={13} /></button>
                  </li>
                ))}
              </ul>
            )}
            {err && <p className="ob-err">{err}</p>}
            <button className="ob-cta" disabled={sendInvites.isPending || finish.isPending}
              onClick={() => (invites.length ? sendInvites.mutate() : finish.mutate())}>
              {invites.length ? t("onboarding.invite", { count: invites.length }) : t("onboarding.enter")}
            </button>
            {invites.length > 0 && <button className="ob-skip" onClick={() => finish.mutate()}>{t("onboarding.skip")}</button>}
          </>
        )}
      </div>
    </div>
  );
}
