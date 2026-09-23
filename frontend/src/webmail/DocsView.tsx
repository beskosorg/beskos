// Documents: what somebody has been working on, across every folder.
//
// A VIEW OVER DRIVE, not a second place to keep things. Every document here is a Drive file, with
// the same owner, the same sharing, the same version history and the same trash. What this app adds
// is the one thing a file browser cannot give: an order based on "what I had open", instead of
// "where I filed it".
//
// NO TEMPLATE GALLERY, deliberately. Google's screen is 60% templates because Google has hundreds
// of them; a gallery holding one blank card advertises an empty shelf.
//
// If this ever grows folders of its own, it stopped being a view and became a second file manager
// -- and from then on there are two things to maintain instead of one.

import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, isOfficeFile } from "./backend";
import { recentDocuments } from "./docsApi";
import { FileTypeIcon } from "./filetypes";
import { openInWindow } from "./nativeWindow";
import { toast } from "./toast";
import { IcPlus, IcSearch } from "../panel/icons";

const KINDS = [
  { kind: "document", ext: "a.docx", labelKey: "files.newDoc", nameKey: "files.untitledDoc" },
  { kind: "spreadsheet", ext: "a.xlsx", labelKey: "files.newSheet", nameKey: "files.untitledSheet" },
  { kind: "presentation", ext: "a.pptx", labelKey: "files.newSlide", nameKey: "files.untitledSlide" },
] as const;

export function DocsView({ search }: { search: string }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);

  const docs = useQuery({ queryKey: ["docs-recent"], queryFn: recentDocuments, retry: false });

  const create = useMutation({
    mutationFn: ({ kind, name }: { kind: "document" | "spreadsheet" | "presentation"; name: string }) =>
      // No folder: it lands at the root of the creator's Drive, which is where they look next.
      backend.createOffice(kind, name),
    onSuccess: (f) => {
      setCreating(false);
      qc.invalidateQueries({ queryKey: ["docs-recent"] });
      qc.invalidateQueries({ queryKey: ["files"] }); // Drive is showing the very same file
      openEditor(f.id);
    },
    onError: (e) => toast((e as Error).message),
  });

  // Search filters the list already loaded: sixty recent documents at most, and going to the server
  // on every keystroke to filter sixty rows would be work for everybody and news for nobody.
  const shown = useMemo(() => {
    const list = docs.data ?? [];
    const q = search.trim().toLowerCase();
    return q ? list.filter((d) => d.name.toLowerCase().includes(q)) : list;
  }, [docs.data, search]);

  function openEditor(id: string) {
    if (!openInWindow(`editor-${id}`, `/mail/files/edit/${id}`, "Beskos — Editor")) {
      window.location.assign(`/mail/files/edit/${id}`);
    }
  }

  const fmtDate = (iso: string) =>
    new Date(iso).toLocaleDateString(i18n.language, { day: "2-digit", month: "short", year: "numeric" });

  return (
    <div className="docs">
      <div className="docs-top">
        <h1>{t("webmail.apps.docs")}</h1>
        <div className="grow" />
        <div className="docs-new">
          <button className="docs-new-btn" onClick={() => setCreating((v) => !v)}>
            <IcPlus size={16} />{t("docs.new")}
          </button>
          {creating && (
            <div className="docs-new-menu">
              {KINDS.map((k) => (
                <button key={k.kind} onClick={() => create.mutate({ kind: k.kind, name: t(k.nameKey) })}>
                  <span className="ic"><FileTypeIcon name={k.ext} size={18} /></span>{t(k.labelKey)}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {docs.isLoading && <div className="docs-empty">{t("common.loading")}</div>}

      {!docs.isLoading && shown.length === 0 && (
        <div className="docs-empty">
          {search.trim()
            ? <><IcSearch size={20} /><p>{t("docs.noMatch")}</p></>
            : <p>{t("docs.empty")}</p>}
        </div>
      )}

      <div className="docs-list">
        {shown.map((d) => (
          <button key={d.id} className="docs-row" onClick={() => openEditor(d.id)}
            disabled={!isOfficeFile(d.name)}>
            <span className="ic"><FileTypeIcon name={d.name} size={22} /></span>
            <span className="docs-name">{d.name}</span>
            {/* Somebody who is not the owner should see it: theirs to edit, not theirs to delete. */}
            {!d.owned && <span className="docs-tag">{t("docs.sharedWithMe")}</span>}
            <span className="docs-date">{fmtDate(d.updated_at)}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
