import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi, ApiError } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { IcRefresh } from "../panel/icons";

// WikiHistoryDrawer shows a page's revision history: list of versions, a text diff
// (added/removed lines) between the chosen version and the current content, and restore (which saves
// a NEW version from the old one — reversible, §6). Only whoever can edit sees the restore button.

type DiffLine = { type: "same" | "add" | "del"; text: string };

// lineDiff is a line-based LCS diff: `del` = line only in the old version, `add` = only in the current one.
function lineDiff(oldText: string, newText: string): DiffLine[] {
  const a = oldText.split("\n"), b = newText.split("\n");
  const n = a.length, m = b.length;
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--)
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const out: DiffLine[] = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { out.push({ type: "same", text: a[i] }); i++; j++; }
    else if (lcs[i + 1][j] >= lcs[i][j + 1]) { out.push({ type: "del", text: a[i] }); i++; }
    else { out.push({ type: "add", text: b[j] }); j++; }
  }
  while (i < n) out.push({ type: "del", text: a[i++] });
  while (j < m) out.push({ type: "add", text: b[j++] });
  return out;
}

export function WikiHistoryDrawer({ pageId, currentVersion, currentText, canEdit, onRestored, onClose }: {
  pageId: string; currentVersion: number; currentText: string; canEdit: boolean; onRestored: () => void; onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<number | null>(null);

  const list = useQuery({ queryKey: ["wiki-versions", pageId], queryFn: () => wikiApi.versions(pageId), retry: false });
  const detail = useQuery({ queryKey: ["wiki-version", pageId, selected], queryFn: () => wikiApi.getVersion(pageId, selected!), enabled: selected !== null, retry: false });

  const restore = useMutation({
    mutationFn: (version: number) => wikiApi.restoreVersion(pageId, version, currentVersion),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["wiki-page", pageId] });
      qc.invalidateQueries({ queryKey: ["wiki-versions", pageId] });
      toast(t("wiki.restored")); onRestored(); onClose();
    },
    onError: (e) => toast(e instanceof ApiError && e.code === "wiki.version_conflict" ? t("wiki.conflict") : (e as Error).message),
  });

  const fmtWhen = (iso: string) => new Date(iso).toLocaleString(i18n.language, { dateStyle: "medium", timeStyle: "short" });
  const diff = detail.data ? lineDiff(detail.data.body_text, currentText) : [];

  return (
    <Drawer wide title={t("wiki.history")} onClose={onClose} footer={<><div className="grow" /><button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button></>}>
      <div className="wiki-hist">
        <div className="wiki-hist-list">
          {list.data && list.data.length === 0 && <div className="wiki-muted">{t("wiki.noVersions")}</div>}
          {list.data?.map((v) => (
            <button key={v.id} className={"wiki-hist-item" + (v.version === selected ? " on" : "")} onClick={() => setSelected(v.version)}>
              <b>v{v.version}</b> · {fmtWhen(v.created_at)}
            </button>
          ))}
        </div>
        <div className="wiki-hist-diff">
          {selected === null ? (
            <div className="wiki-muted">{t("wiki.pickVersion")}</div>
          ) : (
            <>
              <div className="wiki-hist-diff-head">
                <span>{t("wiki.diffVsCurrent", { version: selected })}</span>
                {canEdit && <button className="chipbtn" disabled={restore.isPending} onClick={() => restore.mutate(selected)}>
                  <IcRefresh size={14} /> {t("wiki.restore")}
                </button>}
              </div>
              {detail.isLoading ? <div className="wiki-muted">…</div> : (
                <pre className="wiki-diff">
                  {diff.map((d, k) => (
                    <div key={k} className={"wiki-diff-" + d.type}>
                      {d.type === "add" ? "+ " : d.type === "del" ? "- " : "  "}{d.text || " "}
                    </div>
                  ))}
                </pre>
              )}
            </>
          )}
        </div>
      </div>
    </Drawer>
  );
}
