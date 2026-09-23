//! Security: never write outside the sync folder. The file name comes from the server; a
//! compromised server could send `../../.ssh/authorized_keys`. Here we sanitize the name down to
//! a single safe component and resolve the final path INSIDE the root, rejecting anything that escapes.

use std::path::{Component, Path, PathBuf};

/// Sanitizes a file name down to a single safe component. Returns None if nothing usable is left
/// (the caller treats this as invalid input and skips the file).
pub fn sanitize_name(name: &str) -> Option<String> {
    // Cuts off any path separator (the name has to be ONE component, never a subtree).
    let base = name.rsplit(['/', '\\']).next().unwrap_or("").trim();
    // Rejects empty names, "." and ".." and names with NUL (avoids truncation in OS calls).
    if base.is_empty() || base == "." || base == ".." || base.contains('\0') {
        return None;
    }
    Some(base.to_string())
}

/// Resolves a RELATIVE path with subfolders (segments separated by '/') INSIDE `root`. Each
/// segment is sanitized; any unsafe `..`/`.` errors out and the result has to stay under the root.
pub fn safe_join_rel(root: &Path, rel: &str) -> anyhow::Result<PathBuf> {
    let mut out = root.to_path_buf();
    for seg in rel.split('/') {
        if seg.is_empty() {
            continue;
        }
        let clean = sanitize_name(seg).ok_or_else(|| anyhow::anyhow!("invalid path segment"))?;
        out.push(clean);
    }
    // Final check: no `..` components, and the result has to stay under the root.
    for comp in out.components() {
        if matches!(comp, Component::ParentDir) {
            anyhow::bail!("path attempts to go up a directory");
        }
    }
    if !out.starts_with(root) {
        anyhow::bail!("resolved path is outside the sync root");
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;

    #[test]
    fn rejects_traversal_names() {
        assert_eq!(sanitize_name("../secret"), Some("secret".to_string())); // separator cut off
        assert_eq!(sanitize_name(".."), None);
        assert_eq!(sanitize_name("."), None);
        assert_eq!(sanitize_name(""), None);
        assert_eq!(sanitize_name("a/b/c.txt"), Some("c.txt".to_string()));
        assert_eq!(sanitize_name("evil\0.txt"), None);
    }

    #[test]
    fn safe_join_rel_handles_subdirs() {
        let root = Path::new("/sync/root");
        assert_eq!(
            safe_join_rel(root, "docs/2024/plano.txt").unwrap(),
            Path::new("/sync/root/docs/2024/plano.txt")
        );
        // Leading/double slashes are harmless (empty segments are ignored).
        assert_eq!(
            safe_join_rel(root, "a//b.txt").unwrap(),
            Path::new("/sync/root/a/b.txt")
        );
        // Any ".." segment is rejected (never escapes the root).
        assert!(safe_join_rel(root, "docs/../etc/passwd").is_err());
        assert!(safe_join_rel(root, "../x").is_err());
    }
}
