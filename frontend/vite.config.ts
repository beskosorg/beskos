import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Same-origin reverse proxy (mandatory — Stalwart doesn't send CORS headers, see DIVERGENCES §D7).
// EVERYTHING goes through OUR Go API (:8000): /api are the control plane handlers; /jmap, /auth,
// /.well-known fall to the JMAP proxy with BROKERING (H11) — the API authenticates via the session
// and injects the mailbox credential server-to-server, so the browser never holds the password
// (see jmap_proxy.go).
const API = process.env.API_URL ?? "http://localhost:8000";

// Point the proxy at the PRODUCTION API to work on the frontend without spinning up the backend:
//     API_URL=https://app.beskos.com npm run dev
//
// changeOrigin is mandatory when the target is remote: without it the request arrives with
// `Host: localhost:5173`, and the production nginx, which forwards by name, doesn't know who to
// deliver it to. Against a local backend it makes no difference at all.
//
// The production session cookie is Secure+SameSite=None. It works because browsers treat
// `localhost` as a secure context even over http — which is why this has to run on localhost and
// not on a local network IP, where the cookie would be silently rejected.
const REMOTE = /^https:/.test(API);

// Two similar-looking variables, opposite effects — and easy to swap by mistake:
//   API_URL        the PROXY's destination. The browser still talks to localhost (same origin).
//   VITE_API_BASE  the ABSOLUTE base embedded in the bundle. The browser goes straight to that
//                  address instead, the proxy stops being used, and calls become cross-site.
// In web development you want the first one. The second only makes sense in the Tauri bundle,
// which has no proxy at all. Swapping them gives certificate or CORS errors that don't point to the cause.
if (process.env.VITE_API_BASE && !process.env.TAURI_ENV_PLATFORM) {
  console.warn(
    `\n[warning] VITE_API_BASE=${process.env.VITE_API_BASE} is set.\n` +
    `        Calls go DIRECTLY to that address and the proxy is not used.\n` +
    `        To work against a different backend in development, use instead:\n` +
    `            API_URL=${process.env.VITE_API_BASE} npm run dev\n`,
  );
}

// BUNDLE SECURITY (Tauri desktop/mobile): Vite injects any environment variable with the VITE_
// prefix into the bundle IN PLAIN TEXT. Since the app is distributed (and a user can inspect the
// bundle), NO secret may be a VITE_ one. Only the allow-list below is permitted — the build FAILS
// if another VITE_* shows up. Secrets (AI keys, Stalwart admin, DB, private VAPID) live only in
// the Go backend and never reach the frontend.
// VITE_API_BASE: the backend's URL (native app). VITE_PUBLIC_BASE: the web's public origin for
// shareable links generated in the native app (meeting invites etc.). Both are public, non-secret URLs.
const ALLOWED_VITE_VARS = ["VITE_API_BASE", "VITE_PUBLIC_BASE"];
const leaked = Object.keys(process.env).filter((k) => k.startsWith("VITE_") && !ALLOWED_VITE_VARS.includes(k));
if (leaked.length) {
  throw new Error(
    `[security] Disallowed VITE_ variables would be embedded in the bundle: ${leaked.join(", ")}. ` +
    `Only ${ALLOWED_VITE_VARS.join(", ")} is allowed (non-secret). Secrets stay in the backend.`,
  );
}

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: API, ws: true, changeOrigin: REMOTE },
      "/jmap": { target: API, ws: true, changeOrigin: REMOTE }, // ws for JMAP's SSE eventsource
      "/auth": { target: API, changeOrigin: REMOTE },
      "/.well-known": { target: API, changeOrigin: REMOTE },
    },
  },
  build: {
    // No sourcemaps in the distributed bundle — don't expose the app's full source code.
    sourcemap: false,
  },
});
