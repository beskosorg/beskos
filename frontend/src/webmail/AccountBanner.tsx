// The red bar across the top: the two things that stop this product from working, said before the
// user finds out the hard way.
//
// ONE COMPONENT FOR BOTH because from the user's seat they are the same event -- "mail is not going
// to work until somebody does something" -- and two competing bars would fight for the same strip
// of screen on the day both are true.
//
// The unpublished domain comes FIRST when both apply. A trial that is ending is a decision to make
// this week; a domain that was never published means nothing has ever arrived, and it is almost
// always the reason somebody is looking at an empty inbox wondering what they bought.

import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";

export function AccountBanner() {
  const { t } = useTranslation();
  // Same query key the rest of the app uses, so this costs no extra request.
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false }).data;
  if (!me) return null;

  if (me.domain && !me.domain.published) {
    return (
      <Bar tone="danger" text={t("accountBanner.domainUnpublished", { domain: me.domain.domain })}
        actionLabel={t("accountBanner.domainAction")} href="/panel/domains" />
    );
  }

  const a = me.account;
  if (!a || a.state === "ok") return null;

  // days_left is computed on the server so both frontends round it the same way.
  const days = Math.max(0, a.days_left ?? 0);
  switch (a.state) {
    case "ending":
      return <Bar tone="warn" text={t("accountBanner.trialEnding", { count: days })}
        actionLabel={t("accountBanner.payAction")} href="/panel/billing" />;
    case "past_due":
      return <Bar tone="warn" text={t("accountBanner.pastDue")}
        actionLabel={t("accountBanner.payAction")} href="/panel/billing" />;
    case "send_blocked":
      return <Bar tone="danger" text={t("accountBanner.sendBlocked", { count: days })}
        actionLabel={t("accountBanner.payAction")} href="/panel/billing" />;
    default:
      // "locked" never reaches here: the server has already ended the session and the app is back
      // at the sign-in screen by the time this would render.
      return null;
  }
}

function Bar({ tone, text, actionLabel, href }: {
  tone: "warn" | "danger"; text: string; actionLabel: string; href: string;
}) {
  return (
    <div className={`acct-bar ${tone}`} role="status">
      <span>{text}</span>
      <a href={href}>{actionLabel}</a>
    </div>
  );
}

// MailOrDomainNotice shows the mail app, or -- while the domain is not published -- an explanation
// in its place.
//
// Not a redirect and not a hidden menu entry: the person paid for mail and looked for mail, so the
// place they looked is where the answer belongs. What they would otherwise find is an inbox that is
// empty for a reason no screen mentions, which is indistinguishable from a product that does not
// work.
export function MailOrDomainNotice({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false }).data;
  if (!me?.domain || me.domain.published) return <>{children}</>;
  return (
    <div className="wm-blocked">
      <h2>{t("accountBanner.mailBlockedTitle")}</h2>
      <p>{t("accountBanner.mailBlockedBody", { domain: me.domain.domain })}</p>
      <a className="wm-blocked-cta" href="/panel/domains">{t("accountBanner.domainAction")}</a>
    </div>
  );
}
