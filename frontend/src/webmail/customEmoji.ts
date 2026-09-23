import { mediaUrl } from "../apiBase";

// Global registry of the workspace's custom emoji (:name: -> image URL). Populated once when
// the chat loads the list; renderRich consults it to swap :name: for <img>. Global (rather than
// a prop) so the map doesn't have to be threaded through every place that renders text (chat,
// comments).

let MAP: Record<string, string> = {};

export function setCustomEmoji(list: { name: string; file_id: string }[]) {
  MAP = Object.fromEntries(list.map((e) => [e.name, mediaUrl(`/api/files/${e.file_id}/thumb`)]));
}

export function customEmojiUrl(name: string): string | undefined {
  return MAP[name];
}

export function customEmojiList(): { name: string; url: string }[] {
  return Object.entries(MAP).map(([name, url]) => ({ name, url }));
}
