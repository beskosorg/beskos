// The languages the interface ships in. One list, read by the i18n setup, the language picker and
// the tests -- so a locale directory with no picker entry, or a picker entry with no dictionary,
// fails a test instead of shipping.
//
// Each language is named in itself (its endonym), and that name is the same in every locale. The
// picker is the way back for somebody who switched to a language they cannot read; "日本語" has to
// look like "日本語" whatever the interface is currently in.

export const SUPPORTED = [
  "pt-BR", "en", "pt-PT", "es", "fr", "de", "nl", "pl", "cs", "sv", "nb", "zh-CN", "ja", "ar",
] as const;

export type SupportedLanguage = (typeof SUPPORTED)[number];

// lang:ok -- endonyms: each language's own name for itself, identical in every locale.
export const ENDONYMS: Record<SupportedLanguage, string> = {
  "pt-BR": "Português (Brasil)", // lang:ok
  en: "English",
  "pt-PT": "Português (Portugal)", // lang:ok
  es: "Español", // lang:ok
  fr: "Français", // lang:ok
  de: "Deutsch",
  nl: "Nederlands",
  pl: "Polski",
  cs: "Čeština", // lang:ok
  sv: "Svenska",
  nb: "Norsk bokmål", // lang:ok
  "zh-CN": "简体中文", // lang:ok
  ja: "日本語", // lang:ok
  ar: "العربية", // lang:ok
};

// normalizeLanguage maps what a browser reports onto a supported code.
//
// Browsers report regional and script variants the dictionaries do not split: de-AT, es-419, en-GB,
// ar-SA. They collapse to their base language. Three need more than that. Portuguese has two
// dictionaries, so pt-PT stays apart and every other Portuguese is Brazilian. Chinese has one --
// Simplified -- so zh-TW and zh-Hant also land on it. Norwegian arrives as no, nb or nn, and Bokmål
// is the written standard the dictionary uses.
//
// An unsupported code is returned unchanged rather than mapped to a default, so the detector moves
// on to the browser's next preference: someone whose browser lists Korean and then English gets
// English, not the default.
export function normalizeLanguage(code: string): string {
  const c = code.trim().toLowerCase().replace("_", "-");
  if (c === "pt-pt") return "pt-PT";
  if (c === "pt" || c.startsWith("pt-")) return "pt-BR";
  if (c === "zh" || c.startsWith("zh-")) return "zh-CN";
  if (c === "no" || c === "nb" || c === "nn" || /^(no|nb|nn)-/.test(c)) return "nb";
  const exact = SUPPORTED.find((s) => s.toLowerCase() === c);
  if (exact) return exact;
  const base = SUPPORTED.find((s) => s.toLowerCase() === c.split("-")[0]);
  return base ?? code;
}

// Written right to left. The interface mirrors for these through `dir` on the document.
export const RTL = new Set<string>(["ar"]);
