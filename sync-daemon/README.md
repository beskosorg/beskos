# mailin-sync — file sync engine

Rust library containing the Beskos sync engine. **It is not an application**: it is hosted by the
desktop app (`Beskos.app`), which provides the window, the menu bar icon and the UI. This keeps it
to a single app.

## Model: folder pairs (does not mirror the whole Drive)

The user picks a **local folder**; a matching folder is created in the **Drive**; from then on only
what is inside it syncs, in both directions. There can be **several** such folders. Files elsewhere
in the Drive are simply ignored.

```
~/Projects   ⇄   Drive/Projects      (pair 1)
~/Photos     ⇄   Drive/Photos        (pair 2)
other Drive files                    → not synced
```

In the app: **Settings › Sync** (add/stop folders, status, pause, bandwidth limit) and the
**"Sync folder…"** button in the Drive itself, where synced folders are highlighted.

## How each pass works

**Push before pull** (deliberate order): a local edit is uploaded before a download can overwrite
it.

1. **push** (per pair): uploads changed files with `If-Match` (the base hash); creates new ones
   **inside the pair's folder**, creating missing subfolders in the cloud. If the server changed in
   the meantime → **412**, and a local **conflict copy** is kept (nothing is lost).
2. **pull**: applies the change feed; each file is routed to the pair it belongs to (or ignored).
   If it only changed folder, the local file is **moved** instead of downloaded again.

## Security and robustness

- **Anti-traversal**: names coming from the server are sanitized and the destination is checked to
  be inside the pair's folder (with tests).
- **Never deletes what it does not know**: a *tombstone* only removes files this engine downloaded.
- **Local delete → server TRASH** (recoverable), never a *hard delete*. **Local move → move** on
  the server (detected by content: same hash at another path ⇒ it is a move, not delete+create).
- **Mass-deletion guard**: if >50 files disappear at once, the folder is assumed unavailable and
  **nothing is propagated**.
- **Atomic writes** (`.part` + rename) and **HTTPS required** outside localhost; token in a `0600`
  file.

## Performance

- File system **watcher** (FSEvents/inotify) per folder, with coalesced events; the polling
  interval is only a safety net.
- **mtime+size pre-filter**: files are only read/hashed when the stamp changes.
- **Parallel transfers** (`workers`, default 4) and a global **bandwidth limit** (`bandwidth_kbps`).
- **Delta upload**: for files >8 MiB only the changed blocks are sent (the server rebuilds the
  file); if >70% changed or the server does not support it, the whole file is sent.
- **Hourly reconciliation** by full scan, to catch purges (hard deletes) the feed does not report.

## Config

`~/Library/Application Support/mailin-sync` (macOS) or `~/.config/mailin-sync` (Linux):
`config.json` (server + pairs + workers + bandwidth), `token` (0600), `index.db` (SQLite cache).

## To do

- **Mobile**: depends on the mobile app (blocked on FCM/APNs push and the iOS WebRTC spike); on iOS
  a continuous mirror would require a native File Provider Extension.
