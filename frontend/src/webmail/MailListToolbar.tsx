import type { Dispatch, SetStateAction } from "react";
import type { MailService } from "../jmap/mail";
import type { EmailSummary } from "../jmap/types";
import { IcRefresh, IcX, IcMail, IcStar, IcTrash, IcFolder, IcChevron } from "../panel/icons";

// Mail list toolbar: select-all, refresh, sort, bulk actions, empty folder and pager -- Gmail-style.
// Only shows what makes sense in context. Extracted from MailView.tsx (law 1.4 -- 600 lines).
interface MoveTarget { id: string; label: string }

interface MailListToolbarProps {
  svc: MailService;
  search: string;
  starred: boolean;
  advFilter: Record<string, unknown> | null;
  setAdvFilter: Dispatch<SetStateAction<Record<string, unknown> | null>>;
  onRefresh: () => void;
  sel: Set<string>;
  selIds: () => string[];
  allOnPage: boolean;
  toggleAll: () => void;
  emails: EmailSummary[];
  sortBy: "date" | "from" | "subject";
  sortDir: "asc" | "desc";
  setSortBy: (b: "date" | "from" | "subject") => void;
  setSortDir: (d: "asc" | "desc") => void;
  bulk: (fn: () => Promise<void>, msg?: string) => Promise<void>;
  activeRole: string | null;
  trashId: string | null;
  bulkMove: boolean;
  setBulkMove: Dispatch<SetStateAction<boolean>>;
  moveTargets: MoveTarget[];
  title: string;
  total: number;
  onRequestEmpty: () => void;
  page: number;
  position: number;
  goPage: (n: number) => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function MailListToolbar(props: MailListToolbarProps) {
  const {
    svc, search, starred, advFilter, setAdvFilter, onRefresh, sel, selIds, allOnPage, toggleAll,
    emails, sortBy, sortDir, setSortBy, setSortDir, bulk, activeRole, trashId, bulkMove, setBulkMove,
    moveTargets, title, total, onRequestEmpty, page, position, goPage, t,
  } = props;

  return (
    // Toolbar: select-all, bulk actions, empty (Trash/Spam) and pagination -- Gmail-style.
    <div className="ltoolbar">
      {!search && !starred && (
        <label className="selall" title={t("webmail.selectAll")}>
          <input type="checkbox" checked={allOnPage} ref={(el) => { if (el) el.indeterminate = sel.size > 0 && !allOnPage; }} onChange={toggleAll} />
        </label>
      )}
      <button className="ib" title={t("webmail.refresh")} onClick={onRefresh}><IcRefresh size={16} /></button>
      {advFilter && <button className="chipbtn" onClick={() => { setAdvFilter(null); goPage(1); }}><IcX size={13} /> {t("adv.clear")}</button>}
      {sel.size === 0 && emails.length > 0 && (
        <div className="filter-chip lsort">
          <select value={`${sortBy}:${sortDir}`} title={t("webmail.sortBy")}
            onChange={(e) => { const [b, d] = e.target.value.split(":"); setSortBy(b as "date" | "from" | "subject"); setSortDir(d as "asc" | "desc"); }}>
            <option value="date:desc">{t("webmail.sortNewest")}</option>
            <option value="date:asc">{t("webmail.sortOldest")}</option>
            <option value="from:asc">{t("webmail.sortSender")}</option>
            <option value="subject:asc">{t("webmail.sortSubject")}</option>
          </select>
        </div>
      )}

      {sel.size > 0 ? (
        <div className="bulkbar">
          <span className="n">{t("webmail.nSelected", { n: sel.size })}</span>
          <button className="ib" title={t("webmail.markRead")} onClick={() => void bulk(() => svc.markReadMany(selIds(), true))}><IcMail size={15} /></button>
          <button className="ib" title={t("webmail.star")} onClick={() => void bulk(() => svc.setStarredMany(selIds(), true))}><IcStar size={15} /></button>
          {activeRole === "trash" || activeRole === "junk"
            ? <button className="ib" title={t("webmail.deleteForever")} onClick={() => void bulk(() => svc.deleteMany(selIds()), t("webmail.deleted"))}><IcTrash size={15} /></button>
            : trashId && <button className="ib" title={t("webmail.moveTrash")} onClick={() => void bulk(() => svc.moveMany(selIds(), trashId), t("webmail.movedTrash"))}><IcTrash size={15} /></button>}
          <div className="movewrap">
            <button className={"ib" + (bulkMove ? " on" : "")} title={t("webmail.moveTo")} onClick={() => setBulkMove((v) => !v)}><IcFolder size={15} /></button>
            {bulkMove && (
              <div className="movemenu" onMouseLeave={() => setBulkMove(false)}>
                {moveTargets.map((tg) => (
                  <button key={tg.id} onClick={() => { setBulkMove(false); void bulk(() => svc.moveMany(selIds(), tg.id), t("webmail.moved")); }}>{tg.label}</button>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : (
        <>
          <h2 className="ltitle">{title}</h2>
          {/* Empty: only in Trash and Spam, and only if there is something. Deletes FOR GOOD. */}
          {(activeRole === "trash" || activeRole === "junk") && total > 0 && (
            <button className="chipbtn danger" onClick={onRequestEmpty}>
              <IcTrash size={14} /> {t("webmail.emptyFolder2")}
            </button>
          )}
        </>
      )}

      <div className="grow" />
      {sel.size === 0 && !starred && total > 50 && (
        <div className="pager">
          <span className="rng">{t("webmail.pageRange", { from: position + 1, to: Math.min(position + emails.length, total), total })}</span>
          <button className="ib" title={t("webmail.prevPage")} disabled={page <= 1} onClick={() => goPage(page - 1)}><IcChevron size={16} className="left" /></button>
          <button className="ib" title={t("webmail.nextPage")} disabled={position + 50 >= total} onClick={() => goPage(page + 1)}><IcChevron size={16} className="right" /></button>
        </div>
      )}
    </div>
  );
}
