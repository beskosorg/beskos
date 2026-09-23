import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type { MailService } from "../jmap/mail";
import type { EmailFull } from "../jmap/types";
import { sanitize, unblockImages } from "./mailHtml";
import { IcMail } from "../panel/icons";

// Email body. HTML when it exists (sanitized), otherwise plain text.
//
// The HTML runs in a SANDBOXED IFRAME: even after sanitizing, an email's CSS would clash with the
// app's, and the sandbox guarantees that nothing from it — not even a script that escaped — touches
// the rest of the page. The sandbox does NOT include allow-scripts, so NO email script runs. It
// includes allow-same-origin (only) so the parent can measure the content's height and resize the
// iframe (without it, contentDocument is null). Without allow-scripts, same-origin grants no
// executable access — it's safe.
export function MailBody({ m, service, textBody, spam }: { m: EmailFull; service: MailService; textBody: string; spam?: boolean }) {
  const { t } = useTranslation();
  // Only really HTML if the part is text/html. In a text-only email, Stalwart points htmlBody
  // at the text/plain part — in that case it renders as text (no iframe, blends into the theme).
  const htmlMeta = m.htmlBody?.[0];
  const rawHtml = htmlMeta?.type === "text/html" ? m.bodyValues[htmlMeta.partId]?.value : undefined;

  if (!rawHtml) return <div className="rbody">{textBody}</div>;
  return <HtmlBody html={rawHtml} m={m} service={service} t={t} spam={!!spam} />;
}

// Active theme: the data-theme on <html> (set by the toggle) takes precedence; otherwise, the OS preference.
function isDarkTheme(): boolean {
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "dark") return true;
  if (attr === "light") return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function HtmlBody({ html, m, service, t, spam }: {
  html: string; m: EmailFull; service: MailService; t: TFunction; spam: boolean;
}) {
  const [showImages, setShowImages] = useState(false);
  const [height, setHeight] = useState(120);
  const [cidMap, setCidMap] = useState<Record<string, string>>({});
  const ref = useRef<HTMLIFrameElement>(null);
  const dark = isDarkTheme();

  const { html: clean, blockedImages } = useMemo(() => sanitize(html), [html]);

  // Embedded (cid:) images are attachments of the email itself — they aren't trackers, so they
  // always load. Resolves each cid: to an authenticated blob:.
  useEffect(() => {
    if (spam) return; // in spam, not even embedded (cid) images load
    const cids = m.attachments?.filter((a) => a.cid) ?? [];
    if (!cids.length) return;
    let alive = true;
    const urls: string[] = [];
    (async () => {
      const map: Record<string, string> = {};
      for (const a of cids) {
        try {
          const url = await service.attachmentObjectUrl(a.blobId, a.name ?? "img", a.type);
          map[`cid:${a.cid}`] = url; urls.push(url);
        } catch { /* broken cid attachment: goes without the image, doesn't break the rest */ }
      }
      if (alive) setCidMap(map);
    })();
    return () => { alive = false; urls.forEach((u) => URL.revokeObjectURL(u)); };
  }, [m, service]);

  // Builds the iframe's document: cleaned HTML, with external images restored only if requested
  // and cid: resolved. base target=_blank so links without an explicit target open externally.
  const doc = useMemo(() => {
    // In spam, images are never unblocked; cids also aren't resolved (cidMap stays empty).
    let body = (showImages && !spam) ? unblockImages(clean) : clean;
    for (const [cid, url] of Object.entries(cidMap)) body = body.split(cid).join(url);
    // In spam, links are disabled (pointer-events:none): nothing gets clicked from a suspicious sender.
    const linkCss = spam ? "a{color:#8a8a8a;pointer-events:none;cursor:default;text-decoration:underline}" : "a{color:#116466}";
    // Dark mode: inverts the email (light background→dark, dark text→light) with hue-rotate to
    // preserve the hues, and RE-inverts images/videos/logos so they don't end up as negatives. It's
    // the "Dark Reader" technique — not perfect on very rich emails, but blends well with the theme.
    // The dark inversion goes on an inner WRAPPER (#mroot), NOT on the body: filtering the body
    // breaks height measurement (scrollHeight comes out wrong → short iframe with scroll). On the
    // wrapper, the body measures its height correctly. Images are re-inverted so they don't end up
    // as negatives.
    // TRANSPARENT iframe background: lets the reading panel's black (--paper) show through, so the
    // email blends into the page instead of adding a tone of its own (the panel, the iframe, and
    // the content were 3 different blacks). The content lives in #mroot (inverted); only the outer
    // background is transparent.
    // invert(0.93), not invert(1): full inversion takes the email's white to pure #000 — darker
    // than the site's black (--paper #121313), which makes the email look like a highlighted card.
    // At 0.93, white maps to ~#121313 (blends with the page) and the text stays light. Images/logos
    // are re-inverted so they don't end up as negatives (slightly softer contrast, acceptable in
    // dark mode).
    const themeCss = dark
      ? `html{color-scheme:dark}html,body{background:transparent}` +
        `#mroot{background:#fff;filter:invert(0.93) hue-rotate(180deg)}` +
        `#mroot img,#mroot picture,#mroot video,#mroot svg,#mroot [style*="background-image"]{filter:invert(1) hue-rotate(180deg)}`
      : // Light theme: background is TRANSPARENT too, so the email blends with the page's --paper
        // instead of standing out as a white card. Rich emails that set their own background
        // still show it (only the outer background is transparent).
        `html{color-scheme:light}html,body{background:transparent}`;
    // #mroot in flow-root: creates a formatting context that INCLUDES children's margins in its
    // height (otherwise the last element's margin "leaks through" and the measurement comes up
    // short → scroll).
    return `<!doctype html><html><head><meta charset="utf-8"><base target="_blank">` +
      `<style>${themeCss}html,body{margin:0;padding:0;color:#1c1b1a;` +
      `font:14px/1.5 -apple-system,system-ui,sans-serif;word-wrap:break-word;overflow-wrap:break-word}` +
      `#mroot{display:flow-root}img{max-width:100%;height:auto}${linkCss}` +
      `table{max-width:100%}</style>` +
      `</head><body><div id="mroot">${body}</div></body></html>`;
  }, [clean, showImages, cidMap, spam, dark]);

  // Adjusts the iframe's height to the content — the email shouldn't have its own internal
  // scroll, since the reader itself scrolls. A ResizeObserver INSIDE the iframe tracks any height
  // change (images loading, fonts, late layout) that a one-off measurement in onLoad would miss.
  const onLoad = () => {
    const win = ref.current?.contentWindow as (Window & typeof globalThis) | null;
    const d = ref.current?.contentDocument;
    if (!win || !d) return;
    // Measures the #mroot WRAPPER directly (getBoundingClientRect): it wraps all the content and
    // its height is the real one, without the noise of the body's filter/scrollHeight.
    const measure = () => {
      const root = d.getElementById("mroot");
      const h = Math.max(
        root ? Math.ceil(root.getBoundingClientRect().height) : 0,
        root?.scrollHeight ?? 0,
        d.body?.scrollHeight ?? 0,
        d.documentElement?.scrollHeight ?? 0,
      );
      if (h) setHeight((prev) => (Math.abs(prev - (h + 8)) > 2 ? h + 8 : prev));
    };
    measure();
    const target = d.getElementById("mroot") ?? d.body;
    if (win.ResizeObserver && target) new win.ResizeObserver(measure).observe(target);
    setTimeout(measure, 300);
    setTimeout(measure, 900);
  };

  return (
    <div className="rbody-html">
      {blockedImages > 0 && !showImages && (
        // Blocked trackers: say so, and let the person accept it consciously.
        // In spam there's no button to reveal them: they stay blocked, period.
        <div className="img-blocked">
          <IcMail size={14} /> {t(spam ? "webmail.reader.imagesBlockedSpam" : "webmail.reader.imagesBlocked", { n: blockedImages })}
          {!spam && <button onClick={() => setShowImages(true)}>{t("webmail.reader.showImages")}</button>}
        </div>
      )}
      <iframe ref={ref} title="email"
        sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
        srcDoc={doc} onLoad={onLoad} style={{ height }} className="mail-iframe" />
    </div>
  );
}
