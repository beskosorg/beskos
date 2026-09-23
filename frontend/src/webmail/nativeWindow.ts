import { isTauri } from "./tauriNotify";
import { toast } from "./toast";
import i18n from "../i18n";

// openInWindow opens an app route in a NATIVE Tauri window (desktop). Returns true if it handled
// it (we're in the native app); false on the web, where the caller should follow the normal flow
// (navigate/window.open). The label is unique per resource: reopening focuses the existing window
// instead of duplicating it.
//
// Tauri's asset server doesn't do SPA fallback, so it loads index.html?wpath=<route> and
// main.tsx rewrites the URL to the real route before the router mounts (same technique as the
// Meet room). isDedicatedWindow tells (synchronously) whether we're in a dedicated window opened
// by openInWindow (room/editor). The flag is set in main.tsx when the window loads via ?wpath=.
export function isDedicatedWindow(): boolean {
  return typeof window !== "undefined" && (window as unknown as { __beskosDedicated?: boolean }).__beskosDedicated === true;
}

// closeSelfWindow closes the current window: via the Tauri API in the native app; via window.close() on the web.
export function closeSelfWindow(): void {
  if (isTauri()) {
    void (async () => {
      try {
        const { getCurrentWindow } = await import("@tauri-apps/api/window");
        await getCurrentWindow().close();
      } catch { /* no permission/API: ignore */ }
    })();
    return;
  }
  window.close();
}

export function openInWindow(label: string, path: string, title: string): boolean {
  if (!isTauri()) return false;
  void (async () => {
    const safe = label.replace(/[^a-zA-Z0-9_-]/g, "");
    try {
      const { WebviewWindow } = await import("@tauri-apps/api/webviewWindow");
      const existing = await WebviewWindow.getByLabel(safe);
      if (existing) { await existing.setFocus(); return; }
      const url = `index.html?wpath=${encodeURIComponent(path)}`;
      const win = new WebviewWindow(safe, { url, title, width: 1280, height: 800, resizable: true, focus: true });
      win.once("tauri://error", (e) => toast(String((e as { payload?: unknown }).payload ?? i18n.t("native.windowOpenFailed"))));
    } catch {
      toast(i18n.t("native.windowOpenFailed"));
    }
  })();
  return true;
}

// --- Bridges to the native side (only exist in the desktop app) ---

/** setTrayAccount tells the tray menu who is signed in; pass `null` on logout. */
export async function setTrayAccount(label: string | null): Promise<void> {
  try {
    const { invoke } = await import("@tauri-apps/api/core");
    await invoke("set_account", { label });
  } catch { /* doesn't exist on the web; ignore */ }
}
