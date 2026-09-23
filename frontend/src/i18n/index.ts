/// <reference types="vite/client" />
// i18n setup (law 4). Default pt-BR, en mandatory. Detects the browser's language; a preference
// saved in the account wins over it (App.tsx applies it once the settings arrive).
import i18n, { type BackendModule } from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { SUPPORTED, RTL, normalizeLanguage } from "./supported";

// Each dictionary is its own chunk, fetched when that language is first used. Bundled together,
// fourteen of them add about 1.7 MB to what every user downloads before the first screen, to read
// one. Vite's glob import gives the loaders; this backend is the whole adapter i18next needs, so
// there is no plugin to add for it (law 2.5).
const loaders = import.meta.glob<{ default: Record<string, unknown> }>("./locales/*/common.json");

const lazyLocales: BackendModule = {
  type: "backend",
  init() {},
  read(language, _namespace, callback) {
    const load = loaders[`./locales/${language}/common.json`];
    if (!load) {
      callback(new Error(`i18n: no dictionary for ${language}`), null);
      return;
    }
    load().then(
      (m) => callback(null, m.default),
      (err: unknown) => callback(err as Error, null),
    );
  },
};

// The document follows the language: `lang` for screen readers, hyphenation and font selection;
// `dir` so that Arabic mirrors the whole interface. The stylesheets use logical properties
// (margin-inline-start, inset-inline-end) for exactly this reason.
function applyToDocument(lng: string) {
  // i18n also runs where there is no document -- the unit tests run in Node.
  if (typeof document === "undefined") return;
  const el = document.documentElement;
  el.lang = lng;
  el.dir = RTL.has(lng) ? "rtl" : "ltr";
}
i18n.on("languageChanged", applyToDocument);

// Resolves once the active language's dictionary has arrived. main.tsx waits for it before the
// first render: without that, the first screen paints in the fallback language and switches a
// moment later.
export const i18nReady = i18n
  .use(lazyLocales)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    supportedLngs: [...SUPPORTED],
    // Load "zh-CN" only -- never also try "zh", which has no dictionary.
    load: "currentOnly",
    fallbackLng: "pt-BR",
    defaultNS: "common",
    ns: ["common"],
    detection: {
      convertDetectedLanguage: normalizeLanguage,
    },
    interpolation: { escapeValue: false }, // React already escapes
    react: { useSuspense: false },
  });

export default i18n;
