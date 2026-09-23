//! Desktop-only things the web UI does not have: the Settings window and the account shown in the tray.
//!
//! The tray is built in Rust and does not know who is signed in -- the UI does. So the UI pushes
//! the account label here (`set_account`) and the tray just displays it.

use std::sync::Mutex;

use tauri::{AppHandle, Manager, Runtime, WebviewUrl, WebviewWindowBuilder};

/// Who is signed in, for the tray menu. `None` means no session has started yet.
#[derive(Default)]
pub struct Account(pub Mutex<Option<String>>);

/// Current account label (or `None` if no one has signed in).
pub fn account_label<R: Runtime>(app: &AppHandle<R>) -> Option<String> {
    app.try_state::<Account>()
        .and_then(|a| a.0.lock().ok().and_then(|g| g.clone()))
}

/// The UI calls this when the session opens or closes (passes `None` on sign-out).
#[tauri::command]
pub fn set_account<R: Runtime>(app: AppHandle<R>, label: Option<String>) {
    if let Some(state) = app.try_state::<Account>() {
        if let Ok(mut g) = state.0.lock() {
            *g = label;
        }
    }
}

/// Opens Settings in its own window. If it is already open, brings it to the front instead of
/// opening another one -- opening two copies of the same configuration invites conflicting saves.
pub fn open_settings<R: Runtime>(app: &AppHandle<R>) {
    if let Some(w) = app.get_webview_window("settings") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
        return;
    }
    // `index.html?wpath=…` and not `/mail/settings`: Tauri's asset server does not do SPA
    // fallback, so a direct route 404s in the build. main.tsx reads wpath and rewrites the URL.
    let url = WebviewUrl::App("index.html?wpath=%2Fmail%2Fsettings".into());
    let built = WebviewWindowBuilder::new(app, "settings", url)
        .title("Beskos — Definições")
        .inner_size(1000.0, 720.0)
        .min_inner_size(560.0, 480.0)
        .build();
    if let Err(e) = built {
        eprintln!("warning: could not open Settings: {e}");
    }
}

/// Adds "Import mail" to the File submenu.
///
/// Tauri's default menu builds File on macOS and Windows but not on Linux, and none of its submenus
/// carry a stable id -- so the submenu is found by its label, and a File submenu is created when
/// there is none. Matching on "File" is safe because Tauri writes that label itself, in English,
/// whatever the application's own language is.
pub fn add_import_item<R: Runtime>(
    app: &AppHandle<R>,
    menu: &tauri::menu::Menu<R>,
) -> tauri::Result<()> {
    let item = tauri::menu::MenuItem::with_id(
        app,
        "import-mail",
        "Importar correio…", // lang:ok — native menus are not reachable from i18n
        true,
        None::<&str>,
    )?;

    for entry in menu.items()? {
        if let Some(sub) = entry.as_submenu() {
            if sub.text().map(|t| t == "File").unwrap_or(false) {
                // At the top: "Close window" is the only other entry, and burying an action under it
                // puts the way out above the way in.
                sub.insert(&item, 0)?;
                sub.insert(&tauri::menu::PredefinedMenuItem::separator(app)?, 1)?;
                return Ok(());
            }
        }
    }

    // Linux: no File submenu exists, so make one. Placed after the application submenu when there
    // is one, otherwise first.
    let file = tauri::menu::Submenu::with_items(app, "File", true, &[&item])?;
    let pos = if menu.items()?.is_empty() { 0 } else { 1 };
    menu.insert(&file, pos)?;
    Ok(())
}

/// Raises the main window and asks the UI to open the import screen. The menu does not navigate --
/// only the web side knows the routes, and duplicating them in Rust means two places to be wrong.
pub fn open_import<R: Runtime>(app: &AppHandle<R>) {
    use tauri::Emitter;
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
        let _ = w.emit_to("main", "menu-import-mail", ());
    }
}
