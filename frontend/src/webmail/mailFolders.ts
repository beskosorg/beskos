// Mailbox ordering/layout helpers, extracted from MailView.tsx (law 1.4 -- 600 lines).
import type { Mailbox } from "../jmap/types";

export const ROLE_ORDER = ["inbox", "sent", "drafts", "archive", "junk", "trash"];

export function sortFolders(boxes: Mailbox[]): Mailbox[] {
  return [...boxes].sort((a, b) => {
    const ia = ROLE_ORDER.indexOf(a.role ?? ""), ib = ROLE_ORDER.indexOf(b.role ?? "");
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.name.localeCompare(b.name);
  });
}

// layoutFolders returns the display order with nesting depth: system folders (role) first in
// canonical order; then the user's own folders as a tree by parentId (DFS by name).
export function layoutFolders(boxes: Mailbox[]): { f: Mailbox; depth: number }[] {
  const custom = boxes.filter((b) => !b.role);
  const customIds = new Set(custom.map((c) => c.id));
  const sys = boxes.filter((b) => b.role).sort((a, b) => {
    const ia = ROLE_ORDER.indexOf(a.role ?? ""), ib = ROLE_ORDER.indexOf(b.role ?? "");
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib);
  });
  const byParent = new Map<string | null, Mailbox[]>();
  for (const b of custom) {
    const p = b.parentId && customIds.has(b.parentId) ? b.parentId : null; // non-custom parent -> root
    const arr = byParent.get(p) ?? [];
    arr.push(b);
    byParent.set(p, arr);
  }
  for (const arr of byParent.values()) arr.sort((a, b) => a.name.localeCompare(b.name));
  const out: { f: Mailbox; depth: number }[] = sys.map((f) => ({ f, depth: 0 }));
  const walk = (parent: string | null, depth: number) => {
    for (const c of byParent.get(parent) ?? []) { out.push({ f: c, depth }); walk(c.id, depth + 1); }
  };
  walk(null, 0);
  return out;
}

export function folderLabel(t: (k: string, o?: Record<string, unknown>) => string, f?: Mailbox): string {
  if (!f) return "";
  return f.role ? t(`webmail.folders.${f.role}`, { defaultValue: f.name }) : f.name;
}
