import { hostedBilling } from "../edition";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { api, type Me } from "../api";
import { IcUsers, IcGlobe, IcCard, IcFolder, IcMail } from "./icons";

export function OverviewSection({ me, onNavigate }: { me: Me | undefined; onNavigate: (s: string) => void }) {
  const { t } = useTranslation();
  const usage = useQuery({ queryKey: ["usage"], queryFn: api.usage, retry: false });
  const domains = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const gb = (n: number) => `${(n / (1 << 30)).toFixed(1)} GB`;

  return (
    <>
      <div className="pnl-top"><div><h1>{t("panel.overview")}</h1><div className="desc">{t("panel.overviewDesc")}</div></div></div>
      <div className="pnl-content">
        <div className="pnl-stat-grid">
          <Stat icon={<IcUsers size={16} />} label={t("panel.users")} value={usage.data ? String(usage.data.usage.mailboxes) : "—"}
            sub={usage.data && usage.data.limits.max_mailboxes > 0 ? `${t("billing.of")} ${usage.data.limits.max_mailboxes}` : ""} onClick={() => onNavigate("users")} />
          <Stat icon={<IcGlobe size={16} />} label={t("panel.domains")} value={domains.data ? String(domains.data.length) : "—"} onClick={() => onNavigate("domains")} />
          <Stat icon={<IcFolder size={16} />} label={t("billing.storage")} value={usage.data ? gb(usage.data.usage.storage_bytes) : "—"}
            sub={usage.data && usage.data.limits.max_storage_bytes > 0 ? `${t("billing.of")} ${gb(usage.data.limits.max_storage_bytes)}` : ""} />
          <Stat icon={<IcMail size={16} />} label={t("billing.sendsToday")} value={usage.data ? String(usage.data.usage.mail_sent_today) : "—"}
            sub={usage.data && usage.data.limits.max_daily_send > 0 ? `${t("billing.of")} ${usage.data.limits.max_daily_send}` : ""} />
          <Stat icon={<IcCard size={16} />} label={t("billing.currentPlan")} value={usage.data?.plan.name ?? "—"} onClick={hostedBilling ? () => onNavigate("billing") : undefined} />
        </div>
        <div className="pnl-card">
          <div className="pnl-card-h"><h3>{t("panel.role")}: {t(`members.roles.${me?.role}`, { defaultValue: me?.role })}</h3></div>
          <div className="pnl-card-b"><p className="desc" style={{ margin: "4px 0" }}>{t("panel.overviewHint")}</p></div>
        </div>
      </div>
    </>
  );
}

function Stat({ icon, label, value, sub, onClick }: { icon: JSX.Element; label: string; value: string; sub?: string; onClick?: () => void }) {
  return (
    <div className="pnl-stat" style={onClick ? { cursor: "pointer" } : undefined} onClick={onClick}>
      <div className="l">{icon}{label}</div>
      <div className="v">{value}</div>
      {sub && <div className="d">{sub}</div>}
    </div>
  );
}
