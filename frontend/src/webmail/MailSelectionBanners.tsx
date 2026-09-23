import type { EmailSummary } from "../jmap/types";

// Gmail-style "select all N" banners above the mail list, extracted from MailView.tsx (law 1.4).
interface MailSelectionBannersProps {
  allOnPage: boolean;
  search: string;
  starred: boolean;
  total: number;
  emails: EmailSummary[];
  sel: Set<string>;
  onSelectWholeFolder: () => void;
  onClearSelection: () => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function MailSelectionBanners({
  allOnPage, search, starred, total, emails, sel, onSelectWholeFolder, onClearSelection, t,
}: MailSelectionBannersProps) {
  return (
    <>
      {/* Gmail-style: the whole page is selected, but there is more -- offer to select ALL. */}
      {allOnPage && !search && !starred && total > emails.length && sel.size <= emails.length && (
        <div className="selall-banner">
          {t("webmail.pageAllSelected", { n: emails.length })}{" "}
          <button onClick={onSelectWholeFolder}>{t("webmail.selectWhole", { total })}</button>
        </div>
      )}
      {sel.size > emails.length && (
        <div className="selall-banner on">
          {t("webmail.wholeSelected", { n: sel.size })}{" "}
          <button onClick={onClearSelection}>{t("webmail.clearSelection")}</button>
        </div>
      )}
    </>
  );
}
