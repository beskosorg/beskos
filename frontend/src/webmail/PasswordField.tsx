import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { measure, type Strength } from "./passwordStrength";
import "./password.css";

// Password field with a strength meter. A single component for every place where a password
// is SET (first access, profile, admin create/reset, invite): the rule is the same everywhere,
// and having it written four times was a guarantee that they'd eventually stop matching.
//
// The caller gets the evaluation via `onStrength` and disables the button while `ok` is false — the
// server rejects it either way, but the person finds out before submitting, and why.
export function PasswordField({ value, onChange, label, userInputs = [], autoFocus, autoComplete = "new-password", onStrength }: {
  value: string;
  onChange: (v: string) => void;
  label: string;
  /** Email, name, domain: penalize the password (it's what the server's zxcvbn also does). */
  userInputs?: string[];
  autoFocus?: boolean;
  autoComplete?: string;
  onStrength?: (s: Strength) => void;
}) {
  const { t } = useTranslation();
  const [show, setShow] = useState(false);
  const st = useMemo(() => {
    const s = measure(value, userInputs);
    onStrength?.(s);
    return s;
    // onStrength deliberately left out of the deps: it's a new function on every render of the caller.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, userInputs.join("|")]);

  const levels = ["pwWeak", "pwWeak", "pwFair", "pwGood", "pwStrong"] as const;
  return (
    <div className="pwf">
      <label className="pwf-lb">{label}</label>
      <div className="pwf-box">
        <input type={show ? "text" : "password"} value={value} autoFocus={autoFocus} autoComplete={autoComplete}
          onChange={(e) => onChange(e.target.value)} />
        <button type="button" className="pwf-eye" onClick={() => setShow((v) => !v)}
          title={t(show ? "webmail.pwHide" : "webmail.pwShow")}>
          {show ? t("webmail.pwHide") : t("webmail.pwShow")}
        </button>
      </div>
      {/* Four bars: they fill up with strength and stay red until it passes. */}
      <div className={"pwf-bar s" + st.score}>
        {[1, 2, 3, 4].map((i) => <i key={i} className={st.score >= i ? "on" : ""} />)}
      </div>
      <div className="pwf-msg">
        {value.length > 0 && <b>{t("webmail." + levels[st.score])}</b>}
        {st.hint && <span>{t("webmail." + st.hint)}</span>}
        {!st.hint && value.length === 0 && <span>{t("webmail.pwHintEmpty")}</span>}
      </div>
    </div>
  );
}
