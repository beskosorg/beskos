//! App menu bar icon (Tauri 2's native tray).
//!
//! Shows the sync status without needing to open the window, and gives the quick actions.
//! The engine is the same one the UI uses (`SyncEngine` in Tauri's state) -- this is presentation only.

use std::sync::Arc;
use std::time::Duration;

use beskos_sync::engine::{Engine, Phase};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Manager, Runtime};

/// Opens a path/URL in the user's environment (Finder/Explorer/browser).
fn open_path(target: &str) {
    #[cfg(target_os = "macos")]
    let cmd = "open";
    #[cfg(target_os = "windows")]
    let cmd = "explorer";
    #[cfg(all(unix, not(target_os = "macos")))]
    let cmd = "xdg-open";
    let _ = std::process::Command::new(cmd).arg(target).spawn();
}

/// Brings the main window to the front (the menu's "Abrir Beskos"). lang:ok
fn focus_main<R: Runtime>(app: &AppHandle<R>) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
}

/// Builds the bar icon and starts the thread that keeps it in step with the state.
pub fn setup<R: Runtime>(app: &AppHandle<R>, engine: Arc<Engine>) -> tauri::Result<()> {
    // First menu line: informational only (disabled), updated by the thread below.
    // Who is signed in (informational line, same as the status). Sits at the top because it is
    // the question people ask a tray icon first: "which account is this app connected to?".
    let account_i = MenuItem::with_id(app, "account", "Sessão não iniciada", false, None::<&str>)?;
    let status_i = MenuItem::with_id(app, "status", "A iniciar…", false, None::<&str>)?;
    let sync_i = MenuItem::with_id(app, "sync_now", "Sincronizar agora", true, None::<&str>)?;
    let pause_i = MenuItem::with_id(app, "pause", "Pausar", true, None::<&str>)?;
    let folder_i = MenuItem::with_id(app, "open_folder", "Abrir pasta", true, None::<&str>)?;
    let settings_i = MenuItem::with_id(app, "settings", "Definições…", true, None::<&str>)?;
    let show_i = MenuItem::with_id(app, "show", "Abrir Beskos", true, None::<&str>)?;
    let quit_i = MenuItem::with_id(app, "quit", "Sair", true, None::<&str>)?;

    let menu = Menu::with_items(
        app,
        &[
            &account_i,
            &status_i,
            &PredefinedMenuItem::separator(app)?,
            &sync_i,
            &pause_i,
            &PredefinedMenuItem::separator(app)?,
            &folder_i,
            &settings_i,
            &show_i,
            &PredefinedMenuItem::separator(app)?,
            &quit_i,
        ],
    )?;

    let eng = Arc::clone(&engine);
    let pause_item = pause_i.clone();
    // The tray's OWN icons (template: macOS uses only the shape and recolors it to match the bar).
    // States: monogram (off), check (synced), animated spinner (syncing), error, paused.
    macro_rules! trayimg {
        ($p:expr) => {
            tauri::image::Image::from_bytes(include_bytes!($p)).expect(concat!("icon ", $p))
        };
    }
    let ico_base = trayimg!("../icons/tray/base.png");
    let ico_ok = trayimg!("../icons/tray/ok.png");
    let ico_err = trayimg!("../icons/tray/error.png");
    let ico_paused = trayimg!("../icons/tray/paused.png");
    let spinner: Vec<tauri::image::Image> = vec![
        trayimg!("../icons/tray/sync-00.png"),
        trayimg!("../icons/tray/sync-01.png"),
        trayimg!("../icons/tray/sync-02.png"),
        trayimg!("../icons/tray/sync-03.png"),
        trayimg!("../icons/tray/sync-04.png"),
        trayimg!("../icons/tray/sync-05.png"),
        trayimg!("../icons/tray/sync-06.png"),
        trayimg!("../icons/tray/sync-07.png"),
        trayimg!("../icons/tray/sync-08.png"),
        trayimg!("../icons/tray/sync-09.png"),
        trayimg!("../icons/tray/sync-10.png"),
        trayimg!("../icons/tray/sync-11.png"),
    ];
    let tray = TrayIconBuilder::with_id("beskos-sync")
        .icon(ico_base.clone())
        .icon_as_template(true) // on macOS it adapts to the light/dark bar
        .tooltip("Beskos")
        .menu(&menu)
        .on_menu_event(move |app, event| match event.id.as_ref() {
            "sync_now" => eng.sync_now(),
            "pause" => {
                let paused = !eng.status().paused;
                eng.set_paused(paused);
                let _ = pause_item.set_text(if paused { "Retomar" } else { "Pausar" });
            }
            "open_folder" => {
                // Opens the first synced folder (the rest are visible in Settings > Sync). lang:ok
                if let Some(p) = eng.status().pairs.first() {
                    open_path(&p.local_path);
                }
            }
            "settings" => crate::desktop::open_settings(app),
            "show" => focus_main(app),
            "quit" => app.exit(0),
            _ => {}
        })
        .build(app)?;

    // Keeps the ICON + menu in step with the state. While syncing, animates the spinner (a frame
    // every ~90ms); in the other states, a fixed icon and a longer wait. Text/tooltip only change
    // when they actually change (avoids flickering the menu if it is open).
    let eng = Arc::clone(&engine);
    let app_h = app.clone();
    std::thread::spawn(move || {
        let mut frame = 0usize;
        let mut last_label = String::new();
        let mut last_acct = String::new();
        let mut last_icon = String::new();
        loop {
            let s = eng.status();
            let active = s.configured && !s.pairs.is_empty();
            let syncing = active && !s.paused && matches!(s.phase, Phase::Syncing);

            // Icon per state -- only SWAPPED when it actually changes. Resetting the same icon on
            // every loop made the tray flicker; this way the spinner animates (the key changes per
            // frame) and the fixed states stay still. The template mode comes from the builder and persists.
            let (icon, key): (&tauri::image::Image, String) = if syncing {
                let f = frame % spinner.len();
                frame = frame.wrapping_add(1);
                (&spinner[f], format!("s{f}"))
            } else {
                frame = 0;
                if !active {
                    (&ico_base, "base".into())
                } else if s.paused {
                    (&ico_paused, "paused".into())
                } else if matches!(s.phase, Phase::Error) {
                    (&ico_err, "error".into())
                } else {
                    (&ico_ok, "ok".into())
                }
            };
            if key != last_icon {
                let _ = tray.set_icon(Some(icon.clone()));
                // set_icon resets the template mode on macOS -> restore it on every SWAP (not
                // every loop, or the flicker comes back). Without this the icon goes black and
                // stops following the light/dark bar.
                let _ = tray.set_icon_as_template(true);
                last_icon = key;
            }

            // Menu text + tooltip (only when it changes).
            let acct = crate::desktop::account_label(&app_h)
                .unwrap_or_else(|| "Sessão não iniciada".to_string());
            if acct != last_acct {
                let _ = account_i.set_text(&acct);
                last_acct = acct;
            }
            let label = if !s.configured {
                "Sincronização desligada".to_string()
            } else if s.pairs.is_empty() {
                "Nenhuma pasta a sincronizar".to_string()
            } else if s.paused {
                "Pausado".to_string()
            } else {
                match s.phase {
                    Phase::Syncing => "A sincronizar…".to_string(),
                    Phase::Error => format!("Erro: {}", s.message),
                    _ if s.message.is_empty() => "Tudo sincronizado".to_string(),
                    _ => format!("Sincronizado — {}", s.message),
                }
            };
            if label != last_label {
                let _ = status_i.set_text(&label);
                let _ = tray.set_tooltip(Some(format!("Beskos — {label}")));
                last_label = label;
            }

            std::thread::sleep(Duration::from_millis(if syncing { 90 } else { 700 }));
        }
    });

    Ok(())
}
