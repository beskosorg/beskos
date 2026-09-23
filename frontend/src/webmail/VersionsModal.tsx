import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, type StorageFile } from "./backend";
import { toast } from "./toast";
import { triggerDownload } from "../apiBase";
import { IcDownload, IcRefresh } from "../panel/icons";

// Version history of a file (accumulated on every save in the editor). List, download an older
// version to inspect, and restore (which creates a new version — reversible).
export function VersionsModal({ file, onClose }: { file: StorageFile; onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["versions", file.id], queryFn: () => backend.fileVersions(file.id), retry: false });
  const restore = useMutation({
    mutationFn: (versionId: string) => backend.restoreVersion(file.id, versionId),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["versions", file.id] }); qc.invalidateQueries({ queryKey: ["files"] }); toast(t("files.versionRestored")); },
    onError: (e) => toast((e as Error).message),
  });
  const fmtSize = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);
  const fmtWhen = (iso: string) => new Date(iso).toLocaleString(i18n.language, { dateStyle: "medium", timeStyle: "short" });

  const versions = q.data?.versions ?? [];
  const cur = q.data?.current_version;

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 480 }}>
        <header><span>{t("files.versionsTitle", { name: file.name })}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          {q.isLoading ? <div className="loading">…</div> : (
            <div className="dv-list">
              <div className="share-row" style={{ borderTop: "none" }}>
                <div className="grow"><div className="who">{t("files.versionCurrent", { n: cur ?? file.version })}</div></div>
                <span className="tx" style={{ color: "var(--faint)", fontSize: 12 }}>{t("files.versionNow")}</span>
              </div>
              {versions.length === 0 ? (
                <div className="empty" style={{ padding: "18px 0" }}>{t("files.noVersions")}</div>
              ) : versions.map((v) => (
                <div className="share-row" key={v.id}>
                  <div className="grow">
                    <div className="who">{t("files.versionN", { n: v.version })}</div>
                    <div className="tx" style={{ fontSize: 12, color: "var(--faint)" }}>{fmtWhen(v.created_at)}{v.author ? ` · ${v.author}` : ""} · {fmtSize(v.size)}</div>
                  </div>
                  <button className="ib" title={t("files.download")} onClick={() => triggerDownload(backend.versionDownloadUrl(file.id, v.id), `v-`)}><IcDownload size={15} /></button>
                  <button className="ib" title={t("files.restore")} disabled={restore.isPending} onClick={() => restore.mutate(v.id)}><IcRefresh size={15} /></button>
                </div>
              ))}
            </div>
          )}
        </div>
        <footer><button className="add" onClick={onClose}>{t("common.close")}</button></footer>
      </div>
    </div>
  );
}
