//! The daemon's HTTP client against `/api/sync/*`, authenticated with the device token.
//! Security: requires HTTPS except on localhost (dev); the token is never logged.

use std::io::Read;

use anyhow::{bail, Context, Result};
use serde::Deserialize;

// folder_id/size/updated_at are part of the feed contract but are not read yet
// (folder_id will drive the tree mirror; the others are for future telemetry/UX).
#[derive(Deserialize, Debug, Clone)]
#[allow(dead_code)]
pub struct FileChange {
    pub id: String,
    #[serde(default)]
    pub folder_id: Option<String>,
    pub name: String,
    pub hash: String,
    pub size: i64,
    #[serde(default)]
    pub version: i64,
    pub deleted: bool,
    pub updated_at: String,
    /// false = shared with me (I am not the owner). Treated as READ-ONLY by the agent.
    /// Defaults to `true` so it keeps working with servers older than this field.
    #[serde(default = "default_true")]
    pub owned: bool,
}

fn default_true() -> bool {
    true
}

/// Metadata returned by the server after creating/updating a file (the storage_files row).
#[derive(Deserialize)]
pub struct FileMeta {
    pub id: String,
    pub hash: String,
    #[serde(default)]
    pub versao: i64,
}

/// A folder in the user's tree (for the subfolder mirror).
#[derive(Deserialize, Clone)]
pub struct Folder {
    pub id: String,
    #[serde(default)]
    pub parent_id: Option<String>,
    pub name: String,
}

#[derive(Deserialize)]
struct FoldersPage {
    folders: Vec<Folder>,
}

/// Block map of the current content on the server (base for the delta upload).
#[derive(Deserialize)]
pub struct BlocksInfo {
    pub block_size: u64,
    #[allow(dead_code)]
    pub total_size: u64,
    /// Hash of the WHOLE file -- used to confirm the delta is computed against the same base.
    pub hash: String,
    pub hashes: Vec<String>,
}

/// Result of a conditional upload (PUT with If-Match).
pub enum PutOutcome {
    Updated(FileMeta),
    Conflict, // 412: the server changed since the base -- the daemon keeps a conflict copy.
}

#[derive(Deserialize)]
pub struct ChangesPage {
    pub changes: Vec<FileChange>,
    pub cursor: String,
    pub has_more: bool,
}

pub struct Client {
    base: String,
    token: String,
    /// Bandwidth limit shared by all of this client's transfers.
    throttle: crate::throttle::Throttle,
}

impl Client {
    pub fn new(server_url: &str, token: &str) -> Result<Self> {
        Self::with_bandwidth(server_url, token, 0)
    }

    /// `bytes_per_sec` = 0 turns the limit off.
    pub fn with_bandwidth(server_url: &str, token: &str, bytes_per_sec: u64) -> Result<Self> {
        let base = server_url.trim_end_matches('/').to_string();
        // Requires HTTPS, except localhost/127.0.0.1 (dev). Prevents sending the token in the
        // clear over the network.
        let is_local = base.starts_with("http://localhost")
            || base.starts_with("http://127.0.0.1")
            || base.starts_with("http://[::1]");
        if !base.starts_with("https://") && !is_local {
            bail!("server_url must be https:// (except localhost)");
        }
        Ok(Self {
            base,
            token: token.to_string(),
            throttle: crate::throttle::Throttle::new(bytes_per_sec),
        })
    }

    fn auth(&self, req: ureq::Request) -> ureq::Request {
        req.set("Authorization", &format!("Bearer {}", self.token))
    }

    /// Fetches a page of the changes feed starting at the cursor (empty = beginning).
    pub fn changes(&self, cursor: &str, limit: u32) -> Result<ChangesPage> {
        let url = format!("{}/api/sync/files/changes", self.base);
        let resp = self
            .auth(ureq::get(&url))
            .query("since", cursor)
            .query("limit", &limit.to_string())
            .call()
            .with_context(|| "GET changes failed")?;
        Ok(resp.into_json::<ChangesPage>()?)
    }

    /// Downloads a file's content by id. Returns the bytes.
    pub fn download(&self, id: &str) -> Result<Vec<u8>> {
        let url = format!("{}/api/sync/files/{}/download", self.base, id);
        let resp = self
            .auth(ureq::get(&url))
            .call()
            .with_context(|| "download failed")?;
        let mut buf = Vec::new();
        crate::throttle::ThrottledReader::new(resp.into_reader(), &self.throttle)
            .read_to_end(&mut buf)
            .context("reading download body")?;
        Ok(buf)
    }

    /// Updates an existing file's content with optimistic concurrency control.
    /// `if_match` is the base hash the daemon knew about; a 412 from the server means a conflict.
    pub fn update_file(&self, id: &str, if_match: &str, body: &[u8]) -> Result<PutOutcome> {
        let url = format!("{}/api/sync/files/{}", self.base, id);
        let res = self
            .auth(ureq::put(&url))
            .set("If-Match", if_match)
            .set("Content-Type", "application/octet-stream")
            .send(crate::throttle::ThrottledReader::new(
                std::io::Cursor::new(body),
                &self.throttle,
            ));
        match res {
            Ok(resp) => Ok(PutOutcome::Updated(resp.into_json::<FileMeta>()?)),
            // 412 Precondition Failed = the server changed since the base -> conflict (not a fatal error).
            Err(ureq::Error::Status(412, _)) => Ok(PutOutcome::Conflict),
            Err(e) => Err(anyhow::Error::new(e).context("PUT update failed")),
        }
    }

    /// Creates a new file from the local content.
    pub fn create_file(
        &self,
        name: &str,
        folder_id: Option<&str>,
        body: &[u8],
    ) -> Result<FileMeta> {
        let url = format!("{}/api/sync/files", self.base);
        let mut req = self
            .auth(ureq::post(&url))
            .set("Content-Type", "application/octet-stream")
            .query("name", name);
        if let Some(fid) = folder_id {
            req = req.query("folder_id", fid);
        }
        let resp = req
            .send(crate::throttle::ThrottledReader::new(
                std::io::Cursor::new(body),
                &self.throttle,
            ))
            .with_context(|| "POST create failed")?;
        Ok(resp.into_json::<FileMeta>()?)
    }

    /// Per-block hashes of the CURRENT content on the server (to compute the delta).
    pub fn file_blocks(&self, id: &str, block_size: u64) -> Result<BlocksInfo> {
        let url = format!("{}/api/sync/files/{}/blocks", self.base, id);
        let resp = self
            .auth(ureq::get(&url))
            .query("block_size", &block_size.to_string())
            .call()
            .with_context(|| "GET blocks failed")?;
        Ok(resp.into_json::<BlocksInfo>()?)
    }

    /// Sends ONLY the changed blocks; the server rebuilds the file. `body` is the concatenation of
    /// `changed`'s blocks, in the same order. If-Match guarantees the server did not change meanwhile.
    pub fn update_file_delta(
        &self,
        id: &str,
        if_match: &str,
        block_size: u64,
        total_size: u64,
        changed: &[usize],
        body: Vec<u8>,
    ) -> Result<PutOutcome> {
        let url = format!("{}/api/sync/files/{}/delta", self.base, id);
        let list = changed
            .iter()
            .map(|i| i.to_string())
            .collect::<Vec<_>>()
            .join(",");
        let res = self
            .auth(ureq::put(&url))
            .set("If-Match", if_match)
            .set("Content-Type", "application/octet-stream")
            .query("block_size", &block_size.to_string())
            .query("total_size", &total_size.to_string())
            .query("blocks", &list)
            .send(crate::throttle::ThrottledReader::new(
                std::io::Cursor::new(body),
                &self.throttle,
            ));
        match res {
            Ok(resp) => Ok(PutOutcome::Updated(resp.into_json::<FileMeta>()?)),
            Err(ureq::Error::Status(412, _)) => Ok(PutOutcome::Conflict),
            Err(e) => Err(anyhow::Error::new(e).context("PUT delta failed")),
        }
    }

    /// The file was deleted in the local folder -> send it to the server's TRASH (recoverable).
    pub fn trash_file(&self, id: &str) -> Result<()> {
        let url = format!("{}/api/sync/files/{}/trash", self.base, id);
        self.auth(ureq::post(&url))
            .call()
            .with_context(|| "POST trash failed")?;
        Ok(())
    }

    /// The file was moved/renamed locally -> mirror it on the server. `folder_id` None = root.
    pub fn move_file(&self, id: &str, folder_id: Option<&str>, name: &str) -> Result<FileMeta> {
        let url = format!("{}/api/sync/files/{}/move", self.base, id);
        let mut req = self.auth(ureq::post(&url)).query("name", name);
        if let Some(fid) = folder_id {
            req = req.query("folder_id", fid);
        }
        let resp = req.call().with_context(|| "POST move failed")?;
        Ok(resp.into_json::<FileMeta>()?)
    }

    /// The user's whole folder tree.
    pub fn folders(&self) -> Result<Vec<Folder>> {
        let url = format!("{}/api/sync/folders", self.base);
        let resp = self
            .auth(ureq::get(&url))
            .call()
            .with_context(|| "GET folders failed")?;
        Ok(resp.into_json::<FoldersPage>()?.folders)
    }

    /// Creates a folder (only for the segments that do not exist yet).
    pub fn create_folder(&self, name: &str, parent_id: Option<&str>) -> Result<Folder> {
        let url = format!("{}/api/sync/folders", self.base);
        let mut req = self.auth(ureq::post(&url)).query("name", name);
        if let Some(pid) = parent_id {
            req = req.query("parent_id", pid);
        }
        let resp = req.call().with_context(|| "POST folder failed")?;
        Ok(resp.into_json::<Folder>()?)
    }
}
