import type { JSX } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { IcMail, IcPhone, IcChat, IcCard } from "../panel/icons";

// Pieces shared by customer record cards.

export function StatCards({ items }: { items: { label: string; value: string | number; hint?: string }[] }) {
  return (
    <div className="crm-stats">
      {items.map((s) => (
        <div className="crm-stat" key={s.label}>
          <div className="v">{s.value}</div>
          <div className="l">{s.label}</div>
          {s.hint && <div className="h">{s.hint}</div>}
        </div>
      ))}
    </div>
  );
}

type Action = { icon: JSX.Element; label: string; href?: string; onClick?: () => void; blocked?: boolean };

// ActionBar: whatever consent forbids shows up disabled and says why — it never disappears, so
// it stays clear that the data exists but permission to use it doesn't.
export function ActionBar({ actions }: { actions: Action[] }) {
  const { t } = useTranslation();
  return (
    <div className="crm-actions">
      {actions.map((a) =>
        a.blocked ? (
          <button className="crm-act blocked" key={a.label} disabled title={t("crm.noConsent")}>
            {a.icon} {a.label}
          </button>
        ) : a.href ? (
          <a className="crm-act" key={a.label} href={a.href} target="_blank" rel="noreferrer">{a.icon} {a.label}</a>
        ) : (
          <button className="crm-act" key={a.label} onClick={a.onClick}>{a.icon} {a.label}</button>
        )
      )}
    </div>
  );
}

// contactActions builds email/phone/WhatsApp actions from the contact's data + consent.
export function useContactActions(c: {
  id?: string; name?: string; email?: string; phone?: string; mobile?: string; is_whatsapp?: boolean;
  allow_email?: boolean; allow_phone?: boolean; extra_emails?: string[]; extra_phones?: string[];
}): Action[] {
  const { t } = useTranslation();
  const nav = useNavigate();
  const out: Action[] = [];
  if (c.email) {
    out.push({
      icon: <IcMail size={15} />, label: t("crm.sendEmail"), blocked: c.allow_email === false,
      onClick: () => nav(`/mail/mail?to=${encodeURIComponent(c.email!)}`),
    });
  }
  // Send THIS contact as an attached .vcf card — opens compose with the vCard already attached. Doesn't
  // depend on the contact's own email consent (it's sent to someone else, not to them).
  if (c.id) {
    out.push({
      icon: <IcCard size={15} />, label: t("crm.sendVcard"),
      onClick: () => nav(`/mail/mail?vcard=${encodeURIComponent(c.id!)}${c.name ? `&vname=${encodeURIComponent(c.name)}` : ""}`),
    });
  }
  // Secondary emails: one "compose" per address (the label shows the email itself).
  for (const e of c.extra_emails ?? []) {
    out.push({ icon: <IcMail size={15} />, label: e, blocked: c.allow_email === false, onClick: () => nav(`/mail/mail?to=${encodeURIComponent(e)}`) });
  }
  const tel = c.phone || c.mobile;
  if (tel) out.push({ icon: <IcPhone size={15} />, label: t("crm.call"), blocked: c.allow_phone === false, href: `tel:${tel}` });
  for (const p of c.extra_phones ?? []) {
    out.push({ icon: <IcPhone size={15} />, label: p, blocked: c.allow_phone === false, href: `tel:${p}` });
  }
  if (c.mobile && c.is_whatsapp) {
    out.push({
      icon: <IcChat size={15} />, label: t("crm.whatsapp"), blocked: c.allow_phone === false,
      href: `https://wa.me/${c.mobile.replace(/\D/g, "")}`,
    });
  }
  return out;
}
