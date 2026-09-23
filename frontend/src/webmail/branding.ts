// Applying the organization's brand at runtime: the primary colour overrides the
// --accent variables (and derivatives) via a <style> tag injected with !important, so it wins
// over the theme rules. The logo is used directly in the components (app top bar and login).

/** Beskos mark: shown when the customer has no logo of their own. */
export const BESKOS_MARK = "/beskos-mark.svg";

// Colour palette offered to the admin (name key + hex). "provide a few colours" — no free-form
// colour picker, to guarantee contrast and coherence. `name` is an i18n key under
// `branding.colors.*` (law 4.1) — consumers must run it through `t()` before display.
export const BRAND_COLORS: { id: string; name: string; hex: string }[] = [
  // Black first: it is the accent the product ships with (webmail.css), so the picker opens on
  // what the customer is already looking at rather than proposing a change they didn't ask for.
  // The rest keep their original order and, more importantly, their original hex -- that value is
  // what a tenant has stored, so editing one would silently rebrand whoever picked it.
  { id: "black", name: "branding.colors.black", hex: "#111111" },
  { id: "terracotta", name: "branding.colors.terracotta", hex: "#B4785F" },
  { id: "sage", name: "branding.colors.sage", hex: "#7C8C6A" },
  { id: "slateblue", name: "branding.colors.slateblue", hex: "#6B8299" },
  { id: "mauve", name: "branding.colors.mauve", hex: "#96768A" },
  { id: "teal", name: "branding.colors.teal", hex: "#116466" },
  { id: "blue", name: "branding.colors.blue", hex: "#2563EB" },
  { id: "indigo", name: "branding.colors.indigo", hex: "#4F46E5" },
  { id: "violet", name: "branding.colors.violet", hex: "#7C3AED" },
  { id: "green", name: "branding.colors.green", hex: "#059669" },
  { id: "red", name: "branding.colors.red", hex: "#DC2626" },
  { id: "orange", name: "branding.colors.orange", hex: "#EA580C" },
  { id: "pink", name: "branding.colors.pink", hex: "#DB2777" },
  { id: "slate", name: "branding.colors.slate", hex: "#475569" },
];

const STYLE_ID = "mailin-brand";

// --- Contrast ------------------------------------------------------------------------------
//
// A brand colour cannot be dropped onto either ground and trusted. Black -- the colour the picker
// opens on, because it is what the product ships with -- is invisible on the dark ground, and the
// text that sits on a filled button has to be chosen from the fill, not from the theme: those two
// were decided independently and disagreed, which is how the dark theme ended up with a near-black
// button carrying near-black text and looking disabled.
//
// So both are computed here, together, from the colour itself.

type Rgb = [number, number, number];

const PAGE = {
  // Ground and strongest ink of each theme, from webmail.css.
  light: { bg: [0xFA, 0xFA, 0xFA] as Rgb, ink: [0x0A, 0x0A, 0x0A] as Rgb, own: "#111111" },
  dark: { bg: [0x0A, 0x0A, 0x0A] as Rgb, ink: [0xFA, 0xFA, 0xFA] as Rgb, own: "#FAFAFA" },
};

function parseHex(hex: string): Rgb | null {
  const m = /^#?([0-9a-f]{6}|[0-9a-f]{3})$/i.exec(hex.trim());
  if (!m) return null;
  const h = m[1].length === 3 ? m[1].replace(/./g, (c) => c + c) : m[1];
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

const toHex = (c: Rgb) => "#" + c.map((v) => Math.round(v).toString(16).padStart(2, "0")).join("");
const mix = (a: Rgb, b: Rgb, t: number): Rgb => [0, 1, 2].map((i) => a[i] + (b[i] - a[i]) * t) as Rgb;

// WCAG relative luminance and contrast ratio.
function luminance([r, g, b]: Rgb): number {
  const f = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}
function contrast(a: Rgb, b: Rgb): number {
  const [x, y] = [luminance(a), luminance(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

// HSL saturation, only to answer one question: does this colour carry a hue worth preserving?
function saturation([r, g, b]: Rgb): number {
  const [max, min] = [Math.max(r, g, b) / 255, Math.min(r, g, b) / 255];
  if (max === min) return 0;
  const l = (max + min) / 2;
  return (max - min) / (l > 0.5 ? 2 - max - min : max + min);
}

// The accent this theme should actually use for the brand colour.
//
// A colour with no hue has nothing to preserve, and the product's own accent is exactly that, so a
// grey brand simply becomes the theme's accent -- which is why picking Black now looks the same as
// picking nothing, on both grounds, instead of looking broken on one of them.
//
// A colour with a hue keeps it, and is only moved toward the ground's opposite when it is too close
// to the ground to read as a surface: 3:1 is the floor for a shape, and once it has to move it is
// moved past that, to 4.5:1, so it lands as a button rather than at the threshold of being one.
function accentFor(brand: Rgb, theme: "light" | "dark"): string {
  const page = PAGE[theme];
  if (saturation(brand) < 0.12) return page.own;
  if (contrast(brand, page.bg) >= 3) return toHex(brand);
  const towards: Rgb = theme === "dark" ? [255, 255, 255] : [0, 0, 0];
  for (let t = 0.04; t <= 1; t += 0.04) {
    const c = mix(brand, towards, t);
    if (contrast(c, page.bg) >= 4.5) return toHex(c);
  }
  return toHex(towards);
}

// The label and icons on a filled button: whichever of the two extremes the fill can carry.
const onAccentFor = (fill: Rgb): string =>
  contrast(fill, [0x0A, 0x0A, 0x0A]) >= contrast(fill, [0xFF, 0xFF, 0xFF]) ? "#0A0A0A" : "#FFFFFF";

function blockFor(color: string, theme: "light" | "dark"): string {
  const brand = parseHex(color);
  const page = PAGE[theme];
  const accent = brand ? accentFor(brand, theme) : page.own;
  const fill = parseHex(accent) as Rgb;
  return (
    `--accent:${accent}!important;` +
    `--accent-solid:${accent}!important;` +
    `--on-accent:${onAccentFor(fill)}!important;` +
    // Strong text on the page ground: the accent pushed a quarter of the way to the theme's ink, so
    // it keeps the hue and stays readable where it is used as text rather than as a fill.
    `--accent-ink:${toHex(mix(fill, page.ink, 0.25))}!important;` +
    `--accent-tint:${theme === "dark" ? "rgba(255,255,255,.09)" : `color-mix(in srgb, ${accent} 16%, transparent)`}!important;`
  );
}

// applyBrandColor overrides the accent colour across the whole app. An empty colour removes the
// override and returns to the theme's own accent.
export function applyBrandColor(color: string): void {
  let el = document.getElementById(STYLE_ID) as HTMLStyleElement | null;
  if (!color) { el?.remove(); return; }
  if (!el) { el = document.createElement("style"); el.id = STYLE_ID; document.head.appendChild(el); }
  const light = blockFor(color, "light");
  const dark = blockFor(color, "dark");
  // Same cascade as the theme (webmail.css): media query as the base, explicit data-theme wins.
  const scope = ".wm,.pnl,.sg";
  el.textContent =
    `${scope}{${light}}` +
    `@media (prefers-color-scheme:dark){${scope}{${dark}}}` +
    `:root[data-theme="dark"] ${scope}{${dark}}` +
    `:root[data-theme="light"] ${scope}{${light}}`;
}

// Local cache of the brand (so login shows it immediately on return, without waiting on the network).
const LS_BRAND = "mailin.brand";
export function cacheBrand(b: { name: string; color: string; logo: string; logo_white_dark?: boolean }): void {
  try { localStorage.setItem(LS_BRAND, JSON.stringify(b)); } catch { /* private mode */ }
}
export function cachedBrand(): { name: string; color: string; logo: string; logo_white_dark?: boolean } | null {
  try { const r = localStorage.getItem(LS_BRAND); return r ? JSON.parse(r) : null; } catch { return null; }
}
