// Importing a mailbox from a file on the customer's own disk.
//
// This is the half no login can reach. Years of POP3 in Thunderbird's Local Folders, an archive
// exported before an account was closed, a mailbox handed over on a memory stick when somebody
// left: that mail is on no server anywhere, and until this screen existed the answer was that it
// could not be moved.
//
// Outlook is named here even though its own format cannot be read, because "Outlook" is what the
// customer will look for. Sending them away with nothing is worse than sending them the way round.

import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../api";
import { toast } from "./ui";
import { IcUpload, IcPaperclip } from "./icons";
import { MIGRATION_QUERY_KEY } from "./MigrationSection";

// What the proxy in front of the API accepts. Checked HERE as well as on the server, because the
// server never sees an oversized request: nginx rejects it on the Content-Length and answers with
// its own HTML page, which this code cannot translate and the customer cannot act on. Refusing on
// the form costs a second; finding out after a twenty-minute upload costs the afternoon.
const MAX_UPLOAD_BYTES = 500 * 1024 * 1024;

export function MigrationFileForm() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [pct, setPct] = useState(0);
  const [over, setOver] = useState(false);
  const [err, setErr] = useState("");

  const upload = useMutation({
    mutationFn: (f: File) => api.migrationUploadFile(f, setPct),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: MIGRATION_QUERY_KEY });
      toast(t("migration.started"));
    },
    onError: (e) => { setErr((e as Error).message); setPct(0); },
  });

  const pick = (f: File | null | undefined) => {
    if (!f) return;
    setPct(0);
    if (f.size > MAX_UPLOAD_BYTES) {
      setFile(null);
      setErr(t("migration.fileTooLargeLocal", { size: humanSize(f.size) }));
      return;
    }
    setFile(f);
    setErr("");
  };

  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <IcUpload size={16} />
        <h3>{t("migration.fileTitle")}</h3>
      </div>
      <div className="pnl-card-b">
        <p className="desc">{t("migration.fileBlurb")}</p>

        {/* Drag and drop as well as a button: a mail archive is something people already have in a
            window in front of them, and making them navigate a file dialog to it is friction for
            nothing. */}
        <div
          className={"mig-drop" + (over ? " over" : "") + (file ? " has" : "")}
          onDragOver={(e) => { e.preventDefault(); setOver(true); }}
          onDragLeave={() => setOver(false)}
          onDrop={(e) => { e.preventDefault(); setOver(false); pick(e.dataTransfer.files?.[0]); }}
          onClick={() => inputRef.current?.click()}
        >
          <IcPaperclip size={18} />
          {file
            ? <><b>{file.name}</b><span className="size">{humanSize(file.size)}</span></>
            : <span>{t("migration.fileDrop")}</span>}
          <input ref={inputRef} type="file" hidden
            onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ""; }} />
        </div>

        <div className="pnl-note">{t("migration.fileFormats")}</div>

        {/* Where to find it, per program. Nobody knows that Thunderbird's mail is in a folder called
            "Local Folders" inside a profile directory with a random name. */}
        <details className="mig-where">
          <summary>{t("migration.whereTitle")}</summary>
          <p><b>{t("migration.whereThunderbirdTitle")}</b> {t("migration.whereThunderbird")}</p>
          <p><b>{t("migration.whereOutlookTitle")}</b> {t("migration.whereOutlook")}</p>
        </details>

        {upload.isPending && (
          <>
            {/* The upload's own percentage, which is not the import's. Somebody watching half a
                gigabyte go up needs to see it moving, and calling it "sending" keeps it distinct
                from the copying that starts once it has arrived. */}
            <div className="mig-state">{t("migration.uploading", { pct })}</div>
            <div className="mig-bar"><span style={{ width: `${pct}%` }} /></div>
          </>
        )}

        {err && <p className="pnl-err">{err}</p>}

        <div className="mig-actions">
          <button className="pnl-btn primary" disabled={!file || upload.isPending}
            onClick={() => file && upload.mutate(file)}>
            <IcUpload size={15} /> {t("migration.fileStart")}
          </button>
        </div>
      </div>
    </div>
  );
}

// humanSize keeps the units the operating system showed the customer a moment ago.
function humanSize(bytes: number): string {
  const units = ["B", "kB", "MB", "GB"];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}
