// UI bridge to the sync engine that runs INSIDE the desktop app (Tauri).
//
// Model: folder pairs. The user picks a LOCAL folder; it is created in Drive and only what is
// inside it syncs, in both directions. Several can exist. There is no mirroring the whole drive.
// On the web these commands do not exist — the UI shows a note instead of the controls.

import { isTauri } from "./tauriNotify";
import { devicesApi } from "./backend";
import { apiBase } from "../apiBase";

export type SyncPhase = "idle" | "syncing" | "error" | "paused";

/** A local folder mirrored with a Drive folder. */
export interface SyncPair {
  local_path: string;
  folder_id: string;
  name: string;
}

export interface SyncStatus {
  phase: SyncPhase;
  message: string;
  paused: boolean;
  /** false = this machine is not yet linked TO THIS ACCOUNT (it may be linked to another one). */
  configured: boolean;
  /** Server/account the saved config belongs to (to isolate by account on the same machine). */
  server_url: string;
  account: string;
  pairs: SyncPair[];
}

// URL of the current session's server (same one used on connect).
const serverUrl = (): string => apiBase() || window.location.origin;

export const syncSupported = (): boolean => isTauri();

// invoke only exists in the native app; dynamic import so the web build does not load the module.
async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(cmd, args);
}

export const syncApi = {
  /** `account` = identity of the current account (the user's e-mail). The daemon hides the pairs
   *  if the saved config belongs to another account (user switch on the same machine). */
  status: (account: string) => invoke<SyncStatus>("sync_status", { account, serverUrl: serverUrl() }),
  now: () => invoke<void>("sync_now"),
  setPaused: (paused: boolean) => invoke<void>("sync_set_paused", { paused }),
  bandwidth: () => invoke<number>("sync_bandwidth"),
  setBandwidth: (bandwidthKbps: number) => invoke<void>("sync_set_bandwidth", { bandwidthKbps }),
  removeFolder: (localPath: string) => invoke<void>("sync_remove_folder", { localPath }),

  /** Opens the NATIVE folder picker; returns the chosen path (or null if cancelled). */
  async pickFolder(): Promise<string | null> {
    const { open } = await import("@tauri-apps/plugin-dialog");
    const picked = await open({ directory: true, multiple: false });
    return typeof picked === "string" ? picked : null;
  },

  /**
   * ensureConnected links this machine to the account, if it is not already: creates a device
   * token using the current session (the user never copies anything by hand).
   */
  async ensureConnected(account: string): Promise<void> {
    const st = await syncApi.status(account);
    if (st.configured) return; // already linked TO THIS account (status hides another account's config)
    const name = `Beskos Desktop (${navigator.platform || "computer"})`;
    const created = await devicesApi.create(name);
    await invoke<void>("sync_connect", { serverUrl: serverUrl(), token: created.token, account });
  },

  /**
   * Picks a local folder and starts syncing it: the corresponding folder is created in Drive.
   * Returns the created pair, or null if the user cancelled the picker.
   */
  async addFolder(account: string): Promise<SyncPair | null> {
    const dir = await syncApi.pickFolder();
    if (!dir) return null;
    await syncApi.ensureConnected(account);
    return invoke<SyncPair>("sync_add_folder", { localPath: dir });
  },
};
