import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { EmailAttachment } from "../jmap/types";
import type { MailService } from "../jmap/mail";
import { viewerKind } from "./backend";
import { IcDownload, IcPaperclip } from "../panel/icons";
import { ViewerBar } from "./ViewerBar";

// Classifies the attachment by mime-type; falls back to the name's extension when the mime is generic.
function kindOf(type: string, name: string): string | null {
  const t = (type || "").toLowerCase();
  if (t.startsWith("image/")) return "image";
  if (t === "application/pdf") return "pdf";
  if (t.startsWith("video/")) return "video";
  if (t.startsWith("audio/")) return "audio";
  if (t.startsWith("text/")) return "text";
  return viewerKind(name);
}

// Full-screen viewer for an email attachment (image/pdf/video/audio/text). The blob is
// downloaded with authentication (Basic via header) and served as an object URL — <img>/<iframe>/<a>
// don't send the header, so pointing directly at /jmap/download would give a 401.
export function AttachmentViewer({ service, attachment, onClose }: {
  service: MailService; attachment: EmailAttachment; onClose: () => void;
}) {
  const { t } = useTranslation();
  const name = attachment.name ?? "anexo";
  const kind = kindOf(attachment.type, name);

  const blob = useQuery({
    queryKey: ["attblob", attachment.blobId],
    queryFn: () => service.attachmentObjectUrl(attachment.blobId, name, attachment.type),
    retry: false,
    staleTime: Infinity,
    gcTime: 0, // doesn't cache: we revoke the object URL on close
  });
  const url = blob.data;

  // Revokes the object URL on unmount (avoids a memory leak).
  useEffect(() => () => { if (url) URL.revokeObjectURL(url); }, [url]);

  return (
    <div className="viewer-full">
      <ViewerBar title={name} onClose={onClose}>
        {url && <a className="chipbtn" href={url} download={name}><IcDownload size={15} /> {t("files.download")}</a>}
      </ViewerBar>
      <div className="viewer-stage">
        {blob.isLoading && <div className="editor-msg">{t("editor.loading")}</div>}
        {blob.isError && <div className="editor-msg">{(blob.error as Error).message}</div>}
        {url && kind === "image" && <img className="viewer-img" src={url} alt={name} />}
        {url && kind === "pdf" && <iframe className="viewer-frame" title={name} src={url} />}
        {url && kind === "video" && <video className="viewer-media" src={url} controls autoPlay />}
        {url && kind === "audio" && <div className="viewer-audio"><div className="va-name">{name}</div><audio src={url} controls autoPlay /></div>}
        {url && kind === "text" && <iframe className="viewer-frame" title={name} src={url} />}
        {url && !kind && (
          <div className="viewer-audio">
            <IcPaperclip size={40} />
            <div className="va-name">{name}</div>
            <a className="chipbtn" href={url} download={name}><IcDownload size={15} /> {t("files.download")}</a>
          </div>
        )}
      </div>
    </div>
  );
}
