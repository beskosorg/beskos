import { useState } from "react";
import { useTranslation } from "react-i18next";
import { assistantApi, type ConfirmRequest, type PendingAction } from "./api";

// Confirmation dialog for an action that goes outbound or is destructive (§6.3 of the design).
//
// Shows the EXACT CONTENT that will happen, not a summary — it's the only point where a human
// sees what will actually be done, and a summary that diverged from what runs would grant
// confidence it hasn't earned. It's deliberately inconvenient: one action, one decision.
export function ConfirmAction({ req, onDecided }: { req: ConfirmRequest | PendingAction; onDecided: () => void }) {
  const { t } = useTranslation();
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);

  const decide = async (approve: boolean) => {
    setSending(true);
    setFailed(false);
    try {
      await assistantApi.confirm(req.id, approve);
      onDecided();
    } catch {
      // Failing to deliver the decision is not the same as approving: the panel shows the buttons again.
      setFailed(true);
      setSending(false);
    }
  };

  return (
    <div className="as-confirm">
      <div className="as-confirm-head">
        ⚠ {t("assistant.confirm.title", { action: t(`assistant.tools.${req.tool}`, { defaultValue: req.tool }) })}
      </div>
      <dl className="as-confirm-body">
        {Object.entries(req.args).map(([k, v]) => (
          <div key={k}>
            <dt>{t(`assistant.fields.${k}`, { defaultValue: k })}</dt>
            <dd>{Array.isArray(v) ? v.join(", ") : String(v ?? "")}</dd>
          </div>
        ))}
      </dl>
      {failed && <p className="as-err">{t("assistant.confirm.failed")}</p>}
      <div className="as-confirm-actions">
        <button className="as-reject" disabled={sending} onClick={() => void decide(false)}>
          {t("assistant.confirm.reject")}
        </button>
        <button className="as-approve" disabled={sending} onClick={() => void decide(true)}>
          {t("assistant.confirm.approve")}
        </button>
      </div>
    </div>
  );
}
