import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import type { WebmailSession } from "./auth";
import type { Mailbox, EmailAttachment, EmailSummary, UploadedBlob } from "../jmap/types";
import { MailReader } from "./MailReader";
import { Compose } from "./Compose";
import { ScheduledModal } from "./ScheduledModal";
import { AIDrawer } from "./AIDrawer";
import { AttachmentViewer } from "./AttachmentViewer";
import { RulesView } from "./RulesView";
import { EventModal } from "./EventModal";
import { ExtractTasksModal } from "./ExtractTasksModal";
import { toast } from "./toast";
import { backend, contactsApi, labelKeyword } from "./backend";
import { displayName } from "./util";
import { IcFolder, IcChevron } from "../panel/icons";
import { MailAdvancedSearch } from "./MailAdvancedSearch";
import { sortFolders, folderLabel } from "./mailFolders";
import { useResizableListWidth } from "./useResizableListWidth";
import { useMailKeyboardNav } from "./useMailKeyboardNav";
import { MailSidebar } from "./MailSidebar";
import { MailListToolbar } from "./MailListToolbar";
import { MailSelectionBanners } from "./MailSelectionBanners";
import { MailListRow } from "./MailListRow";
import { MailEmptyFolderModal } from "./MailEmptyFolderModal";

type ComposeInit = { to?: string; cc?: string; subject?: string; body?: string; bodyHtml?: string; draftId?: string; attachFiles?: File[]; attachments?: UploadedBlob[] };

export function MailView({ session, search, sub, sub2, advOpen = false, onAdvClose }: { session: WebmailSession; search: string; sub?: string; sub2?: string; advOpen?: boolean; onAdvClose?: () => void }) {
  const { t, i18n } = useTranslation();
  const svc = session.service;
  // Real-time JMAP push lives in App (global). Here just the refetchInterval as a fallback.

  // Auto-refreshes the folder counters (new emails / read) every 20s and on window focus.
  const boxesQ = useQuery({
    queryKey: ["mailboxes"], queryFn: () => svc.mailboxes(),
    refetchInterval: 20000, refetchOnWindowFocus: true,
  });
  // Multiple compose windows at once (new ones and drafts), stacked at the bottom — like Gmail.
  const [composers, setComposers] = useState<{ id: number; init: ComposeInit }[]>([]);
  const composerSeq = useRef(0);
  const openComposer = (init: ComposeInit) => setComposers((cs) => {
    // Opening a draft that's already open doesn't duplicate it — keeps the existing window.
    if (init.draftId && cs.some((c) => c.init.draftId === init.draftId)) return cs;
    return [...cs, { id: ++composerSeq.current, init }];
  });
  const closeComposer = (id: number) => { setComposers((cs) => cs.filter((c) => c.id !== id)); void listQ.refetch(); void boxesQ.refetch(); };
  const [scheduledOpen, setScheduledOpen] = useState(false);
  const [aiOriginal, setAiOriginal] = useState<{ original: string; reply: { to?: string; cc?: string; subject?: string } } | null>(null);
  const [extract, setExtract] = useState<{ subject: string; body: string; emailId: string } | null>(null);
  const [viewAtt, setViewAtt] = useState<EmailAttachment | null>(null);
  const [meetTitle, setMeetTitle] = useState<string | null>(null);
  const [newFolder, setNewFolder] = useState<string | null>(null);
  const [kebabFolder, setKebabFolder] = useState<string | null>(null);   // folder with the (⋯) menu open
  const [renameFolderId, setRenameFolderId] = useState<string | null>(null);
  const [subfolderOf, setSubfolderOf] = useState<string | null>(null); // create a subfolder inside this one
  const [folderInput, setFolderInput] = useState("");
  const [params, setParams] = useSearchParams();

  // List column width: user-resizable (grabbing the edge), with a guaranteed MINIMUM
  // and persisted per device (localStorage — depends on the screen, not the account).
  const { listW, resizing, startResize } = useResizableListWidth();
  const nav = useNavigate();
  // Automations live INSIDE mail: that's where the rules act. In the URL, like everything else.
  const rules = sub === "rules";
  // Starred is a VIEW (marks $flagged), not a folder: the message stays where it is.
  const starred = sub === "starred";
  // View by label: /mail/mail/label:<id>. Like Starred, filters by keyword, without changing folder.
  const labelId = !rules && !starred && sub?.startsWith("label:") ? sub.slice("label:".length) : null;
  const labelsQ = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels() });
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropOn, setDropOn] = useState<string | null>(null);
  // Batch selection. Set (not array): checking/unchecking 50 rows has to be O(1).
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [confirmEmpty, setConfirmEmpty] = useState(false);
  const [bulkMove, setBulkMove] = useState(false);
  const [folderMenu, setFolderMenu] = useState(false); // folder drawer on phone

  // The URL carries EVERYTHING: /mail/mail/<folder>/<emailId>?p=<page>. Folder is the role (inbox,
  // sent…) or the custom folder's id — this way the link is readable and copyable. Same for the
  // open email and the page.
  const page = Math.max(1, Number(params.get("p")) || 1);
  const selected = sub2 && !rules && !starred ? sub2 : null;

  // ?to=<email>: someone requested "send email" from outside (e.g. a contact's card). Opens the
  // compose window already addressed and clears the parameter, otherwise a refresh would reopen it.
  const to = params.get("to");
  useEffect(() => {
    if (!to) return;
    openComposer({ to });
    setParams({}, { replace: true });
  }, [to, setParams]);

  // ?vcard=<id>: "send contact" from the card — opens compose with the card (.vcf) already
  // attached. Fetches the file from the API and clears the parameter (otherwise a refresh would
  // reattach it).
  const vcardId = params.get("vcard");
  const vcardName = params.get("vname") || "";
  useEffect(() => {
    if (!vcardId) return;
    setParams({}, { replace: true });
    contactsApi.vcardFile(vcardId)
      .then((file) => openComposer({
        attachFiles: [file],
        subject: vcardName ? t("webmail.compose.vcardSubject", { name: vcardName }) : "",
      }))
      .catch((e) => toast((e as Error).message));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vcardId, setParams]);

  const folders = useMemo(() => sortFolders(boxesQ.data ?? []), [boxesQ.data]);
  // The URL's folder (`sub`) can be a ROLE (inbox, sent…) or a custom folder's id. Without `sub`
  // — and not rules/starred — it's the inbox.
  const folderKey = rules || starred || labelId ? null : sub ?? null;
  const activeFolder = folderKey
    ? (folders.find((f) => f.role === folderKey)?.id ?? folders.find((f) => f.id === folderKey)?.id ?? null)
    : (folders.find((f) => f.role === "inbox")?.id ?? folders[0]?.id ?? null);

  // Stable slug for a folder in the URL: the role when it exists (readable), otherwise the id.
  const folderSlug = (f?: Mailbox) => f?.role ?? f?.id ?? "inbox";
  const openFolder = (f: Mailbox) => nav(`/mail/mail/${folderSlug(f)}`);
  // Base URL when opening/closing an email. Label has its own base; the rest keeps the previous
  // behavior (starred falls back to the active folder, as before).
  const viewBase = labelId ? `label:${labelId}` : (() => { const f = folders.find((x) => x.id === activeFolder); return f ? folderSlug(f) : "inbox"; })();
  // Opening/closing an email PRESERVES the query string (?p=2…): without this, clicking an email
  // on page 2 threw the list back to page 1.
  const keepParams = (path: string) => { const q = params.toString(); return q ? `${path}?${q}` : path; };
  const openEmail = (id: string) => nav(keepParams(`/mail/mail/${viewBase}/${id}`));
  const closeEmail = () => nav(keepParams(`/mail/mail/${viewBase}`));
  const goPage = (n: number) => setParams((prev) => { const q = new URLSearchParams(prev); if (n <= 1) q.delete("p"); else q.set("p", String(n)); return q; }, { replace: true });
  const position = (page - 1) * 50;

  // Advanced search (Gmail-style): when a JMAP filter is active, it overrides simple search.
  // The panel opens from the chevron in the top search bar (advOpen comes from App).
  const [advFilter, setAdvFilter] = useState<Record<string, unknown> | null>(null);

  // The list reloads on its own (new emails without reloading the page). Search doesn't stay in polling.
  // Separation by address (Mac Mail style): an alias delivers to the SAME mailbox, so "separating"
  // means filtering by recipient, not opening a new account. Without aliases, none of this shows up.
  const addrQ = useQuery({ queryKey: ["my-addresses"], queryFn: () => backend.myAddresses(), retry: false, staleTime: 300000 });
  const aliases = addrQ.data?.aliases ?? [];
  const [addrFilter, setAddrFilter] = useState(""); // "" = everything arriving at the mailbox
  const [addrOpen, setAddrOpen] = useState(false);   // mailbox menu open
  // "Unread only": the folder counter counts what's in the WHOLE MAILBOX, not what fits on the 1st
  // page — an old unread message sits further down and the number looks made up. This shows them,
  // and only them.
  const [unreadOnly, setUnreadOnly] = useState(false);
  // The address filter turns into an advanced search limited to the current folder: to/cc with the address.
  const addrJmapFilter = useMemo(() => {
    if ((!addrFilter && !unreadOnly) || !activeFolder) return null;
    const conditions: Record<string, unknown>[] = [{ inMailbox: activeFolder }];
    if (addrFilter) conditions.push({ operator: "OR", conditions: [{ to: addrFilter }, { cc: addrFilter }] });
    if (unreadOnly) conditions.push({ notKeyword: "$seen" });
    return { operator: "AND", conditions } as Record<string, unknown>;
  }, [addrFilter, unreadOnly, activeFolder]);

  const listQ = useQuery({
    queryKey: ["emails", advFilter ? `adv:${JSON.stringify(advFilter)}` : search ? `s:${search}` : starred ? "starred" : labelId ? `label:${labelId}` : activeFolder, addrFilter, unreadOnly, page],
    queryFn: () => (advFilter ? svc.searchAdvanced(advFilter, position)
      : search ? svc.search(search, position)
      : starred ? svc.listStarred(position)
      : labelId ? svc.listByLabel(labelKeyword(labelId), position)
      // No collapsing when unread ones are requested: collapsed, the conversation shows the most
      // recent message and the filter would stop finding the old unread reply.
      : addrJmapFilter ? svc.searchAdvanced(addrJmapFilter, position, 50, !unreadOnly)
      : svc.listEmails(activeFolder!, position)),
    enabled: advFilter ? true : search ? search.length > 1 : starred || !!labelId || !!activeFolder,
    refetchInterval: (search || advFilter) ? false : 20000, refetchOnWindowFocus: true,
  });
  const rawEmails = listQ.data?.items ?? [];
  const total = listQ.data?.total ?? 0;

  // List ordering (on the current page; the server already delivers by date). date | sender | subject.
  const [sortBy, setSortBy] = useState<"date" | "from" | "subject">("date");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");
  const emails = useMemo(() => {
    const dir = sortDir === "asc" ? 1 : -1;
    return [...rawEmails].sort((a, b) => {
      if (sortBy === "from") return dir * displayName(a.from).localeCompare(displayName(b.from), undefined, { sensitivity: "base" });
      if (sortBy === "subject") return dir * (a.subject || "").localeCompare(b.subject || "", undefined, { sensitivity: "base" });
      return dir * (a.receivedAt || "").localeCompare(b.receivedAt || "");
    });
  }, [rawEmails, sortBy, sortDir]);

  // The server counter can diverge from the actual content: a folder has shown "1 unread" with
  // nothing unread in it. When "unread only" mode runs, the query total is the REAL count — we
  // store it, and from then on it drives that folder's badge.
  const [realUnread, setRealUnread] = useState<Record<string, number>>({});
  useEffect(() => {
    if (!unreadOnly || !activeFolder || listQ.data === undefined || addrFilter) return;
    setRealUnread((v) => (v[activeFolder] === listQ.data!.total ? v : { ...v, [activeFolder]: listQ.data!.total }));
  }, [unreadOnly, activeFolder, addrFilter, listQ.data]);
  // unreadOf returns the number to show: the real count when we have it, otherwise the server's.
  const unreadOf = (f: Mailbox) => realUnread[f.id] ?? f.unreadEmails;

  // Unread messages the address filter hides: the folder counter counts the WHOLE mailbox, so
  // with an address chosen it can say "1" without the list showing anything unread. Only
  // calculated on the 1st page — on later ones what's visible doesn't represent the folder and the
  // notice would be misleading.
  const hiddenUnread = useMemo(() => {
    if (!addrFilter || page > 1 || search || starred || labelId) return 0;
    const fo = folders.find((f) => f.id === activeFolder);
    const folderUnread = fo ? (realUnread[fo.id] ?? fo.unreadEmails) : 0;
    if (folderUnread === 0) return 0;
    const visible = emails.filter((m) => !m.keywords?.$seen).length;
    return Math.max(0, folderUnread - visible);
  }, [addrFilter, page, search, starred, labelId, folders, activeFolder, emails, realUnread]);

  // Switching folder/page/search resets the selection: a batch marked in a list that's no longer
  // visible would apply actions to messages outside the view.
  useEffect(() => { setSel(new Set()); }, [activeFolder, page, search, starred, labelId]);
  useEffect(() => { if (search || starred || labelId) setUnreadOnly(false); }, [search, starred, labelId]);

  // Keyboard navigation (Gmail-style): j/k moves focus, o/Enter opens, e archives, r replies,
  // # moves to trash, x (un)checks. Ignored while typing in a field or with the composer open.
  const [focusIdx, setFocusIdx] = useState(-1);
  useEffect(() => { setFocusIdx(-1); }, [activeFolder, page, search, starred, labelId, sortBy, sortDir]);
  const focusedId = focusIdx >= 0 && focusIdx < emails.length ? emails[focusIdx].id : null;
  useEffect(() => { if (focusedId) document.querySelector(`[data-mrow="${focusedId}"]`)?.scrollIntoView({ block: "nearest" }); }, [focusedId]);

  // Starring doesn't change folder: it only marks it. Reloads the list so Starred keeps up.
  const toggleStar = async (id: string, on: boolean) => {
    await svc.setStarred(id, on);
    void listQ.refetch(); void boxesQ.refetch();
  };

  // --- Batch selection ---
  const allOnPage = emails.length > 0 && emails.every((m) => sel.has(m.id));
  const toggleOne = (id: string) => setSel((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });
  const toggleAll = () => setSel(allOnPage ? new Set() : new Set(emails.map((m) => m.id)));

  // "Select all N" (across all pages). Fetches the folder's ids and puts them in the batch.
  // Only makes sense in a folder (not in search/starred, where the query is different).
  async function selectWholeFolder() {
    if (!activeFolder) return;
    try { setSel(new Set(await svc.allIds(activeFolder))); }
    catch (e) { toast((e as Error).message); }
  }
  const selIds = () => [...sel];

  async function bulk(fn: () => Promise<void>, msg?: string) {
    try { await fn(); setSel(new Set()); if (selected && sel.has(selected)) closeEmail();
      await listQ.refetch(); void boxesQ.refetch(); if (msg) toast(msg); }
    catch (e) { toast((e as Error).message); }
  }

  // Dragging onto a folder = a real move (Email/set mailboxIds). It's the opposite of starring.
  const dropInto = async (folderId: string) => {
    const id = dragId;
    setDragId(null); setDropOn(null);
    if (!id) return;
    const junkBox = folders.find((f) => f.role === "junk");
    try {
      await svc.moveTo(id, folderId, junkBox ? folderId === junkBox.id : undefined);
      if (selected === id) closeEmail();
      toast(t("webmail.moved"));
      void listQ.refetch(); void boxesQ.refetch();
    } catch (e) { toast((e as Error).message); }
  };

  const inboxId = folders.find((f) => f.role === "inbox")?.id ?? null;
  const junkId = folders.find((f) => f.role === "junk")?.id ?? null;
  const activeRole = folders.find((f) => f.id === activeFolder)?.role ?? null;
  // OUTGOING folders (sent/drafts): the listing shows the RECIPIENT ("To: …"), not the
  // sender — like Gmail. In the others we show the sender.
  const outgoing = activeRole === "sent" || activeRole === "drafts";
  // Views that MIX folders (Starred, search, label): show a chip with each message's folder,
  // like Gmail. In a normal folder it would be redundant (all the same), so it's omitted.
  const mixedView = starred || !!labelId || !!search || !!advFilter;
  const folderChip = (m: EmailSummary) => {
    if (!mixedView || !m.mailboxIds) return null;
    const cands = Object.keys(m.mailboxIds)
      .filter((k) => m.mailboxIds![k])
      .map((id) => folders.find((f) => f.id === id))
      .filter(Boolean) as Mailbox[];
    if (!cands.length) return null;
    const f = cands.find((c) => c.role) ?? cands[0]; // prefers the system folder (inbox, sent…)
    return <span className="row-folder">{folderLabel(t, f)}</span>;
  };
  // "Not spam" in the Spam folder; "Mark as spam" in the others. No action in Sent/Trash.
  const spamAction = (() => {
    if (search || !selected) return undefined;
    if (activeRole === "junk" && inboxId) {
      return { label: t("webmail.notSpam"), junk: false, dest: inboxId };
    }
    if (junkId && (activeRole === "inbox" || activeRole === "archive")) {
      return { label: t("webmail.markSpam"), junk: true, dest: junkId };
    }
    return undefined;
  })();
  async function runSpamAction() {
    if (!spamAction || !selected) return;
    await svc.moveTo(selected, spamAction.dest, spamAction.junk);
    closeEmail();
    await listQ.refetch();
    void boxesQ.refetch();
    toast(t(spamAction.junk ? "webmail.markedSpam" : "webmail.markedNotSpam"));
  }

  const trashId = folders.find((f) => f.role === "trash")?.id ?? null;
  // 1-click archive: moves to the Archive folder (role archive). Only where it makes sense — not
  // in Sent/Drafts/Trash nor already inside Archive, and never during a search.
  const archiveId = folders.find((f) => f.role === "archive")?.id ?? null;
  // Applicable in any folder except Archive/Sent/Drafts/Trash; the Archive folder is created
  // on demand if it doesn't exist yet (ensureArchiveMailbox).
  const canArchive = !search && !["archive", "sent", "drafts", "trash"].includes(activeRole ?? "");
  async function archiveSelected() {
    if (!selected) return;
    try {
      const dest = archiveId ?? (await svc.ensureArchiveMailbox());
      await svc.moveTo(selected, dest);
      closeEmail();
      toast(t("webmail.archived"));
      void listQ.refetch(); void boxesQ.refetch();
    } catch (e) { toast((e as Error).message); }
  }
  // Snooze: moves the message to "Snoozed" and records the time; the worker returns it to the
  // inbox. Applicable outside Sent/Drafts/Trash and without a search.
  const canSnooze = !search && !["sent", "drafts", "trash"].includes(activeRole ?? "");
  async function snoozeSelected(wake: Date) {
    if (!selected) return;
    try {
      const dest = await svc.ensureSnoozedMailbox();
      await svc.moveTo(selected, dest);
      await backend.snoozeEmail(selected, wake.toISOString());
      closeEmail();
      toast(t("webmail.snoozed"));
      void listQ.refetch(); void boxesQ.refetch();
    } catch (e) { toast((e as Error).message); }
  }

  // "Move to" menu targets: every folder except the current one and trash (which has its own button).
  const moveTargets = folders
    .filter((f) => f.id !== activeFolder && f.role !== "trash")
    .map((f) => ({ id: f.id, label: folderLabel(t, f) }));

  async function emptyNow() {
    const box = folders.find((f) => f.id === activeFolder);
    if (!box) return;
    setConfirmEmpty(false);
    try { const n = await svc.emptyMailbox(box.id); await listQ.refetch(); void boxesQ.refetch(); toast(t("webmail.emptied", { n })); }
    catch (e) { toast((e as Error).message); }
  }

  async function afterMutate() { closeEmail(); await listQ.refetch(); void boxesQ.refetch(); }
  // Moves a specific id (keyboard shortcuts), closing the reader only if it's the open one.
  async function moveOne(id: string, dest: string | null, msg: string) {
    if (!id || !dest) return;
    try { await svc.moveTo(id, dest); if (selected === id) closeEmail(); toast(msg); void listQ.refetch(); void boxesQ.refetch(); }
    catch (e) { toast((e as Error).message); }
  }
  async function archiveOne(id: string) { await moveOne(id, archiveId ?? (await svc.ensureArchiveMailbox()), t("webmail.archived")); }

  useMailKeyboardNav({
    emails, focusIdx, setFocusIdx, focusedId, composersCount: composers.length, canArchive, trashId,
    archiveId, selected, openRow: (id, unread) => openRow(id, unread), archiveOne, moveOne, openEmail, toggleOne, t,
  });
  async function moveSelected(mailboxId: string) { if (selected) { await svc.moveTo(selected, mailboxId); await afterMutate(); toast(t("webmail.moved")); } }
  async function trashSelected() { if (selected && trashId) { await svc.moveTo(selected, trashId); await afterMutate(); toast(t("webmail.movedTrash")); } }
  async function markUnread() { if (selected) { await svc.markRead(selected, false); await afterMutate(); } }

  async function createFolder() {
    const name = (newFolder ?? "").trim();
    if (!name) { setNewFolder(null); return; }
    try { await svc.createMailbox(name); void boxesQ.refetch(); toast(t("webmail.folderCreated")); }
    catch (e) { toast((e as Error).message); }
    setNewFolder(null);
  }

  async function renameFolder(id: string) {
    const name = folderInput.trim();
    if (name) {
      try { await svc.renameMailbox(id, name); void boxesQ.refetch(); toast(t("webmail.folderRenamed")); }
      catch (e) { toast((e as Error).message); }
    }
    setRenameFolderId(null); setFolderInput("");
  }
  async function createSubfolder(parentId: string) {
    const name = folderInput.trim();
    if (name) {
      try { await svc.createMailbox(name, parentId); void boxesQ.refetch(); toast(t("webmail.folderCreated")); }
      catch (e) { toast((e as Error).message); }
    }
    setSubfolderOf(null); setFolderInput("");
  }
  async function deleteFolder(id: string) {
    setKebabFolder(null);
    try {
      await svc.deleteMailbox(id);
      if (activeFolder === id) nav("/mail/mail");
      void boxesQ.refetch();
      toast(t("webmail.folderDeleted"));
    } catch (e) {
      const m = (e as Error).message;
      toast(m === "notEmpty" ? t("webmail.folderNotEmpty") : m === "hasChild" ? t("webmail.folderHasChild") : t("webmail.folderDeleteFailed"));
    }
  }

  // Opens a draft in the composer (Drafts folder): loads the email and fills in Compose.
  async function openDraft(id: string) {
    const m = await svc.getEmail(id);
    const part = m.textBody?.[0]?.partId;
    const body = part ? m.bodyValues[part]?.value ?? "" : m.preview;
    // Preserves the draft's HTML (formatting, inline images) instead of only reloading the text.
    const htmlMeta = m.htmlBody?.[0];
    const bodyHtml = htmlMeta?.type === "text/html" ? m.bodyValues[htmlMeta.partId]?.value : undefined;
    // Restores the draft's attachments (blobs already on the server). Excludes inline images (they
    // have a cid — they live in the body's HTML, they aren't file attachments): reattaching them
    // would duplicate the image.
    const attachments: UploadedBlob[] = (m.attachments ?? [])
      .filter((a) => !a.cid)
      .map((a) => ({ blobId: a.blobId, type: a.type, name: a.name || t("webmail.reader.attachment"), size: a.size }));
    openComposer({
      to: (m.to ?? []).map((a) => a.email).join(", "),
      subject: m.subject, body, bodyHtml: bodyHtml || undefined, draftId: id,
      attachments: attachments.length ? attachments : undefined,
    });
  }

  function openRow(id: string, unread: boolean) {
    if (activeRole === "drafts" && !search) { void openDraft(id); return; }
    openEmail(id);
    // On opening an unread one: marks it read and updates the list AND the folder counters.
    // The row is a conversation and opening it shows the whole thing: marking only the
    // representative left its siblings unread and the mailbox still counting them. Without a
    // threadId (uncollapsed list) only this one gets marked.
    if (unread) {
      const m = emails.find((e) => e.id === id);
      const done = () => { void listQ.refetch(); void boxesQ.refetch(); };
      void (m?.threadId ? svc.markThreadRead(m.threadId) : svc.markRead(id, true)).then(done);
    }
  }

  const listTitle = search ? t("webmail.searchResults") : starred ? t("webmail.starred") : folderLabel(t, folders.find((f) => f.id === activeFolder));

  return (
    <div className={"mailgrid" + (rules ? " rules-mode" : "") + (selected ? " reading" : "")}
      style={{ "--listw": listW + "px" } as React.CSSProperties}>
      <MailSidebar
        folderMenu={folderMenu} setFolderMenu={setFolderMenu}
        onCompose={() => { if (rules) nav("/mail/mail"); openComposer({}); }}
        aliases={aliases} primary={addrQ.data?.primary ?? ""} addrFilter={addrFilter} setAddrFilter={setAddrFilter}
        addrOpen={addrOpen} setAddrOpen={setAddrOpen}
        boxes={boxesQ.data ?? []} activeFolder={activeFolder} search={search} starred={starred}
        dragId={dragId} dropOn={dropOn} setDropOn={setDropOn}
        onOpenFolder={openFolder} onDropInto={dropInto}
        unreadOf={unreadOf} unreadOnly={unreadOnly} setUnreadOnly={setUnreadOnly}
        renameFolderId={renameFolderId} setRenameFolderId={setRenameFolderId}
        folderInput={folderInput} setFolderInput={setFolderInput} onRenameFolder={renameFolder}
        kebabFolder={kebabFolder} setKebabFolder={setKebabFolder}
        subfolderOf={subfolderOf} setSubfolderOf={setSubfolderOf} onCreateSubfolder={createSubfolder}
        onDeleteFolder={deleteFolder}
        newFolder={newFolder} setNewFolder={setNewFolder} onCreateFolder={createFolder}
        labels={labelsQ.data ?? []} labelId={labelId}
        nav={nav} onOpenScheduled={() => setScheduledOpen(true)} rules={rules} t={t}
      />

      {rules ? <RulesView session={session} /> : (<>
      <section className="list">
        <div className={"list-resizer" + (resizing ? " dragging" : "")} onPointerDown={startResize}
          role="separator" aria-orientation="vertical" aria-label="resize" />
        <div className="mobtop">
          <button className="ib" onClick={() => setFolderMenu(true)} aria-label="menu"><IcFolder size={18} /></button>
          <b>{listTitle}</b>
        </div>
        {/* Action bar: select-all, batch actions, empty (Trash/Spam), and pagination — like
            Gmail. Only shows what makes sense in context. */}
        <MailListToolbar
          svc={svc} search={search} starred={starred} advFilter={advFilter} setAdvFilter={setAdvFilter}
          onRefresh={() => void listQ.refetch()} sel={sel} selIds={selIds} allOnPage={allOnPage} toggleAll={toggleAll}
          emails={emails} sortBy={sortBy} sortDir={sortDir} setSortBy={setSortBy} setSortDir={setSortDir}
          bulk={bulk} activeRole={activeRole} trashId={trashId} bulkMove={bulkMove} setBulkMove={setBulkMove}
          moveTargets={moveTargets} title={listTitle} total={total} onRequestEmpty={() => setConfirmEmpty(true)}
          page={page} position={position} goPage={goPage} t={t}
        />

        <MailSelectionBanners allOnPage={allOnPage} search={search} starred={starred} total={total} emails={emails}
          sel={sel} onSelectWholeFolder={() => void selectWholeFolder()} onClearSelection={() => setSel(new Set())} t={t} />

        {/* The folder counter is for the WHOLE mailbox; with an address chosen, an unread message
            addressed to another address falls outside the list and the number looked like it was
            lying. This says what's actually going on. */}
        {unreadOnly && (
          <div className="mlist-note">
            {t("webmail.unreadOnly")}{" "}
            <button onClick={() => setUnreadOnly(false)}>{t("webmail.showAllMessages")}</button>
          </div>
        )}
        {!unreadOnly && hiddenUnread > 0 && (
          <div className="mlist-note">
            {t("webmail.unreadElsewhere", { count: hiddenUnread })}{" "}
            <button onClick={() => setAddrFilter("")}>{t("webmail.accountsAll")}</button>
          </div>
        )}

        {listQ.isLoading ? <div className="loading">…</div>
          : emails.length === 0 ? <div className="empty">{t("webmail.emptyFolder")}</div>
          : emails.map((m) => (
            <MailListRow key={m.id} m={m} outgoing={outgoing} selected={selected} focusedId={focusedId}
              dragId={dragId} picked={sel.has(m.id)} locale={i18n.language} folderChip={folderChip}
              onOpen={openRow} onDragStart={setDragId} onDragEnd={() => { setDragId(null); setDropOn(null); }}
              onToggleOne={toggleOne} onToggleStar={toggleStar} t={t} />
          ))}
      </section>

      <section className="reader">
        {selected && (
          <div className="mobtop">
            <button className="ib" onClick={closeEmail} aria-label="back"><IcChevron size={18} className="left" /></button>
            <b>{t("webmail.apps.mail")}</b>
          </div>
        )}
        <MailReader service={svc} emailId={selected} locale={i18n.language} me={session.email}
          isSpam={activeRole === "junk"}
          onReply={(draft) => openComposer(draft)}
          onDraftAI={(original, reply) => setAiOriginal({ original, reply })}
          onExtractTasks={(subject, body) => setExtract({ subject, body, emailId: selected ?? "" })}
          onSchedule={(subject) => setMeetTitle(subject)}
          spamAction={spamAction ? { label: spamAction.label, junk: spamAction.junk, run: () => void runSpamAction() } : undefined}
          moveTargets={moveTargets}
          onMove={(id) => void moveSelected(id)}
          onArchive={canArchive ? () => void archiveSelected() : undefined}
          onSnooze={canSnooze ? (wake) => void snoozeSelected(wake) : undefined}
          onTrash={() => void trashSelected()}
          onMarkUnread={() => void markUnread()}
          onOpenAttachment={(a) => setViewAtt(a)} />
      </section>
      </>)}

      {composers.map((c, i) => (
        <Compose key={c.id} service={svc} from={session.email} initial={c.init} dockIndex={i}
          onClose={() => closeComposer(c.id)} />
      ))}
      {scheduledOpen && <ScheduledModal onClose={() => setScheduledOpen(false)} />}
      {aiOriginal !== null && (
        <AIDrawer original={aiOriginal.original} onClose={() => setAiOriginal(null)}
          onUse={(text) => { const r = aiOriginal.reply; setAiOriginal(null); openComposer({ to: r.to, cc: r.cc, subject: r.subject, body: text }); }} />
      )}
      {extract && <ExtractTasksModal subject={extract.subject} body={extract.body} sourceEmailId={extract.emailId} onClose={() => setExtract(null)} />}
      {confirmEmpty && (
        <MailEmptyFolderModal total={total} onCancel={() => setConfirmEmpty(false)} onConfirm={() => void emptyNow()} t={t} />
      )}

      {advOpen && (
        <MailAdvancedSearch folders={folders} folderLabel={(f) => folderLabel(t, f)}
          onClose={() => onAdvClose?.()}
          onSearch={(filter) => { setAdvFilter(filter); onAdvClose?.(); goPage(1); }} />
      )}
      {viewAtt && <AttachmentViewer service={svc} attachment={viewAtt} onClose={() => setViewAtt(null)} />}
      {meetTitle !== null && (
        <EventModal day={new Date()} prefillTitle={t("webmail.ai.meetingFrom") + (meetTitle ? ` — ${meetTitle}` : "")}
          onClose={() => setMeetTitle(null)} onSaved={() => { setMeetTitle(null); toast(t("cal.created")); }} />
      )}
    </div>
  );
}
