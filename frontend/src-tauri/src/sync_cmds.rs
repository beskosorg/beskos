//! Bridge between the UI (React) and the embedded sync engine.
//!
//! The engine runs on a background thread inside this same app -- there is no second
//! application. The UI only calls these commands; all the logic (watcher, delta, conflicts,
//! trash) lives in `beskos_sync`.

use std::sync::Arc;

use beskos_sync::engine::{Engine, Status};
use tauri::State;

/// Stored in Tauri's state so the commands reach the same engine.
pub struct SyncEngine(pub Arc<Engine>);

/// Sync status for the CURRENT account. Takes the session identity (server_url + account) and,
/// if the saved config belongs to ANOTHER account (a user switch on the same machine), hides its
/// pairs — the UI shows "connect this machine" instead of someone else's folders. Also compares
/// server_url to cover old configs saved without `account`.
#[tauri::command]
pub fn sync_status(engine: State<'_, SyncEngine>, account: String, server_url: String) -> Status {
    let mut s = engine.0.status();
    let mismatch =
        (!server_url.is_empty() && !s.server_url.is_empty() && s.server_url != server_url)
            || (!account.is_empty() && !s.account.is_empty() && s.account != account);
    if mismatch {
        s.configured = false;
        s.pairs = Vec::new();
    }
    s
}

/// Connects this machine to the account (server + token). The token comes from the UI, which
/// created it via `POST /api/devices` using the user's session — no one copies anything by hand.
/// `account` isolates the config per user: switching accounts on the same machine does not
/// inherit the previous one's folders. It does not sync yet: at least one folder still needs to
/// be added.
#[tauri::command]
pub fn sync_connect(
    engine: State<'_, SyncEngine>,
    server_url: String,
    token: String,
    account: String,
) -> Result<(), String> {
    engine
        .0
        .connect(&server_url, &token, &account)
        .map_err(|e| e.to_string())
}

/// Adds a local folder: creates the matching folder in Drive and starts syncing only what is
/// inside it. Returns the created pair (for the UI to show right away).
#[tauri::command]
pub fn sync_add_folder(
    engine: State<'_, SyncEngine>,
    local_path: String,
) -> Result<beskos_sync::config::SyncPair, String> {
    engine.0.add_pair(&local_path).map_err(|e| e.to_string())
}

/// Stops syncing a folder (does not delete files on either side).
#[tauri::command]
pub fn sync_remove_folder(engine: State<'_, SyncEngine>, local_path: String) -> Result<(), String> {
    engine.0.remove_pair(&local_path).map_err(|e| e.to_string())
}

#[tauri::command]
pub fn sync_now(engine: State<'_, SyncEngine>) {
    engine.0.sync_now();
}

#[tauri::command]
pub fn sync_set_paused(engine: State<'_, SyncEngine>, paused: bool) {
    engine.0.set_paused(paused);
}

/// Current bandwidth limit (KB/s; 0 = no limit).
#[tauri::command]
pub fn sync_bandwidth(engine: State<'_, SyncEngine>) -> u64 {
    engine.0.bandwidth()
}

#[tauri::command]
pub fn sync_set_bandwidth(
    engine: State<'_, SyncEngine>,
    bandwidth_kbps: u64,
) -> Result<(), String> {
    engine
        .0
        .set_bandwidth(bandwidth_kbps)
        .map_err(|e| e.to_string())
}
