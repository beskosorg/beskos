// Gmail-style keyboard navigation for the mail list, extracted from MailView.tsx (law 1.4).
// j/k move the focus, o/Enter opens, e archives, r replies, # moves to trash, x (un)selects.
// Ignored while typing in a field or with the composer open.
import { useEffect } from "react";
import type { EmailSummary } from "../jmap/types";

interface UseMailKeyboardNavArgs {
  emails: EmailSummary[];
  focusIdx: number;
  setFocusIdx: (updater: (i: number) => number) => void;
  focusedId: string | null;
  composersCount: number;
  canArchive: boolean;
  trashId: string | null;
  archiveId: string | null;
  selected: string | null;
  openRow: (id: string, unread: boolean) => void;
  archiveOne: (id: string) => Promise<void>;
  moveOne: (id: string, dest: string | null, msg: string) => Promise<void>;
  openEmail: (id: string) => void;
  toggleOne: (id: string) => void;
  t: (key: string) => string;
}

export function useMailKeyboardNav(args: UseMailKeyboardNavArgs): void {
  const {
    emails, focusIdx, setFocusIdx, focusedId, composersCount, canArchive, trashId, archiveId,
    selected, openRow, archiveOne, moveOne, openEmail, toggleOne, t,
  } = args;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable)) return;
      if (e.metaKey || e.ctrlKey || e.altKey || composersCount || !emails.length) return;
      switch (e.key) {
        case "j": case "ArrowDown": e.preventDefault(); setFocusIdx((i) => Math.min(emails.length - 1, i + 1)); break;
        case "k": case "ArrowUp": e.preventDefault(); setFocusIdx((i) => Math.max(0, (i < 0 ? 0 : i) - 1)); break;
        case "o": case "Enter": { if (focusedId) { const m = emails[focusIdx]; openRow(focusedId, m?.keywords ? !m.keywords.$seen : false); } break; }
        case "e": if (focusedId && canArchive) void archiveOne(focusedId); break;
        case "#": if (focusedId && trashId) void moveOne(focusedId, trashId, t("webmail.movedTrash")); break;
        case "r": if (focusedId) openEmail(focusedId); break;
        case "x": if (focusedId) toggleOne(focusedId); break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [emails, focusIdx, focusedId, composersCount, canArchive, trashId, archiveId, selected]);
}
