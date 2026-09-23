import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, type ChatAttachment } from "./backend";
import { IcTable, IcCode, IcMaximize } from "../panel/icons";

// Preview of TEXT attachments right in the conversation: a CSV becomes a table and a txt/json/log
// shows the first lines. Before, they were all a single gray line with the file name — to see
// three columns of a CSV you had to download it and open it in another program.
//
// We only read the START of the file (MAX_BYTES): a 40 MB log can't be allowed to stall the
// conversation. Whoever wants the rest opens the viewer (the expand button) or downloads it.

const MAX_BYTES = 64 * 1024;
const MAX_ROWS = 12;   // data rows shown in the table (excluding the header)
const MAX_COLS = 8;
const MAX_LINES = 10;  // lines shown in text/JSON

export type PreviewKind = "csv" | "text";

// previewKind says whether this attachment has a preview in the conversation (and which kind).
export function previewKind(name: string): PreviewKind | null {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "csv" || ext === "tsv") return "csv";
  if (["txt", "log", "json", "md", "markdown", "yml", "yaml", "xml", "ini", "toml", "env"].includes(ext)) return "text";
  return null;
}

export function ChatAttachmentPreview({ att, kind, onOpen }: {
  att: ChatAttachment; kind: PreviewKind; onOpen: () => void;
}) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["chat-att-preview", att.file_id],
    queryFn: () => fetchHead(att.file_id),
    retry: false,
    staleTime: 5 * 60 * 1000,
  });

  const head = (
    <header className="ap-head">
      <span className="ap-ic">{kind === "csv" ? <IcTable size={14} /> : <IcCode size={14} />}</span>
      <span className="ap-name">{att.name}</span>
      <button className="ib sm" title={t("chat.previewOpen")} onClick={onOpen}><IcMaximize size={14} /></button>
    </header>
  );

  if (q.isLoading) return <div className="att-preview">{head}<div className="ap-msg">{t("editor.loading")}</div></div>;
  // Failing to read isn't a reason to hide the attachment: the normal row (download) is still there.
  if (q.isError || !q.data) return null;

  return (
    <div className="att-preview">
      {head}
      {kind === "csv"
        ? <CsvTable text={q.data.text} truncated={q.data.truncated} tab={att.name.toLowerCase().endsWith(".tsv")} />
        : <TextHead text={q.data.text} json={att.name.toLowerCase().endsWith(".json")} truncated={q.data.truncated} />}
    </div>
  );
}

async function fetchHead(fileId: string): Promise<{ text: string; truncated: boolean }> {
  // Range: the server may ignore it (returns 200 with everything) — so we truncate client-side anyway.
  const res = await fetch(backend.chatAttachmentUrl(fileId, true), {
    credentials: "same-origin",
    headers: { Range: `bytes=0-${MAX_BYTES - 1}` },
  });
  if (!res.ok && res.status !== 206) throw new Error(`HTTP ${res.status}`);
  const full = await res.text();
  return { text: full.slice(0, MAX_BYTES), truncated: full.length > MAX_BYTES || res.status === 206 };
}

// --- CSV ---

// Splits a CSV row while respecting quotes ("a,b" is ONE field) and doubled quotes ("" = ").
function splitRow(line: string, sep: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quoted = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quoted) {
      if (c === '"') {
        if (line[i + 1] === '"') { cur += '"'; i++; } else quoted = false;
      } else cur += c;
    } else if (c === '"') quoted = true;
    else if (c === sep) { out.push(cur); cur = ""; }
    else cur += c;
  }
  out.push(cur);
  return out;
}

// The separator varies by region (Excel in some locales writes with ";"). Decided from the first
// line: whichever appears more often outside quotes wins.
function guessSep(line: string): string {
  const count = (sep: string) => splitRow(line, sep).length;
  return count(";") > count(",") ? ";" : ",";
}

function CsvTable({ text, truncated, tab }: { text: string; truncated: boolean; tab: boolean }) {
  const { t } = useTranslation();
  const lines = text.split(/\r?\n/).filter((l) => l.trim() !== "");
  if (lines.length === 0) return null;
  // The last line of a truncated chunk usually comes in mid-way — not worth showing it.
  const usable = truncated && lines.length > 1 ? lines.slice(0, -1) : lines;
  const sep = tab ? "\t" : guessSep(usable[0]);
  const rows = usable.slice(0, MAX_ROWS + 1).map((l) => splitRow(l, sep).slice(0, MAX_COLS));
  const cols = Math.max(...rows.map((r) => r.length));
  const [header, ...body] = rows;
  const hiddenRows = usable.length - 1 - body.length;
  const hiddenCols = splitRow(usable[0], sep).length - cols;

  return (
    <>
      <div className="ap-table-wrap">
        <table className="ap-table">
          <thead>
            <tr>{Array.from({ length: cols }, (_, i) => <th key={i}>{header[i] ?? ""}</th>)}</tr>
          </thead>
          <tbody>
            {body.map((r, ri) => (
              <tr key={ri}>{Array.from({ length: cols }, (_, i) => <td key={i}>{r[i] ?? ""}</td>)}</tr>
            ))}
          </tbody>
        </table>
      </div>
      {(hiddenRows > 0 || hiddenCols > 0 || truncated) && (
        <div className="ap-more">
          {hiddenRows > 0 && t("chat.csvMoreRows", { count: hiddenRows })}
          {hiddenRows > 0 && hiddenCols > 0 ? " · " : ""}
          {hiddenCols > 0 && t("chat.csvMoreCols", { count: hiddenCols })}
          {hiddenRows <= 0 && hiddenCols <= 0 && truncated ? t("chat.previewPartial") : ""}
        </div>
      )}
    </>
  );
}

// --- Texto / JSON ---

function TextHead({ text, json, truncated }: { text: string; json: boolean; truncated: boolean }) {
  const { t } = useTranslation();
  let body = text;
  // Valid JSON is made readable (indented); invalid or mid-way truncated is shown as-is.
  if (json) { try { body = JSON.stringify(JSON.parse(text), null, 2); } catch { /* keeps raw */ } }
  const lines = body.split("\n");
  const shown = lines.slice(0, MAX_LINES).join("\n");
  const rest = lines.length - MAX_LINES;
  return (
    <>
      <pre className="ap-text">{shown}</pre>
      {(rest > 0 || truncated) && (
        <div className="ap-more">{rest > 0 ? t("chat.textMoreLines", { count: rest }) : t("chat.previewPartial")}</div>
      )}
    </>
  );
}
