//! Local SQLite index: tracks the files THIS daemon has downloaded (server id -> relative path +
//! hash + version) and the feed cursor. It is the source of truth for knowing what is already on
//! disk and -- critical for safety -- what the daemon is allowed to delete (only what it created
//! itself; it never touches unknown local files).

use std::sync::Mutex;

use anyhow::Result;
use rusqlite::{params, Connection};

/// rusqlite's `Connection` is `Send` but not `Sync`, so it sits behind a `Mutex`: this way a
/// `&Index` can be shared by the transfer threads. Serializing SQLite is not a problem -- the
/// bottleneck is the network, and the transfers run in parallel outside this lock.
pub struct Index {
    conn: Mutex<Connection>,
}

pub struct Entry {
    pub rel_path: String,
    pub hash: String,
}

pub struct Tracked {
    pub file_id: String,
    pub rel_path: String,
    pub hash: String,
    /// mtime (epoch s) and size at the last sync -- a cheap pre-filter: if both match the disk,
    /// the content has not changed and the file does not even need to be read/hashed.
    pub mtime: i64,
    pub size: i64,
}

impl Index {
    /// Serialized access to the connection. `unwrap_or_else` recovers from a poisoned lock (a
    /// thread that panicked should not leave the index unusable forever).
    fn lock(&self) -> std::sync::MutexGuard<'_, Connection> {
        self.conn.lock().unwrap_or_else(|e| e.into_inner())
    }

    pub fn open(path: &std::path::Path) -> Result<Self> {
        let conn = Connection::open(path)?;
        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS files (
                file_id  TEXT PRIMARY KEY,
                rel_path TEXT NOT NULL,
                hash     TEXT NOT NULL,
                version  INTEGER NOT NULL,
                mtime    INTEGER NOT NULL DEFAULT 0,
                size     INTEGER NOT NULL DEFAULT 0
             );
             CREATE TABLE IF NOT EXISTS meta (
                key TEXT PRIMARY KEY,
                val TEXT NOT NULL
             );",
        )?;
        // Indexes created before the pre-filter do not have these columns: add them (error = already exist).
        let _ = conn.execute(
            "ALTER TABLE files ADD COLUMN mtime INTEGER NOT NULL DEFAULT 0",
            [],
        );
        let _ = conn.execute(
            "ALTER TABLE files ADD COLUMN size INTEGER NOT NULL DEFAULT 0",
            [],
        );
        // The index is just a local CACHE. When the sync model changes (here: from mirroring the
        // whole drive to folder pairs), the stored paths stop making sense -- start over from
        // zero instead of dragging along invalid entries.
        const SCHEMA: &str = "2-pairs";
        let ver: Option<String> = conn
            .query_row("SELECT val FROM meta WHERE key='schema'", [], |r| r.get(0))
            .ok();
        if ver.as_deref() != Some(SCHEMA) {
            conn.execute("DELETE FROM files", [])?;
            conn.execute(
                "DELETE FROM meta WHERE key IN ('cursor','last_reconcile')",
                [],
            )?;
            conn.execute(
                "INSERT INTO meta(key,val) VALUES('schema',?1)
                 ON CONFLICT(key) DO UPDATE SET val=excluded.val",
                params![SCHEMA],
            )?;
        }
        Ok(Self {
            conn: Mutex::new(conn),
        })
    }

    pub fn cursor(&self) -> Result<String> {
        let v: Option<String> = self
            .lock()
            .query_row("SELECT val FROM meta WHERE key='cursor'", [], |r| r.get(0))
            .ok();
        Ok(v.unwrap_or_default())
    }

    pub fn set_cursor(&self, cursor: &str) -> Result<()> {
        self.lock().execute(
            "INSERT INTO meta(key,val) VALUES('cursor',?1)
             ON CONFLICT(key) DO UPDATE SET val=excluded.val",
            params![cursor],
        )?;
        Ok(())
    }

    /// Reads/writes an arbitrary value in `meta` (e.g. when the last full reconciliation ran).
    pub fn meta_get(&self, key: &str) -> Option<String> {
        self.lock()
            .query_row("SELECT val FROM meta WHERE key=?1", params![key], |r| {
                r.get(0)
            })
            .ok()
    }

    pub fn meta_set(&self, key: &str, val: &str) -> Result<()> {
        self.lock().execute(
            "INSERT INTO meta(key,val) VALUES(?1,?2)
             ON CONFLICT(key) DO UPDATE SET val=excluded.val",
            params![key, val],
        )?;
        Ok(())
    }

    /// Returns a file's record, if the daemon has already downloaded it.
    pub fn get(&self, file_id: &str) -> Result<Option<Entry>> {
        let row = self
            .lock()
            .query_row(
                "SELECT rel_path, hash FROM files WHERE file_id=?1",
                params![file_id],
                |r| {
                    Ok(Entry {
                        rel_path: r.get(0)?,
                        hash: r.get(1)?,
                    })
                },
            )
            .ok();
        Ok(row)
    }

    pub fn upsert(
        &self,
        file_id: &str,
        rel_path: &str,
        hash: &str,
        version: i64,
        mtime: i64,
        size: i64,
    ) -> Result<()> {
        self.lock().execute(
            "INSERT INTO files(file_id,rel_path,hash,version,mtime,size) VALUES(?1,?2,?3,?4,?5,?6)
             ON CONFLICT(file_id) DO UPDATE SET rel_path=excluded.rel_path, hash=excluded.hash,
                 version=excluded.version, mtime=excluded.mtime, size=excluded.size",
            params![file_id, rel_path, hash, version, mtime, size],
        )?;
        Ok(())
    }

    /// Updates only the stamp (mtime/size) -- the content is the same, it was just a "touch" on
    /// the file. Avoids re-reading and re-hashing the file on every subsequent pass.
    pub fn touch_stat(&self, file_id: &str, mtime: i64, size: i64) -> Result<()> {
        self.lock().execute(
            "UPDATE files SET mtime=?2, size=?3 WHERE file_id=?1",
            params![file_id, mtime, size],
        )?;
        Ok(())
    }

    /// All tracked files (for the upload pass to compare against the disk).
    pub fn all(&self) -> Result<Vec<Tracked>> {
        let conn = self.lock();
        let mut stmt = conn.prepare("SELECT file_id, rel_path, hash, mtime, size FROM files")?;
        let rows = stmt.query_map([], |r| {
            Ok(Tracked {
                file_id: r.get(0)?,
                rel_path: r.get(1)?,
                hash: r.get(2)?,
                mtime: r.get(3)?,
                size: r.get(4)?,
            })
        })?;
        let mut out = Vec::new();
        for row in rows {
            out.push(row?);
        }
        Ok(out)
    }

    pub fn remove(&self, file_id: &str) -> Result<()> {
        self.lock()
            .execute("DELETE FROM files WHERE file_id=?1", params![file_id])?;
        Ok(())
    }

    /// Forgets the entries INSIDE a local folder (path prefix). Used when (re-)adding a pair: since
    /// the cloud folder is new, any old file_ids here are invalid -- deleting them makes the push
    /// treat the files as NEW and upload them (otherwise the index thinks they are already synced
    /// and nothing goes up). Compares by path the same way the push does (Path::starts_with), robust
    /// to `%`/`_` in the name.
    pub fn remove_under(&self, abs_prefix: &str) -> Result<()> {
        let conn = self.lock();
        let root = std::path::Path::new(abs_prefix);
        let ids: Vec<String> = {
            let mut stmt = conn.prepare("SELECT file_id, rel_path FROM files")?;
            let rows =
                stmt.query_map([], |r| Ok((r.get::<_, String>(0)?, r.get::<_, String>(1)?)))?;
            let mut v = Vec::new();
            for row in rows {
                let (id, rp) = row?;
                if std::path::Path::new(&rp).starts_with(root) {
                    v.push(id);
                }
            }
            v
        };
        for id in ids {
            conn.execute("DELETE FROM files WHERE file_id=?1", params![id])?;
        }
        Ok(())
    }

    /// Clears the index (files + feed cursor/reconcile), preserving the schema. Used when switching
    /// ACCOUNTS on the same machine: the file_ids belong to the previous account's server and are
    /// not valid for the new one -- start over from zero (re-upload local content, re-read the feed).
    pub fn clear(&self) -> Result<()> {
        let conn = self.lock();
        conn.execute("DELETE FROM files", [])?;
        conn.execute(
            "DELETE FROM meta WHERE key IN ('cursor','last_reconcile')",
            [],
        )?;
        Ok(())
    }

    pub fn count(&self) -> Result<i64> {
        Ok(self
            .lock()
            .query_row("SELECT COUNT(*) FROM files", [], |r| r.get(0))?)
    }
}
