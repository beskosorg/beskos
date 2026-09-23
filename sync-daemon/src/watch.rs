//! Filesystem watcher: instead of waiting for the polling interval, it reacts to changes in the
//! local folder almost immediately (FSEvents on macOS, inotify on Linux, ReadDirectoryChanges on
//! Windows -- via `notify`).
//!
//! Events are **coalesced**: copying 500 files fires hundreds of events, but we only want ONE
//! sync pass at the end. We wait for a quiet period before waking the sync thread. The agent's own
//! download also generates events -- the next pass finds nothing to do (mtime/size match) and
//! settles on its own.

use std::path::PathBuf;
use std::sync::mpsc::{self, Sender};
use std::time::Duration;

use anyhow::Result;
use notify::{RecommendedWatcher, RecursiveMode, Watcher};

/// Quiet time before considering the event burst finished.
const QUIET: Duration = Duration::from_millis(1500);

/// Watches `root` and sends a "nudge" when the disk changes (already coalesced).
/// **The returned watcher has to stay alive** -- if dropped, it stops observing.
pub fn spawn(root: PathBuf, nudge: Sender<()>) -> Result<RecommendedWatcher> {
    let (tx, rx) = mpsc::channel();
    let mut watcher = notify::recommended_watcher(move |res| {
        let _ = tx.send(res);
    })?;
    watcher.watch(&root, RecursiveMode::Recursive)?;

    std::thread::spawn(move || {
        loop {
            // Blocks until the burst's first event.
            if rx.recv().is_err() {
                break; // watcher died
            }
            // Drains the rest of the burst until there is QUIET's worth of silence.
            while rx.recv_timeout(QUIET).is_ok() {}
            if nudge.send(()).is_err() {
                break; // nobody listening: the app is closing
            }
        }
    });

    Ok(watcher)
}
