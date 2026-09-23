//! The user's folder tree, for the subfolder mirror. Translates `folder_id` <-> relative path
//! (with '/'). Also resolves/creates the server folder for a local path when uploading.

use std::collections::HashMap;

use anyhow::Result;

use crate::api::{Client, Folder};
use crate::safepath::sanitize_name;

pub struct Tree {
    by_id: HashMap<String, Folder>,
}

impl Tree {
    pub fn new(folders: Vec<Folder>) -> Self {
        let by_id = folders.into_iter().map(|f| (f.id.clone(), f)).collect();
        Tree { by_id }
    }

    /// Relative path (with '/') of a folder. Root (`None`) -> "". Returns None if there is a cycle
    /// or an unsafe segment name (in that case the caller falls back to the root -- it never
    /// writes outside it).
    pub fn rel_path(&self, folder_id: Option<&str>) -> Option<String> {
        let mut segs = Vec::new();
        let mut cur = folder_id.map(|s| s.to_string());
        let mut guard = 0;
        while let Some(id) = cur {
            guard += 1;
            if guard > 64 {
                return None; // cycle / absurd depth
            }
            let f = self.by_id.get(&id)?;
            segs.push(sanitize_name(&f.name)?);
            cur = f.parent_id.clone();
        }
        segs.reverse();
        Some(segs.join("/"))
    }

    /// Path of `folder_id` RELATIVE to `root_id` (a synced pair's cloud folder).
    /// - `Some("")` if it is the pair's own root folder;
    /// - `Some("sub/dir")` if it is a descendant;
    /// - `None` if it is outside the pair (this is how the rest of the drive gets ignored).
    pub fn rel_path_under(&self, folder_id: Option<&str>, root_id: &str) -> Option<String> {
        let mut segs: Vec<String> = Vec::new();
        let mut cur = folder_id.map(|s| s.to_string());
        let mut guard = 0;
        while let Some(id) = cur {
            if id == root_id {
                segs.reverse();
                return Some(segs.join("/"));
            }
            guard += 1;
            if guard > 64 {
                return None; // cycle
            }
            let f = self.by_id.get(&id)?;
            segs.push(sanitize_name(&f.name)?);
            cur = f.parent_id.clone();
        }
        None // reached the drive root without passing through the pair's folder
    }

    /// Resolves the server's `folder_id` into a sequence of local folder segments, creating
    /// whichever are missing (reuses existing ones by (parent, name), avoiding duplicates).
    /// `[]` -> root (None). Like `resolve_or_create`, but starting from a pair's folder instead of
    /// the drive root.
    pub fn resolve_or_create_under(
        &mut self,
        client: &Client,
        root_id: &str,
        segments: &[String],
    ) -> Result<String> {
        let id = self.resolve_from(client, Some(root_id.to_string()), segments)?;
        Ok(id.unwrap_or_else(|| root_id.to_string()))
    }

    pub fn resolve_or_create(
        &mut self,
        client: &Client,
        segments: &[String],
    ) -> Result<Option<String>> {
        self.resolve_from(client, None, segments)
    }

    fn resolve_from(
        &mut self,
        client: &Client,
        start: Option<String>,
        segments: &[String],
    ) -> Result<Option<String>> {
        let mut parent: Option<String> = start;
        for seg in segments {
            let existing = self
                .by_id
                .values()
                .find(|f| f.parent_id.as_deref() == parent.as_deref() && f.name == *seg)
                .map(|f| f.id.clone());
            let id = match existing {
                Some(id) => id,
                None => {
                    let f = client.create_folder(seg, parent.as_deref())?;
                    let id = f.id.clone();
                    self.by_id.insert(id.clone(), f);
                    id
                }
            };
            parent = Some(id);
        }
        Ok(parent)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn f(id: &str, parent: Option<&str>, name: &str) -> Folder {
        Folder {
            id: id.into(),
            parent_id: parent.map(|s| s.into()),
            name: name.into(),
        }
    }

    #[test]
    fn builds_nested_paths() {
        let t = Tree::new(vec![f("a", None, "docs"), f("b", Some("a"), "2024")]);
        assert_eq!(t.rel_path(None), Some(String::new()));
        assert_eq!(t.rel_path(Some("a")), Some("docs".to_string()));
        assert_eq!(t.rel_path(Some("b")), Some("docs/2024".to_string()));
        assert_eq!(t.rel_path(Some("missing")), None);
    }

    #[test]
    fn detects_cycle() {
        // a -> b -> a: a cycle must not hang the daemon.
        let t = Tree::new(vec![f("a", Some("b"), "x"), f("b", Some("a"), "y")]);
        assert_eq!(t.rel_path(Some("a")), None);
    }
}
