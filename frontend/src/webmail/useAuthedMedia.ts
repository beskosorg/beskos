import { useEffect, useState } from "react";
import { apiBase, apiUrl, authHeaders } from "../apiBase";

// useAuthedMedia returns a src usable by <img>/<iframe>/<video>/<audio> from a path on our API
// that requires an authenticated session.
//
// - On the web (same-origin): uses the direct URL — the cookie authenticates and video/audio keeps streaming.
// - In the native app (cross-origin, API_BASE set): <img>/<iframe> send no header or cookie, and the
//   API origin is not in the CSP's img-src/frame-src. So we fetch the bytes with Authorization and
//   serve a blob: URL (allowed by the CSP under any directive). Revoked on unmount/change.
//
// Returns "" while the blob is not ready (the caller shows a loading state).
export function useAuthedMedia(path: string): string {
  const [url, setUrl] = useState<string>(() => (apiBase() ? "" : apiUrl(path)));

  useEffect(() => {
    if (!apiBase()) { setUrl(apiUrl(path)); return; }
    let cancelled = false;
    let objUrl = "";
    setUrl("");
    fetch(apiUrl(path), { headers: authHeaders(), credentials: "include" })
      .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.blob(); })
      .then((b) => { if (cancelled) return; objUrl = URL.createObjectURL(b); setUrl(objUrl); })
      .catch(() => { /* the caller treats the empty value as error/loading */ });
    return () => { cancelled = true; if (objUrl) URL.revokeObjectURL(objUrl); };
  }, [path]);

  return url;
}
