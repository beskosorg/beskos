import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { MailService } from "../jmap/mail";
import type { EmailAttachment, EmailFull, EmailAddress } from "../jmap/types";
import { backend, contactsApi, settingsApi } from "./backend";
import { toast } from "./toast";
import { AiThinking } from "./Spinner";
import { MailBody } from "./MailBody";
import { LabelChips } from "./MailLabels";
import { labelKeyword } from "./backend";
import { displayName, shortTime, fullDateTime, relativeTime } from "./util";
import { Avatar } from "./Avatar";
import { IcReply, IcReplyAll, IcForward, IcSparkle, IcColumns, IcCalendar, IcShield, IcFolder, IcTrash, IcMail, IcPrinter, IcChevron, IcPlus, IcArchive, IcCheck } from "../panel/icons";
import { TagIcon } from "./MailLabels";

// Email reader with real AI: summary (backend.summarize) and actions that open the AI/task/event flows.
// Loads the ENTIRE CONVERSATION (same threadId): the most recent message stays open, earlier ones
// collapse into a single line — clicking expands them. The actions (reply, forward…) act on the most recent.
export function MailReader({
  service, emailId, locale, me, isSpam, onReply, onDraftAI, onExtractTasks, onSchedule, spamAction,
  moveTargets, onMove, onArchive, onSnooze, onTrash, onMarkUnread, onOpenAttachment,
}: {
  service: MailService;
  emailId: string | null;
  locale: string;
  me: string;
  isSpam?: boolean;
  onReply: (draft: { to?: string; cc?: string; subject?: string; body?: string; bodyHtml?: string }) => void;
  onDraftAI: (original: string, reply: { to?: string; cc?: string; subject?: string }) => void;
  onExtractTasks: (subject: string, body: string) => void;
  onSchedule: (subject: string) => void;
  spamAction?: { label: string; junk: boolean; run: () => void };
  moveTargets: { id: string; label: string }[];
  onMove: (mailboxId: string) => void;
  onArchive?: () => void; // 1-click archive (undefined = not applicable in this folder)
  onSnooze?: (wake: Date) => void; // snooze (undefined = not applicable in this folder)
  onTrash: () => void;
  onMarkUnread: () => void;
  onOpenAttachment: (a: EmailAttachment) => void;
}) {
  const [snoozeOpen, setSnoozeOpen] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [wantSummary, setWantSummary] = useState(false); // summary is on-demand (button), not automatic
  // Gmail-style action bar: only the essentials on display, the rest in a ⋯ menu. `replyOpen` is
  // the Reply button's menu (reply all / forward / AI); `moreOpen`/`moreView` is the ⋯ with
  // sub-levels (move, labels) iOS-style.
  const [replyOpen, setReplyOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [moreView, setMoreView] = useState<"root" | "move" | "labels">("root");
  const labelsQ = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels() });
  const { t } = useTranslation();
  const thread = useQuery({ queryKey: ["thread", emailId], queryFn: () => service.getThread(emailId!), enabled: !!emailId });

  const msgs = thread.data ?? [];
  // By default, only the most recent one is open. Recalculated when the conversation changes.
  useEffect(() => {
    if (msgs.length) setExpanded(new Set([msgs[msgs.length - 1].id]));
  }, [emailId, msgs.length]);
  // Switching emails hides the summary again (it's on-demand per email).
  useEffect(() => { setWantSummary(false); }, [emailId]);
  // "E" shortcut archives the open conversation (Gmail style). Ignored while typing in a field.
  useEffect(() => {
    if (!emailId || !onArchive) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "e" && e.key !== "E") return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)) return;
      e.preventDefault();
      onArchive();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [emailId, onArchive]);
  const toggle = (id: string) => setExpanded((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });

  // Actions act on the most recent message of the conversation (like Gmail).
  const m = msgs[msgs.length - 1];
  // Readable text of the body (for AI, reply quoting, and forwarding). In an HTML-only email,
  // textBody points to the HTML part — there we extract the text from the tags, otherwise the AI
  // would see raw HTML.
  const bodyPart = m?.textBody?.[0];
  const rawBody = bodyPart ? m.bodyValues[bodyPart.partId]?.value ?? "" : (m?.preview ?? "");
  const body = bodyPart?.type === "text/html" ? htmlToText(rawBody) : rawBody;

  // Config: AI summarization can be turned off in Settings (settings.email.aiSummary). Never runs
  // on SPAM — no AI is spent and no suspicious content is sent to the model.
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  const aiSummaryOn = settings.data?.email?.aiSummary !== false && !isSpam; // default on

  // AI summary (best-effort). Content is immutable per id → cached in localStorage.
  const summary = useQuery({
    queryKey: ["summary", m?.id],
    queryFn: async () => {
      const key = `mailin.sum.${m!.id}`;
      const hit = localStorage.getItem(key);
      if (hit) return { summary: hit };
      const r = await backend.summarize(m!.subject, body);
      try { localStorage.setItem(key, r.summary); } catch { /* quota: ignore */ }
      return r;
    },
    enabled: !!m && aiSummaryOn && wantSummary,
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });

  if (!emailId) return <div className="empty">{t("webmail.reader.none")}</div>;
  if (thread.isLoading || !m) return <div className="loading">…</div>;

  const name = displayName(m.from);
  const addr = m.from?.[0]?.email ?? "";
  const myEmail = me.toLowerCase();

  const quoted = () => `\n\n${t("webmail.reader.quoteHeader", { who: name, date: fullDateTime(m.receivedAt, locale) })}\n` +
    body.split("\n").map((l) => "> " + l).join("\n");
  // Preserves the original HTML in the reply/forward (it used to only quote text). The original
  // body goes inside a <blockquote>; Compose sanitizes everything. No HTML part → undefined (falls
  // back to the text above).
  const escHtml = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const origHtml = () => { const h = m.htmlBody?.[0]; return h?.type === "text/html" ? (m.bodyValues[h.partId]?.value ?? "") : ""; };
  const wrapQuote = (headerHtml: string) => {
    const oh = origHtml(); if (!oh) return undefined;
    return `<br><br><div>${headerHtml}</div><blockquote style="margin:0 0 0 .8ex;border-left:2px solid #ccc;padding-left:1ex">${oh}</blockquote>`;
  };
  const quotedHtml = () => wrapQuote(escHtml(t("webmail.reader.quoteHeader", { who: name, date: fullDateTime(m.receivedAt, locale) })));
  const fwdHtml = () => wrapQuote([
    t("webmail.reader.fwdHeader"), `${t("webmail.reader.from")}: ${fmtAddrs(m.from)}`,
    `${t("webmail.reader.to")}: ${fmtAddrs(m.to)}`, `${t("webmail.reader.date")}: ${fullDateTime(m.receivedAt, locale)}`,
    `${t("webmail.reader.subject")}: ${m.subject}`,
  ].map(escHtml).join("<br>"));

  const replyTarget = () => (m.replyTo?.[0]?.email ?? addr);
  const others = (list?: EmailAddress[] | null) => (list ?? []).map((a) => a.email).filter((e) => {
    const l = e.toLowerCase(); return l !== myEmail && l !== replyTarget().toLowerCase();
  });
  const doReply = (all: boolean) => onReply({
    to: replyTarget(),
    cc: all ? [...others(m.to), ...others(m.cc)].join(", ") || undefined : undefined,
    subject: rePrefix("Re: ", m.subject),
    body: quoted(),
    bodyHtml: quotedHtml(),
  });
  // "Draft with AI": opens the assistant already with the REPLY context (recipient + "Re:"),
  // so the generated text turns into a reply instead of a new email.
  const draftAI = () => onDraftAI(body, { to: replyTarget(), subject: rePrefix("Re: ", m.subject) });
  const doForward = () => onReply({
    subject: rePrefix("Fwd: ", m.subject),
    body: `\n\n${t("webmail.reader.fwdHeader")}\n` +
      `${t("webmail.reader.from")}: ${fmtAddrs(m.from)}\n${t("webmail.reader.to")}: ${fmtAddrs(m.to)}\n` +
      `${t("webmail.reader.date")}: ${fullDateTime(m.receivedAt, locale)}\n${t("webmail.reader.subject")}: ${m.subject}\n\n${body}`,
    bodyHtml: fwdHtml(),
  });

  // Per-email AI actions (on-demand, NOT automatic): summarize, draft reply, extract tasks. Goes
  // in as a slot inside the most recent message (between header and body). While the summary
  // hasn't been requested, it shows only the button bar; on request, it expands into the summary card.
  const summaryNode = !aiSummaryOn ? undefined : (
    (wantSummary && (summary.isLoading || summary.data)) ? (
      <div className="ai-card">
        {summary.isLoading
          ? <AiThinking label={t("webmail.ai.summarizing")} />
          : <>
              <h4><IcSparkle size={14} /> {t("webmail.ai.summary")}</h4>
              <div className="txt">{summary.data!.summary}</div>
              <div className="ai-actions">
                <button className="chipbtn" onClick={draftAI}>{t("webmail.ai.draftReply")}</button>
                <button className="chipbtn" onClick={() => onExtractTasks(m.subject, body)}>{t("webmail.ai.extractTasks")}</button>
              </div>
            </>}
      </div>
    ) : (
      <div className="ai-bar">
        {/* AI has a single entry point — Summarize. Draft (== Reply with AI) lives in the Reply
            button's menu; Extract tasks lives in ⋯. No duplication. */}
        <button className="chipbtn" onClick={() => setWantSummary(true)}><IcSparkle size={15} /> {t("webmail.ai.summarize")}</button>
      </div>
    )
  );

  return (
    <div className="rin">
      <div className="ract">
        {/* Reply — split button: clicking replies; the ▾ opens reply all / forward / AI. */}
        <div className="split movewrap">
          <button className="split-main" onClick={() => doReply(false)}><IcReply size={15} /> {t("webmail.reader.reply")}</button>
          <button className={"split-caret" + (replyOpen ? " on" : "")} aria-label={t("webmail.reader.forward")} onClick={() => setReplyOpen((v) => !v)}><IcChevron size={14} className="down" /></button>
          {replyOpen && (
            <div className="move-menu" onMouseLeave={() => setReplyOpen(false)}>
              {hasOthers(m, myEmail) && <button onClick={() => { setReplyOpen(false); doReply(true); }}><IcReplyAll size={15} /> {t("webmail.reader.replyAll")}</button>}
              <button onClick={() => { setReplyOpen(false); doForward(); }}><IcForward size={15} /> {t("webmail.reader.forward")}</button>
              <button onClick={() => { setReplyOpen(false); draftAI(); }}><IcSparkle size={15} /> {t("webmail.ai.replyAI")}</button>
            </div>
          )}
        </div>

        {/* Triage — 3 icons (the most used): archive, snooze, delete. */}
        {onArchive && <button className="iconbtn" title={t("webmail.archive")} onClick={onArchive}><IcArchive size={17} /></button>}
        {onSnooze && (
          <div className="movewrap">
            <button className={"iconbtn" + (snoozeOpen ? " on" : "")} title={t("webmail.snooze")} onClick={() => setSnoozeOpen((v) => !v)}><IcCalendar size={17} /></button>
            {snoozeOpen && (
              <div className="move-menu" onMouseLeave={() => setSnoozeOpen(false)}>
                {snoozeOptions().map((o) => (
                  <button key={o.key} onClick={() => { setSnoozeOpen(false); onSnooze(o.when); }}>
                    <span>{t("webmail.snoozeOpts." + o.key)}</span>
                    <span className="snz-when">{o.when.toLocaleString(locale, { weekday: "short", hour: "2-digit", minute: "2-digit" })}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
        <button className="iconbtn danger" title={t("webmail.delete")} onClick={onTrash}><IcTrash size={17} /></button>

        <div className="grow" />

        {/* ⋯ More — everything else, with sub-menus (move, labels) iOS-style. */}
        <div className="movewrap">
          <button className={"iconbtn more-btn" + (moreOpen ? " on" : "")} title={t("webmail.more")} aria-label={t("webmail.more")}
            onClick={() => { setMoreOpen((v) => !v); setMoreView("root"); }}>⋯</button>
          {moreOpen && (
            <div className="move-menu more-menu" onMouseLeave={() => { setMoreOpen(false); setMoreView("root"); }}>
              {moreView === "root" && <>
                <button onClick={() => { setMoreOpen(false); onExtractTasks(m.subject, body); }}><IcColumns size={15} /> {t("webmail.ai.extractTasks")}</button>
                <button onClick={() => { setMoreOpen(false); onSchedule(m.subject); }}><IcCalendar size={15} /> {t("webmail.ai.schedule")}</button>
                <div className="more-sep" />
                <button onClick={() => { setMoreOpen(false); onMarkUnread(); }}><IcMail size={15} /> {t("webmail.markUnread")}</button>
                {spamAction && <button onClick={() => { setMoreOpen(false); spamAction.run(); }}><IcShield size={15} /> {spamAction.label}</button>}
                <button onClick={() => setMoreView("move")}><IcFolder size={15} /> {t("webmail.moveTo")}<span className="more-chev">›</span></button>
                <button onClick={() => setMoreView("labels")}><TagIcon size={15} /> {t("webmail.labels.label")}<span className="more-chev">›</span></button>
              </>}
              {moreView === "move" && <>
                <button className="more-back" onClick={() => setMoreView("root")}><IcChevron size={14} className="left" /> {t("webmail.moveTo")}</button>
                {moveTargets.map((f) => (
                  <button key={f.id} onClick={() => { setMoreOpen(false); onMove(f.id); }}>{f.label}</button>
                ))}
              </>}
              {moreView === "labels" && <>
                <button className="more-back" onClick={() => setMoreView("root")}><IcChevron size={14} className="left" /> {t("webmail.labels.label")}</button>
                {(labelsQ.data ?? []).length === 0 && <div className="more-empty">{t("webmail.labels.empty")}</div>}
                {(labelsQ.data ?? []).map((l) => {
                  const on = !!m.keywords?.[labelKeyword(l.id)];
                  return (
                    <button key={l.id} onClick={async () => { await service.setLabel(m.id, labelKeyword(l.id), !on); void thread.refetch(); }}>
                      <span className="mlab-dot" style={{ background: l.color }} /> {l.name}
                      {on && <span className="lbl-check"><IcCheck size={14} /></span>}
                    </button>
                  );
                })}
              </>}
            </div>
          )}
        </div>
      </div>

      <h1 className="rsub">{m.subject || t("webmail.noSubject")}
        {msgs.length > 1 && <span className="thread-count">{msgs.length}</span>}
        <LabelChips keywords={m.keywords} />
      </h1>

      {isSpam && (
        // Gmail-style spam banner: explains why it's in Spam and lets it be marked "not spam".
        // The content below has links and images blocked (see MailBody) for security.
        <div className="spam-banner">
          <div className="spam-txt">
            <b>{t("webmail.reader.spamTitle")}</b>
            <span>{t("webmail.reader.spamWhy")}</span>
          </div>
          {spamAction && !spamAction.junk && (
            <button className="chipbtn" onClick={spamAction.run}>{spamAction.label}</button>
          )}
        </div>
      )}

      {msgs.map((msg, i) => (
        <MessageItem key={msg.id} m={msg} service={service} locale={locale} t={t} spam={!!isSpam} me={me}
          single={msgs.length === 1} open={expanded.has(msg.id)} onToggle={() => toggle(msg.id)}
          onOpenAttachment={onOpenAttachment}
          // AI summary sits in the most recent message, between the header and the body.
          summarySlot={i === msgs.length - 1 ? summaryNode : undefined} />
      ))}
    </div>
  );
}

// A message in the conversation. Collapsed shows only sender + snippet + time; open shows the header
// Automatic capture: if the sender isn't in the address book yet, offers to add them with one click.
// Disappears once they're already a contact (or it's the user themself). Compares email case-insensitively.
function AddSenderButton({ name, email }: { name: string; email: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false });
  const add = useMutation({
    mutationFn: () => contactsApi.create({
      kind: "person", name: name || email, email, phone: "", company: "", domain: "", notes: "",
      tags: [], group_ids: [], title: "", mobile: "", is_whatsapp: false, allow_phone: false,
      allow_email: false, company_id: null, extra_emails: [], extra_phones: [],
    }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["contacts"] }); toast(t("webmail.reader.contactAdded")); },
    onError: (e) => toast((e as Error).message),
  });
  const norm = email.trim().toLowerCase();
  if (!norm || add.isSuccess) return null;
  if ((me.data?.email ?? "").toLowerCase() === norm) return null; // don't add myself
  const exists = (contacts.data ?? []).some((c) => (c.email ?? "").toLowerCase() === norm);
  if (exists) return null;
  return (
    <button className="ib" title={t("webmail.reader.addContact")} disabled={add.isPending}
      onClick={(e) => { e.stopPropagation(); add.mutate(); }}><IcPlus size={15} /></button>
  );
}

// full one (with details and print) and the body. In a single-message conversation, with no collapse chrome.
function MessageItem({ m, service, locale, t, single, spam, me, open, onToggle, onOpenAttachment, summarySlot }: {
  m: EmailFull; service: MailService; locale: string; t: TFunction; me: string;
  single: boolean; spam: boolean; open: boolean; onToggle: () => void; onOpenAttachment: (a: EmailAttachment) => void;
  summarySlot?: ReactNode;
}) {
  const [details, setDetails] = useState(false);
  const name = displayName(m.from);
  const addr = m.from?.[0]?.email ?? "";
  const textPart = m.textBody?.[0]?.partId;
  const body = textPart ? m.bodyValues[textPart]?.value ?? "" : m.preview;

  const printEmail = () => {
    const rows: [string, string][] = [
      [t("webmail.reader.from"), fmtAddrs(m.from)],
      [t("webmail.reader.to"), fmtAddrs(m.to)],
      ...(m.cc?.length ? [[t("webmail.reader.cc"), fmtAddrs(m.cc)] as [string, string]] : []),
      [t("webmail.reader.date"), fullDateTime(m.receivedAt, locale)],
    ];
    const head = rows.map(([k, v]) => `<tr><td class="k">${esc(k)}</td><td>${esc(v)}</td></tr>`).join("");
    const w = window.open("", "_blank", "width=800,height=900");
    if (!w) { toast(t("webmail.reader.printBlocked")); return; }
    w.document.write(`<!doctype html><meta charset="utf-8"><title>${esc(m.subject)}</title>` +
      `<style>body{font:14px -apple-system,system-ui,sans-serif;color:#111;padding:32px;max-width:720px}` +
      `h1{font-size:20px}table{margin:12px 0 20px;font-size:13px;color:#444}td.k{padding-right:14px;color:#888;white-space:nowrap;vertical-align:top}` +
      `pre{white-space:pre-wrap;word-wrap:break-word;font:inherit}</style>` +
      `<h1>${esc(m.subject || t("webmail.noSubject"))}</h1><table>${head}</table><pre>${esc(body)}</pre>`);
    w.document.close();
    w.focus();
    setTimeout(() => w.print(), 250);
  };

  // Collapsed row: a clickable summary. Doesn't appear in a single-message conversation.
  if (!open && !single) {
    return (
      <button className="emsg-collapsed" onClick={onToggle}>
        <Avatar name={name} email={addr} size={26} className="sm" />
        <span className="w">{name}</span>
        <span className="snip">{m.preview}</span>
        <span className="tm">{shortTime(m.receivedAt, locale)}</span>
      </button>
    );
  }

  return (
    <div className={"emsg" + (single ? " single" : "")}>
      <div className="rfrom" onClick={single ? undefined : onToggle} style={single ? undefined : { cursor: "pointer" }}>
        <Avatar name={name} email={addr} size={38} />
        <div className="rfrom-id">
          <div className="n">{name} <span className="a">&lt;{addr}&gt;</span></div>
          <button className="rdetails-toggle" onClick={(e) => { e.stopPropagation(); setDetails((v) => !v); }}>
            {recipientSummary(m, t, me)}
            <IcChevron size={12} className={details ? "up" : ""} />
          </button>
        </div>
        <div className="rfrom-r">
          <AddSenderButton name={name} email={addr} />
          <button className="ib" title={t("webmail.reader.print")} onClick={(e) => { e.stopPropagation(); printEmail(); }}><IcPrinter size={15} /></button>
          <span className="tm" title={fullDateTime(m.receivedAt, locale)}>
            {shortTime(m.receivedAt, locale)} <i>({relativeTime(m.receivedAt, locale)})</i>
          </span>
        </div>
      </div>

      {details && <DetailsPanel m={m} locale={locale} />}

      {summarySlot}

      <MailBody m={m} service={service} textBody={body} spam={spam} />

      {m.attachments?.length > 0 && (
        <div className="att-grid">
          {m.attachments.map((a, i) => (
            <button key={i} className="att" onClick={() => onOpenAttachment(a)}>
              <div className="ic">{(a.type.split("/")[1] ?? "bin").slice(0, 4).toUpperCase()}</div>
              <div><div className="nm">{a.name ?? t("webmail.reader.attachment")}</div><div className="z">{Math.max(1, Math.round(a.size / 1024))} KB</div></div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// Gmail-style snooze presets: later (+3h), tonight (6pm), tomorrow (8am), next week
// (Monday 8am). Calculated from now — always in the future.
function snoozeOptions(): { key: string; when: Date }[] {
  const now = new Date();
  const at = (d: Date, h: number) => { const x = new Date(d); x.setHours(h, 0, 0, 0); return x; };
  const later = new Date(now.getTime() + 3 * 3600_000);
  let evening = at(now, 18);
  if (evening <= now) evening = at(new Date(now.getTime() + 86400_000), 18);
  const tomorrow = at(new Date(now.getTime() + 86400_000), 8);
  const nextMon = at(now, 8);
  do { nextMon.setDate(nextMon.getDate() + 1); } while (nextMon.getDay() !== 1); // next Monday
  return [
    { key: "later", when: later },
    { key: "evening", when: evening },
    { key: "tomorrow", when: tomorrow },
    { key: "nextWeek", when: nextMon },
  ];
}

const esc = (s: string) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]!));

// Extracts the readable text from an HTML body (so the AI and reply quotes don't see raw tags).
function htmlToText(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  return (doc.body.textContent ?? "").replace(/\n{3,}/g, "\n\n").trim();
}

function fmtAddrs(list?: EmailAddress[] | null): string {
  if (!list?.length) return "—";
  return list.map((a) => (a.name ? `${a.name} <${a.email}>` : a.email)).join(", ");
}

// "to me", "to Ana", "to dener@x.com and +2" — the short summary next to the sender, Gmail-style.
// It used to always assume "to me"; in Sent (recipient ≠ me) that was wrong. It now uses the real
// recipient and only says "me" when the address is the user's own.
function recipientSummary(m: EmailFull, t: TFunction, me: string): string {
  const all = [...(m.to ?? []), ...(m.cc ?? [])];
  if (!all.length) return t("webmail.reader.toNobody");
  const meL = me.toLowerCase();
  const label = (a: EmailAddress) => (a.email.toLowerCase() === meL ? t("webmail.reader.meWord") : a.name || a.email);
  if (all.length === 1) return t("webmail.reader.toOne", { who: label(all[0]) });
  return t("webmail.reader.toMany", { who: label(all[0]), n: all.length - 1 });
}

// Details panel: the headers Gmail hides behind the little arrow. Only shows what exists.
function DetailsPanel({ m, locale }: { m: EmailFull; locale: string }) {
  const { t } = useTranslation();
  const rows: [string, string][] = [
    [t("webmail.reader.from"), fmtAddrs(m.from)],
    ...(m.replyTo?.length && m.replyTo[0]?.email !== m.from?.[0]?.email
      ? [[t("webmail.reader.replyTo"), fmtAddrs(m.replyTo)] as [string, string]] : []),
    [t("webmail.reader.to"), fmtAddrs(m.to)],
    ...(m.cc?.length ? [[t("webmail.reader.cc"), fmtAddrs(m.cc)] as [string, string]] : []),
    [t("webmail.reader.date"), fullDateTime(m.receivedAt, locale)],
    ...(m.sentAt && m.sentAt !== m.receivedAt
      ? [[t("webmail.reader.sentAt"), fullDateTime(m.sentAt, locale)] as [string, string]] : []),
    [t("webmail.reader.subject"), m.subject || "—"],
  ];
  return (
    <div className="rdetails">
      <table><tbody>
        {rows.map(([k, v]) => <tr key={k}><td className="k">{k}</td><td>{v}</td></tr>)}
      </tbody></table>
    </div>
  );
}

// "Re: " / "Fwd: " without stacking prefixes: "Re: Re: x" is noise.
function rePrefix(prefix: string, subject: string): string {
  const s = subject ?? "";
  return s.toLowerCase().startsWith(prefix.toLowerCase()) ? s : prefix + s;
}

// Is there anyone else besides me and the sender? Decides whether the "reply all" button makes sense.
function hasOthers(m: EmailFull, myEmail: string): boolean {
  const from = m.from?.[0]?.email.toLowerCase();
  return [...(m.to ?? []), ...(m.cc ?? [])].some((a) => {
    const l = a.email.toLowerCase(); return l !== myEmail.toLowerCase() && l !== from;
  });
}
