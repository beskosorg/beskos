import { useTranslation } from "react-i18next";
import { IcStar } from "../panel/icons";

// Shown in place of a Pro-only panel section when the instance has no license granting it (see
// internal/edition on the backend). Every Pro feature's screen still renders its own pnl-top
// (law 3.6 -- a screen dresses like the others even when it can't do anything yet); only the body
// swaps for this card.
export function ProFeatureCard({ feature }: { feature: string }) {
  const { t } = useTranslation();
  return (
    <div className="pnl-card">
      <div className="pnl-card-h"><IcStar size={16} /><h3>{t("pro.upsellTitle")}</h3></div>
      <div className="pnl-card-b">
        <p className="desc">{t("pro.upsellBody", { feature })}</p>
        <a className="pnl-btn primary" href="https://beskos.com/pro" target="_blank" rel="noreferrer">
          {t("pro.upsellCta")}
        </a>
      </div>
    </div>
  );
}
