// Icon per file type (monochrome, stroke currentColor). Different per extension.
type P = { size?: number };
const S = (size = 22) => ({
  width: size, height: size, viewBox: "0 0 24 24", fill: "none",
  stroke: "currentColor", strokeWidth: 1.7, strokeLinecap: "round" as const, strokeLinejoin: "round" as const,
});
const Base = () => (<><path d="M6 2h8l4 4v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2Z" /><path d="M14 2v4h4" /></>);

const Doc = (p: P) => (<svg {...S(p.size)}><Base /><path d="M8 13h8M8 17h8M8 9h3" /></svg>);
const Pdf = (p: P) => (<svg {...S(p.size)}><Base /><path d="M8 12h1.5a1.5 1.5 0 0 1 0 3H8v-3ZM8 15v3" /><path d="M14 12v6M14 12h2M14 15h1.5" /></svg>);
const Sheet = (p: P) => (<svg {...S(p.size)}><Base /><path d="M8 11h8M8 15h8M11 11v7" /></svg>);
const Img = (p: P) => (<svg {...S(p.size)}><Base /><circle cx="9.5" cy="12" r="1.3" /><path d="m7 18 3-3 2 2 2.5-2.5L18 18" /></svg>);
const Zip = (p: P) => (<svg {...S(p.size)}><Base /><path d="M10 6v1M12 8v1M10 10v1M12 12v1M10 14v3h2v-3" /></svg>);
const Video = (p: P) => (<svg {...S(p.size)}><Base /><path d="m10 12 4 2.5-4 2.5v-5Z" /></svg>);
const Audio = (p: P) => (<svg {...S(p.size)}><Base /><path d="M10 17v-5l4-1v5" /><circle cx="9" cy="17" r="1.2" /><circle cx="13" cy="16" r="1.2" /></svg>);
const Code = (p: P) => (<svg {...S(p.size)}><Base /><path d="m10 12-2 2 2 2M14 12l2 2-2 2" /></svg>);
const Generic = (p: P) => (<svg {...S(p.size)}><Base /></svg>);

const MAP: Record<string, (p: P) => JSX.Element> = {
  pdf: Pdf,
  doc: Doc, docx: Doc, txt: Doc, rtf: Doc, md: Doc, odt: Doc,
  xls: Sheet, xlsx: Sheet, csv: Sheet, ods: Sheet,
  png: Img, jpg: Img, jpeg: Img, gif: Img, webp: Img, svg: Img, heic: Img,
  zip: Zip, rar: Zip, "7z": Zip, tar: Zip, gz: Zip,
  mp4: Video, mov: Video, avi: Video, mkv: Video, webm: Video,
  mp3: Audio, wav: Audio, flac: Audio, ogg: Audio, m4a: Audio,
  js: Code, ts: Code, json: Code, html: Code, css: Code, py: Code, go: Code, xml: Code,
};

export function FileTypeIcon({ name, size }: { name: string; size?: number }) {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  const Ic = MAP[ext] ?? Generic;
  return <Ic size={size} />;
}

// Category for the "Type" filter (Drive-style chips).
const CAT: Record<string, string> = {
  pdf: "pdf",
  doc: "doc", docx: "doc", txt: "doc", rtf: "doc", md: "doc", odt: "doc",
  xls: "sheet", xlsx: "sheet", csv: "sheet", ods: "sheet",
  png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", svg: "image", heic: "image",
  zip: "archive", rar: "archive", "7z": "archive", tar: "archive", gz: "archive",
  mp4: "video", mov: "video", avi: "video", mkv: "video", webm: "video",
  mp3: "audio", wav: "audio", flac: "audio", ogg: "audio", m4a: "audio",
  js: "code", ts: "code", json: "code", html: "code", css: "code", py: "code", go: "code", xml: "code",
};
export function fileCategory(name: string): string {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  return CAT[ext] ?? "other";
}
