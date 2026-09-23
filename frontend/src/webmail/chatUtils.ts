import { type ChatMessage } from "./backend";
import { SHORTCODES } from "./emoji";


export const KB = (n: number) => `${Math.max(1, Math.round(n / 1024))} KB`;

// Quick reactions: the few that cover 90% of cases. The rest comes from the full picker.
export const QUICK = ["👍", "❤️", "😄", "🎉", "👀", "🙏", "🔥", "✅"];

// "Call" convention: a system message that the frontend renders as a joining card.
export const CALL_RE = /^📞 __call__ (video|audio) (\S+)$/;

export const callBody = (mode: "video" | "audio", room: string) => `📞 __call__ ${mode} ${room}`;

export function parseCall(body: string): { mode: "video" | "audio"; room: string } | null {
  const m = CALL_RE.exec(body);
  return m ? { mode: m[1] as "video" | "audio", room: m[2] } : null;
}

// applyShortcodes swaps ":shortcut: " or ":) " for the emoji, only after the trailing space.
export function applyShortcodes(v: string): string {
  if (!v.endsWith(" ")) return v;
  const body = v.slice(0, -1);
  for (const [code, ch] of Object.entries(SHORTCODES)) {
    if (body.endsWith(code)) return body.slice(0, -code.length) + ch + " ";
  }
  return v;
}

export const SLASH_COMMANDS: { key: string; desc: string; args?: string }[] = [
  { key: "task", desc: "chat.cmdTaskDesc" },
  { key: "me", desc: "chat.cmdMeDesc", args: "chat.cmdMeArgs" },
  { key: "shrug", desc: "chat.cmdShrugDesc" },
  { key: "tableflip", desc: "chat.cmdTableflipDesc" },
  { key: "unflip", desc: "chat.cmdUnflipDesc" },
];

export function applySlash(body: string): string {
  const m = /^\/(\w+)(?:\s+([\s\S]*))?$/.exec(body);
  if (!m) return body;
  const cmd = m[1].toLowerCase();
  const rest = (m[2] || "").trim();
  switch (cmd) {
    case "shrug": return (rest ? rest + " " : "") + "¯\\_(ツ)_/¯";
    case "me": return rest ? "_" + rest + "_" : body;
    case "tableflip": return (rest ? rest + " " : "") + "(╯°□°)╯︵ ┻━┻";
    case "unflip": return (rest ? rest + " " : "") + "┬─┬ノ( º _ ºノ)";
    default: return body;
  }
}

// Client-side slash-commands (Slack-style): transform the text before sending. Unknown commands
// pass through as plain text. `/me` and `/shrug` are the useful ones; the flips are just for fun.
// Commands available in the composer. `menu` is what the "/" list shows; the text ones are applied
// on send (applySlash), and /task opens the task picker.
// Formatting bar visibility (per device, like Slack).
export const LS_FMT = "mailin.chat.fmtbar";

// Quick time options (schedule send / remind me), Slack-style.
export function quickTimes(t: (k: string) => string): { label: string; date: Date }[] {
  const now = Date.now();
  const tomorrow9 = new Date(); tomorrow9.setDate(tomorrow9.getDate() + 1); tomorrow9.setHours(9, 0, 0, 0);
  return [
    { label: t("chat.in30m"), date: new Date(now + 30 * 60000) },
    { label: t("chat.in1h"), date: new Date(now + 60 * 60000) },
    { label: t("chat.in3h"), date: new Date(now + 180 * 60000) },
    { label: t("chat.tomorrow9"), date: tomorrow9 },
  ];
}

// Applies a reaction event (WS) to a message, incrementally — without refetching the channel.
// Idempotent via membership in `users`: the echo of the action itself (or a redelivery) doesn't
// count twice. `mine` is recomputed with the current user's id, so it works for whichever member
// receives the event.
export function applyReaction(m: ChatMessage, emoji: string, userId: string, remove: boolean, myId: string): ChatMessage {
  const reactions = [...(m.reactions ?? [])];
  const idx = reactions.findIndex((r) => r.emoji === emoji);
  if (remove) {
    if (idx < 0 || !reactions[idx].users.includes(userId)) return m; // already removed → no-op
    const users = reactions[idx].users.filter((u) => u !== userId);
    if (users.length === 0) reactions.splice(idx, 1);
    else reactions[idx] = { ...reactions[idx], users, count: users.length, mine: users.includes(myId) };
  } else {
    if (idx < 0) {
      reactions.push({ emoji, users: [userId], count: 1, mine: userId === myId });
    } else {
      if (reactions[idx].users.includes(userId)) return m; // dedup of the echo of our own action
      const users = [...reactions[idx].users, userId];
      reactions[idx] = { ...reactions[idx], users, count: users.length, mine: users.includes(myId) };
    }
  }
  return { ...m, reactions };
}

