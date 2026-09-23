import { useTranslation } from "react-i18next";
import { useEdition } from "../useEdition";
import { BrandingSection } from "../ee/BrandingSection";
import { ProFeatureCard } from "../ee/ProFeatureCard";

// Mounts the real editor only when the instance has the Pro branding capability; otherwise shows
// the upsell card in the same screen shell (law 3.6). BrandingSection renders its own pnl-top/
// pnl-content when it mounts, so this gate only supplies that shell for the other two states.
export function BrandingSectionGate() {
  const { t } = useTranslation();
  const { data, isLoading } = useEdition();
  if (!isLoading && data?.branding) return <BrandingSection />;
  return (
    <>
      <div className="pnl-top"><div><h1>{t("panel.branding")}</h1><div className="desc">{t("branding.subtitle")}</div></div></div>
      <div className="pnl-content">
        {isLoading ? <div className="loading">…</div> : <ProFeatureCard feature={t("panel.branding")} />}
      </div>
    </>
  );
}
