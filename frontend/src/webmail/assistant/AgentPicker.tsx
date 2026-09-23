import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useQuery } from "@tanstack/react-query";
import { assistantApi } from "./api";

// Agent picker. Pinning one holds for the rest of the thread; "automatic" hands the decision
// back to routing (§3.2).
//
// The choice is deliberately visible AND reversible: silent routing would leave the person
// unable to tell why the assistant sometimes manages something and sometimes doesn't.
export function AgentPicker({ pinned, onPin }: { pinned: string; onPin: (key: string) => void }) {
  const { t } = useTranslation();
  const agents = useQuery({
    queryKey: ["assistant-agents"],
    queryFn: () => assistantApi.agents(),
    staleTime: 5 * 60 * 1000, // built-in agents are part of the product: they don't change during a session
    retry: false,
  });

  return (
    <select
      className="as-agent"
      value={pinned}
      title={t("assistant.agent.pick")}
      onChange={(e) => onPin(e.target.value)}
    >
      <option value="">{t("assistant.agent.auto")}</option>
      {agents.data?.items.filter((a) => a.enabled).map((a) => (
        <option key={a.key} value={a.key}>{agentLabel(a.key, a.name, t)}</option>
      ))}
    </select>
  );
}

// AgentBadge shows who answered this message. Only appears when the agent isn't the general one:
// a label on every response would turn into noise and go unnoticed when it actually changes.
export function AgentBadge({ agent }: { agent?: string }) {
  const { t } = useTranslation();
  const agents = useQuery({
    queryKey: ["assistant-agents"],
    queryFn: () => assistantApi.agents(),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  if (!agent || agent === "assistant") return null;
  const name = agents.data?.items.find((a) => a.key === agent)?.name;
  return <span className="as-agent-badge">{agentLabel(agent, name, t)}</span>;
}

// agentLabel: the customer's own name when it exists, otherwise the key's translation. A built-in
// agent never carries a name from the server — visible text comes from the frontend (law 4.1).
function agentLabel(key: string, name: string | undefined, t: TFunction): string {
  return name || t(`assistant.agent.${key}`, { defaultValue: key });
}
