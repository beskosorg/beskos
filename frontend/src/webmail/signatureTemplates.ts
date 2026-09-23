// 10 HTML signature templates (inline styles — that's what survives in email clients). These are
// starting points: the user picks one and edits it in the rich text editor. The texts are
// placeholders (Your Name, Job Title...) that get replaced directly.
//
// name is an i18n KEY (under "signatures.*"), not display text -- the picker translates it via
// t(tpl.name). The placeholder content inside html (N/R/E/M/P/W below) is not translated: it is
// example text the user immediately overwrites, not persistent UI copy -- lang:ok

export interface SignatureTemplate { id: string; name: string; html: string }

const N = "Seu Nome";
const R = "Cargo";
const E = "Empresa";
const M = "email@empresa.com";
const P = "+55 (11) 90000-0000";
const W = "www.empresa.com";
const AC = "#B4785F"; // accent

export const SIGNATURE_TEMPLATES: SignatureTemplate[] = [
  {
    id: "simple", name: "signatures.simple",
    html: `<div style="font-family:Arial,sans-serif;font-size:13px;color:#333"><b>${N}</b><br>${R} · ${E}<br>${M}</div>`,
  },
  {
    id: "divider", name: "signatures.divider",
    html: `<div style="font-family:Arial,sans-serif;font-size:13px;color:#333"><b>${N}</b><br><span style="color:#777">${R} · ${E}</span><hr style="border:none;border-top:1px solid #ddd;margin:6px 0;max-width:260px"><span style="color:#777">${M} · ${P}</span></div>`,
  },
  {
    id: "accent-bar", name: "signatures.accent_bar",
    html: `<table style="font-family:Arial,sans-serif;font-size:13px;color:#333"><tr><td style="border-left:3px solid ${AC};padding-left:12px"><b>${N}</b><br><span style="color:#777">${R}, ${E}</span><br><a href="mailto:${M}" style="color:${AC}">${M}</a> · ${P}</td></tr></table>`,
  },
  {
    id: "bold-name", name: "signatures.bold_name",
    html: `<div style="font-family:Helvetica,Arial,sans-serif;font-size:13px;color:#333"><span style="font-size:16px;font-weight:700;color:${AC}">${N}</span><br>${R} — ${E}<br>${M} | ${P}</div>`,
  },
  {
    id: "two-col", name: "signatures.two_col",
    html: `<table style="font-family:Arial,sans-serif;font-size:13px;color:#333"><tr><td style="padding-right:14px;border-right:2px solid ${AC}"><b>${N}</b><br>${R}</td><td style="padding-left:14px;color:#777">${E}<br>${M}<br>${P}</td></tr></table>`,
  },
  {
    id: "minimal-gray", name: "signatures.minimal_gray",
    html: `<div style="font-family:Georgia,serif;font-size:13px;color:#555">${N} · ${R}<br>${E} · ${M}</div>`,
  },
  {
    id: "corporate", name: "signatures.corporate",
    html: `<div style="font-family:Arial,sans-serif;font-size:13px;color:#222"><b style="font-size:15px">${N}</b><br><span style="color:${AC};font-weight:600">${R}</span> · ${E}<br>📧 ${M}  📱 ${P}<br>🌐 <a href="https://${W}" style="color:${AC}">${W}</a></div>`,
  },
  {
    id: "quote", name: "signatures.quote",
    html: `<div style="font-family:Arial,sans-serif;font-size:13px;color:#333"><b>${N}</b> — ${R}, ${E}<br>${M}<br><span style="color:#999;font-style:italic;font-size:12px">"A sua frase inspiradora aqui."</span></div>`,
  },
  {
    id: "compact", name: "signatures.compact",
    html: `<div style="font-family:Arial,sans-serif;font-size:12px;color:#444"><b>${N}</b>, ${R} @ ${E} — ${M} — ${P}</div>`,
  },
  {
    id: "card", name: "signatures.card",
    html: `<table style="font-family:Arial,sans-serif;font-size:13px;color:#333;background:#f6f7f8;border:1px solid #e3e5e8;border-radius:8px"><tr><td style="padding:12px 16px"><b style="color:${AC}">${N}</b><br>${R} · ${E}<br><span style="color:#777">${M} · ${P}</span></td></tr></table>`,
  },
];
