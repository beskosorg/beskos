import { describe, it, expect } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { SUPPORTED, ENDONYMS, normalizeLanguage } from "./supported";

// i18n guard (law 4.2): every language carries every string, in the grammatical forms it needs.
//
// "Exactly the same keys as English" stopped being the right test once there were fourteen
// languages. Plural keys differ by design: Japanese has one form where English has two and Arabic
// has six. So keys are compared after folding plural suffixes, and each language is then required to
// have precisely the forms its own Intl.PluralRules asks for -- which is what i18next looks up at
// runtime, so a missing one is a real grammatical error on screen, not a style point.

type Dict = Record<string, unknown>;
const DIR = path.join(__dirname, "locales");
const load = (lng: string): Dict => JSON.parse(fs.readFileSync(path.join(DIR, lng, "common.json"), "utf8"));

function leaves(obj: Dict, prefix = ""): [string, unknown][] {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k;
    return v && typeof v === "object" && !Array.isArray(v) ? leaves(v as Dict, key) : [[key, v] as [string, unknown]];
  });
}

const FORMS = /_(zero|one|two|few|many|other)$/;
const en = Object.fromEntries(leaves(load("en")));
// A plural group is a base that English spells with both _one and _other. A key that merely ends
// in a plural word -- server.mfa.too_many -- is not one.
const pluralBases = new Set(
  Object.keys(en).filter((k) => k.endsWith("_one") && `${k.slice(0, -4)}_other` in en).map((k) => k.slice(0, -4)),
);
const fold = (k: string) => {
  const m = k.match(FORMS);
  return m && pluralBases.has(k.slice(0, -m[0].length)) ? k.slice(0, -m[0].length) : k;
};
const placeholders = (s: unknown): string[] => (typeof s === "string" ? (s.match(/\{\{\s*[^}]+?\s*\}\}/g) ?? []).sort() : []);

describe("i18n locales", () => {
  it("every supported language has a dictionary, and every dictionary is supported", () => {
    const dirs = fs.readdirSync(DIR).filter((d) => fs.existsSync(path.join(DIR, d, "common.json"))).sort();
    expect(dirs).toEqual([...SUPPORTED].sort());
    expect(Object.keys(ENDONYMS).sort()).toEqual([...SUPPORTED].sort());
  });

  const enFolded = new Set(Object.keys(en).map(fold));

  for (const lng of SUPPORTED) {
    describe(lng, () => {
      const dict = Object.fromEntries(leaves(load(lng)));

      it("has the same strings as English", () => {
        const folded = new Set(Object.keys(dict).map(fold));
        const missing = [...enFolded].filter((k) => !folded.has(k));
        const extra = [...folded].filter((k) => !enFolded.has(k));
        expect({ missing, extra }).toEqual({ missing: [], extra: [] });
      });

      it("has exactly the plural forms its grammar needs", () => {
        const need = new Intl.PluralRules(lng).resolvedOptions().pluralCategories.slice().sort();
        const wrong: string[] = [];
        for (const base of pluralBases) {
          const have = Object.keys(dict)
            .filter((k) => k.startsWith(`${base}_`) && FORMS.test(k) && fold(k) === base)
            .map((k) => k.slice(base.length + 1))
            .sort();
          if (have.join() !== need.join()) wrong.push(`${base}: ${have.join(",")} (needs ${need.join(",")})`);
        }
        expect(wrong).toEqual([]);
      });

      it("keeps every placeholder", () => {
        const lost: string[] = [];
        for (const [k, v] of Object.entries(dict)) {
          const src = en[k] ?? en[`${fold(k)}_other`];
          // _zero, _one and _two may spell the number out instead of printing it.
          const optional = /_(zero|one|two)$/.test(k) && pluralBases.has(fold(k));
          const want = placeholders(src).filter((p) => !(optional && p.includes("count")));
          const have = placeholders(v);
          if (want.some((p) => !have.includes(p))) lost.push(`${k}: ${want.join(" ")} -> ${have.join(" ")}`);
        }
        expect(lost).toEqual([]);
      });

      it("has no empty strings", () => {
        expect(Object.entries(dict).filter(([, v]) => v === "").map(([k]) => k)).toEqual([]);
      });
    });
  }
});

describe("normalizeLanguage", () => {
  it.each([
    ["pt-PT", "pt-PT"], ["pt-pt", "pt-PT"], ["pt", "pt-BR"], ["pt-BR", "pt-BR"], ["pt-AO", "pt-BR"],
    ["zh", "zh-CN"], ["zh-TW", "zh-CN"], ["zh-Hans-CN", "zh-CN"],
    ["no", "nb"], ["nn-NO", "nb"], ["nb-NO", "nb"],
    ["de-AT", "de"], ["es-419", "es"], ["en-GB", "en"], ["ar-SA", "ar"], ["fr_CA", "fr"],
  ])("%s -> %s", (input, want) => expect(normalizeLanguage(input)).toBe(want));

  // Unsupported stays as it is, so the detector falls through to the browser's next preference
  // rather than landing on a default ahead of a language the user actually listed.
  it("leaves an unsupported language alone", () => expect(normalizeLanguage("ko-KR")).toBe("ko-KR"));
});
