import { isTauri } from "./tauriNotify";
import { contactsApi } from "./backend";

// Opening a .vcf with Beskos on desktop (double-click / "Open with"): Rust reads the file and
// queues the TEXT (the webview has no disk access). Here we drain that queue and import the
// contacts into the user's address book — reusing the same vCard/CSV import as the web.
//
// Dynamic import of @tauri-apps/api: outside Tauri it's never loaded (isTauri() is false), so
// the web build doesn't depend on it at runtime.

export type VcardImportResult = { imported: number; skipped: number };

// initVcardOpen wires up the drain (once, when the authenticated shell mounts) and the listener
// for the "vcard-pending" event (openings while the app is already running). Returns a cleanup
// that removes the listener.
export function initVcardOpen(
  onDone: (r: VcardImportResult) => void,
  onError: (msg: string) => void,
): () => void {
  if (!isTauri()) return () => {};
  let disposed = false;

  const drain = async () => {
    try {
      const { invoke } = await import("@tauri-apps/api/core");
      const texts = await invoke<string[]>("take_pending_vcards");
      for (const text of texts) {
        if (!text.trim()) continue;
        const file = new File([text], "contact.vcf", { type: "text/vcard" });
        try {
          onDone(await contactsApi.importContacts(file));
        } catch (e) {
          onError((e as Error).message);
        }
      }
    } catch {
      /* outside Tauri or command unavailable */
    }
  };

  void drain(); // cold start: the .vcf that opened the app

  let unlisten: (() => void) | undefined;
  void (async () => {
    try {
      const { listen } = await import("@tauri-apps/api/event");
      const un = await listen("vcard-pending", () => { if (!disposed) void drain(); });
      if (disposed) un(); else unlisten = un;
    } catch {
      /* no events: the startup drain already covered the most common case */
    }
  })();

  return () => { disposed = true; unlisten?.(); };
}
