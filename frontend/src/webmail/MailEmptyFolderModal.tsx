import { IcX } from "../panel/icons";

// Confirmation dialog for "empty folder" (Trash/Spam), extracted from MailView.tsx (law 1.4).
interface MailEmptyFolderModalProps {
  total: number;
  onCancel: () => void;
  onConfirm: () => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function MailEmptyFolderModal({ total, onCancel, onConfirm, t }: MailEmptyFolderModalProps) {
  return (
    <div className="tk-bg open" onMouseDown={onCancel}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 380 }}>
        <header><span>{t("webmail.emptyFolder2")}</span><button className="ib" title={t("common.close")} onClick={onCancel}><IcX size={15} /></button></header>
        <div className="body"><p className="desc">{t("webmail.emptyConfirm", { n: total })}</p>
          <div className="drawer-actions">
            <button className="chipbtn" onClick={onCancel}>{t("common.cancel")}</button>
            <button className="add danger" onClick={onConfirm}>{t("webmail.emptyFolder2")}</button>
          </div>
        </div>
      </div>
    </div>
  );
}
