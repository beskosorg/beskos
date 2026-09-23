import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";
import { backend } from "./backend";
import { toast } from "./toast";

// Rename dialog for a Drive file or folder. Extracted from FilesView.tsx (law 1.4 -- 600 lines).
export function RenameModal({ item, onClose, onDone }: { item: { kind: "file" | "folder"; id: string; name: string }; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(item.name);
  const m = useMutation({
    mutationFn: () => item.kind === "folder" ? backend.renameFolder(item.id, name) : backend.renameFile(item.id, name),
    onSuccess: () => { onDone(); toast(t("files.renamed")); }, onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("contacts.rename")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body"><input autoFocus className="prompt" style={{ minHeight: "auto" }} value={name} onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && name) m.mutate(); }} /></div>
        <footer><button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!name || m.isPending} onClick={() => m.mutate()}>{t("contacts.save")}</button></footer>
      </div>
    </div>
  );
}
