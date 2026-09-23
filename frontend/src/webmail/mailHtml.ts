import DOMPurify from "dompurify";

// Sanitization of email HTML. This is NOT done by hand: the body comes from a hostile sender by
// definition, and a homemade filter always lets a vector through (svg onload, javascript: in href,
// css expression, …). DOMPurify is the responsible choice — this is a security dependency,
// not a convenience one (law §2.5).

export interface SanitizedMail {
  html: string;
  blockedImages: number; // how many external images were removed (to offer "show")
}

// Single hook: neutralizes links and images BEFORE DOMPurify returns the result.
let hookInstalled = false;
function installHook() {
  if (hookInstalled) return;
  hookInstalled = true;
  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    // Every link opens in a new tab without passing the referer/opener — an email doesn't control our tab.
    if (node.tagName === "A") {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer nofollow");
    }
    // External images are trackers until proven otherwise (the "pixel" that confirms the open).
    // We keep the original src in data-src and remove the real one; the UI decides whether to load them.
    if (node.tagName === "IMG") {
      const src = node.getAttribute("src") ?? "";
      if (/^https?:/i.test(src)) {
        node.setAttribute("data-blocked-src", src);
        node.removeAttribute("src");
      } else if (!src.startsWith("cid:") && !src.startsWith("data:")) {
        node.removeAttribute("src"); // odd schemes: out
      }
    }
  });
}

const CONFIG = {
  // Allow-list: only what makes sense in an email. No <script>, <iframe>, <object>, <form>, <style>
  // with global selectors, etc. FORBID_TAGS is redundant with the allow-list, but makes the intent clear.
  ALLOWED_TAGS: [
    "a", "b", "strong", "i", "em", "u", "s", "strike", "sub", "sup", "br", "hr", "p", "div", "span",
    "blockquote", "pre", "code", "ul", "ol", "li", "dl", "dt", "dd",
    "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "colgroup", "col",
    "h1", "h2", "h3", "h4", "h5", "h6", "img", "figure", "figcaption", "font", "center", "small",
  ],
  ALLOWED_ATTR: [
    "href", "title", "alt", "src", "width", "height", "align", "valign", "border", "cellpadding",
    "cellspacing", "colspan", "rowspan", "bgcolor", "color", "face", "size", "dir", "style", "target", "rel",
  ],
  FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button", "style", "link", "meta", "base"],
  FORBID_ATTR: ["srcset", "onerror", "onload", "onclick", "formaction"],
  ALLOW_DATA_ATTR: false,
  ADD_ATTR: ["target", "data-blocked-src"],
  // No javascript:/vbscript:/data:text-html in href — DOMPurify already blocks it, but being explicit is better.
  ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto|tel|cid):|[^a-z]|[a-z+.-]+(?:[^a-z+.\-:]|$))/i,
};

// sanitize returns the safe HTML and how many external images were blocked.
export function sanitize(html: string): SanitizedMail {
  installHook();
  const clean = DOMPurify.sanitize(html, CONFIG as Parameters<typeof DOMPurify.sanitize>[1]) as unknown as string;
  const blockedImages = (clean.match(/data-blocked-src=/g) ?? []).length;
  return { html: clean, blockedImages };
}

// sanitizeRich cleans up internal collaborative-content HTML before injecting it via innerHTML
// (chat canvas, signatures). The author is a colleague, not an anonymous sender, so we keep
// formatting and inline images/links — but we NEVER trust third-party HTML: DOMPurify removes
// <script>, hostile SVG, and event handlers (onerror/onload/…), which is the real stored-XSS
// vector here.
export function sanitizeRich(html: string): string {
  return DOMPurify.sanitize(html, {
    FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button", "style", "link", "meta", "base"],
    FORBID_ATTR: ["onerror", "onload", "onclick", "onmouseover", "onfocus", "formaction", "srcset"],
    ADD_ATTR: ["target"],
  }) as unknown as string;
}

// unblockImages returns the same HTML with the external images restored — called only when the
// person clicks "show images", a conscious decision to accept the tracking on that email.
export function unblockImages(html: string): string {
  return html.replace(/data-blocked-src="([^"]*)"/g, (_m, src) => `src="${src}"`);
}
