import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type DomainDetail, type DNSRecord, type DNSCheck, type DNSCheckResult } from "../api";
import { toast } from "./ui";
import { IcArrowLeft, IcChevron } from "./icons";

// Domain page (used to be a sidecar). Became a page because the information doesn't fit in a
// drawer: there are ~18 records with long values (DKIM, MTA-STS) plus each one's status — in the
// drawer the table got squeezed to the point the "Copy" column came out in vertical letters.
//
// Three blocks, in this order of usefulness: what's left to publish, how to publish it with your
// provider, and the danger zone.

// What mail actually needs: delivery (MX), being accepted out there (SPF/DKIM/MAIL FROM) and the
// policy that ties them together (DMARC). The rest — discovery SRV, MTA-STS, TLS-RPT, auto-config
// — is convenience for email clients and transport hardening: mail works without them, so they go
// under "advanced", collapsed. Asking for 18 records when 4 are enough is what made this page scary.
const ESSENTIAL_PURPOSES = ["mx", "spf", "dkim", "dkim_ses", "mailfrom", "dmarc"];

// Providers covered by the guide. `import` marks the ones that accept a BIND zone file — for
// those, the export resolves everything at once and is the path to recommend first.
const PROVIDERS = [
  { key: "cloudflare", importsZone: true },
  { key: "route53", importsZone: true },
  { key: "godaddy", importsZone: false },
  { key: "namecheap", importsZone: false },
  { key: "google", importsZone: false },
  { key: "other", importsZone: false },
];

export function DomainPage({ id, canManage, onBack }: { id: string; canManage: boolean; onBack: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery<DomainDetail>({
    queryKey: ["domain", id], queryFn: () => api.getDomain(id),
    refetchInterval: (query) => (query.state.data && query.state.data.provision_state !== "active" ? 5000 : false),
  });
  const check = useQuery<DNSCheckResult>({ queryKey: ["dns-check", id], queryFn: () => api.dnsCheck(id), retry: false });
  const [confirmName, setConfirmName] = useState("");
  const [delErr, setDelErr] = useState("");
  const del = useMutation({
    mutationFn: () => api.deleteDomain(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["domains"] }); toast(t("domains.deleted")); onBack(); },
    onError: (e) => setDelErr((e as Error).message),
  });

  const d = q.data;
  const records = d?.dns_records ?? [];
  const byKey = useMemo(() => {
    const m = new Map<string, DNSCheck>();
    for (const c of check.data?.records ?? []) m.set(`${c.type}|${c.host}`, c);
    return m;
  }, [check.data]);
  const sum = check.data?.summary;
  const essential = records.filter((r) => ESSENTIAL_PURPOSES.includes(r.purpose));
  const essOk = essential.filter((r) => byKey.get(`${r.type}|${r.host}`)?.status === "ok").length;
  const essPending = essential.length - essOk;
  const optional = records.filter((r) => !ESSENTIAL_PURPOSES.includes(r.purpose));

  return (
    <>
      <div className="pnl-top">
        <button className="pnl-icon-btn" title={t("common.back")} onClick={onBack}><IcArrowLeft size={16} /></button>
        <div>
          <h1 className="mono">{d?.domain ?? "…"}</h1>
          <div className="desc">{t("domains.dnsHint")}</div>
        </div>
      </div>

      <div className="pnl-content dom-page">
        {/* Summary: how many are missing and whether what's missing is essential. It's the only line most people read. */}
        <div className="pnl-card">
          <div className="pnl-card-b dom-summary">
            <StatePill state={d?.provision_state ?? ""} />
            <span className={"dns-sum" + (essPending > 0 ? " warn" : check.data ? " ok" : "")}>
              {check.data ? t("domains.dnsSummaryEssential", { ok: essOk, total: essential.length }) : "…"}
              {essPending > 0 && ` · ${t("domains.dnsRequiredPending", { n: essPending })}`}
              {sum && sum.total > essential.length && ` · ${t("domains.dnsOptionalCount", { n: sum.total - essential.length })}`}
            </span>
            {check.data?.checked_at && (
              <span className="desc">{t("domains.dnsCheckedAt", { when: new Date(check.data.checked_at).toLocaleTimeString(i18n.language) })}</span>
            )}
            <span className="grow" />
            <button className="pnl-btn" disabled={check.isFetching} onClick={() => void check.refetch()}>
              {check.isFetching ? "…" : t("domains.dnsRecheck")}
            </button>
            <button className="pnl-btn ghost" onClick={() => void api.dnsExport(id, "zone", d?.domain ?? "").catch((e) => toast((e as Error).message))}>
              {t("domains.dnsExportZone")}
            </button>
            <button className="pnl-btn ghost" onClick={() => void api.dnsExport(id, "csv", d?.domain ?? "").catch((e) => toast((e as Error).message))}>CSV</button>
          </div>
        </div>

        <ProviderGuide domain={d?.domain ?? ""} onExport={() => void api.dnsExport(id, "zone", d?.domain ?? "").catch((e) => toast((e as Error).message))} />

        <RecordsCard title={t("domains.dnsRequiredTitle")} hint={t("domains.dnsRequiredHint")} records={essential} checks={byKey} />
        <RecordsCard title={t("domains.dnsOptionalTitle")} hint={t("domains.dnsOptionalHint")} records={optional} checks={byKey} collapsible />

        {canManage && d && (
          <div className="pnl-card danger">
            <div className="pnl-card-h"><div><h3>{t("domains.dangerTitle")}</h3><div className="desc">{t("domains.deleteHint")}</div></div></div>
            <div className="pnl-card-b dom-danger">
              <input className="pnl-input" style={{ width: 260 }} placeholder={d.domain} value={confirmName}
                onChange={(e) => { setConfirmName(e.target.value); setDelErr(""); }} />
              <button className="pnl-btn danger" disabled={confirmName !== d.domain || del.isPending} onClick={() => del.mutate()}>
                {t("domains.deleteBtn")}
              </button>
              {delErr && <p className="pnl-err">{delErr}</p>}
            </div>
          </div>
        )}
      </div>
    </>
  );
}

function StatePill({ state }: { state: string }) {
  const { t } = useTranslation();
  const cls = state === "active" ? "ok" : state === "error" ? "warn" : state === "dns_pending" ? "warn" : "muted";
  return <span className={"pnl-pill " + cls}>{t(`domains.states.${state}`, { defaultValue: state })}</span>;
}

// Per-provider guide: concrete steps on where to click. Whoever imports the zone (Cloudflare,
// Route 53) resolves everything at once — that's why those appear first, with the export button at hand.
function ProviderGuide({ domain, onExport }: { domain: string; onExport: () => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<string | null>("cloudflare");
  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <div><h3>{t("domains.guideTitle")}</h3><div className="desc">{t("domains.guideHint")}</div></div>
      </div>
      <div className="pnl-card-b">
        {PROVIDERS.map((p) => {
          const on = open === p.key;
          const steps = t(`domains.guide.${p.key}.steps`, { returnObjects: true, domain }) as unknown;
          const list = Array.isArray(steps) ? (steps as string[]) : [];
          return (
            <div className={"guide-item" + (on ? " on" : "")} key={p.key}>
              <button className="guide-head" onClick={() => setOpen(on ? null : p.key)}>
                <IcChevron size={14} className={on ? "chev-open" : ""} />
                <b>{t(`domains.guide.${p.key}.name`)}</b>
                {p.importsZone && <span className="pnl-pill ok">{t("domains.guideImports")}</span>}
              </button>
              {on && (
                <div className="guide-body">
                  <ol>{list.map((step, i) => <li key={i}>{step}</li>)}</ol>
                  {p.importsZone && (
                    <button className="pnl-btn" onClick={onExport}>{t("domains.dnsExportZone")}</button>
                  )}
                </div>
              )}
            </div>
          );
        })}
        <p className="desc">{t("domains.guidePropagation")}</p>
      </div>
    </div>
  );
}

// Table of records with status. A page has room for the value to breathe; anything large scrolls
// inside its own cell instead of pushing the table wider.
function RecordsCard({ title, hint, records, checks, collapsible }: {
  title: string; hint?: string; records: DNSRecord[]; checks: Map<string, DNSCheck>; collapsible?: boolean;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(-1);
  // Optional ones start collapsed: whoever wants to fine-tune can, whoever doesn't won't even see them.
  const [open, setOpen] = useState(!collapsible);
  if (records.length === 0) return null;
  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <div><h3>{title}</h3>{hint && <div className="desc">{hint}</div>}</div>
        {collapsible && (
          <>
            <div className="grow" />
            <button className="pnl-btn ghost" onClick={() => setOpen((v) => !v)}>
              {open ? t("domains.dnsHide") : t("domains.dnsShow", { n: records.length })}
            </button>
          </>
        )}
      </div>
      {open && (
      <div className="pnl-card-b">
        <div className="dns-scroll">
        <table className="dns-table">
          <thead>
            <tr>
              <th>{t("domains.colStatus")}</th><th>{t("domains.colPurpose")}</th><th>{t("domains.colType")}</th>
              <th>{t("domains.colHost")}</th><th>{t("domains.colValue")}</th><th />
            </tr>
          </thead>
          <tbody>
            {records.map((r, i) => {
              const c = checks.get(`${r.type}|${r.host}`);
              const cls = !c ? "muted" : c.status === "ok" ? "ok" : c.required ? "err" : "warn";
              return (
                <tr key={i}>
                  {/* data-label feeds the stacked mode (narrow screen), where the header disappears. */}
                  <td data-label={t("domains.colStatus")}><span className={"dns-pill " + cls}>{c ? t(`domains.dnsStatus.${c.status}`) : "…"}</span></td>
                  <td data-label={t("domains.colPurpose")}><span className="pnl-pill muted">{t(`domains.purpose.${r.purpose}`, { defaultValue: r.purpose })}</span></td>
                  <td className="mono" data-label={t("domains.colType")}>{r.type}</td>
                  <td className="mono" data-label={t("domains.colHost")}>{r.host}</td>
                  <td data-label={t("domains.colValue")}>
                    {/* When it diverges, BOTH rows labeled: without saying which is which, seeing
                        two values side by side doesn't help anyone decide what to fix. */}
                    {c?.status === "mismatch" ? (
                      <div className="dns-diff">
                        <div className="dns-diff-row"><span className="dns-lbl">{t("domains.dnsExpected")}</span><span className="dns-val mono">{r.value}</span></div>
                        <div className="dns-diff-row bad"><span className="dns-lbl">{t("domains.dnsFound")}</span><span className="dns-val mono">{c.found.join(" · ")}</span></div>
                      </div>
                    ) : (
                      <div className="dns-val mono">{r.value}</div>
                    )}
                  </td>
                  <td className="dns-copy">
                    <button className="pnl-btn ghost" onClick={() => { void navigator.clipboard.writeText(r.value); setCopied(i); setTimeout(() => setCopied(-1), 1500); }}>
                      {copied === i ? t("domains.copied") : t("domains.copy")}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        </div>
      </div>
      )}
    </div>
  );
}
