import type { ReactNode } from "react";
import { customEmojiUrl } from "./customEmoji";
import { URL_TOKEN, splitTrailing, href as linkHref } from "./links";

// Lightweight markdown renderer (Slack-style subset), no external dependency. Builds React nodes
// (React already escapes text → no XSS risk). Supports: **bold**, _italic_, __underline__,
// ~~strikethrough~~, `code`, ```block```, links [t](u) and bare URLs, @mentions, :emoji_custom:,
// > quote, and lists (bulleted and numbered).

// `__underline__` comes BEFORE `_italic_`: in alternation the first one that matches wins, and
// with the order swapped, underline would be read as italic with extra underscores.
// The URL alternative comes from links.ts so the renderer and the unfurl preview agree on what a
// link is. They used to carry a copy each, both demanding "https://", and a link pasted the way
// people paste them ("meet.google.com/abc") was neither linked nor previewed.
const INLINE = new RegExp(
  "(`[^`]+`)|(\\*\\*[^*]+\\*\\*)|(~~[^~]+~~)|(__[^_]+__)|(_[^_]+_)|(\\[[^\\]]+\\]\\([^)]+\\))" +
  `|(${URL_TOKEN})|(@[\\w.\\-]+)|(:[a-z0-9_]{2,32}:)`,
  "gi",
);

// Which alternative fired. Testing the token against the URL pattern is the only reliable way now
// that a link need not start with "http" -- `tok.startsWith("http")` would miss every bare host.
const IS_URL = new RegExp(`^(?:${URL_TOKEN})$`, "i");


// Link to a task (/mail/kanban/<board>/<task>): instead of a blue link in the middle of the
// sentence, it becomes a card with the key highlighted — that's what stands out in a
// text-heavy conversation.
const TASK_HREF = /^\/mail\/kanban\/[0-9a-fA-F-]{36}\/[0-9a-fA-F-]{36}$/;

// INTERNAL navigation without a reload: a normal href would cause a reload, and in the native app
// (Tauri) that leaves the application — the asset server has no SPA fallback. pushState + popstate:
// the router reacts. Ctrl/Cmd/Shift still work (open in another tab/window) because we don't
// intercept them.
function goInternal(e: React.MouseEvent, href: string): void {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
  e.preventDefault();
  window.history.pushState({}, "", href);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

// internalPath returns the path when the target is this app, otherwise null. Compared by ORIGIN: a
// host that merely starts the same ("app.beskos.com.evil.com") is not ours.
function internalPath(href: string): string | null {
  if (href.startsWith("/")) return href;
  try {
    const u = new URL(href);
    return u.origin === window.location.origin ? u.pathname + u.search + u.hash : null;
  } catch { return null; }
}

function TaskRef({ href, label }: { href: string; label: string }): ReactNode {
  // The link text comes as "QUA-12 — Title" (that's how sharing and /task write it); when it
  // doesn't, we show the whole label without inventing a key.
  const m = /^([A-Za-z0-9]{1,8}-\d+)\s*[—–-]\s*([\s\S]+)$/.exec(label);
  return (
    <a className="task-ref" href={href} onClick={(e) => goInternal(e, href)}>
      <svg className="tr-ic" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        <rect x="3" y="4" width="18" height="16" rx="2" /><path d="M8 10h8M8 14h5" />
      </svg>
      {m ? <><b className="tr-key">{m[1]}</b><span className="tr-title">{m[2]}</span></> : <span className="tr-title">{label}</span>}
    </a>
  );
}

function renderInline(text: string, keyBase: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let i = 0;
  for (const m of text.matchAll(INLINE)) {
    const idx = m.index ?? 0;
    if (idx > last) out.push(text.slice(last, idx));
    const tok = m[0];
    const key = `${keyBase}-${i++}`;
    if (tok.startsWith("`")) out.push(<code key={key} className="md-code">{tok.slice(1, -1)}</code>);
    else if (tok.startsWith("**")) out.push(<strong key={key}>{tok.slice(2, -2)}</strong>);
    else if (tok.startsWith("~~")) out.push(<s key={key}>{tok.slice(2, -2)}</s>);
    else if (tok.startsWith("__")) out.push(<u key={key}>{tok.slice(2, -2)}</u>);
    else if (tok.startsWith("_")) out.push(<em key={key}>{tok.slice(1, -1)}</em>);
    else if (tok.startsWith("[")) {
      const mm = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(tok);
      if (mm && TASK_HREF.test(mm[2])) out.push(<TaskRef key={key} href={mm[2]} label={mm[1]} />);
      // One of OUR links navigates inside the app, whether it arrives as a path or as an absolute
      // URL. Sharing a note produces the second form (the link has to work for whoever copies it
      // out), and without this it opened a new tab for a page that was already open.
      else if (mm && internalPath(mm[2])) {
        const path = internalPath(mm[2])!;
        out.push(<a key={key} href={path} onClick={(e) => goInternal(e, path)}>{mm[1]}</a>);
      }
      else if (mm) out.push(<a key={key} href={mm[2]} target="_blank" rel="noreferrer">{mm[1]}</a>);
      else out.push(tok);
    } else if (IS_URL.test(tok)) {
      // The sentence's punctuation is not part of the address: "see beskos.com." must not link to a
      // host ending in a dot. Whatever gets trimmed goes back as plain text so the line still reads.
      const [url, tail] = splitTrailing(tok);
      out.push(<a key={key} href={linkHref(url)} target="_blank" rel="noreferrer noopener">{url}</a>);
      if (tail) out.push(tail);
    } else if (tok.startsWith("@")) out.push(<span key={key} className="md-mention">{tok}</span>);
    else if (tok.startsWith(":")) {
      const url = customEmojiUrl(tok.slice(1, -1));
      out.push(url ? <img key={key} className="cemoji" src={url} alt={tok} title={tok} /> : tok);
    }
    last = idx + tok.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

// renderRich turns the text into blocks (code, quote, lists, paragraphs with inline formatting).
export function renderRich(text: string): ReactNode {
  const parts = text.split(/```/);
  const blocks: ReactNode[] = [];
  parts.forEach((part, pi) => {
    if (pi % 2 === 1) { // segment between ``` = code block
      blocks.push(<pre key={`c${pi}`} className="md-block">{part.replace(/^\n/, "")}</pre>);
      return;
    }
    const lines = part.split("\n");
    // One list at a time: `ordered` says whether what's being accumulated is numbered (<ol>) or
    // bulleted (<ul>). Switching type closes the previous one — otherwise "1." and "-" would fall
    // into the same list.
    let list: ReactNode[] | null = null;
    let ordered = false;
    const flush = () => {
      if (!list) return;
      const key = `u${pi}-${blocks.length}`;
      blocks.push(ordered
        ? <ol key={key} className="md-list md-olist">{list}</ol>
        : <ul key={key} className="md-list">{list}</ul>);
      list = null;
    };
    lines.forEach((line, li) => {
      const key = `l${pi}-${li}`;
      const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
      const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
      if (bullet || numbered) {
        const isOrdered = !bullet;
        if (list && ordered !== isOrdered) flush();
        ordered = isOrdered;
        (list ??= []).push(<li key={key}>{renderInline((bullet ?? numbered)![1], key)}</li>);
        return;
      }
      flush();
      if (/^\s*>\s?/.test(line)) { blocks.push(<blockquote key={key} className="md-quote">{renderInline(line.replace(/^\s*>\s?/, ""), key)}</blockquote>); return; }
      if (line.trim() === "") { if (li > 0 && li < lines.length - 1) blocks.push(<br key={key} />); return; }
      blocks.push(<span key={key} className="md-line">{renderInline(line, key)}</span>);
    });
    flush();
  });
  return <>{blocks}</>;
}
