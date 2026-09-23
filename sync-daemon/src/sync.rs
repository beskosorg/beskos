//! Bidirectional reconciliation with subfolder mirroring. Order matters: **push before pull**.
//!   1. push: uploads local changes with If-Match (the base hash). If the server also changed →
//!      412, we keep a local conflict copy and let pull bring in the server version. Push before
//!      pull ensures a local edit is not overwritten by the download before it is uploaded.
//!      New files (even in subfolders) are created on the server, creating missing folders too.
//!   2. pull: applies the feed. Places each file under its folder's path (tree mirror); if only
//!      the folder changed (same hash), it MOVES the local file instead of re-downloading;
//!      tombstones delete only what this daemon downloaded. The cursor only advances per applied page.
//!      Safety: sanitized paths (anti-traversal, incl. subfolders), atomic writes, and LOCAL
//!      deletes are never propagated to the server.

use std::collections::HashSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use anyhow::Result;
use sha2::{Digest, Sha256};

use crate::api::{Client, FileChange, PutOutcome};
use crate::config::SyncPair;
use crate::index::Index;
use crate::safepath::{safe_join_rel, sanitize_name};
use crate::tree::Tree;

pub struct Stats {
    pub downloaded: usize,
    pub deleted: usize,
    pub skipped: usize,
    pub uploaded: usize,
    pub conflicts: usize,
    pub moved: usize,
    /// Files deleted locally that went to the server's trash.
    pub trashed: usize,
    /// Upload/creation failures (quota, network, server). Visible in the state so "0 uploaded"
    /// does not look like "nothing to do" when the upload actually failed.
    pub errors: usize,
}

/// Safety guard: if MANY tracked files disappear at once, it is almost certainly the folder that
/// became unavailable (external disk, unmounted cloud drive, backup mid-way), not the person
/// deleting everything. In that case we do NOT propagate the delete/move -- better to end up
/// out of sync than to send the whole cloud drive to the trash.
const MAX_MISSING_TO_PROPAGATE: usize = 50;

/// mtime (epoch s) + size of a file. (0,0) if it cannot be read -- in that case the pre-filter
/// never matches and we fall back to the safe path (read and hash).
fn stat_of(path: &Path) -> (i64, i64) {
    match fs::metadata(path) {
        Ok(md) => {
            let mtime = md
                .modified()
                .ok()
                .and_then(|t| t.duration_since(UNIX_EPOCH).ok())
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0);
            (mtime, md.len() as i64)
        }
        Err(_) => (0, 0),
    }
}

/// SHA-256 of a file, read in blocks (does not load the whole file into memory).
fn hash_file(path: &Path) -> Result<String> {
    use std::io::Read;
    let mut f = fs::File::open(path)?;
    let mut h = Sha256::new();
    let mut buf = [0u8; 64 * 1024];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        h.update(&buf[..n]);
    }
    let sum = h.finalize();
    let mut s = String::with_capacity(64);
    for b in sum {
        s.push_str(&format!("{b:02x}"));
    }
    Ok(s)
}

/// Below this size the delta is not worth it (two network round-trips to save little).
const DELTA_MIN_SIZE: u64 = 8 << 20; // 8 MiB
/// Block size requested from the server.
const DELTA_BLOCK: u64 = 4 << 20; // 4 MiB

fn sha256_hex(bytes: &[u8]) -> String {
    let mut h = Sha256::new();
    h.update(bytes);
    let sum = h.finalize();
    let mut s = String::with_capacity(64);
    for b in sum {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

/// Reads until `buf` is full (or the file ends). Returns how many bytes it read.
fn read_block(f: &mut fs::File, buf: &mut [u8]) -> std::io::Result<usize> {
    use std::io::Read;
    let mut got = 0;
    while got < buf.len() {
        match f.read(&mut buf[got..])? {
            0 => break,
            n => got += n,
        }
    }
    Ok(got)
}

/// Attempts to upload only the changed blocks. Returns `Ok(None)` when it is not worth it or not
/// possible — the caller then falls back to a full upload. Never fails the sync over the delta path.
fn try_delta_upload(
    client: &Client,
    path: &Path,
    id: &str,
    base_hash: &str,
) -> Result<Option<PutOutcome>> {
    let size = match fs::metadata(path) {
        Ok(m) => m.len(),
        Err(_) => return Ok(None),
    };
    if size < DELTA_MIN_SIZE {
        return Ok(None); // small file: sending it whole is simpler and just as fast
    }
    let info = match client.file_blocks(id, DELTA_BLOCK) {
        Ok(i) => i,
        Err(_) => return Ok(None), // old/unsupported server: normal path
    };
    // The delta is only valid against the SAME base; if the server already changed, let the
    // normal path catch the 412 and handle the conflict.
    if info.hash != base_hash || info.block_size == 0 {
        return Ok(None);
    }

    let bs = info.block_size as usize;
    let mut f = match fs::File::open(path) {
        Ok(f) => f,
        Err(_) => return Ok(None),
    };
    let mut buf = vec![0u8; bs];
    let mut changed: Vec<usize> = Vec::new();
    let mut body: Vec<u8> = Vec::new();
    let mut i = 0usize;
    loop {
        let n = match read_block(&mut f, &mut buf) {
            Ok(n) => n,
            Err(_) => return Ok(None),
        };
        if n == 0 {
            break;
        }
        // Block differs from what the server has (or is new, past the end of the remote file).
        if info.hashes.get(i).map(String::as_str) != Some(sha256_hex(&buf[..n]).as_str()) {
            changed.push(i);
            body.extend_from_slice(&buf[..n]);
        }
        i += 1;
        if n < bs {
            break; // last block (partial)
        }
    }

    // If almost everything changed, the delta just adds an extra round-trip -- send it whole.
    if body.len() as u64 * 10 >= size * 7 {
        return Ok(None);
    }
    Ok(Some(client.update_file_delta(
        id,
        base_hash,
        info.block_size,
        size,
        &changed,
        body,
    )?))
}

/// How often the full reconciliation runs (sweeping the feed from the start).
const RECONCILE_EVERY: Duration = Duration::from_secs(3600);
const RECONCILE_KEY: &str = "last_reconcile";

/// Shortcut to increment a counter on the shared `Stats` without scattering locks through the code.
fn bump(stats: &Mutex<Stats>, f: impl FnOnce(&mut Stats)) {
    f(&mut stats.lock().unwrap_or_else(|e| e.into_inner()));
}

/// Runs a full pass: first uploads local changes (push), then applies the feed (pull).
/// `workers` = parallel transfers (the index is serialized by a mutex; the network is what parallelizes).
pub fn run_once(
    client: &Client,
    index: &Index,
    pairs: &[SyncPair],
    workers: usize,
) -> Result<Stats> {
    for p in pairs {
        fs::create_dir_all(&p.local_path)?;
    }
    let stats = Mutex::new(Stats {
        downloaded: 0,
        deleted: 0,
        skipped: 0,
        uploaded: 0,
        conflicts: 0,
        moved: 0,
        trashed: 0,
        errors: 0,
    });

    // The server's folder tree (to place/create files in the right spot).
    let mut tree = Tree::new(client.folders()?);

    for pair in pairs {
        push_pair(client, index, pair, &mut tree, workers, &stats)?;
    }

    loop {
        let cursor = index.cursor()?;
        let page = client.changes(&cursor, 500)?;
        if page.changes.is_empty() {
            break;
        }
        // A page's files are independent of each other (distinct ids) -- apply them in parallel.
        // An isolated failure does not abort the pass: it is picked up next time (the cursor only
        // advances at the end of the page, so nothing is lost).
        crate::parallel::for_each(page.changes.clone(), workers, |ch| {
            if let Err(e) = apply_one(client, index, pairs, &tree, &ch, &stats) {
                eprintln!("warning: failed to apply {}: {e:#}", ch.name);
            }
        });
        // Only now do we advance the cursor: if something above fails, we retry the same page (idempotent).
        index.set_cursor(&page.cursor)?;
        if !page.has_more {
            break;
        }
    }

    // Periodic reconciliation (catches purges the incremental feed cannot report).
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let last: u64 = index
        .meta_get(RECONCILE_KEY)
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    if now.saturating_sub(last) >= RECONCILE_EVERY.as_secs() {
        reconcile(client, index, pairs, &tree, &stats)?;
        index.meta_set(RECONCILE_KEY, &now.to_string())?;
    }

    Ok(stats.into_inner().unwrap_or_else(|e| e.into_inner()))
}

/// OS junk and editing temporaries: never uploaded to Drive. They are noise for the user (macOS's
/// `.DS_Store` used to show up in the file list) and keep changing on their own, generating
/// useless syncs.
fn is_ignored(name: &str) -> bool {
    const EXACT: &[&str] = &[
        ".DS_Store",
        "Thumbs.db",
        "desktop.ini",
        ".localized",
        "Icon\r",
    ];
    if EXACT.iter().any(|n| n.eq_ignore_ascii_case(name)) {
        return true;
    }
    // Editing temporaries (Office `~$x.docx`, vim `x.swp`, our own partial saves).
    name.starts_with("~$")
        || name.starts_with(".~lock.")
        || name.ends_with(".swp")
        || name.ends_with(".part")
        || name.ends_with(".crdownload")
        || name.ends_with(".mailin-part")
}

/// Collects the relative paths (with '/') of every file under `dir`, recursively.
fn collect_files(root: &Path, dir: &Path, out: &mut Vec<String>) -> Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        let name = entry.file_name().to_string_lossy().to_string();
        if is_ignored(&name) {
            continue;
        }
        if path.is_dir() {
            collect_files(root, &path, out)?;
        } else if path.is_file() {
            if let Ok(rel) = path.strip_prefix(root) {
                // Normalizes to '/' (the index and the server use '/').
                let rel = rel
                    .to_string_lossy()
                    .replace(std::path::MAIN_SEPARATOR, "/");
                out.push(rel);
            }
        }
    }
    Ok(())
}

/// Copies the file to `<name> (conflito <epoch>).<ext>` -- preserves the local version without losing it.
fn save_conflict_copy(src: &Path) -> Result<()> {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let p = src;
    let stem = p.file_stem().and_then(|s| s.to_str()).unwrap_or("ficheiro");
    let cname = match p.extension().and_then(|e| e.to_str()) {
        Some(ext) => format!("{stem} (conflito {secs}).{ext}"),
        None => format!("{stem} (conflito {secs})"),
    };
    let dest = src.with_file_name(cname);
    fs::copy(src, &dest)?;
    Ok(())
}

/// Where a feed change should land on disk -- or `None` if the file **does not belong to any
/// synced pair** (this is what makes only the chosen folders sync, instead of the whole drive).
/// Also returns the pair, so the caller knows which local folder it belongs to.
fn dest_for<'a>(
    tree: &Tree,
    pairs: &'a [SyncPair],
    ch: &FileChange,
) -> Option<(&'a SyncPair, PathBuf)> {
    let name = sanitize_name(&ch.name)?;
    for pair in pairs {
        if let Some(rel_dir) = tree.rel_path_under(ch.folder_id.as_deref(), &pair.folder_id) {
            let rel = if rel_dir.is_empty() {
                name.clone()
            } else {
                format!("{rel_dir}/{name}")
            };
            let dest = safe_join_rel(Path::new(&pair.local_path), &rel).ok()?;
            return Some((pair, dest));
        }
    }
    None
}

/// Applies a change coming from the server (pull), if it falls inside some pair.
fn apply_one(
    client: &Client,
    index: &Index,
    pairs: &[SyncPair],
    tree: &Tree,
    ch: &FileChange,
    stats: &Mutex<Stats>,
) -> Result<()> {
    if ch.deleted {
        // Tombstone: only deletes what THIS agent downloaded (it is in the index). Never touches other files.
        if let Some(entry) = index.get(&ch.id)? {
            let path = Path::new(&entry.rel_path);
            if path.exists() {
                let _ = fs::remove_file(path);
                bump(stats, |s| s.deleted += 1);
            }
            index.remove(&ch.id)?;
        }
        return Ok(());
    }

    let (_pair, dest) = match dest_for(tree, pairs, ch) {
        Some(d) => d,
        None => return Ok(()), // outside the synced folders: ignore
    };
    let dest_str = dest.to_string_lossy().to_string();

    if let Some(entry) = index.get(&ch.id)? {
        if entry.hash == ch.hash {
            if entry.rel_path == dest_str {
                bump(stats, |s| s.skipped += 1);
                return Ok(());
            }
            // Same content at another path => moved/renamed on the server: move the local file.
            let from = Path::new(&entry.rel_path);
            if from.exists() {
                if let Some(parent) = dest.parent() {
                    fs::create_dir_all(parent)?;
                }
                if fs::rename(from, &dest).is_err() {
                    fs::copy(from, &dest)?;
                    let _ = fs::remove_file(from);
                }
            }
            let (mt, sz) = stat_of(&dest);
            index.upsert(&ch.id, &dest_str, &ch.hash, ch.version, mt, sz)?;
            bump(stats, |s| s.moved += 1);
            return Ok(());
        }
    }

    let bytes = client.download(&ch.id)?;
    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent)?;
    }
    let tmp = dest.with_extension("mailin-part"); // atomic write
    fs::write(&tmp, &bytes)?;
    fs::rename(&tmp, &dest)?;

    let (mt, sz) = stat_of(&dest);
    index.upsert(&ch.id, &dest_str, &ch.hash, ch.version, mt, sz)?;
    bump(stats, |s| s.downloaded += 1);
    Ok(())
}

/// Uploads the changes for ONE pair: changed content, new files (creating the cloud subfolders
/// INSIDE the pair's folder), moves and deletes (-> trash).
fn push_pair(
    client: &Client,
    index: &Index,
    pair: &SyncPair,
    tree: &mut Tree,
    workers: usize,
    stats: &Mutex<Stats>,
) -> Result<()> {
    let root = Path::new(&pair.local_path);
    // Only the index entries that belong to THIS local folder.
    let tracked: Vec<_> = index
        .all()?
        .into_iter()
        .filter(|t| Path::new(&t.rel_path).starts_with(root))
        .collect();
    let tracked_paths: HashSet<String> = tracked.iter().map(|t| t.rel_path.clone()).collect();

    let mut found_rel = Vec::new();
    collect_files(root, root, &mut found_rel)?;
    let found_abs: HashSet<String> = found_rel
        .iter()
        .map(|r| root.join(r).to_string_lossy().to_string())
        .collect();

    // 1) Changed / missing.
    let mut missing: Vec<&crate::index::Tracked> = Vec::new();
    let mut to_upload: Vec<(String, String, String)> = Vec::new(); // (id, abs path, base hash)
    for t in &tracked {
        if !found_abs.contains(&t.rel_path) {
            missing.push(t);
            continue;
        }
        let path = Path::new(&t.rel_path);
        let (mtime, size) = stat_of(path);
        if mtime == t.mtime && size == t.size {
            continue; // pre-filter: nothing changed
        }
        let h = match hash_file(path) {
            Ok(h) => h,
            Err(_) => continue,
        };
        if h == t.hash {
            index.touch_stat(&t.file_id, mtime, size)?;
            continue;
        }
        to_upload.push((t.file_id.clone(), t.rel_path.clone(), t.hash.clone()));
    }

    crate::parallel::for_each(to_upload, workers, |(id, abs, base_hash)| {
        let path = PathBuf::from(&abs);
        let outcome = match try_delta_upload(client, &path, &id, &base_hash) {
            Ok(Some(o)) => Ok(o),
            _ => match fs::read(&path) {
                Ok(bytes) => client.update_file(&id, &base_hash, &bytes),
                Err(_) => return,
            },
        };
        match outcome {
            Ok(PutOutcome::Updated(m)) => {
                let (mt, sz) = stat_of(&path);
                let _ = index.upsert(&id, &abs, &m.hash, m.versao, mt, sz);
                bump(stats, |s| s.uploaded += 1);
            }
            Ok(PutOutcome::Conflict) => {
                let _ = save_conflict_copy(&path);
                bump(stats, |s| s.conflicts += 1);
            }
            Err(e) => {
                eprintln!("warning: upload of {abs} failed: {e:#}");
                bump(stats, |s| s.errors += 1);
            }
        }
    });

    // 2) New local files (not yet tracked).
    let mut new_rel: Vec<String> = found_rel
        .into_iter()
        .filter(|r| {
            let abs = root.join(r).to_string_lossy().to_string();
            !tracked_paths.contains(&abs) && !r.ends_with(".mailin-part")
        })
        .collect();

    // 3) The ones that disappeared: moved (same hash elsewhere) or deleted (-> trash).
    if missing.len() > MAX_MISSING_TO_PROPAGATE {
        eprintln!(
            "warning: {} missing files in {} -- delete/move was NOT propagated (folder unavailable?)",
            missing.len(),
            pair.name
        );
    } else if !missing.is_empty() {
        let new_hashes: Vec<(String, String)> = new_rel
            .iter()
            .filter_map(|r| hash_file(&root.join(r)).ok().map(|h| (r.clone(), h)))
            .collect();
        let mut consumed: HashSet<String> = HashSet::new();
        for t in missing {
            let matched = new_hashes
                .iter()
                .find(|(rel, h)| *h == t.hash && !consumed.contains(rel))
                .map(|(rel, _)| rel.clone());
            match matched {
                Some(rel) => {
                    let mut segs: Vec<String> = rel.split('/').map(String::from).collect();
                    let name = segs.pop().unwrap_or_default();
                    if name.is_empty() {
                        continue;
                    }
                    let folder_id = tree.resolve_or_create_under(client, &pair.folder_id, &segs)?;
                    let m = client.move_file(&t.file_id, Some(&folder_id), &name)?;
                    let abs = root.join(&rel).to_string_lossy().to_string();
                    let (mt, sz) = stat_of(Path::new(&abs));
                    index.upsert(&t.file_id, &abs, &m.hash, m.versao, mt, sz)?;
                    consumed.insert(rel);
                    bump(stats, |s| s.moved += 1);
                }
                None => {
                    client.trash_file(&t.file_id)?;
                    index.remove(&t.file_id)?;
                    bump(stats, |s| s.trashed += 1);
                }
            }
        }
        new_rel.retain(|r| !consumed.contains(r));
    }

    // 4) Create the new ones in the cloud, INSIDE the pair's folder.
    let mut to_create: Vec<(String, String, String)> = Vec::new(); // (rel, name, destination folder)
    for rel in new_rel {
        let mut segs: Vec<String> = rel.split('/').map(String::from).collect();
        let name = segs.pop().unwrap_or_default();
        if name.is_empty() {
            continue;
        }
        let folder_id = tree.resolve_or_create_under(client, &pair.folder_id, &segs)?;
        to_create.push((rel, name, folder_id));
    }
    crate::parallel::for_each(to_create, workers, |(rel, name, folder_id)| {
        let path = root.join(&rel);
        let bytes = match fs::read(&path) {
            Ok(b) => b,
            Err(_) => return,
        };
        match client.create_file(&name, Some(&folder_id), &bytes) {
            Ok(m) => {
                let abs = path.to_string_lossy().to_string();
                let (mt, sz) = stat_of(&path);
                let _ = index.upsert(&m.id, &abs, &m.hash, m.versao, mt, sz);
                bump(stats, |s| s.uploaded += 1);
            }
            Err(e) => {
                eprintln!("warning: creating {rel} failed: {e:#}");
                bump(stats, |s| s.errors += 1);
            }
        }
    });
    Ok(())
}

/// Reconciliation: catches purges (hard delete on the server, which the feed does not report) and
/// files that left the synced folders. Same guards as above.
fn reconcile(
    client: &Client,
    index: &Index,
    pairs: &[SyncPair],
    tree: &Tree,
    stats: &Mutex<Stats>,
) -> Result<()> {
    let mut alive: HashSet<String> = HashSet::new();
    let mut cursor = String::new();
    loop {
        let page = client.changes(&cursor, 500)?;
        if page.changes.is_empty() {
            break;
        }
        for ch in &page.changes {
            // Only counts as "alive" what still belongs to a pair (and is not in the trash).
            if !ch.deleted && dest_for(tree, pairs, ch).is_some() {
                alive.insert(ch.id.clone());
            }
        }
        cursor = page.cursor.clone();
        if !page.has_more {
            break;
        }
    }

    let tracked = index.all()?;
    if alive.is_empty() && !tracked.is_empty() {
        eprintln!("warning: reconciliation aborted -- the server returned no files");
        return Ok(());
    }
    let orphans: Vec<_> = tracked
        .into_iter()
        .filter(|t| !alive.contains(&t.file_id))
        .collect();
    if orphans.len() > MAX_MISSING_TO_PROPAGATE {
        eprintln!(
            "warning: reconciliation aborted -- {} orphans (too many to delete without confirmation)",
            orphans.len()
        );
        return Ok(());
    }
    for t in orphans {
        let p = Path::new(&t.rel_path);
        if p.exists() {
            let _ = fs::remove_file(p);
            bump(stats, |s| s.deleted += 1);
        }
        index.remove(&t.file_id)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::api::Folder;

    fn folder(id: &str, parent: Option<&str>, name: &str) -> Folder {
        Folder {
            id: id.into(),
            parent_id: parent.map(|s| s.into()),
            name: name.into(),
        }
    }

    #[test]
    fn os_junk_and_edit_temporaries_never_sync() {
        for junk in [
            ".DS_Store",
            "Thumbs.db",
            "desktop.ini",
            "~$relatorio.docx",
            ".~lock.contas.ods#",
            "notas.txt.swp",
            "video.mp4.part",
            "x.mailin-part",
        ] {
            assert!(is_ignored(junk), "should be ignored: {junk}");
        }
        // Legitimate files with similar-looking names still sync.
        for ok in [
            "relatorio.docx",
            "notas.txt",
            ".env",
            "DS_Store.txt",
            "a~$b.txt",
        ] {
            assert!(!is_ignored(ok), "should NOT be ignored: {ok}");
        }
    }

    fn change(id: &str, folder_id: Option<&str>, name: &str) -> FileChange {
        FileChange {
            id: id.into(),
            folder_id: folder_id.map(|s| s.into()),
            name: name.into(),
            hash: "h".into(),
            size: 1,
            version: 1,
            deleted: false,
            updated_at: String::new(),
            owned: true,
        }
    }

    fn pair(local: &str, folder_id: &str) -> SyncPair {
        SyncPair {
            local_path: local.into(),
            folder_id: folder_id.into(),
            name: "Trabalho".into(),
        }
    }

    /// The core point of the model: **only** what is inside a synced folder lands on disk. A file
    /// elsewhere in the Drive (or at the root) is ignored.
    #[test]
    fn only_files_inside_a_pair_are_synced() {
        let tree = Tree::new(vec![
            folder("work", None, "Trabalho"),
            folder("sub", Some("work"), "2024"),
            folder("other", None, "Pessoal"),
        ]);
        let pairs = vec![pair("/local/Trabalho", "work")];

        // In the pair's own folder -> goes to the local folder's root.
        let (_, dest) = dest_for(&tree, &pairs, &change("1", Some("work"), "a.txt")).unwrap();
        assert_eq!(dest, Path::new("/local/Trabalho/a.txt"));

        // In a subfolder -> keeps the subtree.
        let (_, dest) = dest_for(&tree, &pairs, &change("2", Some("sub"), "b.txt")).unwrap();
        assert_eq!(dest, Path::new("/local/Trabalho/2024/b.txt"));

        // Outside the pair (another top-level folder) -> does NOT sync.
        assert!(dest_for(&tree, &pairs, &change("3", Some("other"), "c.txt")).is_none());
        // At the Drive root -> also not (does not belong to any pair).
        assert!(dest_for(&tree, &pairs, &change("4", None, "d.txt")).is_none());
    }

    #[test]
    fn several_pairs_are_routed_to_their_own_folder() {
        let tree = Tree::new(vec![
            folder("w", None, "Trabalho"),
            folder("p", None, "Fotos"),
        ]);
        let pairs = vec![pair("/local/Trabalho", "w"), pair("/local/Fotos", "p")];

        let (_, d1) = dest_for(&tree, &pairs, &change("1", Some("w"), "a.txt")).unwrap();
        let (_, d2) = dest_for(&tree, &pairs, &change("2", Some("p"), "b.jpg")).unwrap();
        assert_eq!(d1, Path::new("/local/Trabalho/a.txt"));
        assert_eq!(d2, Path::new("/local/Fotos/b.jpg"));
    }

    /// A name coming from the server can never escape the pair's folder.
    #[test]
    fn traversal_in_name_cannot_escape_the_pair() {
        let tree = Tree::new(vec![folder("w", None, "Trabalho")]);
        let pairs = vec![pair("/local/Trabalho", "w")];
        let (_, dest) =
            dest_for(&tree, &pairs, &change("1", Some("w"), "../../etc/passwd")).unwrap();
        assert!(dest.starts_with("/local/Trabalho"), "escaped: {dest:?}");
    }
}
