//! The embeddable engine: a background thread that syncs, a watcher that wakes it when the folder
//! changes, and an observable state for the UI (desktop app or menu bar icon) to show the user.
//!
//! It was extracted from agent mode so the same code serves both hosts -- the desktop app and the
//! headless binary -- without duplicating logic.

use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use anyhow::Result;
use serde::Serialize;

use crate::{api, config, index, sync};

/// Where the sync currently stands (what the UI shows).
#[derive(Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Phase {
    /// Never ran / waiting for the next cycle.
    Idle,
    Syncing,
    Error,
    Paused,
}

/// State published to the UI. It is `Serialize` to cross the Tauri bridge as-is.
#[derive(Clone, Serialize)]
pub struct Status {
    pub phase: Phase,
    /// Summary of the last pass ("3 downloaded, 1 uploaded") or the error message.
    pub message: String,
    pub paused: bool,
    /// Whether credentials are already saved (otherwise the UI shows the "connect this machine" screen).
    pub configured: bool,
    /// Server and account the saved config belongs to — the UI compares this against the current
    /// session so it does not show another account's pairs on the same machine.
    pub server_url: String,
    pub account: String,
    /// Synced folders (local<->cloud pairs).
    pub pairs: Vec<crate::config::SyncPair>,
}

impl Status {
    fn new() -> Self {
        Status {
            phase: Phase::Idle,
            message: String::new(),
            paused: false,
            configured: false,
            server_url: String::new(),
            account: String::new(),
            pairs: Vec::new(),
        }
    }
}

/// Sync engine running in the background. As long as this object is alive, the thread keeps running.
pub struct Engine {
    state: Arc<Mutex<Status>>,
    nudge: Sender<()>,
    /// One watcher per synced folder. They must stay alive (if dropped, they stop observing).
    _watchers: Mutex<Vec<notify::RecommendedWatcher>>,
}

impl Engine {
    /// Starts the engine. `interval` is the safety net between passes (the watcher wakes it up before that).
    pub fn start(interval: u64) -> Arc<Self> {
        let state = Arc::new(Mutex::new(Status::new()));
        let (tx, rx) = mpsc::channel();

        let engine = Arc::new(Engine {
            state: Arc::clone(&state),
            nudge: tx.clone(),
            _watchers: Mutex::new(Vec::new()),
        });

        spawn_loop(Arc::clone(&state), rx, interval);
        engine.refresh_config();
        engine.rearm_watcher();
        engine
    }

    /// Current state (for the UI).
    pub fn status(&self) -> Status {
        self.state.lock().unwrap_or_else(|e| e.into_inner()).clone()
    }

    /// Requests a sync right away, without waiting for the interval.
    pub fn sync_now(&self) {
        let _ = self.nudge.send(());
    }

    pub fn set_paused(&self, paused: bool) {
        let mut s = self.state.lock().unwrap_or_else(|e| e.into_inner());
        s.paused = paused;
        s.phase = if paused { Phase::Paused } else { Phase::Idle };
    }

    /// Re-reads the config from disk into the state (after the UI changes it) and re-points the watchers.
    pub fn refresh_config(&self) {
        let cfg = config::load().ok();
        let has_token = config::load_token().map(|t| !t.is_empty()).unwrap_or(false);
        let mut s = self.state.lock().unwrap_or_else(|e| e.into_inner());
        s.configured = cfg.is_some() && has_token;
        s.server_url = cfg
            .as_ref()
            .map(|c| c.server_url.clone())
            .unwrap_or_default();
        s.account = cfg.as_ref().map(|c| c.account.clone()).unwrap_or_default();
        s.pairs = cfg.map(|c| c.pairs).unwrap_or_default();
    }

    /// (Re-)points a watcher at each synced folder.
    pub fn rearm_watcher(&self) {
        let pairs = self.status().pairs;
        let mut watchers = Vec::new();
        for p in pairs {
            match crate::watch::spawn(std::path::PathBuf::from(&p.local_path), self.nudge.clone()) {
                Ok(w) => watchers.push(w),
                Err(e) => eprintln!("warning: watcher unavailable on {} ({e})", p.local_path),
            }
        }
        *self._watchers.lock().unwrap_or_else(|e| e.into_inner()) = watchers;
    }

    /// Connects this machine to the account (server + device token). Nothing syncs yet -- at
    /// least one folder still needs to be added with `add_pair`.
    pub fn connect(&self, server_url: &str, token: &str, account: &str) -> Result<()> {
        let _ = api::Client::new(server_url, token)?; // validates early (requires https off localhost)
        let prev = config::load().ok();
        // Only inherits the pairs/settings if it is the SAME account and server. Switching users
        // on the same machine starts from zero -- otherwise one user would inherit another's folders.
        let same = prev
            .as_ref()
            .map(|c| c.server_url == server_url && c.account == account)
            .unwrap_or(false);
        config::save(
            &config::Config {
                server_url: server_url.to_string(),
                account: account.to_string(),
                pairs: if same {
                    prev.as_ref().map(|c| c.pairs.clone()).unwrap_or_default()
                } else {
                    Vec::new()
                },
                workers: if same {
                    prev.as_ref().map(|c| c.workers).unwrap_or(0)
                } else {
                    0
                },
                bandwidth_kbps: if same {
                    prev.as_ref().map(|c| c.bandwidth_kbps).unwrap_or(0)
                } else {
                    0
                },
            },
            token,
        )?;
        // Switched account/server => the index (file_ids from the previous server) is not valid
        // for the new account: clear it to start from zero (re-upload local content, re-read the feed).
        if !same {
            if let Ok(idx) = index::Index::open(&index_db_path()?) {
                let _ = idx.clear();
            }
        }
        self.refresh_config();
        self.rearm_watcher();
        Ok(())
    }

    /// Adds a local folder to the sync: **creates the matching folder in Drive** (with the local
    /// folder's name) and starts syncing only what is inside it, in both directions.
    pub fn add_pair(&self, local_path: &str) -> Result<crate::config::SyncPair> {
        let mut cfg = config::load()?;
        let token = config::load_token()?;
        if cfg.pairs.iter().any(|p| p.local_path == local_path) {
            // User-facing error, not yet localized (same as the tray status message below). lang:ok
            anyhow::bail!("esta pasta já está a ser sincronizada");
        }
        std::fs::create_dir_all(local_path)?;
        let name = std::path::Path::new(local_path)
            .file_name()
            .map(|n| n.to_string_lossy().to_string())
            .unwrap_or_else(|| "Sincronizada".to_string());

        let client = api::Client::with_bandwidth(&cfg.server_url, &token, cfg.bandwidth_bps())?;
        // Reuses a top-level folder with the same name if one already exists; otherwise creates it.
        let existing = client
            .folders()?
            .into_iter()
            .find(|f| f.parent_id.is_none() && f.name == name);
        let folder = match existing {
            Some(f) => f,
            None => client.create_folder(&name, None)?,
        };

        let pair = crate::config::SyncPair {
            local_path: local_path.to_string(),
            folder_id: folder.id,
            name,
        };
        cfg.pairs.push(pair.clone());
        config::save(&cfg, &token)?;
        // New cloud folder => any old index entry under this path is invalid (it points to
        // file_ids that no longer exist/belong to another account). Clear them so the push treats
        // the files as new and uploads them -- otherwise the index thinks they are synced and
        // nothing goes up.
        if let Ok(idx) = index::Index::open(&index_db_path()?) {
            let _ = idx.remove_under(local_path);
        }
        self.refresh_config();
        self.rearm_watcher();
        self.sync_now();
        Ok(pair)
    }

    /// Stops syncing a folder. Does NOT delete files -- neither local ones nor cloud ones.
    pub fn remove_pair(&self, local_path: &str) -> Result<()> {
        let mut cfg = config::load()?;
        let token = config::load_token()?;
        cfg.pairs.retain(|p| p.local_path != local_path);
        config::save(&cfg, &token)?;
        self.refresh_config();
        self.rearm_watcher();
        Ok(())
    }

    /// Current bandwidth limit (KB/s).
    pub fn bandwidth(&self) -> u64 {
        config::load().map(|c| c.bandwidth_kbps).unwrap_or(0)
    }

    pub fn set_bandwidth(&self, bandwidth_kbps: u64) -> Result<()> {
        let mut cfg = config::load()?;
        cfg.bandwidth_kbps = bandwidth_kbps;
        let token = config::load_token()?;
        config::save(&cfg, &token)?;
        Ok(())
    }
}

fn index_db_path() -> Result<std::path::PathBuf> {
    Ok(config::config_dir()?.join("index.db"))
}

/// The background thread: syncs, publishes the state, and sleeps until the next cycle or until woken.
fn spawn_loop(state: Arc<Mutex<Status>>, nudge: Receiver<()>, interval: u64) {
    std::thread::spawn(move || loop {
        let (paused, configured) = {
            let s = state.lock().unwrap_or_else(|e| e.into_inner());
            (s.paused, s.configured)
        };
        if !paused && configured {
            {
                let mut s = state.lock().unwrap_or_else(|e| e.into_inner());
                s.phase = Phase::Syncing;
            }
            // Reloads config/index on every pass: changing the folder or the token in the UI
            // takes effect without restarting the app.
            let outcome = (|| -> Result<sync::Stats> {
                let cfg = config::load()?;
                let token = config::load_token()?;
                let client =
                    api::Client::with_bandwidth(&cfg.server_url, &token, cfg.bandwidth_bps())?;
                let idx = index::Index::open(&index_db_path()?)?;
                sync::run_once(&client, &idx, &cfg.pairs, cfg.workers())
            })();
            let mut s = state.lock().unwrap_or_else(|e| e.into_inner());
            match outcome {
                Ok(st) => {
                    s.phase = Phase::Idle;
                    // User-facing tray status text; not routed through the frontend's i18next (this
                    // is native Rust, no JS runtime here). Not yet localized -- see docs/OPEN-SOURCE.md.
                    s.message = format!(
                        "{} baixados, {} enviados, {} movidos, {} p/ lixeira, {} conflitos{}", // lang:ok
                        st.downloaded,
                        st.uploaded,
                        st.moved,
                        st.trashed,
                        st.conflicts,
                        if st.errors > 0 {
                            format!(", {} erros", st.errors) // lang:ok
                        } else {
                            String::new()
                        }
                    );
                    // If there were upload errors, the state is ERROR (not "everything synced") --
                    // this way the UI and the tray show that something failed instead of faking success.
                    if st.errors > 0 {
                        s.phase = Phase::Error;
                    }
                }
                Err(e) => {
                    s.phase = Phase::Error;
                    s.message = format!("{e}");
                }
            }
        }
        let _ = nudge.recv_timeout(Duration::from_secs(interval));
    });
}
