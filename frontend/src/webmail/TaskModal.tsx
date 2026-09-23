import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { renderRich } from "./richtext";
import { MdEditor } from "./MdEditor";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { backend, contactsApi, colleaguesApi, checklistApi, cardActivityApi, cardAttachmentsApi, type Card, type Column, type Label as LabelType } from "./backend";
import { triggerDownload } from "../apiBase";
import { TaskComments } from "./TaskComments";
import { api } from "../api";
import { toast, toastAction } from "./toast";
import { Spinner } from "./Spinner";
import { publicUrl } from "../apiBase";
import { hueFor, initials } from "./util";
import { IcTrash, IcX, IcSparkle, IcPencil, IcLink, IcChevron, IcPaperclip, IcDownload, IcArchive, IcShare, IcPlus } from "../panel/icons";
import { useDisabledApps } from "./AdminView";
import { ShareCardChatDrawer } from "./ShareCardChatDrawer";

const COLORS = ["#B4785F", "#3B6FB0", "#8257C7", "#B44D3A", "#2E7D5B", "#C0692F", "#C9871F"];
const PRIORITIES = ["low", "normal", "high", "urgent"] as const;

// Create/edit task (card) modal. Assignee via contact autocomplete; label with color;
// due date; column choice. Delete button in edit mode. No prompt/alert.
// Task modal in two columns: the task on the left, the comments on the right (with their own
// scroll). Opens in READ mode — editing is one click on the pencil. It used to be a giant form,
// and the conversation ended up pushed way below the fold.
//
// `card == null` (new task) → opens already in edit mode and with no comments column: there is
// nothing to comment on in a task that does not exist yet.
// Soft tints (guaranteed contrast: fixed dark text over any of them).
const CARD_COLORS = ["#fde2dd", "#fcecc5", "#d8f0d3", "#d7e9f7", "#e6def3", "#fbdaeb", "#e0f0ee", "#efe9df"];

export function TaskModal({ boardId, columns, card, columnId, initialTitle, boardCards = [], assignees = [], deps = [], onChanged, onClose, onSaved }: {
  boardId: string;
  columns: Column[];
  card?: Card | null;
  columnId?: string;
  initialTitle?: string; // e.g. a selected snippet from the meeting notes
  boardCards?: Card[]; // for the dependency picker
  assignees?: { card_id: string; user_id: string; name: string }[]; // additional assignees (all cards)
  deps?: { card_id: string; depends_on_id: string; dep_title: string; dep_column: string }[];
  onChanged?: () => void; // reloads the board after touching assignees/dependencies
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t, i18n } = useTranslation();
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false }).data;
  const [editing, setEditing] = useState(!card);
  // ESC closes — the same gesture as the standard drawer container (Drawer). The task keeps its
  // two-column layout, so it reuses the .drawer classes instead of the component, but the behavior is the same.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const [title, setTitle] = useState(card?.title ?? initialTitle ?? "");
  const [description, setDescription] = useState(card?.description ?? "");
  const [label, setLabel] = useState(card?.label ?? "");
  const [assigneeId, setAssigneeId] = useState(card?.assignee_id ?? "");
  const [due, setDue] = useState(card?.due ?? "");
  const [startDate, setStartDate] = useState(card?.start_date ?? "");
  const [priority, setPriority] = useState(card?.priority ?? "normal");
  const [col, setCol] = useState(card?.column_id ?? columnId ?? columns[0]?.id ?? "");
  const [contactId, setContactId] = useState(card?.contact_id ?? "");
  const [color, setColor] = useState(card?.color ?? "");
  const [shareChat, setShareChat] = useState(false);
  const [idea, setIdea] = useState("");

  const people = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  // Tenant's label catalog (name + color): the card's labels link to it.
  const labelsCat = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels(), retry: false });
  const labelColor = (name: string) => labelsCat.data?.find((l) => l.name === name)?.color;

  // AI: expands a short idea into a title + objective description. If the idea field is
  // empty, uses the Title itself as the seed (avoids a "button does nothing" feel when there's only a title).
  const aiSeed = idea.trim() || title.trim();
  const plan = useMutation({
    mutationFn: () => backend.planTask(aiSeed),
    onSuccess: (d) => { if (d.title) setTitle(d.title); if (d.description) setDescription(d.description); setIdea(""); },
    onError: (e) => toast((e as Error).message),
  });

  const save = useMutation({
    mutationFn: async () => {
      const fields = { title: title.trim(), description, label, due, start_date: startDate, priority, assignee_id: assigneeId, contact_id: contactId, color };
      if (card) {
        await backend.updateCard(card.id, fields);
        if (col !== card.column_id) await backend.moveCard(card.id, col, card.position);
      } else {
        await backend.createCard(boardId, { column_id: col, ...fields });
      }
    },
    // Saving an existing task goes back to read mode; for a new one, it closes (the card is already on the board).
    onSuccess: () => { toast(t(card ? "webmail.kanban.saved" : "webmail.kanban.created")); if (card) { setEditing(false); onSaved(); } else onSaved(); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: () => backend.deleteCard(card!.id),
    onSuccess: () => { toast(t("webmail.kanban.deleted")); onSaved(); },
    onError: (e) => toast((e as Error).message),
  });
  // Archiving removes it from the board without deleting; the "undo" toast restores it by id.
  const archive = useMutation({
    mutationFn: () => backend.archiveCard(card!.id, true),
    onSuccess: () => { const id = card!.id; toastAction(t("webmail.kanban.cardArchived"), t("common.undo"), () => backend.archiveCard(id, false).then(() => onSaved())); onSaved(); },
    onError: (e) => toast((e as Error).message),
  });

  // The task's link is its own address — the same one the notification email uses.
  const copyLink = async () => {
    const url = publicUrl(`/mail/kanban/${boardId}/${card!.id}`);
    try {
      await navigator.clipboard.writeText(url);
      toast(t("webmail.kanban.linkCopied"));
    } catch {
      // clipboard requires a secure context (https/localhost); without it, show the link to copy by hand.
      toast(url);
    }
  };

  const assigneeName = (people.data ?? []).find((p) => p.id === assigneeId);
  const contactName = (contacts.data ?? []).find((c) => c.id === contactId);
  const colName = columns.find((c) => c.id === col)?.name;
  const chatOn = !useDisabledApps().includes("chat");

  return (
    <div className="drawer-bg open" onMouseDown={onClose}>
      <div className={"drawer tk-split" + (card ? "" : " tk-new")} onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span>{card?.card_key ?? (card ? t("webmail.kanban.task") : t("webmail.kanban.newTask"))}</span>
          <div className="grow" />
          {card && chatOn && <button className="ib" title={t("webmail.kanban.shareChat")} onClick={() => setShareChat(true)}><IcShare size={15} /></button>}
          {card && <button className="ib" title={t("webmail.kanban.copyLink")} onClick={copyLink}><IcLink size={15} /></button>}
          {card && !editing && <button className="ib" title={t("webmail.kanban.editTask")} onClick={() => setEditing(true)}><IcPencil size={15} /></button>}
          <button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
        </header>

        <div className="tk-cols">
          <div className="tk-main">
            {editing ? (
              <div className="body meet-form">
                <div className="ai-nl">
                  <IcSparkle size={15} />
                  <input value={idea} placeholder={t("webmail.kanban.aiPlaceholder")} onChange={(e) => setIdea(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Enter" && aiSeed) plan.mutate(); }} />
                  <button className="chipbtn" disabled={!aiSeed || plan.isPending} onClick={() => plan.mutate()}>{plan.isPending ? <Spinner size={14} /> : t("webmail.kanban.aiFill")}</button>
                </div>
                <label>{t("webmail.kanban.title")}<input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("webmail.kanban.titlePh")} /></label>
                <label>{t("webmail.kanban.description")}
                  <MdEditor value={description} onChange={setDescription} placeholder={t("webmail.kanban.mdHint")} />
                </label>
                <AssigneePicker value={assigneeId} onChange={setAssigneeId} meId={me?.id} />
                <div className="row3">
                  <label>{t("webmail.kanban.column")}
                    <select value={col} onChange={(e) => setCol(e.target.value)}>
                      {columns.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
                    </select>
                  </label>
                  <label>{t("webmail.kanban.startDate")}<input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} /></label>
                  <label>{t("webmail.kanban.due")}<input type="date" value={due} onChange={(e) => setDue(e.target.value)} /></label>
                </div>
                <label>{t("webmail.kanban.priority")}
                  <select value={priority} onChange={(e) => setPriority(e.target.value)}>
                    {PRIORITIES.map((p) => <option key={p} value={p}>{t("webmail.kanban.priorities." + p)}</option>)}
                  </select>
                </label>
                <label>{t("webmail.kanban.cardColor")}
                  <div className="card-colors">
                    <button type="button" className={"cc-dot cc-none" + (color === "" ? " on" : "")} title={t("webmail.kanban.colorNone")} onClick={() => setColor("")}>×</button>
                    {CARD_COLORS.map((cc) => (
                      <button key={cc} type="button" className={"cc-dot" + (color === cc ? " on" : "")} style={{ background: cc }} onClick={() => setColor(cc)} />
                    ))}
                  </div>
                </label>
                <LabelPicker value={label} onChange={setLabel} labels={labelsCat.data ?? []} />
                <ContactPicker value={contactId} onChange={setContactId} />
              </div>
            ) : (
              <div className="tk-read">
                <h3>{title}</h3>
                {description ? <div className="tk-desc md">{renderRich(description)}</div> : <p className="tk-desc none">{t("webmail.kanban.noDescription")}</p>}
                <dl className="tk-meta">
                  <Row label={t("webmail.kanban.assignee")}>
                    {assigneeName
                      ? <span className="tk-who"><span className="av" style={{ background: hueFor(assigneeName.email) }}>{initials(assigneeName.name || assigneeName.email)}</span>{assigneeName.name || assigneeName.email}</span>
                      : card?.assignee
                        // Legacy text: someone who was never a user (migration 00026). Cannot be notified.
                        ? <span className="tk-who muted" title={t("webmail.kanban.legacyAssignee")}>{card.assignee}</span>
                        : <span className="muted">{t("webmail.kanban.noAssignee")}</span>}
                  </Row>
                  <Row label={t("webmail.kanban.column")}><span className="pnl-pill">{colName}</span></Row>
                  {priority !== "normal" && <Row label={t("webmail.kanban.priority")}><span className={"prio prio-" + priority}>{t("webmail.kanban.priorities." + priority)}</span></Row>}
                  {due && <Row label={t("webmail.kanban.due")}>{new Date(due + "T00:00").toLocaleDateString(i18n.language)}</Row>}
                  {label && <Row label={t("webmail.kanban.label")}><span className="kb-lab" style={labelColor(label) ? { background: labelColor(label)!, color: "#fff" } : undefined}>{label}</span></Row>}
                  {contactName && <Row label={t("crm.linkContact")}>{contactName.name}</Row>}
                </dl>
                {card && (
                  <div className="tk-planning">
                    <div className="tk-plan-sec">
                      <b>{t("webmail.kanban.assignees")}</b>
                      <div className="tk-chips">
                        {assignees.filter((a) => a.card_id === card.id).map((a) => (
                          <span key={a.user_id} className="tk-chip">{a.name}<button title={t("common.remove")} onClick={() => void backend.removeCardAssignee(card.id, a.user_id).then(() => onChanged?.())}>×</button></span>
                        ))}
                        <SearchAddPicker
                          triggerLabel={"+ " + t("webmail.kanban.addAssignee")}
                          searchPlaceholder={t("webmail.kanban.searchPeople")}
                          emptyLabel={t("webmail.kanban.noPeople")}
                          options={(people.data ?? [])
                            .filter((p) => p.id !== card.assignee_id && !assignees.some((a) => a.card_id === card.id && a.user_id === p.id))
                            .map((p) => ({ id: p.id, title: p.name || p.email, subtitle: p.email, hueKey: p.email, avatar: true }))}
                          onPick={(id) => void backend.addCardAssignee(card.id, id).then(() => onChanged?.())}
                        />
                      </div>
                    </div>
                    <div className="tk-plan-sec">
                      <b>{t("webmail.kanban.dependencies")}</b>
                      <div className="tk-chips">
                        {deps.filter((d) => d.card_id === card.id).map((d) => (
                          <span key={d.depends_on_id} className="tk-chip dep">{d.dep_title}<button title={t("common.remove")} onClick={() => void backend.removeCardDep(card.id, d.depends_on_id).then(() => onChanged?.())}>×</button></span>
                        ))}
                        <SearchAddPicker
                          triggerLabel={"+ " + t("webmail.kanban.addDependency")}
                          searchPlaceholder={t("webmail.kanban.searchIssues")}
                          emptyLabel={t("webmail.kanban.noIssues")}
                          options={boardCards
                            .filter((c) => c.id !== card.id && !deps.some((d) => d.card_id === card.id && d.depends_on_id === c.id))
                            .map((c) => ({ id: c.id, title: c.title, subtitle: c.card_key }))}
                          onPick={(id) => void backend.addCardDep(card.id, id).then(() => onChanged?.()).catch((er) => toast((er as Error).message))}
                        />
                      </div>
                    </div>
                  </div>
                )}
                {card && <Checklist cardId={card.id} />}
                {card && <Attachments cardId={card.id} />}
                {card && <CardActivityLog cardId={card.id} columns={columns} locale={i18n.language} />}
              </div>
            )}

            <footer>
              {card && editing && <button className="mc-cancel" onClick={() => del.mutate()}><IcTrash size={14} /> {t("webmail.kanban.delete")}</button>}
              {card && editing && <button className="chipbtn" onClick={() => archive.mutate()}><IcArchive size={14} /> {t("webmail.kanban.archive")}</button>}
              <div className="grow" />
              {editing ? (
                <>
                  <button className="chipbtn" onClick={() => (card ? setEditing(false) : onClose())}>{t("common.cancel")}</button>
                  <button className="add" disabled={!title.trim() || save.isPending} onClick={() => save.mutate()}>{t("contacts.save")}</button>
                </>
              ) : <button className="chipbtn" onClick={onClose}>{t("common.close")}</button>}
            </footer>
          </div>

          {card && (
            <div className="tk-side">
              <TaskComments cardId={card.id} meId={me?.id} />
            </div>
          )}
        </div>
      </div>
      {shareChat && card && <ShareCardChatDrawer boardId={boardId} cardId={card.id} title={card.card_key ? `${card.card_key} — ${card.title}` : card.title} onClose={() => setShareChat(false)} />}
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (<><dt>{label}</dt><dd>{children}</dd></>);
}

// Card attachments: links files from Files (search by name) and downloads/removes. Download is tenant-scoped.
function Attachments({ cardId }: { cardId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const list = useQuery({ queryKey: ["card-attachments", cardId], queryFn: () => cardAttachmentsApi.list(cardId), retry: false });
  const search = useQuery({ queryKey: ["files-search", q], queryFn: () => backend.searchFiles(q), enabled: q.trim().length > 0, retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["card-attachments", cardId] });
  const attach = useMutation({ mutationFn: (fileId: string) => cardAttachmentsApi.attach(cardId, fileId), onSuccess: () => { setQ(""); refresh(); }, onError: (e) => toast((e as Error).message) });
  const detach = useMutation({ mutationFn: (attId: string) => cardAttachmentsApi.detach(cardId, attId), onSuccess: refresh });
  const fmtSize = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);
  const attached = new Set((list.data ?? []).map((a) => a.file_id));
  const hits = (search.data?.files ?? []).filter((f) => !attached.has(f.id)).slice(0, 5);
  return (
    <div className="tk-att">
      <div className="tk-check-h"><span>{t("webmail.kanban.attachments")}</span></div>
      {(list.data ?? []).map((a) => (
        <div className="tk-att-item" key={a.id}>
          <span className="ic"><IcPaperclip size={14} /></span>
          <span className="grow nm">{a.name}</span>
          <span className="sz">{fmtSize(a.size)}</span>
          <button className="ib" title={t("files.download")} onClick={() => triggerDownload(cardAttachmentsApi.downloadUrl(cardId, a.id), a.name)}><IcDownload size={14} /></button>
          <button className="ib" title={t("contacts.remove")} onClick={() => detach.mutate(a.id)}><IcX size={13} /></button>
        </div>
      ))}
      <div className="tk-att-add">
        <input value={q} placeholder={t("webmail.kanban.attachSearch")} onChange={(e) => setQ(e.target.value)} />
        {hits.length > 0 && (
          <div className="tk-att-sugg">
            {hits.map((f) => (
              <button key={f.id} type="button" onMouseDown={(e) => { e.preventDefault(); attach.mutate(f.id); }}>
                <IcPaperclip size={13} /> {f.name}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// Card history/activity: who created/moved/edited it and when. Read-only, most recent first.
function CardActivityLog({ cardId, columns, locale }: { cardId: string; columns: Column[]; locale: string }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["card-activity", cardId], queryFn: () => cardActivityApi.list(cardId), retry: false });
  const items = q.data ?? [];
  if (items.length === 0) return null;
  const colName = (id: string) => columns.find((c) => c.id === id)?.name ?? "";
  const describe = (kind: string, detail: string) => {
    if (kind === "moved") return t("webmail.kanban.actMoved", { column: colName(detail) });
    if (kind === "created") return t("webmail.kanban.actCreated");
    if (kind === "attached") return t("webmail.kanban.actAttached");
    return t("webmail.kanban.actUpdated");
  };
  const when = (iso: string) => new Date(iso).toLocaleString(locale, { dateStyle: "short", timeStyle: "short" });
  return (
    <div className="tk-activity">
      <div className="tk-check-h"><span>{t("webmail.kanban.activity")}</span></div>
      {items.map((a) => (
        <div className="tk-act-item" key={a.id}>
          <span className="tk-act-who">{a.actor_name || "—"}</span> {describe(a.kind, a.detail)}
          <span className="tk-act-when">{when(a.created_at)}</span>
        </div>
      ))}
    </div>
  );
}

// Card checklist/subtasks: checkable items with a "2/5" progress bar.
function Checklist({ cardId }: { cardId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const q = useQuery({ queryKey: ["checklist", cardId], queryFn: () => checklistApi.list(cardId), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["checklist", cardId] });
  const add = useMutation({ mutationFn: () => checklistApi.add(cardId, text.trim()), onSuccess: () => { setText(""); refresh(); }, onError: (e) => toast((e as Error).message) });
  const toggle = useMutation({ mutationFn: (i: { id: string; done: boolean }) => checklistApi.toggle(cardId, i.id, i.done), onSuccess: refresh });
  const remove = useMutation({ mutationFn: (id: string) => checklistApi.remove(cardId, id), onSuccess: refresh });
  const items = q.data ?? [];
  const done = items.filter((i) => i.done).length;
  return (
    <div className="tk-check">
      <div className="tk-check-h">
        <span>{t("webmail.kanban.checklist")}</span>
        {items.length > 0 && <span className="tk-check-count">{done}/{items.length}</span>}
      </div>
      {items.length > 0 && (
        <div className="tk-check-bar"><span style={{ width: `${Math.round((done / items.length) * 100)}%` }} /></div>
      )}
      {items.map((i) => (
        <div className={"tk-check-item" + (i.done ? " done" : "")} key={i.id}>
          <label>
            <input type="checkbox" checked={i.done} onChange={(e) => toggle.mutate({ id: i.id, done: e.target.checked })} />
            <span>{i.text}</span>
          </label>
          <button className="ib" title={t("contacts.remove")} onClick={() => remove.mutate(i.id)}><IcX size={13} /></button>
        </div>
      ))}
      <div className="tk-check-add">
        <input value={text} placeholder={t("webmail.kanban.checklistAdd")} onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && text.trim()) add.mutate(); }} />
        <button className="chipbtn" disabled={!text.trim() || add.isPending} onClick={() => add.mutate()}>{t("webmail.kanban.add")}</button>
      </div>
    </div>
  );
}

// Assignee picker: search + avatar (initials) + name/email, like the one used to add
// chat members. Includes "Assign to me". Picks a PERSON (colleague) so they can be notified.
function AssigneePicker({ value, onChange, meId }: { value: string; onChange: (v: string) => void; meId?: string }) {
  const { t } = useTranslation();
  const people = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const list = people.data ?? [];
  const selected = list.find((p) => p.id === value);
  const term = q.trim().toLowerCase();
  const filtered = (term ? list.filter((p) => (p.name || "").toLowerCase().includes(term) || p.email.toLowerCase().includes(term)) : list).slice(0, 8);
  const pick = (id: string) => { onChange(id); setOpen(false); setQ(""); };

  return (
    <label className="assignee-lbl">{t("webmail.kanban.assignee")}
      <div className="assignee-picker">
        <button type="button" className="assignee-current" onClick={() => setOpen((v) => !v)}>
          {selected
            ? <><span className="av" style={{ background: hueFor(selected.email) }}>{initials(selected.name || selected.email)}</span><span className="ap-nm">{selected.name || selected.email}</span></>
            : <span className="muted">{t("webmail.kanban.noAssignee")}</span>}
          <span className="grow" /><IcChevron size={15} />
        </button>
        {open && (
          <div className="assignee-menu">
            <input className="assignee-search" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("webmail.kanban.searchPeople")} />
            {meId && list.some((p) => p.id === meId) && <button type="button" className="assignee-me" onClick={() => pick(meId)}>{t("webmail.kanban.assignToMe")}</button>}
            <div className="assignee-list">
              {value && <button type="button" className="ap-row" onClick={() => pick("")}><span className="av none">—</span><span className="nm">{t("webmail.kanban.noAssignee")}</span></button>}
              {filtered.map((p) => (
                <button type="button" key={p.id} className={"ap-row" + (p.id === value ? " on" : "")} onClick={() => pick(p.id)}>
                  <span className="av" style={{ background: hueFor(p.email) }}>{initials(p.name || p.email)}</span>
                  <span className="nm">{p.name || p.email}{p.id === meId && <em> ({t("webmail.kanban.you")})</em>}</span>
                  <span className="em">{p.email}</span>
                </button>
              ))}
              {filtered.length === 0 && <div className="empty" style={{ padding: 14 }}>{t("webmail.kanban.noPeople")}</div>}
            </div>
          </div>
        )}
      </div>
    </label>
  );
}

// "+ Add" picker with SEARCH — reuses the AssigneePicker look (input + list with avatar/
// subtitle). Used to add assignees (colleagues) and dependencies (issues), where there can be
// many and the native <select> doesn't scale. Closes on pick.
type AddOption = { id: string; title: string; subtitle?: string; hueKey?: string; avatar?: boolean };
function SearchAddPicker({ triggerLabel, searchPlaceholder, emptyLabel, options, onPick }: {
  triggerLabel: string; searchPlaceholder: string; emptyLabel: string; options: AddOption[]; onPick: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const term = q.trim().toLowerCase();
  const filtered = (term ? options.filter((o) => o.title.toLowerCase().includes(term) || (o.subtitle || "").toLowerCase().includes(term)) : options).slice(0, 20);
  const pick = (id: string) => { onPick(id); setOpen(false); setQ(""); };
  return (
    <div className="assignee-picker tk-addpick">
      <button type="button" className="tk-add-trigger" onClick={() => setOpen((v) => !v)}>
        {triggerLabel}<IcChevron size={14} />
      </button>
      {open && (
        <div className="assignee-menu">
          <input className="assignee-search" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={searchPlaceholder} />
          <div className="assignee-list">
            {filtered.map((o) => (
              <button type="button" key={o.id} className="ap-row" onClick={() => pick(o.id)}>
                {o.avatar && <span className="av" style={{ background: hueFor(o.hueKey || o.title) }}>{initials(o.title)}</span>}
                <span className="nm">{o.title}</span>
                {o.subtitle && <span className="em">{o.subtitle}</span>}
              </button>
            ))}
            {filtered.length === 0 && <div className="empty" style={{ padding: 14 }}>{emptyLabel}</div>}
          </div>
        </div>
      )}
    </div>
  );
}

export { COLORS as TASK_COLORS };

// Card label linked to the tenant's catalog: pick an existing one (with color) or create a new one.
function LabelPicker({ value, onChange, labels }: { value: string; onChange: (v: string) => void; labels: LabelType[] }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [color, setColor] = useState(COLORS[0]);
  const create = useMutation({
    mutationFn: () => backend.createLabel(name.trim(), color),
    onSuccess: (l) => { qc.invalidateQueries({ queryKey: ["labels"] }); onChange(l.name); setCreating(false); setName(""); },
    onError: (e) => toast((e as Error).message),
  });
  const cur = labels.find((l) => l.name === value);
  return (
    <label>{t("webmail.kanban.label")}
      {creating ? (
        <div className="lbl-new">
          <input autoFocus value={name} placeholder={t("webmail.kanban.labelName")} onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && name.trim()) create.mutate(); if (e.key === "Escape") setCreating(false); }} />
          <div className="lbl-colors">
            {COLORS.map((c) => <button type="button" key={c} className={"ev-color" + (color === c ? " on" : "")} style={{ background: c }} onClick={() => setColor(c)} />)}
          </div>
          <button type="button" className="chipbtn" disabled={!name.trim() || create.isPending} onClick={() => create.mutate()}>{t("webmail.kanban.add")}</button>
          <button type="button" className="chipbtn" onClick={() => setCreating(false)}>{t("common.cancel")}</button>
        </div>
      ) : (
        <SearchSelect value={value} onChange={onChange}
          placeholder={t("webmail.kanban.noLabel")} searchPlaceholder={t("webmail.kanban.searchLabel")} emptyLabel={t("webmail.kanban.noLabel")}
          options={[
            // current label outside the catalog (legacy text): stays selectable
            ...(value && !cur ? [{ id: value, label: value }] : []),
            ...labels.map((l) => ({ id: l.name, label: l.name, dot: l.color })),
          ]}
          footer={<button type="button" className="ap-row" onClick={() => setCreating(true)}><IcPlus size={13} /> <span className="nm">{t("webmail.kanban.newLabel")}</span></button>} />
      )}
    </label>
  );
}

// Single-value picker with SEARCH (reuses the AssigneePicker look). For fields that can have
// many options (contacts, labels), where the native <select> doesn't scale. `footer` allows extra
// actions in the list (e.g. "+ new label").
type SelOption = { id: string; label: string; sub?: string; dot?: string };
function SearchSelect({ value, options, onChange, placeholder, searchPlaceholder, emptyLabel, footer }: {
  value: string; options: SelOption[]; onChange: (id: string) => void;
  placeholder: string; searchPlaceholder: string; emptyLabel?: string; footer?: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const cur = options.find((o) => o.id === value);
  const term = q.trim().toLowerCase();
  const filtered = (term ? options.filter((o) => o.label.toLowerCase().includes(term) || (o.sub || "").toLowerCase().includes(term)) : options).slice(0, 30);
  const pick = (id: string) => { onChange(id); setOpen(false); setQ(""); };
  return (
    <div className="assignee-picker">
      <button type="button" className="assignee-current" onClick={() => setOpen((v) => !v)}>
        {cur ? <>{cur.dot && <span className="lbl-dot" style={{ background: cur.dot }} />}<span className="ap-nm">{cur.label}</span></> : <span className="muted">{placeholder}</span>}
        <span className="grow" /><IcChevron size={15} />
      </button>
      {open && (
        <div className="assignee-menu">
          <input className="assignee-search" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={searchPlaceholder} />
          <div className="assignee-list">
            {value && <button type="button" className="ap-row" onClick={() => pick("")}><span className="nm">{placeholder}</span></button>}
            {filtered.map((o) => (
              <button type="button" key={o.id} className={"ap-row" + (o.id === value ? " on" : "")} onClick={() => pick(o.id)}>
                {o.dot && <span className="lbl-dot" style={{ background: o.dot }} />}
                <span className="nm">{o.label}</span>{o.sub && <span className="em">{o.sub}</span>}
              </button>
            ))}
            {filtered.length === 0 && <div className="empty" style={{ padding: 14 }}>{emptyLabel}</div>}
            {footer}
          </div>
        </div>
      )}
    </div>
  );
}

// Links the task to a contact — SEARCH (there can be thousands). The backend revalidates (only visible contacts).
function ContactPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  return (
    <label>{t("crm.linkContact")}
      <SearchSelect value={value} onChange={onChange}
        placeholder={t("crm.noContact")} searchPlaceholder={t("crm.searchContact")} emptyLabel={t("crm.noContact")}
        options={(q.data ?? []).map((c) => ({ id: c.id, label: c.name, sub: c.email ?? undefined }))} />
    </label>
  );
}
