import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me } from "../api";
import { Modal, toast } from "./ui";
import { IcPlus } from "./icons";
import { DomainPage } from "./DomainPage";

export function DomainsSection({ me, detail, onOpen }: { me: Me | undefined; detail?: string; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "domains.manage");
  const list = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [err, setErr] = useState("");

  const add = useMutation({
    mutationFn: () => api.createDomain(name),
    onSuccess: (d) => { setAdding(false); setName(""); setErr(""); qc.invalidateQueries({ queryKey: ["domains"] }); toast(t("domains.added")); onOpen(d.id); },
    onError: (e) => setErr((e as Error).message),
  });

  // With a domain in the URL, its page takes over the screen — it used to be a drawer and the
  // information didn't fit there (18 records with long values, plus each one's status).
  if (detail) return <DomainPage id={detail} canManage={canManage} onBack={() => onOpen("")} />;

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("panel.domains")}</h1><div className="desc">{t("domainsSection.desc")}</div></div>
        <div className="grow" />
        {canManage && <button className="pnl-btn primary" onClick={() => setAdding(true)}><IcPlus size={16} />{t("domains.add")}</button>}
      </div>
      <div className="pnl-content">
        <div className="pnl-card"><div className="pnl-card-b">
          {(list.data?.length ?? 0) === 0 ? <div className="pnl-empty">{t("domains.empty")}</div>
            : list.data!.map((dm) => (
              <div className="pnl-row" key={dm.id}>
                <div className="grow"><div className="mono">{dm.domain}</div></div>
                <StatePill state={dm.provision_state} />
                <button className="pnl-btn ghost" onClick={() => onOpen(dm.id)}>{t("domainsSection.details")}</button>
              </div>
            ))}
        </div></div>
      </div>

      {adding && (
        <Modal title={t("domains.add")} onClose={() => setAdding(false)}
          footer={<>
            <button className="pnl-btn ghost" onClick={() => setAdding(false)}>{t("common.cancel")}</button>
            <button className="pnl-btn primary" disabled={add.isPending} onClick={() => add.mutate()}>{t("domains.add")}</button>
          </>}>
          <div className="pnl-field"><label>{t("panel.domains")}</label>
            <input className="pnl-input" placeholder={t("domains.addPlaceholder")} value={name} onChange={(e) => setName(e.target.value)} autoFocus /></div>
          {err && <p className="pnl-err">{err}</p>}
        </Modal>
      )}

    </>
  );
}

function StatePill({ state }: { state: string }) {
  const { t } = useTranslation();
  const cls = state === "active" ? "ok" : state === "error" ? "warn" : state === "dns_pending" ? "warn" : "muted";
  return <span className={"pnl-pill " + cls}>{t(`domains.states.${state}`, { defaultValue: state })}</span>;
}
