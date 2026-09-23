import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { IcX } from "../panel/icons";

// Shared full-screen viewer bar (Drive file, email attachment, chat attachment…): "Close" +
// actions (e.g. Download) on the LEFT, title always CENTERED. One place for the layout, so every
// preview screen ends up with the same bar.
export function ViewerBar({ title, onClose, children }: {
  title: string; onClose: () => void; children?: ReactNode; // children = extra actions (e.g. Download)
}) {
  const { t } = useTranslation();
  return (
    <header className="viewer-bar">
      <span className="vb-title" title={title}>{title}</span>
      <div className="vb-actions">
        {children}
        <button className="chipbtn" onClick={onClose}><IcX size={15} /> {t("editor.close")}</button>
      </div>
    </header>
  );
}
