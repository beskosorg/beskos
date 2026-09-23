//! OS notifications **with a click**: showing the alert is only half the job -- clicking it has to
//! open the item that raised it (the email, the conversation, the file, the event...).
//!
//! Why not the plugin: `tauri-plugin-notification` on desktop delivers the notification and
//! forgets about it (`notify_rust::…show()` and that is it). The plugin's
//! `onAction`/`onNotificationReceived` only exists on Android/iOS. Hence this module: it shows the
//! notification, WAITS for the interaction on its own thread and, if the person clicked, brings
//! the main window to the front and emits `notif-open` with the link for the UI to navigate to
//! (the same contract as the web service worker's `notificationclick`).
//!
//! Per platform:
//!  - macOS: `mac-notification-sys` directly, with `wait_for_click` -- `notify-rust` only waits
//!    when the notification has buttons, and we do not want an "Open" button hanging off every alert.
//!  - Windows/Linux: `notify-rust` (WinRT toast / D-Bus). On Linux the click on the body only
//!    arrives if the notification declares the `default` action.

use tauri::{AppHandle, Emitter, Manager, Runtime};

/// Shows an OS notification. `link` is the app route to open on click (e.g.
/// `/mail/mail/inbox/<id>`); empty = notification with no destination (just an alert).
///
/// Returns immediately: waiting for the interaction runs on a separate thread, otherwise the
/// `wait_for_*` block would hold up the invoke (and, on macOS, the main run loop).
#[tauri::command]
pub fn notify_show<R: Runtime>(app: AppHandle<R>, title: String, body: String, link: String) {
    let identifier = app.config().identifier.clone();
    std::thread::spawn(move || {
        if show_and_wait_for_click(&identifier, &title, &body) {
            open_link(&app, &link);
        }
    });
}

/// Brings the main window to the front and tells the UI to navigate to the link.
fn open_link<R: Runtime>(app: &AppHandle<R>, link: &str) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
    // emit_to("main"): only the main window's webview navigates. A global emit would send the
    // dedicated windows (Meet room, editor) to another route in the middle of what they are doing.
    let _ = app.emit_to("main", "notif-open", link.to_string());
}

/// Shows the notification and blocks until the user interacts. `true` = clicked (open the item);
/// `false` = dismissed, closed, or the notification failed.
#[cfg(target_os = "macos")]
fn show_and_wait_for_click(identifier: &str, title: &str, body: &str) -> bool {
    use mac_notification_sys::{Notification, NotificationResponse};
    // `NSUserNotificationCenter` requires a registered bundle. In `tauri dev` the binary is not
    // packaged, so we borrow the Terminal's (the same trick the official plugin uses).
    let bundle = if tauri::is_dev() {
        "com.apple.Terminal"
    } else {
        identifier
    };
    let _ = mac_notification_sys::set_application(bundle); // Err = already set: nothing to do

    let mut n = Notification::new();
    n.title(title).message(body).wait_for_click(true);
    match n.send() {
        Ok(r) => matches!(r, NotificationResponse::Click),
        Err(e) => {
            // Safety net: waiting for the click must never cost the ALERT. If delivery with a
            // wait fails, retry in fire-and-forget mode (the usual path) -- no click, but the
            // person still sees the notification.
            eprintln!("warning: notification with click failed ({e}); retrying without a wait");
            let mut plain = Notification::new();
            plain.title(title).message(body).asynchronous(true);
            let _ = plain.send();
            false
        }
    }
}

#[cfg(not(target_os = "macos"))]
fn show_and_wait_for_click(identifier: &str, title: &str, body: &str) -> bool {
    use notify_rust::NotificationResponse;
    use std::sync::atomic::{AtomicBool, Ordering};

    let mut n = notify_rust::Notification::new();
    n.summary(title).body(body);
    // In dev the executable is not registered with the OS; an unknown AppUserModelID makes the
    // toast fail, so we only set it on the installed app.
    #[cfg(windows)]
    if !tauri::is_dev() {
        n.app_id(identifier);
    }
    // On D-Bus the click on the body is only reported if the notification declares the `default`
    // action; the label never actually appears (it is the notification's implicit action).
    #[cfg(not(windows))]
    {
        let _ = identifier;
        n.action("default", "default");
    }

    let handle = match n.show() {
        Ok(h) => h,
        Err(e) => {
            eprintln!("warning: notification not delivered: {e}");
            return false;
        }
    };
    let clicked = AtomicBool::new(false);
    let _ = handle.wait_for_response(|r: &NotificationResponse| {
        clicked.store(
            matches!(
                r,
                NotificationResponse::Default | NotificationResponse::Action(_)
            ),
            Ordering::Relaxed,
        );
    });
    clicked.load(Ordering::Relaxed)
}
