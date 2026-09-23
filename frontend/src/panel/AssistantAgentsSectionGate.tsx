import { useTranslation } from "react-i18next";
import { useEdition } from "../useEdition";
import { AssistantAgentsSection } from "../ee/AssistantAgentsSection";
import { ProFeatureCard } from "../ee/ProFeatureCard";

// Mounts the real section only when the instance has the Pro Agents capability; otherwise shows
// the upsell card in the same screen shell (law 3.6). AssistantAgentsSection renders its own
// pnl-top/pnl-content when it mounts, so this gate only supplies that shell for the other states.
export function AssistantAgentsSectionGate() {
  const { t } = useTranslation();
  const { data, isLoading } = useEdition();
  if (!isLoading && data?.agents) return <AssistantAgentsSection />;
  return (
    <>
      <div className="pnl-top"><div><h1>{t("agents.title")}</h1><div className="desc">{t("agents.intro")}</div></div></div>
      <div className="pnl-content">
        {isLoading ? <div className="loading">…</div> : <ProFeatureCard feature={t("agents.title")} />}
      </div>
    </>
  );
}
