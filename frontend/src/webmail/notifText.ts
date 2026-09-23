import type { TFunction } from "i18next";
import type { Notif } from "./backend";

// Translates an in-app notification into the user's language (docs/engineering-standards.md §4.6): the backend sends
// a structured `kind` + `args`; here we build the text via `server.notif.<kind>`. The title is
// always translated; the body only when a `.b` key exists (meeting/event/file). For chat/task the
// body is user content (comment/preview) and falls into `defaultValue` = the raw stored text. Old
// rows without `args` also fall back to title/body. ISO dates become locale format via Intl.
export function notifText(n: Notif, t: TFunction, lang: string): { title: string; body: string } {
  const args: Record<string, string> = { ...(n.args ?? {}) };
  if (args.when) args.when = fmtWhen(args.when, lang);
  if (args.status) args.status = t(`server.rsvp.${args.status}`, { defaultValue: args.status });

  // @channel/@here uses a different title from a personal mention (same kind in the backend).
  const key = n.kind === "chat_mention" && args.at ? "chat_mention_at" : n.kind;
  const base = `server.notif.${key}`;
  // An instant meeting has no time: use the variant without "when".
  const bKey = key === "meeting_invite" && !args.when ? `${base}.b0` : `${base}.b`;

  const title = t(`${base}.t`, { ...args, defaultValue: n.title });
  const body = t(bKey, { ...args, defaultValue: n.body });
  return { title, body };
}

function fmtWhen(iso: string, lang: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(lang, { dateStyle: "short", timeStyle: "short" }).format(d);
}
