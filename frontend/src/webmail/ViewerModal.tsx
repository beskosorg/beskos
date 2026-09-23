import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, fetchText, viewerKind, type StorageFile } from "./backend";
import { triggerDownload } from "../apiBase";
import { useAuthedMedia } from "./useAuthedMedia";
import { IcDownload } from "../panel/icons";
import { ViewerBar } from "./ViewerBar";

// Embedded full-screen viewer for image / PDF / audio / video. Other types still
// go straight to download (Drive decides before opening this).
export function ViewerModal({ file, onClose }: { file: StorageFile; onClose: () => void }) {
  const { t } = useTranslation();
  const kind = viewerKind(file.name);
  // authenticated src: direct URL on the web (cookie), blob in the native app (see useAuthedMedia).
  const url = useAuthedMedia(`/api/files/${file.id}/download?inline=1`);
  const loading = !url && kind !== "text";

  return (
    <div className="viewer-full">
      <ViewerBar title={file.name} onClose={onClose}>
        <button className="chipbtn" onClick={() => triggerDownload(backend.downloadFileUrl(file.id), file.name)}>
          <IcDownload size={15} /> {t("files.download")}
        </button>
      </ViewerBar>
      <div className="viewer-stage">
        {loading && <div className="editor-msg">{t("editor.loading")}</div>}
        {!loading && kind === "image" && <img className="viewer-img" src={url} alt={file.name} />}
        {!loading && kind === "pdf" && <iframe className="viewer-frame" title={file.name} src={url} />}
        {!loading && kind === "video" && <video className="viewer-media" src={url} controls autoPlay />}
        {!loading && kind === "audio" && (
          <div className="viewer-audio">
            <div className="va-name">{file.name}</div>
            <audio src={url} controls autoPlay />
          </div>
        )}
        {kind === "text" && <TextView file={file} />}
      </div>
    </div>
  );
}

// Text/code viewer (txt, json, xml, md, yaml, code…). JSON gets formatted.
function TextView({ file }: { file: StorageFile }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["text", file.id], queryFn: () => fetchText(file.id), retry: false });
  const ext = file.name.split(".").pop()?.toLowerCase() ?? "";
  let body = q.data ?? "";
  if (ext === "json" && body) { try { body = JSON.stringify(JSON.parse(body), null, 2); } catch { /* keep raw */ } }
  return (
    <div className="viewer-text">
      {q.isLoading ? <div className="editor-msg">{t("editor.loading")}</div>
        : q.isError ? <div className="editor-msg">{(q.error as Error).message}</div>
        : <pre>{body}</pre>}
    </div>
  );
}
