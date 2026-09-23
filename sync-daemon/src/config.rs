//! The daemon's configuration. The secret (device token) lives in a SEPARATE file with 0600
//! permissions (only the owner can read it), isolated from the non-sensitive config. The same
//! posture as ~/.ssh and ~/.aws/credentials.
//! (Future hardening: move the token to the OS keychain via `keyring`.)

use std::fs;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

/// A synced pair: a LOCAL folder mirrored with a cloud folder. Only what is inside this pair
/// syncs -- never mirroring the whole drive. Several can exist.
#[derive(Serialize, Deserialize, Clone)]
pub struct SyncPair {
    /// Absolute path of the folder on this computer.
    pub local_path: String,
    /// Id of the matching folder in Drive (created when the pair was added).
    pub folder_id: String,
    /// Name of the folder in the cloud (only for display in the UI).
    pub name: String,
}

#[derive(Serialize, Deserialize, Clone)]
pub struct Config {
    pub server_url: String,
    /// Identity of the ACCOUNT this config belongs to (e.g. the user's id/email). Isolates the
    /// config per account on the same machine: switching users does not show or inherit another
    /// account's pairs. Empty = old config (pre-isolation) -- in that case only server_url is compared.
    #[serde(default)]
    pub account: String,
    /// Synced folder pairs. Empty = nothing syncs (a folder still needs to be added).
    #[serde(default)]
    pub pairs: Vec<SyncPair>,
    /// Parallel transfers. 0/absent = 4.
    #[serde(default)]
    pub workers: usize,
    /// Bandwidth cap in KB/s (uploads plus downloads). 0/absent = no limit.
    #[serde(default)]
    pub bandwidth_kbps: u64,
}

impl Config {
    /// Number of transfer threads (with a sensible default and ceiling).
    pub fn workers(&self) -> usize {
        if self.workers == 0 {
            4
        } else {
            self.workers.min(32)
        }
    }
    /// Limit in bytes/s (0 = no limit).
    pub fn bandwidth_bps(&self) -> u64 {
        self.bandwidth_kbps.saturating_mul(1024)
    }
}

/// The daemon's config directory: $MAILIN_SYNC_HOME, else the platform default under $HOME.
pub fn config_dir() -> Result<PathBuf> {
    if let Ok(h) = std::env::var("MAILIN_SYNC_HOME") {
        return Ok(PathBuf::from(h));
    }
    let home = std::env::var("HOME").context("HOME not set")?;
    let base = if cfg!(target_os = "macos") {
        Path::new(&home).join("Library/Application Support/mailin-sync")
    } else {
        Path::new(&home).join(".config/mailin-sync")
    };
    Ok(base)
}

fn config_path() -> Result<PathBuf> {
    Ok(config_dir()?.join("config.json"))
}

fn token_path() -> Result<PathBuf> {
    Ok(config_dir()?.join("token"))
}

pub fn load() -> Result<Config> {
    let raw = fs::read_to_string(config_path()?)
        .context("config not found -- run `mailin-sync login` first")?;
    Ok(serde_json::from_str(&raw)?)
}

pub fn load_token() -> Result<String> {
    let t =
        fs::read_to_string(token_path()?).context("token missing -- run `mailin-sync login`")?;
    Ok(t.trim().to_string())
}

/// Saves config + token. The token goes to a 0600 file; the config to 0644.
pub fn save(cfg: &Config, token: &str) -> Result<()> {
    let dir = config_dir()?;
    fs::create_dir_all(&dir)?;
    restrict_dir(&dir);

    fs::write(config_path()?, serde_json::to_vec_pretty(cfg)?)?;

    let tp = token_path()?;
    fs::write(&tp, token)?;
    restrict_file(&tp);
    Ok(())
}

#[cfg(unix)]
fn restrict_file(p: &Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = fs::set_permissions(p, fs::Permissions::from_mode(0o600));
}

#[cfg(unix)]
fn restrict_dir(p: &Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = fs::set_permissions(p, fs::Permissions::from_mode(0o700));
}

#[cfg(not(unix))]
fn restrict_file(_p: &Path) {}

#[cfg(not(unix))]
fn restrict_dir(_p: &Path) {}
