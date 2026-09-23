// Shared entry point (desktop + mobile). mobile_entry_point is used by Android/iOS.
#[cfg(desktop)]
mod desktop;
// Notifications with click: desktop only (on mobile the plugin already delivers the click via `onAction`).
#[cfg(desktop)]
mod notify;
mod sync_cmds;
// Menu bar icon: desktop only (mobile has no menu bar).
#[cfg(desktop)]
mod tray;

/// Safety net between sync passes; the filesystem watcher wakes the engine before this fires
/// whenever the folder changes.
const SYNC_INTERVAL_SECS: u64 = 30;

/// Contents of .vcf files the OS told Beskos to open before the UI was ready (e.g. a cold start
/// from double-clicking a contact). The UI drains this on mount (take_pending_vcards) and imports
/// the contacts. We store the TEXT, not the path -- the webview has no disk access.
#[derive(Default)]
struct PendingVcards(std::sync::Mutex<Vec<String>>);

/// Reads the contents of paths ending in .vcf (ignores flags and other arguments). The argument
/// envelope varies by platform, so we filter by extension instead of by position.
fn read_vcards<I: IntoIterator<Item = String>>(paths: I) -> Vec<String> {
    let mut out = Vec::new();
    for p in paths {
        if p.to_lowercase().ends_with(".vcf") {
            if let Ok(text) = std::fs::read_to_string(&p) {
                if !text.trim().is_empty() {
                    out.push(text);
                }
            }
        }
    }
    out
}

/// Stores the vCards in the pending queue, notifies the UI (event) and brings the window to the
/// front. Used when the app is ALREADY running (single-instance on Windows/Linux, RunEvent::Opened
/// on macOS).
#[cfg(desktop)]
fn stash_vcards(app: &tauri::AppHandle, texts: Vec<String>) {
    if texts.is_empty() {
        return;
    }
    use tauri::{Emitter, Manager};
    if let Some(state) = app.try_state::<PendingVcards>() {
        state.0.lock().unwrap().extend(texts);
    }
    let _ = app.emit("vcard-pending", ());
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.set_focus();
    }
}

/// Returns and clears the pending vCards. The UI calls this on mount and on every "vcard-pending" event.
#[tauri::command]
fn take_pending_vcards(state: tauri::State<PendingVcards>) -> Vec<String> {
    let mut v = state.0.lock().unwrap();
    std::mem::take(&mut *v)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // The sync engine lives INSIDE the app (there is no second application). It starts right
    // away, but only actually syncs once credentials have been saved -- otherwise it stays quiet
    // waiting for the UI to call `sync_configure`.
    let engine = beskos_sync::engine::Engine::start(SYNC_INTERVAL_SECS);

    let tray_engine = std::sync::Arc::clone(&engine);

    let mut builder = tauri::Builder::default();
    // single-instance MUST be the first plugin. When the OS opens a .vcf with the app already
    // alive, it forwards the arguments here (Windows/Linux) instead of opening a second window.
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, argv, _cwd| {
            stash_vcards(app, read_vcards(argv));
        }));
    }

    let app = builder
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(PendingVcards::default())
        .setup(move |app| {
            #[cfg(desktop)]
            {
                use tauri::Manager;
                app.manage(desktop::Account::default());
                // Cold start on Windows/Linux: the .vcf comes in the process arguments. On macOS
                // it arrives via RunEvent::Opened (handled below), so this is a no-op there.
                let opened = read_vcards(std::env::args());
                if !opened.is_empty() {
                    app.state::<PendingVcards>()
                        .0
                        .lock()
                        .unwrap()
                        .extend(opened);
                }
                // Menu bar icon with the sync status + action menu.
                tray::setup(app.handle(), tray_engine)?;
                // Adds the settings item to the application menu (label below), with the ⌘
                // shortcut everyone expects on macOS. We start from the default menu so we do not
                // lose Edit/Window -- without them, copy and paste would stop working across the
                // whole app.
                // Menu label: "Definições…" lang:ok
                let menu = tauri::menu::Menu::default(app.handle())?;
                if let Some(first) = menu.items()?.first().and_then(|i| i.as_submenu().cloned()) {
                    let item = tauri::menu::MenuItem::with_id(
                        app.handle(),
                        "settings",
                        "Definições…",
                        true,
                        Some("CmdOrCtrl+,"),
                    )?;
                    first.insert(&item, 1)?;
                }
                // "Import mail" under File: that is where somebody looks for bringing things INTO
                // an application, and File is a submenu Tauri already builds on macOS and Windows.
                desktop::add_import_item(app.handle(), &menu)?;
                app.set_menu(menu)?;
                app.on_menu_event(|app, event| match event.id().as_ref() {
                    "settings" => desktop::open_settings(app),
                    // The native menu cannot navigate: it raises the window and tells the UI where
                    // to go, because the UI is what knows the routes.
                    "import-mail" => desktop::open_import(app),
                    _ => {}
                });
            }
            Ok(())
        })
        .manage(sync_cmds::SyncEngine(engine))
        .invoke_handler(tauri::generate_handler![
            sync_cmds::sync_status,
            sync_cmds::sync_connect,
            sync_cmds::sync_add_folder,
            sync_cmds::sync_remove_folder,
            sync_cmds::sync_now,
            sync_cmds::sync_set_paused,
            sync_cmds::sync_bandwidth,
            sync_cmds::sync_set_bandwidth,
            take_pending_vcards,
            #[cfg(desktop)]
            desktop::set_account,
            #[cfg(desktop)]
            notify::notify_show,
        ])
        .on_window_event(|window, event| {
            // Closing the window does NOT close the app: it stays in the tray syncing (the
            // pattern every sync client follows). Actually quitting is via the tray menu -> "Sair".
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    api.prevent_close();
                    let _ = window.hide();
                    // The window only HIDES -- the webview stays alive, so the UI does not notice
                    // that the user "closed" the app. We notify whoever needs to wind things down
                    // live (e.g. someone presenting in Meet has to end the presentation).
                    // emit_to("main"): only this window's webview reacts. A global emit would also
                    // catch the dedicated windows (Meet room, editor), which were not closed.
                    use tauri::Emitter;
                    let _ = window.emit_to("main", "window-hidden", ());
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("failed to start the Tauri application");

    // Event loop. On macOS, opening a .vcf with the app (cold start OR already running) arrives
    // as RunEvent::Opened with file:// URLs. We convert to a path, read it and queue it for the UI.
    app.run(move |_app_handle, _event| {
        #[cfg(target_os = "macos")]
        if let tauri::RunEvent::Opened { urls } = _event {
            let paths: Vec<String> = urls
                .iter()
                .filter_map(|u| u.to_file_path().ok())
                .map(|p| p.to_string_lossy().into_owned())
                .collect();
            stash_vcards(_app_handle, read_vcards(paths));
        }
    });
}
