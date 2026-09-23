import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { backend } from "./backend";
import { toast } from "./toast";
import { isDedicatedWindow, closeSelfWindow } from "./nativeWindow";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare global { interface Window { DocsAPI?: any } }

// Loads the Document Server's api.js once (caches the Promise per URL).
const loaded = new Map<string, Promise<void>>();
function loadScript(src: string): Promise<void> {
  if (!loaded.has(src)) {
    loaded.set(src, new Promise((resolve, reject) => {
      const el = document.createElement("script");
      el.src = src; el.async = true;
      el.onload = () => resolve();
      el.onerror = () => reject(new Error("editor_script_failed"));
      document.body.appendChild(el);
    }));
  }
  return loaded.get(src)!;
}

// Office editor (OnlyOffice Document Server) in full screen, with NO bar of ours on top: close
// and rename live in the editor's own header, which already carries the Beskos brand. Both only
// exist because we register the events here — it's `onRequestClose` that makes the X appear, and
// `onRequestRename` that makes the title editable (see api.js: canRequestClose/canRename derive
// from the events).
export function EditorView() {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  const { id } = useParams<{ id: string }>();
  const mount = useRef<HTMLDivElement>(null);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const editor = useRef<any>(null);

  const themeNow = () => {
    const attr = document.documentElement.getAttribute("data-theme");
    if (attr) return attr;
    return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  };
  const session = useQuery({
    queryKey: ["edit-session", id],
    queryFn: () => backend.editSession(id!, i18n.language || "pt-BR", themeNow()),
    retry: false,
    enabled: !!id,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (!session.data || !mount.current) return;
    let cancelled = false;
    loadScript(session.data.apiJs).then(() => {
      if (cancelled || !window.DocsAPI || !mount.current) return;
      editor.current = new window.DocsAPI.DocEditor(mount.current.id, {
        ...session.data!.config,
        events: {
          // Header X: a dedicated window closes itself; in a normal tab, it goes back to Drive.
          onRequestClose: () => { if (isDedicatedWindow()) closeSelfWindow(); else nav("/mail/files"); },
          // Header title becomes editable; the new name arrives in `data`.
          onRequestRename: (e: { data?: string }) => {
            const name = String(e?.data ?? "").trim();
            if (!name) return;
            backend.renameFile(id!, name)
              .then(() => toast(t("files.renamed")))
              .catch((err) => toast((err as Error).message));
          },
        },
      });
    }).catch(() => { /* error shown below by the query in case of config failure */ });
    return () => {
      cancelled = true;
      try { editor.current?.destroyEditor?.(); } catch { /* noop */ }
      editor.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.data]);

  return (
    <div className="editor-full">
      <div className="editor-stage">
        {session.isLoading && <div className="editor-msg">{t("editor.loading")}</div>}
        {session.isError && <div className="editor-msg">{(session.error as Error).message}</div>}
        <div id="oo-editor" ref={mount} className="editor-frame" />
      </div>
    </div>
  );
}
