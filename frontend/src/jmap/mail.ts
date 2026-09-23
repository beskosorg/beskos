// Webmail operations on top of JmapClient. Payloads per DIVERGENCES §D7 (tested).
import { JmapClient, USING_MAIL, USING_SEND } from "./client";
import type { Mailbox, EmailSummary, EmailFull, EmailPage, SendDraft, UploadedBlob } from "./types";
import { SieveApi } from "./sieve";

export class MailService {
  constructor(private c: JmapClient, private accountId: string) {}

  // rules returns this account's Sieve API. Kept behind the service instead of exposing the raw
  // client: whoever needs rules asks for rules, not an open door to the whole of JMAP.
  rules(): SieveApi {
    return new SieveApi(this.c, this.accountId);
  }

  // Same-origin URL (via proxy) to download an attachment/blob. Path per DIVERGENCES §D7.
  attachmentUrl(blobId: string, name: string, type: string): string {
    return `/jmap/download/${encodeURIComponent(this.accountId)}/${encodeURIComponent(blobId)}/` +
      `${encodeURIComponent(name)}?accept=${encodeURIComponent(type)}`;
  }

  // Downloads an authenticated attachment and returns an object URL (blob:) usable in <img>/<iframe>/<a>.
  // The caller must revoke it with URL.revokeObjectURL when done.
  async attachmentObjectUrl(blobId: string, name: string, type: string): Promise<string> {
    const blob = await this.c.downloadBlob(this.accountId, blobId, name, type);
    return URL.createObjectURL(blob);
  }

  async mailboxes(): Promise<Mailbox[]> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/get", { accountId: this.accountId, ids: null }, "0"],
    ]);
    return (r.list as Mailbox[]) ?? [];
  }

  // REAL per-folder unread count. Mailbox/get's `unreadEmails` is maintained by the server
  // separately from the messages and can get stuck on a stale value -- we found a mailbox saying
  // "1 unread" with everything actually read. This counts what really exists, in a single request
  // with one Email/query per folder (JMAP allows several methods per call).
  async unreadCounts(mailboxIds: string[]): Promise<Record<string, number>> {
    if (mailboxIds.length === 0) return {};
    const calls = mailboxIds.map((id, i) => [
      "Email/query",
      {
        accountId: this.accountId,
        filter: { operator: "AND", conditions: [{ inMailbox: id }, { notKeyword: "$seen" }] },
        limit: 0, calculateTotal: true,
      },
      `u${i}`,
    ] as [string, Record<string, unknown>, string]);
    const res = await this.c.request(USING_MAIL, calls);
    const out: Record<string, number> = {};
    res.forEach((r, i) => {
      const total = (r as { total?: number }).total;
      if (typeof total === "number") out[mailboxIds[i]] = total;
    });
    return out;
  }


  // unreadThreads returns the threads with SOME unread message. A single round trip: it asks for
  // the keywords of the siblings in conversations with more than one message (single-message
  // threads are already known from their own row).
  private async unreadThreads(threadList: { id: string; emailIds: string[] }[], shown: string[]): Promise<Set<string>> {
    const shownSet = new Set(shown);
    const siblings: string[] = [];
    for (const th of threadList) {
      for (const id of th.emailIds) if (!shownSet.has(id)) siblings.push(id);
    }
    if (siblings.length === 0) return new Set();
    // Safety cap: a page of long conversations can't turn into a giant request.
    const ids = siblings.slice(0, 300);
    const [get] = await this.c.request(USING_MAIL, [
      ["Email/get", { accountId: this.accountId, ids, properties: ["id", "threadId", "keywords"] }, "s"],
    ]);
    const out = new Set<string>();
    for (const e of (get.list as EmailSummary[]) ?? []) {
      if (!e.keywords?.$seen && e.threadId) out.add(e.threadId);
    }
    return out;
  }

  // Lists a folder's emails: collapses by conversation (one row per thread, like Gmail).
  async listEmails(mailboxId: string, position = 0, limit = 50): Promise<EmailPage> {
    return this.queryAndGet({ inMailbox: mailboxId }, position, limit, true);
  }

  // Plain search. Stalwart's FTS matches WHOLE words (with stemming), so "reser" doesn't find
  // "reservation". To give the Gmail experience (partial match), we try FTS first (fast, whole
  // mailbox) and, if it comes back empty, fall back to a SUBSTRING scan over recent emails.
  async search(text: string, position = 0, limit = 50): Promise<EmailPage> {
    const term = text.trim();
    if (!term) return { items: [], total: 0, position: 0 };
    const fts = await this.queryAndGet({ text: term }, position, limit, true);
    if (fts.total > 0) return fts;
    return this.substringSearch(term.toLowerCase(), position, limit);
  }

  // Substring scan over the most recent emails across all folders (matches partials). Covers the
  // common case (looking for something recent); deep queries use FTS or advanced search.
  private async substringSearch(term: string, position: number, limit: number): Promise<EmailPage> {
    const [query] = await this.c.request(USING_MAIL, [
      ["Email/query", { accountId: this.accountId, sort: [{ property: "receivedAt", isAscending: false }], limit: 400 }, "q"],
    ]);
    const ids = (query.ids as string[]) ?? [];
    if (!ids.length) return { items: [], total: 0, position };
    const [get] = await this.c.request(USING_MAIL, [
      ["Email/get", {
        accountId: this.accountId, ids,
        properties: MailService.SUMMARY_PROPS,
      }, "g"],
    ]);
    const byId = new Map((get.list as EmailSummary[] ?? []).map((e) => [e.id, e]));
    const ordered = ids.map((id) => byId.get(id)).filter(Boolean) as EmailSummary[];
    const matched = ordered.filter((e) => {
      const from = (e.from ?? []).map((a) => `${a.name ?? ""} ${a.email}`).join(" ");
      return `${e.subject} ${from} ${e.preview}`.toLowerCase().includes(term);
    });
    return { items: matched.slice(position, position + limit), total: matched.length, position };
  }

  // Advanced search (Gmail-style): receives an already-built JMAP filter (from/to/subject/text/
  // hasAttachment/after/before/minSize/maxSize/inMailbox + NOT). Collapses by conversation.
  async searchAdvanced(filter: Record<string, unknown>, position = 0, limit = 50, collapse = true): Promise<EmailPage> {
    return this.queryAndGet(filter, position, limit, collapse);
  }

  // Starred: a VIEW, not a folder. Filters by the $flagged mark across the whole account -- the
  // message stays in whatever folder it was in. Does NOT collapse: here every starred message is
  // its own row (collapsing would hide the starred one if the thread's representative was another
  // message).
  async listStarred(position = 0, limit = 50): Promise<EmailPage> {
    return this.queryAndGet({ hasKeyword: "$flagged" }, position, limit, false);
  }

  // calculateTotal gives the "of 35,547"; position is the page offset (0, 50, 100…). A single
  // request: the query computes this page's ids, the get fetches them, and Thread/get counts each
  // conversation's messages (for the "(3)" on the row), all chained via back-reference.
  // Note: with collapseThreads, Stalwart returns the NON-collapsed total -- pagination uses it as
  // an approximation (the last page can be one row short); this is a known server limitation.
  private async queryAndGet(filter: Record<string, unknown>, position: number, limit: number, collapse: boolean): Promise<EmailPage> {
    const [query, get, threads] = await this.c.request(USING_MAIL, [
      ["Email/query", {
        accountId: this.accountId, filter, collapseThreads: collapse,
        sort: [{ property: "receivedAt", isAscending: false }],
        position, limit, calculateTotal: true,
      }, "q"],
      ["Email/get", {
        accountId: this.accountId,
        "#ids": { resultOf: "q", name: "Email/query", path: "/ids" },
        properties: MailService.SUMMARY_PROPS,
      }, "g"],
      ["Thread/get", {
        accountId: this.accountId,
        "#ids": { resultOf: "g", name: "Email/get", path: "/list/*/threadId" },
      }, "t"],
    ]);
    // Message count per conversation, for the "(3)" badge. Only matters when collapsed and >1.
    const threadList = (threads.list as { id: string; emailIds: string[] }[]) ?? [];
    const sizeByThread = new Map(threadList.map((th) => [th.id, th.emailIds.length]));
    const byId = new Map((get.list as EmailSummary[] ?? []).map((e) => [e.id, e]));
    const ids = (query.ids as string[]) ?? [];
    // Unread at the THREAD level: collapsed, the row showed only the most recent message's state,
    // so an older unread reply stayed invisible -- the mailbox said "1 unread" while everything
    // looked read. Here we look at every message in the thread and flag the row accordingly.
    const unreadThreads = collapse ? await this.unreadThreads(threadList, ids) : new Set<string>();
    const items = ids.map((id) => byId.get(id)).filter(Boolean).map((e) => {
      const n = collapse ? sizeByThread.get(e!.threadId ?? "") ?? 1 : 1;
      return {
        ...e!,
        threadCount: n > 1 ? n : undefined,
        threadUnread: unreadThreads.has(e!.threadId ?? "") || undefined,
      };
    });
    return {
      items,
      total: (query.total as number) ?? ids.length,
      position: (query.position as number) ?? position,
    };
  }

  private static readonly FULL_PROPS = ["id", "threadId", "messageId", "from", "to", "cc", "replyTo", "subject",
    "receivedAt", "sentAt", "preview", "hasAttachment", "keywords", "bodyValues", "textBody",
    "htmlBody", "attachments"];

  // Properties for a list ROW. Includes mailboxIds for the "which folder is this in" chip in views
  // that mix folders (Starred, search, label) — as in Gmail.
  private static readonly SUMMARY_PROPS = ["id", "threadId", "from", "to", "subject", "receivedAt",
    "preview", "hasAttachment", "keywords", "mailboxIds"];

  async getEmail(id: string): Promise<EmailFull> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Email/get", {
        accountId: this.accountId, ids: [id],
        properties: MailService.FULL_PROPS,
        fetchTextBodyValues: true, fetchHTMLBodyValues: true,
        bodyProperties: ["partId", "blobId", "type", "name", "size", "cid"],
      }, "0"],
    ]);
    const list = (r.list as EmailFull[]) ?? [];
    if (!list.length) throw new Error("email not found");
    return list[0];
  }

  // The whole conversation (same threadId), in chronological order. A single round trip: Email/get
  // for the requested email to discover the thread → Thread/get by id → Email/get for all of them
  // by reference.
  async getThread(id: string): Promise<EmailFull[]> {
    const resp = await this.c.request(USING_MAIL, [
      ["Email/get", { accountId: this.accountId, ids: [id], properties: ["id", "threadId"] }, "e"],
      ["Thread/get", { accountId: this.accountId, "#ids": { resultOf: "e", name: "Email/get", path: "/list/*/threadId" } }, "t"],
      ["Email/get", {
        accountId: this.accountId,
        "#ids": { resultOf: "t", name: "Thread/get", path: "/list/*/emailIds" },
        properties: MailService.FULL_PROPS,
        fetchTextBodyValues: true, fetchHTMLBodyValues: true,
        bodyProperties: ["partId", "blobId", "type", "name", "size", "cid"],
      }, "m"],
    ]);
    const list = (resp[2]?.list as EmailFull[]) ?? [];
    const sorted = list.sort((a, b) => (a.receivedAt ?? "").localeCompare(b.receivedAt ?? ""));
    // Sending to yourself leaves the SAME message in Sent AND in the Inbox (same Message-ID): two
    // JMAP objects, but a single message. It's shown once in the thread. Without a Message-ID
    // (e.g. a draft) it isn't deduplicated -- there's no way to assert they're the same message.
    const seen = new Set<string>();
    return sorted.filter((e) => {
      const mid = (e.messageId ?? []).join(",");
      if (!mid) return true;
      if (seen.has(mid)) return false;
      seen.add(mid);
      return true;
    });
  }

  async markRead(id: string, read: boolean): Promise<void> {
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: { "keywords/$seen": read ? true : null } } }, "0"],
    ]);
  }

  // markThreadRead marks the WHOLE conversation as read. Opening a conversation shows all of it,
  // so leaving sibling messages unread would keep the mailbox counting messages the person already
  // saw -- that was the "1 unread" that couldn't be found. A single round trip: find the
  // conversation's ids and mark them.
  async markThreadRead(threadId: string): Promise<void> {
    if (!threadId) return;
    const [th] = await this.c.request(USING_MAIL, [
      ["Thread/get", { accountId: this.accountId, ids: [threadId] }, "t"],
    ]);
    const ids = ((th.list as { emailIds: string[] }[]) ?? []).flatMap((x) => x.emailIds ?? []);
    await this.markReadMany(ids, true);
  }

  // Star/unstar. Just a mark ($flagged, the same one IMAP and Sieve use), so it doesn't touch the
  // folder: the message stays where it is and starts also showing up in Starred.
  async setStarred(id: string, starred: boolean): Promise<void> {
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: { "keywords/$flagged": starred ? true : null } } }, "0"],
    ]);
  }

  // Labels: applied as a JMAP keyword (the name/color catalog lives in our own API). Like
  // starring/spam, it's just a mark -- it doesn't change folder. `keyword` comes from labelKeyword(labelId).
  async setLabel(id: string, keyword: string, on: boolean): Promise<void> {
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: { [`keywords/${keyword}`]: on ? true : null } } }, "0"],
    ]);
  }

  // Label view: filters the whole account by the label's keyword (doesn't collapse, like Starred).
  async listByLabel(keyword: string, position = 0, limit = 50): Promise<EmailPage> {
    return this.queryAndGet({ hasKeyword: keyword }, position, limit, false);
  }

  // Moves the email to a folder (replaces membership → removes it from the others). Used by
  // "mark as spam" / "not spam" and archiving. Clears the $junk keyword according to the destination.
  async moveTo(id: string, mailboxId: string, junk?: boolean): Promise<void> {
    const patch: Record<string, unknown> = { mailboxIds: { [mailboxId]: true } };
    if (junk !== undefined) patch["keywords/$junk"] = junk ? true : null;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: patch } }, "0"],
    ]);
  }

  // --- BATCH operations ---
  // A single Email/set accepts many ids at once: moving/marking/deleting 50 messages is ONE
  // request, not 50. Fewer round trips, and the server applies everything atomically.

  async moveMany(ids: string[], mailboxId: string, junk?: boolean): Promise<void> {
    if (!ids.length) return;
    const patch: Record<string, unknown> = { mailboxIds: { [mailboxId]: true } };
    if (junk !== undefined) patch["keywords/$junk"] = junk ? true : null;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: Object.fromEntries(ids.map((id) => [id, patch])) }, "0"],
    ]);
  }

  async markReadMany(ids: string[], read: boolean): Promise<void> {
    if (!ids.length) return;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId,
        update: Object.fromEntries(ids.map((id) => [id, { "keywords/$seen": read ? true : null }])) }, "0"],
    ]);
  }

  async setStarredMany(ids: string[], starred: boolean): Promise<void> {
    if (!ids.length) return;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId,
        update: Object.fromEntries(ids.map((id) => [id, { "keywords/$flagged": starred ? true : null }])) }, "0"],
    ]);
  }

  // deleteMany deletes PERMANENTLY (destroy). Used only when already in Trash/Spam -- everywhere
  // else, "delete" means moving to Trash, which can be undone.
  async deleteMany(ids: string[]): Promise<void> {
    if (!ids.length) return;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, destroy: ids }, "0"],
    ]);
  }

  // allIds returns ALL ids in the folder -- for "select all N", not just the visible page.
  // In batches of 500: in a huge mailbox, a single query would bring back too many ids at once.
  async allIds(mailboxId: string): Promise<string[]> {
    const out: string[] = [];
    for (;;) {
      const [q] = await this.c.request(USING_MAIL, [
        ["Email/query", { accountId: this.accountId, filter: { inMailbox: mailboxId }, position: out.length, limit: 500 }, "q"],
      ]);
      const ids = (q.ids as string[]) ?? [];
      out.push(...ids);
      if (ids.length < 500) break;
    }
    return out;
  }

  // emptyMailbox empties a whole folder (Trash/Spam): asks for all the ids and destroys them. A
  // single Email/set only destroys known ids, so they really do need listing first -- in batches,
  // to avoid building a giant request on a mailbox with thousands of messages.
  async emptyMailbox(mailboxId: string): Promise<number> {
    let removed = 0;
    for (;;) {
      const [q] = await this.c.request(USING_MAIL, [
        ["Email/query", { accountId: this.accountId, filter: { inMailbox: mailboxId }, limit: 500 }, "q"],
      ]);
      const ids = (q.ids as string[]) ?? [];
      if (!ids.length) break;
      await this.deleteMany(ids);
      removed += ids.length;
      if (ids.length < 500) break;
    }
    return removed;
  }

  // Opens the JMAP push (SSE): calls onState() whenever the account changes (new email, read…).
  // Delegated to the client (fetch streaming). Ends when the connection drops or the signal aborts.
  async streamPush(onState: () => void, signal: AbortSignal): Promise<void> {
    return this.c.streamEvents(onState, signal);
  }

  // Creates a folder (Mailbox) at the root level (or under parentId). Returns the new id.
  // Ensures the Archive folder (role archive): returns the existing one or creates one. If the
  // server refuses the role on create, creates it by name alone -- what matters is having
  // somewhere to archive to.
  async ensureArchiveMailbox(): Promise<string> {
    const boxes = await this.mailboxes();
    const found = boxes.find((b) => b.role === "archive");
    if (found) return found.id;
    const withRole = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, create: { arch: { name: "Archive", role: "archive", parentId: null } } }, "0"],
    ]).then(([r]) => (r.created as Record<string, { id: string }> | undefined)?.["arch"]?.id).catch(() => undefined);
    if (withRole) return withRole;
    const [r2] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, create: { arch: { name: "Archive", parentId: null } } }, "0"],
    ]);
    const created = (r2.created as Record<string, { id: string }>) ?? {};
    if (created["arch"]) return created["arch"].id;
    throw new Error(`archive mailbox create failed: ${JSON.stringify((r2 as Record<string, unknown>).notCreated)}`);
  }

  async renameMailbox(id: string, name: string): Promise<void> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, update: { [id]: { name } } }, "0"],
    ]);
    if ((r.notUpdated as Record<string, unknown> | undefined)?.[id]) throw new Error("rename failed");
  }

  // Deletes the folder. WITHOUT removing emails (onDestroyRemoveEmails absent): if it isn't empty,
  // the server refuses and we surface it -- we don't delete messages out from under the user.
  async deleteMailbox(id: string): Promise<void> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, destroy: [id] }, "0"],
    ]);
    const nd = (r.notDestroyed as Record<string, { type?: string }> | undefined) ?? {};
    if (nd[id]) throw new Error(nd[id].type === "mailboxHasChild" ? "hasChild" : "notEmpty");
  }

  // "Snoozed" folder (no standard JMAP role): holds postponed messages until the worker returns them.
  async ensureSnoozedMailbox(): Promise<string> {
    const boxes = await this.mailboxes();
    const found = boxes.find((b) => b.name.toLowerCase() === "snoozed");
    if (found) return found.id;
    return this.createMailbox("Snoozed");
  }

  async createMailbox(name: string, parentId?: string): Promise<string> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", {
        accountId: this.accountId,
        create: { nm: { name, parentId: parentId ?? null } },
      }, "0"],
    ]);
    const created = (r.created as Record<string, { id: string }>) ?? {};
    const box = created["nm"];
    if (!box) throw new Error(`mailbox create failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
    return box.id;
  }

  // Uploads an attachment to the JMAP blobstore (same-origin via proxy). Standard path /jmap/upload/{accountId}/.
  async uploadBlob(file: File): Promise<UploadedBlob> {
    const meta = await this.c.upload(this.accountId, file);
    return { blobId: meta.blobId, type: file.type || meta.type || "application/octet-stream", name: file.name, size: file.size };
  }

  // Builds the Email object from the draft. The body is the "content" (text/plain, or
  // multipart/alternative text+html when there's HTML); with attachments, it's wrapped in multipart/mixed.
  private buildEmail(draft: SendDraft, draftsId: string): Record<string, unknown> {
    const atts = draft.attachments ?? [];
    const email: Record<string, unknown> = {
      mailboxIds: { [draftsId]: true },
      keywords: { $draft: true },
      from: [draft.from],
      to: draft.to,
      subject: draft.subject,
    };
    if (draft.cc?.length) email.cc = draft.cc;
    if (draft.bcc?.length) email.bcc = draft.bcc;

    const hasHtml = !!draft.htmlBody;
    const bodyValues: Record<string, { value: string }> = { p1: { value: draft.textBody } };
    if (hasHtml) bodyValues.p2 = { value: draft.htmlBody! };
    // The email's "content": text only, or a text+HTML alternative (the client picks what to render).
    const content = hasHtml
      ? { type: "multipart/alternative", subParts: [{ type: "text/plain", partId: "p1" }, { type: "text/html", partId: "p2" }] }
      : { type: "text/plain", partId: "p1" };

    if (!atts.length) {
      email.bodyStructure = content;
    } else {
      email.bodyStructure = {
        type: "multipart/mixed",
        subParts: [content, ...atts.map((a) => ({ blobId: a.blobId, type: a.type, name: a.name, disposition: "attachment" }))],
      };
    }
    email.bodyValues = bodyValues;
    return email;
  }

  // Saves/updates a draft in the Drafts folder. Returns the draft's id.
  async saveDraft(draft: SendDraft, existingId?: string): Promise<string> {
    const draftsId = await this.draftsMailboxId();
    const email = this.buildEmail(draft, draftsId);
    if (existingId) {
      // Recreates it (an Email's content is immutable): destroys the old one and creates the new
      // one, atomically in the same /set.
      const [r] = await this.c.request(USING_MAIL, [
        ["Email/set", { accountId: this.accountId, create: { d: email }, destroy: [existingId] }, "0"],
      ]);
      const created = (r.created as Record<string, { id: string }>) ?? {};
      if (!created["d"]) throw new Error(`draft save failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
      return created["d"].id;
    }
    const [r] = await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, create: { d: email } }, "0"],
    ]);
    const created = (r.created as Record<string, { id: string }>) ?? {};
    if (!created["d"]) throw new Error(`draft save failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
    return created["d"].id;
  }

  // Sending identities ("From:" addresses the account can use — the primary one + aliases). The
  // server populates them from the account's addresses; the UI offers a choice when there's more than one.
  async identities(): Promise<{ id: string; email: string; name: string }[]> {
    const [r] = await this.c.request(USING_SEND, [
      ["Identity/get", { accountId: this.accountId, ids: null }, "0"],
    ]);
    return ((r.list as { id: string; email?: string; name?: string }[]) ?? [])
      .map((i) => ({ id: i.id, email: i.email ?? "", name: i.name ?? "" }));
  }

  // identityId picks the identity whose email matches the requested "From:" (so the Identity and
  // the envelope line up); with no match, falls back to the first one.
  private async identityId(fromEmail?: string): Promise<string> {
    const list = await this.identities();
    if (!list.length) throw new Error("no sending identity available");
    if (fromEmail) {
      const m = list.find((i) => i.email.toLowerCase() === fromEmail.toLowerCase());
      if (m) return m.id;
    }
    return list[0].id;
  }

  private async draftsMailboxId(): Promise<string> {
    const boxes = await this.mailboxes();
    const drafts = boxes.find((b) => b.role === "drafts") ?? boxes[0];
    return drafts.id;
  }

  // Sent folder id, or null if it doesn't exist. Used to keep a copy of the sent email there.
  private async sentMailboxId(): Promise<string | null> {
    const boxes = await this.mailboxes();
    return boxes.find((b) => b.role === "sent")?.id ?? null;
  }

  // prepareOutbox only creates the draft (Email/set) and returns what the server needs to submit
  // it later (server-side outbox: undo-send and scheduled sends). The actual sending is done by
  // the worker via JMAP -- so this does NOT submit here. Returns the email's id, the identity, and
  // the envelope.
  async prepareOutbox(draft: SendDraft, draftId?: string): Promise<{ emailId: string; identityId: string; from: string; rcpts: string[] }> {
    const [identityId, draftsId] = await Promise.all([this.identityId(draft.from.email), this.draftsMailboxId()]);
    const [r] = await this.c.request(USING_SEND, [
      ["Email/set", {
        accountId: this.accountId,
        create: { draft: this.buildEmail(draft, draftsId) },
        ...(draftId ? { destroy: [draftId] } : {}),
      }, "c1"],
    ]);
    const created = (r.created as Record<string, { id: string }>) ?? {};
    if (!created["draft"]) throw new Error(`draft failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
    const rcpt = [...draft.to, ...(draft.cc ?? []), ...(draft.bcc ?? [])];
    return { emailId: created["draft"].id, identityId, from: draft.from.email, rcpts: rcpt.map((t) => t.email) };
  }

  // Sends an email: creates the draft (Email/set) and submits it (EmailSubmission/set). On
  // success, it MOVES the email to Sent (onSuccessUpdateEmail) instead of destroying it --
  // otherwise the sent message would vanish, since Stalwart doesn't keep a copy in Sent on its
  // own. Without a Sent folder, it falls back to destroying it.
  // mailboxIds uses Drafts's REAL id; identityId is mandatory (§D7).
  // draftId optional: an existing draft to destroy along with it (avoids a duplicate in Drafts).
  async send(draft: SendDraft, draftId?: string, sendAt?: Date): Promise<void> {
    const [identityId, draftsId, sentId] = await Promise.all([
      this.identityId(draft.from.email),
      this.draftsMailboxId(),
      this.sentMailboxId(),
    ]);
    const rcpt = [...draft.to, ...(draft.cc ?? []), ...(draft.bcc ?? [])];
    const onSuccess = sentId
      ? { onSuccessUpdateEmail: { "#sub": { mailboxIds: { [sentId]: true }, "keywords/$draft": null, "keywords/$seen": true } } }
      : { onSuccessDestroyEmail: ["#sub"] };
    const responses = await this.c.request(USING_SEND, [
      ["Email/set", {
        accountId: this.accountId,
        create: { draft: this.buildEmail(draft, draftsId) },
        ...(draftId ? { destroy: [draftId] } : {}),
      }, "c1"],
      ["EmailSubmission/set", {
        accountId: this.accountId,
        ...onSuccess,
        create: {
          sub: {
            emailId: "#draft",
            identityId,
            envelope: {
              // HOLDUNTIL: the server holds the message until this instant (RFC 8621 futureRelease).
              // Tested against Stalwart 0.16.4 — it accepts it even without advertising the capability.
              mailFrom: { email: draft.from.email, ...(sendAt ? { parameters: { HOLDFOR: String(holdForSecs(sendAt)) } } : {}) },
              rcptTo: rcpt.map((t) => ({ email: t.email })),
            },
          },
        },
      }, "c2"],
    ]);
    const submitResp = responses[1] as { notCreated?: Record<string, unknown> };
    if (submitResp.notCreated && Object.keys(submitResp.notCreated).length) {
      throw new Error(`send failed: ${JSON.stringify(submitResp.notCreated)}`);
    }
  }
}

// Seconds to wait for HOLDFOR (RFC 4865). Stalwart 0.16.4 accepts HOLDFOR but rejects HOLDUNTIL
// (tested), so we convert the target instant into a delay in seconds.
function holdForSecs(sendAt: Date): number {
  return Math.max(1, Math.round((sendAt.getTime() - Date.now()) / 1000));
}
