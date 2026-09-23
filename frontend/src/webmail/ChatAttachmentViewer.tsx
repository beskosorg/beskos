import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, viewerKind } from "./backend";
import type { ChatAttachment } from "./backend";
import { IcDownload, IcPaperclip } from "../panel/icons";
import { ViewerBar } from "./ViewerBar";

// Full-screen viewer for a chat attachment. Since the download is same-origin (session cookie),
// <img>/<iframe>/<video>/<audio> elements get the URL directly — no blob fetch needed.
export function ChatAttachmentViewer({ attachment, onClose }: { attachment: ChatAttachment; onClose: () => void }) {
  const { t } = useTranslation();
  const name = attachment.name || "anexo";
  const url = backend.chatAttachmentUrl(attachment.file_id, true);
  const dl = backend.chatAttachmentUrl(attachment.file_id, false);
  const kind = viewerKind(name);

  return (
    <div className="viewer-full">
      <ViewerBar title={name} onClose={onClose}>
        <a className="chipbtn" href={dl} download={name}><IcDownload size={15} /> {t("files.download")}</a>
      </ViewerBar>
      <div className="viewer-stage">
        {kind === "image" && <img className="viewer-img" src={url} alt={name} />}
        {kind === "pdf" && <iframe className="viewer-frame" title={name} src={url} />}
        {kind === "video" && <video className="viewer-media" src={url} controls autoPlay />}
        {kind === "audio" && <div className="viewer-audio"><div className="va-name">{name}</div><audio src={url} controls autoPlay /></div>}
        {kind === "text" && <ChatTextView fileId={attachment.file_id} name={name} />}
        {!kind && (
          <div className="viewer-audio">
            <IcPaperclip size={40} />
            <div className="va-name">{name}</div>
            <a className="chipbtn" href={dl} download={name}><IcDownload size={15} /> {t("files.download")}</a>
          </div>
        )}
      </div>
    </div>
  );
}

function ChatTextView({ fileId, name }: { fileId: string; name: string }) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["chat-att-text", fileId],
    queryFn: async () => {
      const res = await fetch(backend.chatAttachmentUrl(fileId, true), { credentials: "same-origin" });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return res.text();
    },
    retry: false,
  });
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
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
