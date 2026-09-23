import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { SIGNATURE_TEMPLATES } from "./signatureTemplates";
import { sanitizeRich } from "./mailHtml";
import { IcLink, IcImage } from "../panel/icons";

// HTML signature editor: basic formatting, image upload (inline, data URI), and a gallery of
// 10 templates that load into the editor for further editing. The value is the innerHTML.
export function SignatureEditor({ value, onChange }: { value: string; onChange: (html: string) => void }) {
  const { t } = useTranslation();
  const ed = useRef<HTMLDivElement>(null);
  const imgIn = useRef<HTMLInputElement>(null);
  const [gallery, setGallery] = useState(false);

  // Seeds the editor with the saved value only once (contentEditable is not controlled by React).
  useEffect(() => {
    if (ed.current && value && !ed.current.innerHTML) ed.current.innerHTML = sanitizeRich(value);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  const emit = () => onChange(ed.current?.innerHTML ?? "");
  const exec = (e: React.MouseEvent, cmd: string, val?: string) => { e.preventDefault(); document.execCommand(cmd, false, val); ed.current?.focus(); emit(); };

  const insertLink = () => {
    const url = window.prompt(t("webmail.compose.linkPrompt"), "https://");
    if (!url) return;
    ed.current?.focus();
    const sel = window.getSelection();
    if (sel && sel.toString()) document.execCommand("createLink", false, url);
    else document.execCommand("insertHTML", false, `<a href="${url}">${url}</a>`);
    emit();
  };
  const onImg = (files: FileList | null) => {
    const f = files?.[0]; if (!f) return;
    const r = new FileReader();
    r.onload = () => { ed.current?.focus(); document.execCommand("insertHTML", false, `<img src="${r.result}" style="max-width:220px">`); emit(); };
    r.readAsDataURL(f);
  };
  const loadTemplate = (html: string) => { if (ed.current) { ed.current.innerHTML = html; emit(); } setGallery(false); };

  return (
    <div className="sig-editor">
      <div className="tools sig-tools">
        <button title="Bold" onMouseDown={(e) => exec(e, "bold")}><b>B</b></button>
        <button title="Italic" onMouseDown={(e) => exec(e, "italic")}><i>I</i></button>
        <button title="Underline" onMouseDown={(e) => exec(e, "underline")} style={{ textDecoration: "underline" }}>U</button>
        <label className="color-btn" title={t("webmail.compose.textColor")}>
          <b style={{ borderBottom: "3px solid currentColor", lineHeight: 1 }}>A</b>
          <input type="color" onChange={(e) => { ed.current?.focus(); document.execCommand("foreColor", false, e.target.value); emit(); }} />
        </label>
        <span className="sep" />
        <button title={t("webmail.compose.insertLink")} onMouseDown={(e) => { e.preventDefault(); insertLink(); }}><IcLink size={15} /></button>
        <button title={t("webmail.compose.insertImage")} onMouseDown={(e) => { e.preventDefault(); imgIn.current?.click(); }}><IcImage size={15} /></button>
        <button title="Clear" onMouseDown={(e) => exec(e, "removeFormat")}>⌫</button>
        <span className="grow" />
        <button className={"chipbtn" + (gallery ? " on" : "")} onMouseDown={(e) => { e.preventDefault(); setGallery((v) => !v); }}>{t("settings.sigTemplates")}</button>
      </div>

      {gallery && (
        <div className="sig-gallery">
          {SIGNATURE_TEMPLATES.map((tpl) => (
            <button key={tpl.id} className="sig-card" onClick={() => loadTemplate(tpl.html)}>
              <div className="sig-preview" dangerouslySetInnerHTML={{ __html: tpl.html }} />
              <span className="sig-name">{t(tpl.name)}</span>
            </button>
          ))}
        </div>
      )}

      <div className="sig-surface" ref={ed} contentEditable suppressContentEditableWarning
        onInput={emit} data-ph={t("settings.sigPlaceholder")} />
      <input ref={imgIn} type="file" accept="image/*" hidden onChange={(e) => { onImg(e.target.files); e.target.value = ""; }} />
    </div>
  );
}
