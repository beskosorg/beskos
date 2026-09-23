import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { TotpDrawer, SmsDrawer } from "../panel/SecuritySection";
import { IcShield, IcKey, IcPhone, IcChevron, IcLogout } from "../panel/icons";

// Blocking gate for the organization's mandatory MFA: its own screen (mfagate-* classes, webmail
// tokens — no panel styles out of context). It cannot be closed; the only ways out are completing
// enrollment or signing out. The policy can pin the method (only the allowed option shows).
export function MFAGate({ method = "any", email = "", onLogout }: {
  method?: string; email?: string; onLogout?: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [drawer, setDrawer] = useState<"totp" | "sms" | null>(null);
  const [recovery, setRecovery] = useState<string[]>([]);

  const done = (codes: string[]) => { setRecovery(codes); setDrawer(null); };

  return (
    <div className="wm mfagate">
      <div className="mfagate-card">
        <div className="mfagate-badge"><IcShield size={26} /></div>
        <h1>{t("mfaGate.title")}</h1>

        {recovery.length === 0 ? (
          <>
            <p className="mfagate-desc">{t("mfaGate.desc")}</p>
            <div className="mfagate-opts">
              {method !== "sms" && (
                <button className="mfagate-opt" onClick={() => setDrawer("totp")}>
                  <span className="mfagate-opt-ic"><IcKey size={20} /></span>
                  <span className="mfagate-opt-tx">
                    <b>{t("mfa.enableTotp")}</b>
                    <span>{t("mfaGate.totpHint")}</span>
                  </span>
                  <IcChevron size={16} className="mfagate-opt-ch" />
                </button>
              )}
              {method !== "totp" && (
                <button className="mfagate-opt" onClick={() => setDrawer("sms")}>
                  <span className="mfagate-opt-ic"><IcPhone size={20} /></span>
                  <span className="mfagate-opt-tx">
                    <b>{t("mfa.enableSms")}</b>
                    <span>{t("mfaGate.smsHint")}</span>
                  </span>
                  <IcChevron size={16} className="mfagate-opt-ch" />
                </button>
              )}
            </div>
          </>
        ) : (
          <>
            <p className="mfagate-desc">{t("mfa.recoveryHint")}</p>
            <div className="mfagate-codes">{recovery.map((c) => <span key={c}>{c}</span>)}</div>
            <button className="mfagate-continue"
              onClick={() => qc.invalidateQueries({ queryKey: ["me-presence"] })}>
              {t("mfaGate.done")}
            </button>
          </>
        )}

        <div className="mfagate-foot">
          {email && <span>{email}</span>}
          {onLogout && (
            <button onClick={onLogout}><IcLogout size={14} /> {t("webmail.logout")}</button>
          )}
        </div>
      </div>

      {drawer === "totp" && <TotpDrawer onClose={() => setDrawer(null)} onDone={done} />}
      {drawer === "sms" && <SmsDrawer onClose={() => setDrawer(null)} onDone={done} />}
    </div>
  );
}
