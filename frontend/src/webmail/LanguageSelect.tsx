import { SUPPORTED, ENDONYMS, normalizeLanguage } from "../i18n/supported";

// The one control for choosing the interface language: settings, the public booking page and the
// meeting guest page. There used to be three -- a two-option select in settings and two copies of a
// "PT | EN" button bar -- which a fourteenth language does not fit into.
//
// Options are endonyms, each tagged with its own `lang` so a screen reader pronounces "Deutsch" as
// German and "日本語" as Japanese rather than reading them in the interface's current voice.
export function LanguageSelect({
  value,
  onChange,
  className,
  label,
}: {
  value: string;
  onChange: (language: string) => void;
  className?: string;
  label?: string;
}) {
  const current = normalizeLanguage(value);
  return (
    <select className={className} value={current} aria-label={label} onChange={(e) => onChange(e.target.value)}>
      {SUPPORTED.map((code) => (
        <option key={code} value={code} lang={code}>
          {ENDONYMS[code]}
        </option>
      ))}
    </select>
  );
}
