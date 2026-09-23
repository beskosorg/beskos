import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { assistantApi, type AgentDraft, type AgentProfile } from "../webmail/assistant/api";
import { toast } from "../panel/ui";
import { Drawer } from "../webmail/Drawer";
import { IcPlus } from "../panel/icons";

// Assistant agents (RBAC org.settings). Two lists with different rules, and the difference is
// deliberate (§3.2 of the assistant's design):
//
//   - BUILT-IN ones are part of the product: the admin turns them on and off, doesn't edit them.
//     If they could change their instructions, the product would end up looking bad without support
//     being able to tell a bug of ours from a local edit — and an agent's tool set is its risk
//     surface;
//   - CUSTOMER ones are theirs: create, change and delete. Never with more tools than the cap, and
//     that's validated on the server — hiding a checkbox here would be no defense at all.
//
// Pro only: mounted by ../panel/AssistantAgentsSectionGate.tsx behind the Agents capability. The
// backend enforces the same boundary independently (ee/agents_pro.go) -- this gate is UX, not the
// security line. Reading the agent list stays Community (chat needs it regardless of edition).
export function AssistantAgentsSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [editing, setEditing] = useState<AgentProfile | "new" | null>(null);

  const q = useQuery({ queryKey: ["assistant-agents"], queryFn: () => assistantApi.agents(), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["assistant-agents"] });

  const toggle = useMutation({
    mutationFn: ({ key, enabled }: { key: string; enabled: boolean }) => assistantApi.agentEnable(key, enabled),
    // Also invalidates ["me"]: turning off the general agent hides the assistant bubble across the
    // whole app, and that's read from /api/me — without this it would only change after a reload.
    onSuccess: () => { refresh(); qc.invalidateQueries({ queryKey: ["me"] }); },
    onError: (e: Error) => toast(e.message),
  });
  const remove = useMutation({
    mutationFn: (key: string) => assistantApi.agentDelete(key),
    onSuccess: refresh,
    onError: (e: Error) => toast(e.message),
  });

  const builtins = q.data?.items.filter((a) => a.builtin) ?? [];
  const custom = q.data?.items.filter((a) => !a.builtin) ?? [];

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("agents.title")}</h1><div className="desc">{t("agents.intro")}</div></div>
        <div className="grow" />
        <button className="pnl-btn primary" onClick={() => setEditing("new")}>
          <IcPlus size={16} />{t("agents.create")}
        </button>
      </div>

      <div className="pnl-content">
        {q.isError && <p className="desc">{(q.error as Error).message}</p>}

        <div className="pnl-card">
          <div className="pnl-card-h">{t("agents.builtin")}<div className="desc">{t("agents.builtinHint")}</div></div>
          <div className="pnl-card-b">
            {builtins.map((a) => (
              <div className="pnl-row" key={a.key}>
                <div>
                  <b>{t(`assistant.agent.${a.key}`, { defaultValue: a.key })}</b>
                  <div className="sub">{a.tools.map((x) => t(`assistant.tools.${x}`, { defaultValue: x })).join(" · ")}</div>
                </div>
                <div className="grow" />
                {a.key === "assistant" && (
                  // The general agent IS the assistant: turning it off turns off the AI for the
                  // whole organization. The warning makes the consequence explicit — the switch
                  // itself looks the same as the others.
                  <span className="desc agent-master-hint">{t("agents.masterHint")}</span>
                )}
                <label className="switch" title={a.enabled ? t("agents.on") : t("agents.off")}>
                  <input type="checkbox" checked={a.enabled} disabled={toggle.isPending}
                    onChange={() => toggle.mutate({ key: a.key, enabled: !a.enabled })} />
                  <span className="track" />
                </label>
              </div>
            ))}
          </div>
        </div>

        <ToolsCard />

        <div className="pnl-card">
          <div className="pnl-card-h">{t("agents.custom")}</div>
          <div className="pnl-card-b">
            {custom.length === 0 ? <div className="pnl-empty">{t("agents.noneYet")}</div> : custom.map((a) => (
              <div className="pnl-row" key={a.key}>
                <div>
                  <b>{a.name}</b>
                  <div className="sub">{a.purpose || a.tools.join(" · ")}</div>
                </div>
                <div className="grow" />
                <button className="pnl-btn" onClick={() => setEditing(a)}>{t("agents.edit")}</button>
                <button className="pnl-btn ghost" onClick={() => remove.mutate(a.key)}>{t("agents.delete")}</button>
              </div>
            ))}
          </div>
        </div>
      </div>

      {editing && (
        <AgentEditor
          agent={editing === "new" ? null : editing}
          builtins={builtins}
          maxTools={q.data?.max_tools ?? []}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); refresh(); }}
        />
      )}
    </>
  );
}

// AgentEditor creates or changes a customer profile.
function AgentEditor({
  agent, builtins, maxTools, onClose, onSaved,
}: {
  agent: AgentProfile | null;
  builtins: AgentProfile[];
  maxTools: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(agent?.name ?? "");
  const [purpose, setPurpose] = useState(agent?.purpose ?? "");
  const [instructions, setInstructions] = useState(agent?.instructions ?? "");
  const [tools, setTools] = useState<string[]>(agent?.tools ?? []);
  const [duplicateOf, setDuplicateOf] = useState("");

  const save = useMutation({
    mutationFn: () => {
      const draft: AgentDraft = { name, purpose, instructions, tools };
      if (agent) return assistantApi.agentUpdate(agent.key, draft);
      return assistantApi.agentCreate(duplicateOf ? { ...draft, duplicate_of: duplicateOf } : draft);
    },
    onSuccess: onSaved,
    onError: (e: Error) => toast(e.message),
  });

  // Duplicating brings the source agent's tools as a STARTING POINT. The copy is independent from
  // then on — it doesn't receive our improvements — and that's what the warning says.
  const applyDuplicate = (key: string) => {
    setDuplicateOf(key);
    const src = builtins.find((b) => b.key === key);
    setTools(src ? src.tools : []);
  };

  const toggleTool = (tool: string) =>
    setTools((prev) => (prev.includes(tool) ? prev.filter((x) => x !== tool) : [...prev, tool]));

  return (
    <Drawer
      title={agent ? t("agents.edit") : t("agents.create")}
      onClose={onClose}
      footer={
        <>
          <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
          <div className="grow" />
          <button className="pnl-btn primary" disabled={save.isPending || !name.trim()} onClick={() => save.mutate()}>
            {t("agents.save")}
          </button>
        </>
      }
    >
      {!agent && (
        <div className="pnl-field">
          <label>{t("agents.duplicateOf")}</label>
          <select className="pnl-select" value={duplicateOf} onChange={(e) => applyDuplicate(e.target.value)}>
            <option value="">{t("agents.fromScratch")}</option>
            {builtins.map((b) => (
              <option key={b.key} value={b.key}>{t(`assistant.agent.${b.key}`, { defaultValue: b.key })}</option>
            ))}
          </select>
          {duplicateOf && <p className="desc">{t("agents.duplicateWarning")}</p>}
        </div>
      )}

      <div className="pnl-field">
        <label>{t("agents.name")}</label>
        <input className="pnl-input" autoFocus value={name} maxLength={60} onChange={(e) => setName(e.target.value)} />
      </div>

      <div className="pnl-field">
        <label>{t("agents.purpose")}</label>
        <input className="pnl-input" value={purpose} maxLength={200}
          placeholder={t("agents.purposePlaceholder")} onChange={(e) => setPurpose(e.target.value)} />
        <p className="desc">{t("agents.purposeHint")}</p>
      </div>

      <div className="pnl-field">
        <label>{t("agents.instructions")}</label>
        <textarea className="pnl-input" rows={5} value={instructions} maxLength={4000}
          onChange={(e) => setInstructions(e.target.value)} />
      </div>

      <div className="pnl-field">
        <label>{t("agents.tools")}</label>
        <div className="agent-tools">
          {maxTools.map((tool) => (
            <label key={tool} className="agent-tool">
              <span className="switch"><input type="checkbox" checked={tools.includes(tool)} onChange={() => toggleTool(tool)} /><span className="track" /></span>
              <span>{t(`assistant.tools.${tool}`, { defaultValue: tool })}</span>
            </label>
          ))}
        </div>
        <p className="desc">{t("agents.toolsHint")}</p>
      </div>
    </Drawer>
  );
}

// Assistant tools, one by one. This is the FINE cut on top of turning a whole agent on/off:
// turning off "send email" here removes the capability from ALL agents, built-in or customer's,
// and it also applies to background tasks. It only restricts — never gives an agent something it
// didn't already have.
function ToolsCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["assistant-tools"], queryFn: () => assistantApi.tools(), retry: false });
  const set = useMutation({
    mutationFn: ({ tool, enabled }: { tool: string; enabled: boolean }) => assistantApi.toolEnable(tool, enabled),
    // The agent list shows each one's tools: it has to reflect the cut immediately.
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["assistant-tools"] }); qc.invalidateQueries({ queryKey: ["assistant-agents"] }); },
    onError: (e: Error) => toast(e.message),
  });
  const off = new Set(q.data?.disabled ?? []);
  return (
    <div className="pnl-card">
      <div className="pnl-card-h">{t("agents.toolsTitle")}<div className="desc">{t("agents.toolsHint")}</div></div>
      <div className="pnl-card-b">
        {(q.data?.tools ?? []).map((tool) => (
          <div className="pnl-row" key={tool}>
            <div>
              <b>{t(`assistant.tools.${tool}`, { defaultValue: tool })}</b>
              <div className="sub mono">{tool}</div>
            </div>
            <div className="grow" />
            <label className="switch" title={off.has(tool) ? t("agents.off") : t("agents.on")}>
              <input type="checkbox" checked={!off.has(tool)} disabled={set.isPending}
                onChange={() => set.mutate({ tool, enabled: off.has(tool) })} />
              <span className="track" />
            </label>
          </div>
        ))}
      </div>
    </div>
  );
}
