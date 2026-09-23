import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { IcLink } from "../panel/icons";

// SHARED rich text editor (markdown, same dialect as chat and card description). A formatting
// toolbar wraps the textarea's selection with markup; portable text is stored and rendered with
// renderRich. Used in EVERY long text field: description, comments, notes… — so the user always
// gets the same editor.

// MdToolbar: just the toolbar. For cases where the textarea already exists and has its own logic
// (e.g. comment composer with @mentions) — calls onWrap(before, after) to wrap the selection.
export function MdToolbar({ onWrap }: { onWrap: (before: string, after: string) => void }) {
  const { t } = useTranslation();
  const b = (title: string, before: string, after: string, node: React.ReactNode) => (
    <button type="button" title={title} onMouseDown={(e) => { e.preventDefault(); onWrap(before, after); }}>{node}</button>
  );
  return (
    <div className="md-tools">
      {b("Bold", "**", "**", <b>B</b>)}
      {b("Italic", "_", "_", <i>I</i>)}
      {b("Strikethrough", "~~", "~~", <s>S</s>)}
      {b("Code", "`", "`", <>{"<>"}</>)}
      {b("List", "\n- ", "", "•")}
      {b(t("webmail.compose.insertLink"), "[", "](https://)", <IcLink size={13} />)}
    </div>
  );
}

// wrapSelection wraps a textarea's current selection with markup and returns the new value, leaving
// the selection where it was (useful for sharing the "wrap" logic outside of MdEditor).
export function wrapSelection(ta: HTMLTextAreaElement | null, value: string, before: string, after: string, onChange: (v: string) => void) {
  if (!ta) return;
  const s = ta.selectionStart, e = ta.selectionEnd;
  onChange(value.slice(0, s) + before + value.slice(s, e) + after + value.slice(e));
  requestAnimationFrame(() => { ta.focus(); ta.selectionStart = s + before.length; ta.selectionEnd = e + before.length; });
}

export function MdEditor({ value, onChange, placeholder, autoFocus, onKeyDown, taRef, minHeight }: {
  value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean;
  onKeyDown?: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void;
  taRef?: React.RefObject<HTMLTextAreaElement>; minHeight?: number;
}) {
  const inner = useRef<HTMLTextAreaElement>(null);
  const ref = taRef ?? inner;
  return (
    <div className="md-editor">
      <MdToolbar onWrap={(b, a) => wrapSelection(ref.current, value, b, a, onChange)} />
      <textarea ref={ref} className="md-ta" value={value} autoFocus={autoFocus}
        style={minHeight ? { minHeight } : undefined}
        onChange={(e) => onChange(e.target.value)} onKeyDown={onKeyDown} placeholder={placeholder} />
    </div>
  );
}
