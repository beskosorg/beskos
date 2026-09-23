import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { backend } from "../webmail/backend";
import { BRAND_COLORS, applyBrandColor } from "../webmail/branding";
import { toast } from "../panel/ui";
import { Switch } from "../webmail/Switch";

// Organization appearance (admin/owner only -- RBAC org.branding): primary color + logo. The
// logo appears at the top of the app and on the login screen, the same idea as Google Workspace.
// Pro only: mounted by ../panel/BrandingSectionGate.tsx behind the branding capability. The
// backend enforces the same boundary independently (ee/branding_pro.go) -- this gate is UX, not
// the security line.
export function BrandingSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [color, setColor] = useState("");
  const [logo, setLogo] = useState("");
  const [logoWhiteDark, setLogoWhiteDark] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const fileIn = useRef<HTMLInputElement>(null);

  useEffect(() => {
    backend.brandingGet().then((b) => { setColor(b.color); setLogo(b.logo); setLogoWhiteDark(b.logo_white_dark ?? false); }).catch(() => {}).finally(() => setLoading(false));
  }, []);

  // Preview the color in the app while editing; on leaving without saving, App reapplies the saved one.
  useEffect(() => { applyBrandColor(color); }, [color]);

  const onFile = (files: FileList | null) => {
    const f = files?.[0];
    if (!f) return;
    if (!f.type.startsWith("image/")) { toast(t("branding.notImage")); return; }
    // Resize to a max height of 80px (retina top bar) → small data URI.
    const img = new Image();
    const url = URL.createObjectURL(f);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const h = Math.min(80, img.height), w = Math.round(img.width * (h / img.height));
      const cv = document.createElement("canvas"); cv.width = w; cv.height = h;
      cv.getContext("2d")?.drawImage(img, 0, 0, w, h);
      setLogo(cv.toDataURL("image/png"));
    };
    img.onerror = () => { URL.revokeObjectURL(url); toast(t("branding.notImage")); };
    img.src = url;
  };

  const save = async () => {
    setSaving(true);
    try {
      await backend.brandingUpdate({ color, logo, logo_white_dark: logoWhiteDark });
      qc.invalidateQueries({ queryKey: ["branding"] });
      toast(t("branding.saved"));
    } catch (e) { toast((e as Error).message); }
    finally { setSaving(false); }
  };

  if (loading) return <div className="loading">…</div>;
  return (
    <>
      <div className="pnl-top"><div><h1>{t("panel.branding")}</h1><div className="desc">{t("branding.subtitle")}</div></div></div>
      <div className="pnl-content">
        <div className="pnl-card"><div className="pnl-card-h"><h3>{t("branding.color")}</h3></div>
          <div className="pnl-card-b">
            <div className="brand-swatches">
              {BRAND_COLORS.map((c) => (
                <button key={c.id} type="button" title={t(c.name)}
                  className={"brand-swatch" + (color.toLowerCase() === c.hex.toLowerCase() ? " on" : "")}
                  style={{ background: c.hex }} onClick={() => setColor(c.hex)}>
                  {color.toLowerCase() === c.hex.toLowerCase() && <span>✓</span>}
                </button>
              ))}
              <button type="button" className={"brand-swatch none" + (color === "" ? " on" : "")}
                title={t("branding.default")} onClick={() => setColor("")}>—</button>
            </div>
            <p className="desc">{t("branding.colorHint")}</p>
          </div>
        </div>

        <div className="pnl-card"><div className="pnl-card-h"><h3>{t("branding.logo")}</h3></div>
          <div className="pnl-card-b">
            <div className="brand-logo-row">
              {/* Preview in BOTH themes: light and dark side by side — the dark one applies
                  the white filter live when the option is enabled. */}
              <div className="brand-logo-duo">
                <div className="brand-logo-preview light">
                  {logo ? <img src={logo} alt="logo" /> : <span className="desc">{t("branding.noLogo")}</span>}
                  <span className="tag">{t("branding.previewLight")}</span>
                </div>
                <div className="brand-logo-preview dark">
                  {logo ? <img src={logo} alt="logo" style={logoWhiteDark ? { filter: "brightness(0) invert(1)" } : undefined} /> : <span className="desc">{t("branding.noLogo")}</span>}
                  <span className="tag">{t("branding.previewDark")}</span>
                </div>
              </div>
              <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
                <button className="pnl-btn" onClick={() => fileIn.current?.click()}>{t("branding.upload")}</button>
                {logo && <button className="pnl-btn danger" onClick={() => setLogo("")}>{t("branding.remove")}</button>}
              </div>
            </div>
            <p className="desc">{t("branding.logoHint")}</p>
            <div className="setrow" style={{ borderTop: "1px solid var(--border)", marginTop: 10 }}>
              <div className="setrow-tx">
                <b>{t("branding.logoWhiteDark")}</b>
                <span className="desc">{t("branding.logoWhiteDarkHint")}</span>
              </div>
              <Switch checked={logoWhiteDark} onChange={setLogoWhiteDark} label={t("branding.logoWhiteDark")} />
            </div>
            <input ref={fileIn} type="file" accept="image/*" hidden onChange={(e) => { onFile(e.target.files); e.target.value = ""; }} />
          </div>
        </div>

        <div className="brand-actions">
          <button className="pnl-btn primary" disabled={saving} onClick={save}>{saving ? "…" : t("branding.save")}</button>
        </div>
      </div>
    </>
  );
}
