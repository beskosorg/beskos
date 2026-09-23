import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useQuery } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { SCRIPT_NAME } from "../jmap/sieve";
import { compile, parse, parseVacation, emptyVacation, newRule, type Rule, type VacationConfig, type Condition, type Action, type Field, type Op, type ActionKind, type Trigger } from "./rules";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { IcPlus, IcTrash, IcPencil, IcX, IcMail, IcFolder, IcCheck } from "../panel/icons";

const FIELDS: Field[] = ["from", "to", "cc", "subject", "body"];
const OPS: Op[] = ["contains", "is", "starts", "ends", "domain"];
const ACTIONS: ActionKind[] = ["move", "flag", "read", "keyword", "reply", "forward", "delete", "stop"];
// OUTBOUND rules: the webmail can only apply Bcc (forward) and stop, on send. The rest are Sieve
// (delivery). Restricting the editor avoids promising actions that would have no effect when sending.
const OUTBOUND_ACTIONS: ActionKind[] = ["forward", "stop"];

// Email automations. The flow reads top to bottom: WHEN -> IF -> THEN.
//
// INBOUND rules become a Sieve script on Stalwart and run on delivery — they apply with everything
// closed. OUTBOUND ones only the webmail applies, and that's spelled out to whoever creates them.
export function RulesView({ session }: { session: WebmailSession }) {
  const { t } = useTranslation();
  const api = session.service.rules();
  const [rules, setRules] = useState<Rule[] | null>(null);
  const [vacation, setVacation] = useState<VacationConfig | null>(null);
  const [editing, setEditing] = useState<Rule | null>(null);
  const [saving, setSaving] = useState(false);
  const [foreign, setForeign] = useState(false);
  const myDomain = session.email.split("@")[1] ?? "";

  const folders = useQuery({ queryKey: ["mailboxes"], queryFn: () => session.service.mailboxes() });

  // Loads from the script itself (the JSON comes in the comment) — Stalwart is the single source of truth.
  const load = useQuery({
    queryKey: ["sieve"],
    queryFn: async () => {
      const s = (await api.list()).find((x: { name: string }) => x.name === SCRIPT_NAME);
      if (!s?.blobId) return { rules: [] as Rule[], vacation: null as VacationConfig | null };
      const body = await api.download(s.blobId);
      const parsed = parse(body);
      if (parsed === null) { setForeign(true); return { rules: [], vacation: null }; }
      return { rules: parsed, vacation: parseVacation(body) };
    },
    retry: false,
  });
  useEffect(() => {
    if (load.data && rules === null) {
      setRules(load.data.rules);
      setVacation(load.data.vacation ?? { ...emptyVacation(), domain: myDomain });
    }
  }, [load.data, rules, myDomain]);

  // persist ALWAYS recompiles rules + vacation into the same script (single source of truth). Optional overrides.
  const persist = async (nextRules?: Rule[], nextVac?: VacationConfig) => {
    const r = nextRules ?? rules ?? [];
    const v = nextVac ?? vacation ?? undefined;
    if (nextRules) setRules(nextRules);
    if (nextVac) setVacation(nextVac);
    setSaving(true);
    try {
      await api.save(compile(r, v));
      toast(t("rules.saved"));
    } catch (e) {
      toast((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  if (load.isLoading || rules === null) return <div className="loading">…</div>;

  return (
    <section className="cts-main rules">
      <div className="cts-head">
        <h2>{t("rules.title")}</h2>
        {saving && <Spinner size={14} />}
        <div className="grow" />
        {/* No "x": this is a mail page, and you leave it by picking a folder. A close
            button said "modal" and that was a lie. */}
        <button className="chipbtn" onClick={() => setEditing(newRule())}><IcPlus size={15} /> {t("rules.new")}</button>
      </div>

      {/* Body with the same metrics as the other pages (padding + max width). Without this, the
          header came out indented and the content glued to the edges, and on a wide screen the
          vacation panel's fields stretched edge to edge. */}
      <div className="rules-body">
        <p className="rule-hint">{t("rules.explain")}</p>
        {foreign && <p className="rule-hint warn">{t("rules.foreign")}</p>}

        {vacation && <VacationPanel value={vacation} domain={myDomain} saving={saving}
          onSave={(v) => void persist(undefined, { ...v, domain: myDomain })} />}

        {rules.length === 0 ? <div className="empty">{t("rules.empty")}</div> : (
          <div className="rules-list">
            {rules.map((r) => (
              <RuleCard key={r.id} r={r} folders={(folders.data ?? []).map((f) => f.name)}
                onEdit={() => setEditing(r)}
                onToggle={() => persist(rules.map((x) => (x.id === r.id ? { ...x, enabled: !x.enabled } : x)))}
                onDelete={() => persist(rules.filter((x) => x.id !== r.id))} />
            ))}
          </div>
        )}
      </div>

      {editing && (
        <RuleEditor rule={editing} folders={(folders.data ?? []).map((f) => f.name)}
          onClose={() => setEditing(null)}
          onSave={(r) => {
            const exists = rules.some((x) => x.id === r.id);
            void persist(exists ? rules.map((x) => (x.id === r.id ? r : x)) : [...rules, r]);
            setEditing(null);
          }} />
      )}
    </section>
  );
}

// Vacation reply panel: on/off, date window, scope (all/internal), subject and
// message. Saves into the same Sieve script (via onSave -> persist). Local state until "Save".
function VacationPanel({ value, domain, saving, onSave }: {
  value: VacationConfig; domain: string; saving: boolean; onSave: (v: VacationConfig) => void;
}) {
  const { t } = useTranslation();
  const [v, setV] = useState<VacationConfig>(value);
  useEffect(() => { setV(value); }, [value]);
  const set = <K extends keyof VacationConfig>(k: K, val: VacationConfig[K]) => setV((p) => ({ ...p, [k]: val }));
  const dirty = JSON.stringify({ ...v, domain }) !== JSON.stringify({ ...value, domain });
  return (
    <div className="vac-panel">
      <div className="vac-head">
        <div>
          <b>{t("vacation.title")}</b>
          <p className="rule-hint" style={{ margin: "2px 0 0" }}>{t("vacation.explain")}</p>
        </div>
        <label className="switch">
          <input type="checkbox" checked={v.enabled} onChange={(e) => set("enabled", e.target.checked)} />
          <span className="track" />
        </label>
      </div>
      {v.enabled && (
        <div className="vac-body">
          <div className="row3">
            <label>{t("vacation.start")}<input type="date" value={v.start ?? ""} onChange={(e) => set("start", e.target.value || undefined)} /></label>
            <label>{t("vacation.end")}<input type="date" value={v.end ?? ""} onChange={(e) => set("end", e.target.value || undefined)} /></label>
            <label>{t("vacation.scope")}
              <select value={v.scope} onChange={(e) => set("scope", e.target.value as "all" | "internal")}>
                <option value="all">{t("vacation.scopeAll")}</option>
                <option value="internal">{t("vacation.scopeInternal", { domain })}</option>
              </select>
            </label>
          </div>
          <label>{t("vacation.subject")}<input value={v.subject} placeholder={t("vacation.subjectPh")} onChange={(e) => set("subject", e.target.value)} /></label>
          <label>{t("vacation.message")}<textarea rows={4} value={v.message} placeholder={t("vacation.messagePh")} onChange={(e) => set("message", e.target.value)} /></label>
        </div>
      )}
      <div className="vac-actions">
        <button className="add" disabled={saving || (!dirty) || (v.enabled && !v.message.trim())} onClick={() => onSave(v)}>{t("contacts.save")}</button>
      </div>
    </div>
  );
}

// The card uses the SAME nodes as the editor, read-only: whoever creates the rule and whoever
// rereads it later sees the same design. Two visual languages for the same thing would make it look like two things.
function RuleCard({ r, folders, onEdit, onToggle, onDelete }: {
  r: Rule; folders: string[]; onEdit: () => void; onToggle: () => void; onDelete: () => void;
}) {
  const { t } = useTranslation();
  const conds = r.conditions.filter((c) => c.value.trim());
  const badFolder = folders.length > 0 && r.actions.some((a) => a.kind === "move" && a.value && !folders.includes(a.value));

  return (
    <div className={"rule-card" + (r.enabled ? "" : " off")}>
      <div className="rule-top">
        <b>{r.name || t("rules.unnamed")}</b>
        <span className={"pnl-pill" + (r.trigger === "send" ? "" : " muted")}>{t(`rules.trigger_${r.trigger}`)}</span>
        <div className="grow" />
        <label className="switch" title={t(r.enabled ? "rules.on" : "rules.off")}>
          <input type="checkbox" checked={r.enabled} onChange={onToggle} /><span className="track" />
        </label>
        <button className="ib" title={t("contacts.edit")} onClick={onEdit}><IcPencil size={14} /></button>
        <button className="ib" title={t("contacts.delete")} onClick={onDelete}><IcTrash size={14} /></button>
      </div>

      <div className="canvas read" onClick={onEdit}>
        <Node kind="when" icon={<IcMail size={14} />} title={t("rules.whenTitle")}>
          <span className="node-text">{t(`rules.when_${r.trigger}`)}</span>
        </Node>
        <Link />
        <Node kind="if" icon={<IcCheck size={14} />} title={t("rules.ifTitle")}>
          {conds.length === 0 ? <span className="node-text dim">{t("rules.always")}</span> : conds.map((c, i) => (
            <div className="node-text" key={i}>
              {i > 0 && <span className="joiner">{t(r.match === "any" ? "rules.or" : "rules.and")}</span>}
              {t(`rules.f_${c.field}`)} <em>{t(`rules.op_${c.op}`)}</em> <b>“{c.value}”</b>
            </div>
          ))}
        </Node>
        <Link />
        <Node kind="then" icon={<IcFolder size={14} />} title={t("rules.thenTitle")} last>
          {r.actions.map((a, i) => <div className="node-text" key={i}>{describeAction(a, t)}</div>)}
        </Node>
      </div>

      {r.trigger === "send" && <p className="rule-warn">{t("rules.sendWarn")}</p>}
      {/* Folder deleted after the rule was created: Sieve would silently create a new one. */}
      {badFolder && <p className="rule-warn">{t("rules.folderGone")}</p>}
    </div>
  );
}

function describeAction(a: Action, t: TFunction): string {
  switch (a.kind) {
    case "move": return t("rules.a_move", { folder: a.value || "—" });
    case "forward": return t("rules.a_forward", { to: a.value || "—" });
    case "keyword": return t("rules.a_keyword", { tag: a.value || "—" });
    case "reply": return t("rules.a_reply");
    default: return t(`rules.a_${a.kind}`);
  }
}

// FLOW editor: a vertical spine with connected nodes. Each step has its own color and icon —
// trigger, condition, and action are different things and should look different. The "+" lives on the
// line, between the nodes, because that's where the person wants to add something.
function RuleEditor({ rule, folders, onClose, onSave }: {
  rule: Rule; folders: string[]; onClose: () => void; onSave: (r: Rule) => void;
}) {
  const { t } = useTranslation();
  const [r, setR] = useState<Rule>({ ...rule });
  const set = <K extends keyof Rule>(k: K, v: Rule[K]) => setR((p) => ({ ...p, [k]: v }));
  const setCond = (i: number, c: Partial<Condition>) =>
    set("conditions", r.conditions.map((x, j) => (j === i ? { ...x, ...c } : x)));
  const setAct = (i: number, a: Partial<Action>) =>
    set("actions", r.actions.map((x, j) => (j === i ? { ...x, ...a } : x)));
  // When switching triggers, coerces the actions to the mode's valid ones — avoids leaving a delivery
  // action (e.g. move) in an outbound rule, which would be ignored on send.
  const setTrigger = (tg: Trigger) => setR((p) => ({
    ...p, trigger: tg,
    actions: tg === "send"
      ? p.actions.map((a) => (OUTBOUND_ACTIONS.includes(a.kind) ? a : { kind: "forward", value: "" }))
      : p.actions,
  }));

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk rule-modal" onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span>{t("rules.editor")}</span>
          <div className="grow" />
          <button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
        </header>

        <div className="body rule-body">
          <input className="rule-name" autoFocus value={r.name} placeholder={t("rules.namePh")}
            onChange={(e) => set("name", e.target.value)} />

          <div className="canvas">
            {/* 1 — WHEN */}
            <Node kind="when" icon={<IcMail size={14} />} title={t("rules.whenTitle")}>
              <div className="node-chips">
                {(["receive", "send"] as const).map((tg) => (
                  <button key={tg} type="button" className={"nchip" + (r.trigger === tg ? " on" : "")}
                    onClick={() => setTrigger(tg)}>{t(`rules.when_${tg}`)}</button>
                ))}
              </div>
              {r.trigger === "send" && <p className="rule-warn">{t("rules.sendWarn")}</p>}
            </Node>

            <Link />

            {/* 2 — IF */}
            <Node kind="if" icon={<IcCheck size={14} />} title={t("rules.ifTitle")}
              aside={
                <div className="node-chips sm">
                  {(["all", "any"] as const).map((m) => (
                    <button key={m} type="button" className={"nchip" + (r.match === m ? " on" : "")}
                      onClick={() => set("match", m)}>{t(`rules.match_${m}`)}</button>
                  ))}
                </div>
              }>
              {r.conditions.map((c, i) => (
                <div className="node-row" key={i}>
                  {i > 0 && <span className="joiner">{t(r.match === "any" ? "rules.or" : "rules.and")}</span>}
                  <select value={c.field} onChange={(e) => setCond(i, { field: e.target.value as Field })}>
                    {FIELDS.map((f) => <option key={f} value={f}>{t(`rules.f_${f}`)}</option>)}
                  </select>
                  <select value={c.op} disabled={c.field === "body"} onChange={(e) => setCond(i, { op: e.target.value as Op })}>
                    {OPS.filter((o) => o !== "domain" || c.field === "from" || c.field === "to" || c.field === "cc")
                      .map((o) => <option key={o} value={o}>{t(`rules.op_${o}`)}</option>)}
                  </select>
                  <input value={c.value} placeholder={c.op === "domain" ? "acme.com.br" : t("rules.valuePh")}
                    onChange={(e) => setCond(i, { value: e.target.value })} />
                  {r.conditions.length > 1 && (
                    <button className="ib" title={t("contacts.delete")}
                      onClick={() => set("conditions", r.conditions.filter((_, j) => j !== i))}><IcTrash size={13} /></button>
                  )}
                </div>
              ))}
              {r.conditions.every((c) => !c.value.trim()) && <p className="node-note">{t("rules.always")}</p>}
              <button className="node-add" onClick={() => set("conditions", [...r.conditions, { field: "subject", op: "contains", value: "" }])}>
                <IcPlus size={12} /> {t("rules.addCondition")}
              </button>
            </Node>

            <Link />

            {/* 3 — THEN */}
            <Node kind="then" icon={<IcFolder size={14} />} title={t("rules.thenTitle")} last>
              {r.actions.map((a, i) => (
                <div className="node-row" key={i}>
                  <select value={a.kind} onChange={(e) => setAct(i, { kind: e.target.value as ActionKind, value: "" })}>
                    {/* Outbound only offers what the webmail actually applies on send (Bcc/stop); the rest is Sieve, inbound only. */}
                    {(r.trigger === "send" ? OUTBOUND_ACTIONS : ACTIONS).map((k) => <option key={k} value={k}>{t(`rules.act_${k}`)}</option>)}
                  </select>
                  {a.kind === "move" && (
                    <select value={a.value ?? ""} onChange={(e) => setAct(i, { value: e.target.value })}>
                      <option value="">{t("rules.pickFolder")}</option>
                      {folders.map((f) => <option key={f} value={f}>{f}</option>)}
                    </select>
                  )}
                  {(a.kind === "forward" || a.kind === "keyword") && (
                    <input value={a.value ?? ""} placeholder={a.kind === "forward" ? "alguem@empresa.com" : t("rules.tagPh")}
                      onChange={(e) => setAct(i, { value: e.target.value })} />
                  )}
                  {a.kind === "reply" && (
                    <input value={a.value ?? ""} placeholder={t("rules.replyPh")} onChange={(e) => setAct(i, { value: e.target.value })} />
                  )}
                  {r.actions.length > 1 && (
                    <button className="ib" title={t("contacts.delete")}
                      onClick={() => set("actions", r.actions.filter((_, j) => j !== i))}><IcTrash size={13} /></button>
                  )}
                </div>
              ))}
              {r.actions.some((a) => a.kind === "reply") && <p className="node-note">{t("rules.replyHint")}</p>}
              <button className="node-add" onClick={() => set("actions", [...r.actions, { kind: r.trigger === "send" ? "forward" : "flag" }])}>
                <IcPlus size={12} /> {t("rules.addAction")}
              </button>
            </Node>
          </div>
        </div>

        <footer>
          <div className="grow" />
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!r.name.trim()} onClick={() => onSave(r)}>{t("contacts.save")}</button>
        </footer>
      </div>
    </div>
  );
}

// Node: a step in the flow. The colored dot sits ON the spine line — it's what makes the cards
// look connected instead of stacked.
function Node({ kind, icon, title, aside, children, last }: {
  kind: "when" | "if" | "then"; icon: React.ReactNode; title: string;
  aside?: React.ReactNode; children: React.ReactNode; last?: boolean;
}) {
  return (
    <div className={"node " + kind + (last ? " last" : "")}>
      <span className="node-dot">{icon}</span>
      <div className="node-card">
        <div className="node-head"><b>{title}</b><div className="grow" />{aside}</div>
        {children}
      </div>
    </div>
  );
}

// Link: just the line segment between two nodes. No "+": the flow is fixed (when -> if -> then), there's
// no step to insert here — adding a condition/action happens inside the node that holds them.
function Link() {
  return <div className="node-link"><span className="link-arrow">↓</span></div>;
}
