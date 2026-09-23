import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, type Notif } from "./backend";
import { notifText } from "./notifText";
import { IcBell } from "../panel/icons";

// In-app notifications bell (meeting invites, etc.). Lightweight polling; unread badge.
export function NotificationsBell() {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: ["notifications"], queryFn: () => backend.notifications(), retry: false, refetchInterval: 30000 });
  const readAll = useMutation({ mutationFn: () => backend.notificationsReadAll(), onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }) });
  const readOne = useMutation({ mutationFn: (id: string) => backend.notificationRead(id), onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }) });

  const items = q.data?.items ?? [];
  const unread = q.data?.unread ?? 0;
  const when = (iso: string) => new Date(iso).toLocaleString(i18n.language, { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });

  const openItem = (n: Notif) => {
    if (!n.read_at) readOne.mutate(n.id);
    setOpen(false);
    if (n.link) nav(n.link);
  };

  return (
    <div className="notif-wrap">
      <button className="ib" title={t("notif.title")} onClick={() => setOpen((v) => !v)}>
        <IcBell size={18} />
        {unread > 0 && <span className="notif-badge">{unread > 9 ? "9+" : unread}</span>}
      </button>
      {open && (
        <>
          <div className="menu-backdrop" onClick={() => setOpen(false)} />
          <div className="notif-panel">
            <header>
              <b>{t("notif.title")}</b>
              {unread > 0 && <button className="notif-readall" onClick={() => readAll.mutate()}>{t("notif.readAll")}</button>}
            </header>
            <div className="notif-list">
              {items.length === 0 ? <div className="notif-empty">{t("notif.empty")}</div>
                : items.map((n) => {
                  const txt = notifText(n, t, i18n.language);
                  return (
                  <button key={n.id} className={"notif-item" + (n.read_at ? "" : " unread")} onClick={() => openItem(n)}>
                    {!n.read_at && <span className="notif-dot" />}
                    <div className="grow"><div className="nt">{txt.title}</div><div className="nb">{txt.body}</div><div className="nw">{when(n.created_at)}</div></div>
                  </button>
                  );
                })}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
