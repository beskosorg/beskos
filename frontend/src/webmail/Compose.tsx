import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { settingsApi } from "./backend";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { MailService } from "../jmap/mail";
import type { UploadedBlob } from "../jmap/types";
import { backend, contactsApi } from "./backend";
import { publicUrl } from "../apiBase";
import { useFileDrop } from "./useFileDrop";
import { RecipientField } from "./RecipientField";
import { Spinner } from "./Spinner";
import { outboundBcc, parse } from "./rules";
import { SCRIPT_NAME } from "../jmap/sieve";
import { toast, toastAction } from "./toast";
import { useDisabledApps } from "./AdminView";
import {
  IcPaperclip, IcSparkle, IcTrash, IcX, IcMaximize, IcRestoreWin, IcCalendar, IcChevron, IcLink, IcImage, IcSend,
} from "../panel/icons";
import { EmojiPicker } from "./EmojiPicker";
import { sanitize } from "./mailHtml";

const parseList = (s?: string) => (s ?? "").split(",").map((e) => e.trim()).filter(Boolean);
const toAddrs = (list: string[]) => list.map((email) => ({ email }));

const UNDO_MS = 5000; // "undo send" window, Gmail-style.
// Forgotten attachment warning: matches attachment mentions (pt/en) to warn whoever wrote "see attached"
// but did not actually attach anything. Uses \b word boundaries to avoid firing on "attachment policy" etc.
const ATTACH_RE = /\b(anexo|anexos|anexad[oa]s?|anexei|anexad|em anexo|segue anexo|attach|attached|attachment|attachments|enclosed)\b/i;
const INLINE_LIMIT = 10 * 1024 * 1024; // >10MB does not go as an attachment: it is uploaded to Files and a link is sent instead.
const DRIVE_LIMIT = 100 * 1024 * 1024 * 1024; // fallback per-file cap on Files (the real one comes from the env via /api/files/limits).

// Large attachment: lives in Files, enters the email as a secure download link (token).
interface BigAttachment { fileId: string; name: string; size: number; token: string }
const publicLink = (token: string) => publicUrl(`/api/public/files/${token}/download`);
const escapeHtml = (s: string) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]!));

// Modern Compose: recipients with contact autocomplete, Cc/Bcc, rich-text editor,
// attachments (JMAP upload), "Write with AI", and draft saving.
export function Compose({ service, from, onClose, initial, dockIndex = 0 }: {
  service: MailService;
  from: string;
  onClose: () => void;
  initial?: { to?: string; cc?: string; subject?: string; body?: string; bodyHtml?: string; draftId?: string; attachFiles?: File[]; attachments?: UploadedBlob[] };
  dockIndex?: number; // position in the window stack (0 = rightmost) — like Gmail
}) {
  const { t } = useTranslation();
  const [to, setTo] = useState<string[]>(parseList(initial?.to));
  const [cc, setCc] = useState<string[]>(parseList(initial?.cc));
  const [bcc, setBcc] = useState<string[]>([]);
  const [showCc, setShowCc] = useState<boolean>(!!initial?.cc);
  const [subject, setSubject] = useState(initial?.subject ?? "");
  const filesOff = useDisabledApps().includes("files"); // without Files, there is no Drive for large attachments
  // Attachments already on the server (e.g. when reopening a draft): rehydrates the state with the existing blobs,
  // otherwise the draft would lose the attachment when reopened/re-saved. New files come in via onFiles.
  const [atts, setAtts] = useState<UploadedBlob[]>(initial?.attachments ?? []);
  const [bigAtts, setBigAtts] = useState<BigAttachment[]>([]);
  const [pendingBig, setPendingBig] = useState<File[]>([]); // large files awaiting confirmation
  // Dropping files on the window attaches them. `onFiles` is a function declaration and therefore
  // hoisted, so it can be referenced here, above where it is written -- and it already decides
  // between a normal attachment and a large file that goes up to the Drive instead.
  const { over: dropOver, handlers: dropHandlers } = useFileDrop((files) => void onFiles(files));
  const [uploading, setUploading] = useState(0);
  const [aiOpen, setAiOpen] = useState(false);
  const [draftId, setDraftId] = useState<string | undefined>(initial?.draftId);
  const [maxed, setMaxed] = useState(false);
  const [min, setMin] = useState(false);
  const [err, setErr] = useState("");
  const [schedOpen, setSchedOpen] = useState(false);
  const [sending, setSending] = useState(false);
  const [emojiAnchor, setEmojiAnchor] = useState<DOMRect | null>(null);
  const editor = useRef<HTMLDivElement>(null);
  const fileIn = useRef<HTMLInputElement>(null);
  const imgIn = useRef<HTMLInputElement>(null);
  const savedRange = useRef<Range | null>(null);

  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  // Contact groups as distribution lists in the recipient autocomplete.
  const contactGroups = useQuery({ queryKey: ["contact-groups"], queryFn: () => contactsApi.groups(), retry: false });
  // Sending identities (From:): the primary one + aliases. Only shows the selector when there is more than one.
  const identities = useQuery({ queryKey: ["identities"], queryFn: () => service.identities(), retry: false });
  // Our own aliases: an alias delivers to the same mailbox, but the server may not publish a JMAP
  // identity for it. We combine both sources so the address shows up in "From:" regardless.
  const myAddr = useQuery({ queryKey: ["my-addresses"], queryFn: () => backend.myAddresses(), retry: false });
  const fromOptions = useMemo(() => {
    const seen = new Set<string>();
    const out: { email: string; label: string }[] = [];
    const add = (email: string, label?: string) => {
      const k = email.toLowerCase();
      if (!email || seen.has(k)) return;
      seen.add(k);
      out.push({ email, label: label || email });
    };
    add(from);
    for (const i of identities.data ?? []) add(i.email, i.name ? `${i.name} <${i.email}>` : i.email);
    for (const a of myAddr.data?.aliases ?? []) add(a);
    return out;
  }, [from, identities.data, myAddr.data]);
  const [fromAddr, setFromAddr] = useState(from);
  // Outbound rules (same cache as RulesView): evaluated on send to apply automatic Bcc.
  const rulesQ = useQuery({
    queryKey: ["sieve"],
    queryFn: async () => {
      const api = service.rules();
      const s = (await api.list()).find((x) => x.name === SCRIPT_NAME);
      if (!s?.blobId) return [];
      return parse(await api.download(s.blobId)) ?? [];
    },
    retry: false,
  });
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  const limits = useQuery({ queryKey: ["file-limits"], queryFn: () => backend.fileLimits(), retry: false, staleTime: Infinity });
  const [tplOpen, setTplOpen] = useState(false);
  const templatesQ = useQuery({ queryKey: ["mail-templates"], queryFn: () => backend.templatesList(), retry: false });
  // Insert template: fills in the subject if empty and prepends the (sanitized HTML) body in the editor.
  const insertTemplate = (tpl: { subject: string; body_html: string }) => {
    if (!subject.trim() && tpl.subject) setSubject(tpl.subject);
    if (editor.current) {
      const clean = sanitize(tpl.body_html).html;
      editor.current.innerHTML = clean + (editor.current.innerHTML || "");
    }
    dirty.current = true;
    setTplOpen(false);
  };
  const qcTpl = useQueryClient();
  const saveAsTemplate = async () => {
    const name = window.prompt(t("webmail.templates.namePrompt"));
    if (!name || !name.trim()) return;
    try {
      await backend.templateCreate({ name: name.trim(), subject, body_html: editor.current?.innerHTML ?? "" });
      qcTpl.invalidateQueries({ queryKey: ["mail-templates"] });
      toast(t("webmail.templates.saved"));
    } catch (e) { toast((e as Error).message); }
  };
  const deleteTemplate = async (id: string) => {
    try { await backend.templateDelete(id); qcTpl.invalidateQueries({ queryKey: ["mail-templates"] }); }
    catch (e) { toast((e as Error).message); }
  };
  const driveLimit = limits.data?.max_upload_bytes ?? DRIVE_LIMIT; // per-file cap on Files (env)

  // Initial body = what came in (reply/draft) + signature. The signature is added ONLY for a
  // new message (no predefined body) and when enabled — it is not stacked onto a long reply.
  const sig = settings.data?.email;
  // Initial editor content in HTML: the body that came in (reply/draft, text -> HTML) and, only for a
  // new message with the signature enabled, the HTML signature after the "-- " separator.
  const startBodyHtml = () => {
    // Reply/draft preserving HTML: uses the original (sanitized) HTML instead of text->HTML.
    if (initial?.bodyHtml) return sanitize(initial.bodyHtml).html;
    const base = initial?.body ?? "";
    const baseHtml = base ? escapeHtml(base).replace(/\n/g, "<br>") : "";
    if (base || draftId || !sig?.signature || sig.signatureOn === false) return baseHtml;
    return `<br><br>-- <br>${sig.signature}`;
  };
  // Only injects after settings load (the editor starts empty and receives the signature).
  const sigApplied = useRef(false);
  useEffect(() => {
    if (sigApplied.current || settings.isLoading) return;
    sigApplied.current = true;
    if (editor.current && !editor.current.innerHTML.trim()) editor.current.innerHTML = startBodyHtml();
  }, [settings.isLoading]);

  // Block of links for the large attachments, appended at the end of the body (plain text).
  const bigAttBlock = () => {
    if (!bigAtts.length) return "";
    const lines = bigAtts.map((b) => `${b.name} (${Math.round(b.size / (1024 * 1024))} MB): ${publicLink(b.token)}`);
    return `\n\n${t("webmail.compose.bigAttHeader")}\n${lines.join("\n")}\n`;
  };
  // The same block in HTML (for the HTML body).
  const bigAttBlockHtml = () => {
    if (!bigAtts.length) return "";
    const items = bigAtts.map((b) => `<li><a href="${publicLink(b.token)}">${escapeHtml(b.name)}</a> (${Math.round(b.size / (1024 * 1024))} MB)</li>`).join("");
    return `<br><br><b>${escapeHtml(t("webmail.compose.bigAttHeader"))}</b><ul>${items}</ul>`;
  };

  const buildDraft = () => ({
    from: { email: fromAddr },
    to: toAddrs(to),
    cc: cc.length ? toAddrs(cc) : undefined,
    bcc: bcc.length ? toAddrs(bcc) : undefined,
    subject,
    textBody: (() => { const b = (editor.current?.innerText ?? "") + bigAttBlock(); return b.endsWith("\n") ? b : b + "\n"; })(),
    // We always send HTML (like Gmail): preserves formatting, images, links, and the HTML signature.
    htmlBody: `<div>${editor.current?.innerHTML ?? ""}${bigAttBlockHtml()}</div>`,
    attachments: atts,
  });

  const saveDraft = useMutation({
    mutationFn: () => service.saveDraft(buildDraft(), draftId),
    onSuccess: (id) => { setDraftId(id); toast(t("webmail.compose.draftSaved")); },
    onError: (e) => setErr((e as Error).message),
  });

  // Autosave draft: saves silently 2s after the person stops typing, so what was typed is never
  // lost. Does not run right after sending, nor while sending is in progress.
  const dirty = useRef(false);
  useEffect(() => {
    if (!dirty.current || sending) return;
    const id = setTimeout(() => {
      if (!editor.current) return;
      const empty = !to.length && !subject.trim() && !editor.current.innerText.trim() && !atts.length;
      if (empty) return; // does not create an empty draft
      service.saveDraft(buildDraft(), draftId).then((newId) => setDraftId(newId)).catch(() => { /* silent */ });
    }, 2000);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [to, cc, bcc, subject, atts, sending]);
  const markDirty = () => { dirty.current = true; };

  // Shares the large attachments with recipients from the same organization — so they show up in
  // "Shared with me" in their Files. External recipients are ignored by the backend (they only get the link).
  const shareBigWithOrg = () => {
    if (!bigAtts.length) return;
    const emails = [...to, ...cc, ...bcc];
    if (!emails.length) return;
    for (const b of bigAtts) backend.shareRecipients(b.fileId, emails).catch(() => { /* best-effort */ });
  };

  // Send with an "undo" window: closes the composer and only actually fires after UNDO_MS.
  // The toast offers "Undo" during that window. sendAt (optional) schedules it on the server.
  function doSend(sendAt?: Date) {
    setErr("");
    // Forgotten attachment: the body/subject mentions an attachment but nothing was attached -> confirm before sending.
    const hasAtt = atts.length > 0 || bigAtts.length > 0 || pendingBig.length > 0;
    if (!hasAtt && ATTACH_RE.test(`${subject} ${editor.current?.innerText ?? ""}`) && !window.confirm(t("webmail.compose.attachWarn"))) return;
    const draft = buildDraft();
    // OUTBOUND rules: adds an automatic Bcc for the ones that match (e.g. "when sending to @client.com, Bcc archive").
    const extraBcc = outboundBcc(rulesQ.data ?? [], {
      to: to.join(", "), cc: cc.join(", "), subject, body: editor.current?.innerText ?? "",
    });
    if (extraBcc.length) {
      draft.bcc = toAddrs([...new Set([...bcc, ...extraBcc])]);
    }
    const dId = draftId;
    dirty.current = false;
    setSending(true);
    // Server-side send: creates the draft in JMAP and registers the send in the outbox. The worker submits it
    // when it is due — scheduled (chosen time) or undo-send (now + window). Survives closing the tab, and
    // "undo" cancels it on the server (it does not depend on a timer on the client).
    const when = sendAt ?? new Date(Date.now() + UNDO_MS);
    service.prepareOutbox(draft, dId)
      .then((prep) => backend.outboxCreate({
        email_id: prep.emailId, identity_id: prep.identityId, from: prep.from, rcpts: prep.rcpts,
        subject, send_at: when.toISOString(),
      }))
      .then(({ id }) => {
        shareBigWithOrg();
        // Saves the recipients as contacts, to suggest them in the next "To". Does not block
        // sending or show an error if it fails — it is a nice-to-have.
        const rcpts = [...to, ...cc, ...bcc];
        if (rcpts.length) void contactsApi.seen(rcpts).then(() => qcTpl.invalidateQueries({ queryKey: ["contacts"] })).catch(() => {});
        if (sendAt) {
          toast(t("webmail.compose.scheduledFor", { when: fmtWhen(sendAt) }));
        } else {
          toastAction(t("webmail.compose.sending"), t("webmail.compose.undo"), () => {
            backend.outboxCancel(id).then(() => toast(t("webmail.compose.undone")))
              .catch(() => toast(t("webmail.compose.undoTooLate")));
          }, UNDO_MS);
        }
        onClose();
      })
      .catch((e) => { setSending(false); dirty.current = true; setErr((e as Error).message); });
  }

  // Total already attached inline (the 10 MB limit applies to the WHOLE set, not each file).
  const inlineTotal = () => atts.reduce((s, a) => s + a.size, 0);

  // Files already prepared to attach when opening the composer (e.g. the .vcf from "send contact"). Runs
  // only once, on mount. They are small, so they always fall into the inline attachment.
  const preAttached = useRef(false);
  useEffect(() => {
    const files = initial?.attachFiles;
    if (preAttached.current || !files?.length) return;
    preAttached.current = true;
    const dt = new DataTransfer();
    files.forEach((f) => dt.items.add(f));
    void onFiles(dt.files);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onFiles(files: FileList | null) {
    if (!files) return;
    markDirty();
    const gb = driveLimit / (1024 * 1024 * 1024);
    let budget = INLINE_LIMIT - inlineTotal(); // remaining space in the 10 MB total
    const toInline: File[] = [];
    const toBig: File[] = [];
    for (const f of Array.from(files)) {
      if (f.size > driveLimit) { toast(t("webmail.compose.bigTooLarge", { name: f.name, limit: gb.toFixed(gb < 10 ? 1 : 0) })); continue; }
      // Does it fit in what remains of the attachment total? Goes inline; otherwise it goes via Files.
      if (f.size <= budget) { toInline.push(f); budget -= f.size; }
      else toBig.push(f);
    }
    if (toBig.length) {
      // Without the Files app, there is no Drive to upload the large attachment to: says it cannot, instead
      // of suggesting a path the organization has turned off.
      if (filesOff) toBig.forEach((f) => toast(t("webmail.compose.bigNoDrive", { name: f.name })));
      else setPendingBig((p) => [...p, ...toBig]);
    }
    for (const f of toInline) {
      setUploading((n) => n + 1);
      try { const b = await service.uploadBlob(f); setAtts((a) => [...a, b]); }
      catch (e) { toast((e as Error).message); }
      finally { setUploading((n) => n - 1); }
    }
  }

  // Confirmed: uploads the large files to Files and generates a secure link for each one.
  async function confirmBig() {
    const files = pendingBig;
    setPendingBig([]);
    for (const f of files) {
      setUploading((n) => n + 1);
      try {
        const rec = await backend.uploadFile(f);
        const { token } = await backend.fileLink(rec.id);
        setBigAtts((a) => [...a, { fileId: rec.id, name: f.name, size: f.size, token }]);
      } catch (e) { toast((e as Error).message); }
      finally { setUploading((n) => n - 1); }
    }
  }

  const fmt = (e: React.MouseEvent, cmd: string, val?: string) => {
    e.preventDefault();
    document.execCommand(cmd, false, val);
    editor.current?.focus();
    markDirty();
  };

  // Saves the cursor position (the emoji/image picker steals focus from the editor).
  const saveSelection = () => {
    const sel = window.getSelection();
    if (sel && sel.rangeCount && editor.current?.contains(sel.anchorNode)) savedRange.current = sel.getRangeAt(0).cloneRange();
  };
  const restoreSelection = () => {
    editor.current?.focus();
    const sel = window.getSelection();
    if (savedRange.current && sel) { sel.removeAllRanges(); sel.addRange(savedRange.current); }
  };

  // Insert link: wraps the selection (or inserts the URL itself) in an <a>.
  const insertLink = () => {
    saveSelection();
    const url = window.prompt(t("webmail.compose.linkPrompt"), "https://");
    if (!url) return;
    restoreSelection();
    const sel = window.getSelection();
    if (sel && sel.toString()) document.execCommand("createLink", false, url);
    else document.execCommand("insertHTML", false, `<a href="${escapeHtml(url)}">${escapeHtml(url)}</a>`);
    markDirty();
  };

  // Insert inline image: reads the file as a data URI and puts it in the body (renders right away in the editor).
  const onImageFile = (files: FileList | null) => {
    const f = files?.[0];
    if (!f) return;
    const reader = new FileReader();
    reader.onload = () => {
      restoreSelection();
      document.execCommand("insertHTML", false, `<img src="${reader.result}" style="max-width:100%">`);
      markDirty();
    };
    reader.readAsDataURL(f);
  };

  const pickEmoji = (ch: string) => { restoreSelection(); document.execCommand("insertText", false, ch); markDirty(); };

  // Gmail-style stacking: each window docks to the right, offset by its position. When
  // maximized it takes over the screen (ignores the offset).
  const dockStyle = maxed ? undefined : { insetInlineEnd: 24 + dockIndex * 736 };
  return (
    <div className="cmp-bg open">
      {/* Dropping files anywhere on the window attaches them, which is where people aim -- not at
          the paperclip. The handlers go on the window and not on the body so that a file let go
          over the header or the address fields lands too, instead of falling through to the page
          behind and making the browser navigate away from the draft. */}
      <div className={"cmp" + (maxed ? " maxed" : "") + (min ? " min" : "")} style={dockStyle} {...dropHandlers}>
        {dropOver && !min && (
          <div className="drop-overlay"><div className="drop-card"><IcPaperclip size={22} /> {t("webmail.compose.dropHere")}</div></div>
        )}
        <header
          onClick={(e) => { if (min && !(e.target as HTMLElement).closest(".cmp-hdr-actions")) setMin(false); }}
          onDoubleClick={() => { if (!min) setMaxed((v) => !v); }}>
          <span className="cmp-title">{t("webmail.compose.title")}</span>
          <div className="cmp-hdr-actions">
            <button className="ib" title={min ? t("webmail.compose.expand") : t("webmail.compose.minimize")} onClick={() => setMin((v) => !v)}>{min ? "▢" : "—"}</button>
            <button className="ib" title={maxed ? t("webmail.compose.restore") : t("webmail.compose.maximize")}
              onClick={() => { if (min) { setMin(false); setMaxed(true); } else { setMaxed((v) => !v); } }}>
              {maxed ? <IcRestoreWin size={15} /> : <IcMaximize size={15} />}
            </button>
            <button className="ib" title={t("webmail.compose.close")} onClick={onClose}><IcX size={15} /></button>
          </div>
        </header>

        {fromOptions.length > 1 && (
          <div className="rcpt from-row">
            <label>{t("webmail.compose.from")}</label>
            <select value={fromAddr} onChange={(e) => { markDirty(); setFromAddr(e.target.value); }}>
              {fromOptions.map((o) => <option key={o.email} value={o.email}>{o.label}</option>)}
            </select>
          </div>
        )}
        <RecipientField label={t("webmail.compose.to")} value={to} onChange={(v) => { markDirty(); setTo(v); }} contacts={contacts.data ?? []} groups={contactGroups.data ?? []} autoFocus />
        {showCc
          ? <>
              <RecipientField label={t("webmail.compose.cc")} value={cc} onChange={(v) => { markDirty(); setCc(v); }} contacts={contacts.data ?? []} groups={contactGroups.data ?? []} />
              <RecipientField label={t("webmail.compose.bcc")} value={bcc} onChange={(v) => { markDirty(); setBcc(v); }} contacts={contacts.data ?? []} groups={contactGroups.data ?? []} />
            </>
          : <button className="cc-toggle" onClick={() => setShowCc(true)}>{t("webmail.compose.addCc")}</button>}

        <div className="f"><label>{t("webmail.compose.subject")}</label>
          <input value={subject} onChange={(e) => { markDirty(); setSubject(e.target.value); }} placeholder={t("webmail.compose.subject")} /></div>

        <div className="tools">
          <button title="Bold" onMouseDown={(e) => fmt(e, "bold")}><b>B</b></button>
          <button title="Italic" onMouseDown={(e) => fmt(e, "italic")}><i>I</i></button>
          <button title="Underline" onMouseDown={(e) => fmt(e, "underline")} style={{ textDecoration: "underline" }}>U</button>
          <label className="color-btn" title={t("webmail.compose.textColor")} onMouseDown={saveSelection}>
            <b style={{ borderBottom: "3px solid currentColor", lineHeight: 1 }}>A</b>
            <input type="color" onChange={(e) => { restoreSelection(); document.execCommand("foreColor", false, e.target.value); markDirty(); }} />
          </label>
          <span className="sep" />
          <button title="Bulleted list" onMouseDown={(e) => fmt(e, "insertUnorderedList")}>☰</button>
          <button title="Numbered list" onMouseDown={(e) => fmt(e, "insertOrderedList")}>№</button>
          <button title="Quote" onMouseDown={(e) => fmt(e, "formatBlock", "blockquote")}>❝</button>
          <button title="Clear" onMouseDown={(e) => fmt(e, "removeFormat")}>⌫</button>
          <span className="sep" />
          <button title={t("webmail.compose.insertLink")} onMouseDown={(e) => { e.preventDefault(); insertLink(); }}><IcLink size={15} /></button>
          <button title={t("webmail.compose.insertImage")} onMouseDown={(e) => { e.preventDefault(); saveSelection(); imgIn.current?.click(); }}><IcImage size={15} /></button>
          <button title={t("webmail.compose.emoji")} onMouseDown={(e) => { e.preventDefault(); saveSelection(); setEmojiAnchor(emojiAnchor ? null : (e.currentTarget as HTMLElement).getBoundingClientRect()); }}>😊</button>
          <span className="sep" />
          <button title={t("webmail.compose.attach")} onMouseDown={(e) => { e.preventDefault(); fileIn.current?.click(); }}><IcPaperclip size={15} /></button>
          <button title={t("webmail.ai.writeWithAI")} className={aiOpen ? "on" : ""} onMouseDown={(e) => { e.preventDefault(); setAiOpen((v) => !v); }}><IcSparkle size={15} /></button>
        </div>

        {emojiAnchor && <EmojiPicker anchor={emojiAnchor} onPick={pickEmoji} onClose={() => setEmojiAnchor(null)} />}
        <input ref={imgIn} type="file" accept="image/*" hidden onChange={(e) => { onImageFile(e.target.files); e.target.value = ""; }} />

        {aiOpen && <AICompose onUse={(text) => { if (editor.current) editor.current.innerText = text; setAiOpen(false); }} onClose={() => setAiOpen(false)} />}

        <div className="editor" ref={editor} contentEditable suppressContentEditableWarning
          onInput={markDirty} data-ph={t("webmail.compose.body")} />

        <input ref={fileIn} type="file" multiple hidden onChange={(e) => { onFiles(e.target.files); e.target.value = ""; }} />
        {(atts.length > 0 || bigAtts.length > 0 || uploading > 0) && (
          <div className="cmp-atts">
            {atts.map((a) => (
              <span key={a.blobId} className="cmp-att"><IcPaperclip size={13} /> {a.name}
                <small>{Math.max(1, Math.round(a.size / 1024))} KB</small>
                <button title={t("common.removeAttachment")} onClick={() => setAtts((x) => x.filter((y) => y !== a))}><IcTrash size={12} /></button>
              </span>
            ))}
            {bigAtts.map((b) => (
              // Large attachment = a Files link. Different icon and caption to make it clear it goes
              // as a link (not as a file attached to the email).
              <span key={b.fileId} className="cmp-att big" title={t("webmail.compose.bigAttTip")}>
                <IcLink size={13} /> {b.name}
                <small>{Math.round(b.size / (1024 * 1024))} MB · {t("webmail.compose.bigAttLabel")}</small>
                <button title={t("common.removeAttachment")} onClick={() => setBigAtts((x) => x.filter((y) => y !== b))}><IcTrash size={12} /></button>
              </span>
            ))}
            {uploading > 0 && <span className="cmp-att up">{t("webmail.compose.uploading")}…</span>}
          </div>
        )}

        {pendingBig.length > 0 && (
          // File above the limit: ask before sending via Files (it does not upload on its own).
          <div className="bigask">
            <div className="bigask-txt">
              <b>{t("webmail.compose.bigAskTitle", { limit: 10 })}</b>
              <p>{t("webmail.compose.bigAskBody", { limit: 10, names: pendingBig.map((f) => f.name).join(", ") })}</p>
            </div>
            <div className="bigask-actions">
              <button className="chipbtn" onClick={() => setPendingBig([])}>{t("webmail.compose.bigAskCancel")}</button>
              <button className="send" onClick={() => void confirmBig()}>{t("webmail.compose.bigAskConfirm")}</button>
            </div>
          </div>
        )}

        <footer>
          <div className="send-split">
            <button className="send" disabled={sending || !to.length || uploading > 0} onClick={() => doSend()}>
              {sending ? "…" : <><IcSend size={15} /> {t("webmail.compose.send")}</>}
            </button>
            <button className="send send-more" disabled={sending || !to.length || uploading > 0}
              title={t("webmail.compose.scheduleSend")} onClick={() => setSchedOpen((v) => !v)}>
              <IcChevron size={14} />
            </button>
            {schedOpen && (
              <ScheduleMenu onPick={(d) => { setSchedOpen(false); doSend(d); }} onClose={() => setSchedOpen(false)} />
            )}
          </div>
          <button className="chipbtn" disabled={saveDraft.isPending} onClick={() => saveDraft.mutate()}>{t("webmail.compose.saveDraft")}</button>
          <div className="tpl-wrap">
            <button className="chipbtn" onClick={() => setTplOpen((v) => !v)} title={t("webmail.templates.title")}><IcSparkle size={14} /> {t("webmail.templates.short")}</button>
            {tplOpen && (
              <>
                <div className="menu-backdrop" onClick={() => setTplOpen(false)} />
                <div className="tpl-menu">
                  <div className="tpl-menu-head">{t("webmail.templates.title")}</div>
                  {(templatesQ.data ?? []).length === 0 && <div className="tpl-empty">{t("webmail.templates.empty")}</div>}
                  {(templatesQ.data ?? []).map((tpl) => (
                    <div key={tpl.id} className="tpl-row">
                      <button className="tpl-pick" onClick={() => insertTemplate(tpl)}>{tpl.name}</button>
                      <button className="tpl-del" title={t("common.delete")} onClick={() => void deleteTemplate(tpl.id)}><IcTrash size={13} /></button>
                    </div>
                  ))}
                  <button className="tpl-save" onClick={() => void saveAsTemplate()}>{t("webmail.templates.saveCurrent")}</button>
                </div>
              </>
            )}
          </div>
          {err && <span className="err">{err}</span>}
        </footer>
      </div>
    </div>
  );
}

// Formats the scheduled instant for the toast/label, in the active locale.
function fmtWhen(d: Date): string {
  return new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" }).format(d);
}

// Scheduling menu: useful presets + a custom time. The presets are computed from
// now (only here, in UI code — no Date restriction). datetime-local returns local time.
function ScheduleMenu({ onPick, onClose }: { onPick: (d: Date) => void; onClose: () => void }) {
  const { t } = useTranslation();
  const [custom, setCustom] = useState("");
  const now = new Date();

  const atHour = (dayOffset: number, h: number) => {
    const d = new Date(now);
    d.setDate(d.getDate() + dayOffset);
    d.setHours(h, 0, 0, 0);
    return d;
  };
  // Tomorrow 08:00; today/tomorrow 13:00; next Monday 08:00.
  const tomorrowAm = atHour(1, 8);
  const laterToday = now.getHours() < 12 ? atHour(0, 13) : atHour(1, 13);
  const nextMon = (() => { const d = atHour(1, 8); while (d.getDay() !== 1) d.setDate(d.getDate() + 1); return d; })();

  const presets = [
    { label: t("webmail.compose.schedTomorrowAm"), d: tomorrowAm },
    { label: t("webmail.compose.schedLater"), d: laterToday },
    { label: t("webmail.compose.schedNextMon"), d: nextMon },
  ];

  const pickCustom = () => {
    const d = new Date(custom);
    if (isNaN(d.getTime()) || d.getTime() <= Date.now()) return;
    onPick(d);
  };

  return (
    <>
      <div className="sched-backdrop" onClick={onClose} />
      <div className="sched-menu">
        <div className="sched-head"><IcCalendar size={15} /> {t("webmail.compose.scheduleSend")}</div>
        {presets.map((p) => (
          <button key={p.label} className="sched-opt" onClick={() => onPick(p.d)}>
            <span>{p.label}</span><small>{fmtWhen(p.d)}</small>
          </button>
        ))}
        <div className="sched-custom">
          <input type="datetime-local" value={custom} onChange={(e) => setCustom(e.target.value)} />
          <button className="chipbtn" disabled={!custom} onClick={pickCustom}>{t("webmail.compose.schedPick")}</button>
        </div>
      </div>
    </>
  );
}

// Inline "Write with AI" panel: generates the body from scratch based on an instruction.
function AICompose({ onUse, onClose }: { onUse: (text: string) => void; onClose: () => void }) {
  const { t } = useTranslation();
  const [instruction, setInstruction] = useState("");
  const gen = useMutation({
    mutationFn: (tone: string) => backend.draft("", instruction, tone),
    onSuccess: (r) => onUse(r.draft),
    onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="cmp-ai">
      <div className="cmp-ai-row">
        <IcSparkle size={15} />
        <input autoFocus value={instruction} placeholder={t("webmail.ai.composePlaceholder")}
          onChange={(e) => setInstruction(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && instruction.trim()) gen.mutate("neutro"); }} />
        <button className="chipbtn" disabled={!instruction.trim() || gen.isPending} onClick={() => gen.mutate("neutro")}>
          {gen.isPending ? <Spinner size={14} /> : t("webmail.ai.generate")}
        </button>
        <button className="ib" title={t("common.close")} onClick={onClose}><IcX size={14} /></button>
      </div>
    </div>
  );
}
