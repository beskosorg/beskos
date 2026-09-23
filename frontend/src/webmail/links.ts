// URL detection in free text, shared by the message renderer and by the unfurl preview.
//
// One module because there used to be two regexes -- richtext.tsx linkified, ChatView.tsx picked the
// URL to preview -- and they had already drifted: both demanded "https://", so a link pasted the way
// people actually paste them ("meet.google.com/abc") became neither a link nor a preview.
//
// Deliberately conservative. A false positive turns ordinary prose into a link, which is worse than
// missing one: "check report.txt again" must stay text. So a bare host only counts when its last
// label is a TLD from the list below, and the file extensions people type in chat (txt, js, md, go,
// sql, json) are absent from it on purpose.

// TLDs common enough to be worth auto-linking. Not the full IANA list -- that one contains .zip and
// .mov, which would turn half the filenames in a conversation into links.
const TLD =
  "com|org|net|edu|gov|mil|int|io|dev|app|ai|co|me|tv|gg|xyz|cloud|info|biz|online|site|tech|" + // lang:ok
  "br|pt|uk|de|fr|es|it|nl|pl|eu|us|ca|au|nz|jp|cn|in|ru|ch|se|no|dk|fi|be|at|ie|za|mx|ar|cl"; // lang:ok

// Order matters: the scheme form is tried first so "https://x.io/a" is taken whole instead of the
// bare matcher grabbing "x.io" and leaving the scheme behind as loose text.
const WITH_SCHEME = String.raw`https?:\/\/[^\s<>"']+`;
const BARE = String.raw`(?:www\.[^\s<>"']+|[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9-]+)*\.(?:${TLD})\b(?:[\/?#][^\s<>"']*)?)`;

// The lookbehind keeps us out of things that merely contain a dot: e-mail local parts (@), paths
// already inside a longer URL (/), and words glued to it. Without it, "user@acme.com" would linkify
// its domain half and the address would come apart on screen.
export const URL_TOKEN = String.raw`(?<![\w@\/.-])(?:${WITH_SCHEME}|${BARE})`;

export const URL_RE = new RegExp(URL_TOKEN, "i");

// Trailing punctuation belongs to the sentence, not to the link. "veja beskos.com." must not send
// people to a host with a dot on the end -- and the same bug was already there for https:// links.
const TRAILING = /[.,;:!?)\]}>'"]+$/;

// splitTrailing separates the link from the punctuation that ended the sentence, so the caller can
// render the first as a link and the second as plain text.
export function splitTrailing(token: string): [string, string] {
  const m = TRAILING.exec(token);
  if (!m) return [token, ""];
  // A closing bracket that has its opening inside the URL is part of the URL (wikipedia does this).
  let cut = m.index;
  const head = token.slice(0, cut);
  if (token[cut] === ")" && (head.match(/\(/g)?.length ?? 0) > (head.match(/\)/g)?.length ?? 0)) cut += 1;
  return [token.slice(0, cut), token.slice(cut)];
}

// href turns a detected token into something a browser can follow. A bare host has no scheme, and
// an <a> without one is read as a RELATIVE path -- the link would stay inside Beskos and 404.
export function href(token: string): string {
  return /^https?:\/\//i.test(token) ? token : "https://" + token;
}
