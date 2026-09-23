import { useState } from "react";
import { useTranslation } from "react-i18next";
import { IcX } from "../panel/icons";

// Composer's "Add link" (Slack style): two fields — the readable text and the address.
// Typing `[text](url)` by hand in the middle of the message is easy to get wrong and you can't
// see the result; here the person fills the fields in and the composer inserts the ready-made markdown.
export function LinkModal({ initialText = "", onSave, onClose }: {
  /** Text selected in the composer, if any — saves having to retype it. */
  initialText?: string;
  onSave: (text: string, url: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState(initialText);
  const [url, setUrl] = useState("");

  const save = () => {
    const href = url.trim();
    if (!href) return;
    // Without a scheme the link would be relative to the app (/mail/…) and would lead nowhere.
    const full = /^[a-z][a-z0-9+.-]*:|^\//i.test(href) ? href : `https://${href}`;
    onSave(text.trim() || full, full);
  };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span>{t("chat.addLink")}</span>
          <button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
        </header>
        <div className="tk-body link-modal">
          <label className="fld">
            <span>{t("chat.linkText")}</span>
            <input autoFocus value={text} onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } if (e.key === "Escape") onClose(); }} />
          </label>
          <label className="fld">
            <span>{t("chat.linkUrl")}</span>
            <input value={url} placeholder="https://" inputMode="url" onChange={(e) => setUrl(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } if (e.key === "Escape") onClose(); }} />
          </label>
          <div className="link-acts">
            <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
            <button className="send" disabled={!url.trim()} onClick={save}>{t("common.save")}</button>
          </div>
        </div>
      </div>
    </div>
  );
}
