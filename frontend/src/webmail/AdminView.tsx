import "../panel/panel.css";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { api, hasPerm, type Me } from "../api";
import { OverviewSection } from "../panel/OverviewSection";
import { DomainsSection } from "../panel/DomainsSection";
import { UsersSection } from "../panel/UsersSection";
import { OrgSecuritySection } from "../panel/OrgSecuritySection";
import { hostedBilling, hostedPages } from "../edition";
import { GroupsSection } from "../panel/GroupsSection";
import { BrandingSectionGate } from "../panel/BrandingSectionGate";
import { AppsSection } from "../panel/AppsSection";
import { AssistantAgentsSectionGate } from "../panel/AssistantAgentsSectionGate";
import { IcHome, IcGlobe, IcUsers, IcShield, IcCard, IcColumns, IcStar, IcSparkle } from "../panel/icons";

type Section = "overview" | "domains" | "users" | "groups" | "billing" | "branding" | "apps" | "agents" | "security";

// Administration inside the webmail (/app no longer exists). Internal menu in the same pattern as
// Mail: section rail + content. RBAC decides what shows up — and the API refuses either
// way, so hiding the item isn't the only defense.
const SECTIONS: Section[] = ["overview", "domains", "users", "groups", "billing", "branding", "apps", "agents", "security"];

// The active section comes from the URL (/mail/admin/<section>) — navigable, reloadable link.
export function AdminView({ section: param, detail, onSection, onDetail }: {
  section?: string; detail?: string; onSection: (s: string) => void; onDetail: (s: string, id: string) => void;
}) {
  const { t } = useTranslation();
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false });
  // iOS-style on mobile: no section in the URL = list; with a section = detail + back. `sel` null
  // means "on the list". On desktop the side rail always shows everything, defaulting to Overview.
  const sel: Section | null = SECTIONS.includes(param as Section) ? (param as Section) : null;

  if (me.isLoading) return <div className="loading">…</div>;
  if (me.isError || !me.data) return <div className="empty">{(me.error as Error)?.message ?? "—"}</div>;
  const d: Me = me.data;

  const items: { id: Section; label: string; icon: JSX.Element; show: boolean }[] = [
    { id: "overview", label: t("panel.overview"), icon: <IcHome size={17} />, show: true },
    { id: "domains", label: t("panel.domains"), icon: <IcGlobe size={17} />, show: hasPerm(d, "domains.view") },
    { id: "users", label: t("panel.users"), icon: <IcUsers size={17} />, show: hasPerm(d, "members.view") || hasPerm(d, "domains.view") },
    { id: "groups", label: t("panel.groups"), icon: <IcColumns size={17} />, show: hasPerm(d, "members.view") || hasPerm(d, "groups.manage") },
    { id: "billing", label: t("panel.billing"), icon: <IcCard size={17} />, show: hostedBilling && (hasPerm(d, "billing.view") || hasPerm(d, "usage.view")) },
    { id: "branding", label: t("panel.branding"), icon: <IcStar size={17} />, show: hasPerm(d, "org.branding") },
    { id: "apps", label: t("panel.apps"), icon: <IcColumns size={17} />, show: hasPerm(d, "org.branding") },
    { id: "agents", label: t("agents.title"), icon: <IcSparkle size={17} />, show: hasPerm(d, "assistant.manage") },
    { id: "security", label: t("panel.security"), icon: <IcShield size={17} />, show: true },
  ];
  const visible = items.filter((i) => i.show);
  const current: Section | null = sel && visible.some((i) => i.id === sel) ? sel : null;
  const shown: Section = current ?? "overview";

  return (
    <div className={"adm" + (current ? " has-section" : "")}>
      <aside className="adm-rail">
        <div className="adm-title">{t("panel.adminConsole")}</div>
        {visible.map((i) => (
          <button key={i.id} className={shown === i.id ? "on" : ""} onClick={() => onSection(i.id)}>
            {i.icon} <span>{i.label}</span>
          </button>
        ))}
      </aside>
      <section className="adm-main">
          {/* Back to the list (mobile only; hidden by CSS on desktop). */}
          <button className="adm-back" onClick={() => onSection("")}>‹ {t("panel.adminConsole")}</button>
          {shown === "overview" && <OverviewSection me={d} onNavigate={(s) => onSection(s)} />}
          {shown === "domains" && <DomainsSection me={d} detail={detail} onOpen={(id) => onDetail("domains", id)} />}
          {shown === "users" && <UsersSection me={d} />}
          {shown === "groups" && <GroupsSection me={d} />}
          {shown === "billing" && hostedPages.billing && <hostedPages.billing me={d} detail={detail} onDetail={(id) => onDetail("billing", id)} />}
          {shown === "branding" && <BrandingSectionGate />}
          {shown === "apps" && <AppsSection />}
          {shown === "agents" && <AssistantAgentsSectionGate />}
          {shown === "security" && <OrgSecuritySection />}
      </section>
    </div>
  );
}

// adminEnabled says whether the admin icon shows up in the rail: whoever has NO
// administrative permission at all doesn't see the area.
export function useAdminEnabled(): boolean {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false });
  const d = me.data;
  if (!d) return false;
  return ["domains.view", "members.view", "usage.view", "billing.manage", "mailboxes.manage"].some((p) => hasPerm(d, p));
}

// Apps disabled by the organization (the admin managed them). The rail hides them for everyone.
export function useDisabledApps(): string[] {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false });
  return me.data?.disabled_apps ?? [];
}

// Assistant enabled for the organization. False when the admin disabled it (or the AI isn't
// configured): the assistant bubble disappears for everyone. Defaults to true while loading, so
// the bubble doesn't flicker in/out or vanish because of a stale response missing the field.
export function useAssistantEnabled(): boolean {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false });
  return me.data?.assistant_enabled ?? true;
}
