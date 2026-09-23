import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Mailbox } from "../jmap/types";

// Gmail-style advanced search: builds a JMAP filter from the fields (From, To, Subject,
// Contains, Doesn't have, Size, Date, folder, has attachment) and returns it to MailView to run.
export function MailAdvancedSearch({ folders, folderLabel, onSearch, onClose }: {
  folders: Mailbox[];
  folderLabel: (f?: Mailbox) => string;
  onSearch: (filter: Record<string, unknown>) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [subject, setSubject] = useState("");
  const [contains, setContains] = useState("");
  const [notWords, setNotWords] = useState("");
  const [sizeOp, setSizeOp] = useState<"gt" | "lt">("gt");
  const [sizeMB, setSizeMB] = useState("");
  const [after, setAfter] = useState("");
  const [before, setBefore] = useState("");
  const [folder, setFolder] = useState("all");
  const [hasAtt, setHasAtt] = useState(false);

  const submit = () => {
    const conds: Record<string, unknown>[] = [];
    if (from.trim()) conds.push({ from: from.trim() });
    if (to.trim()) conds.push({ to: to.trim() });
    if (subject.trim()) conds.push({ subject: subject.trim() });
    if (contains.trim()) conds.push({ text: contains.trim() });
    if (hasAtt) conds.push({ hasAttachment: true });
    if (folder !== "all") conds.push({ inMailbox: folder });
    if (after) conds.push({ after: new Date(after).toISOString() });
    if (before) conds.push({ before: new Date(before).toISOString() });
    const mb = parseFloat(sizeMB);
    if (!isNaN(mb) && mb > 0) {
      const bytes = Math.round(mb * 1024 * 1024);
      conds.push(sizeOp === "gt" ? { minSize: bytes } : { maxSize: bytes });
    }
    if (notWords.trim()) conds.push({ operator: "NOT", conditions: [{ text: notWords.trim() }] });
    if (conds.length === 0) return;
    onSearch(conds.length === 1 ? conds[0] : { operator: "AND", conditions: conds });
  };

  return (
    <>
      <div className="adv-backdrop" onClick={onClose} />
      <div className="adv-panel">
        <Row label={t("adv.from")}><input autoFocus value={from} onChange={(e) => setFrom(e.target.value)} /></Row>
        <Row label={t("adv.to")}><input value={to} onChange={(e) => setTo(e.target.value)} /></Row>
        <Row label={t("adv.subject")}><input value={subject} onChange={(e) => setSubject(e.target.value)} /></Row>
        <Row label={t("adv.contains")}><input value={contains} onChange={(e) => setContains(e.target.value)} /></Row>
        <Row label={t("adv.notHas")}><input value={notWords} onChange={(e) => setNotWords(e.target.value)} /></Row>
        <Row label={t("adv.size")}>
          <select value={sizeOp} onChange={(e) => setSizeOp(e.target.value as "gt" | "lt")}>
            <option value="gt">{t("adv.greater")}</option>
            <option value="lt">{t("adv.less")}</option>
          </select>
          <input className="adv-num" type="number" value={sizeMB} onChange={(e) => setSizeMB(e.target.value)} />
          <span className="adv-unit">MB</span>
        </Row>
        <Row label={t("adv.dateRange")}>
          <input type="date" value={after} onChange={(e) => setAfter(e.target.value)} />
          <span className="adv-unit">→</span>
          <input type="date" value={before} onChange={(e) => setBefore(e.target.value)} />
        </Row>
        <Row label={t("adv.searchIn")}>
          <select value={folder} onChange={(e) => setFolder(e.target.value)}>
            <option value="all">{t("adv.allMail")}</option>
            {folders.map((f) => <option key={f.id} value={f.id}>{folderLabel(f)}</option>)}
          </select>
        </Row>
        <div className="adv-foot">
          <label className="adv-chk"><input type="checkbox" checked={hasAtt} onChange={(e) => setHasAtt(e.target.checked)} /> {t("adv.hasAttachment")}</label>
          <div className="grow" />
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="send" onClick={submit}>{t("adv.search")}</button>
        </div>
      </div>
    </>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="adv-row"><span className="adv-l">{label}</span><div className="adv-f">{children}</div></div>;
}
