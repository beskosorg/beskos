# Beskos — desktop and mobile app (Tauri 2)

Packages the same React frontend as a native **desktop** (macOS/Windows/Linux) and **mobile**
(Android/iOS) app via [Tauri 2](https://tauri.app). No code is duplicated: it is `frontend/` running
in a native WebView.

## Prerequisites (one-time)
- **Rust**: https://rustup.rs
- **Tauri CLI**: already in devDependencies (`npm i` in `frontend/`).
- Tauri system dependencies (WebKit on Linux, Xcode on macOS/iOS, Android Studio + NDK on Android).

## Icons (required before the first build)
The `icons/` folder is empty. Generate the icons from a square PNG (≥512px):
```
cd frontend
npm run tauri icon caminho/para/logo.png
```

## API base (IMPORTANT)
On the web, `/api` and `/jmap` work through a proxy (Vite/Caddy, same origin). The **packaged** app
has no proxy — the frontend loads from an internal protocol. Calls therefore use `VITE_API_BASE`
(see `frontend/src/apiBase.ts`): set it in the app build to the server's public URL, e.g.:
```
VITE_API_BASE=https://app.suaempresa.com npm run tauri build
```
Without `VITE_API_BASE`, calls stay relative (they only work on the web/dev with a proxy).

Point `VITE_API_BASE` at the **Go backend** (control plane), not at Stalwart. The backend serves:
- `/api/*` — our API (token session: in the app, login stores the token and sends it in
  `Authorization: Bearer`, since the cross-site cookie does not reach it).
- `/jmap/*`, `/auth/*`, `/.well-known/*` — a **transparent proxy to Stalwart** (the webmail speaks
  JMAP with the user's own Basic auth, which passes straight through; the backend never sees the
  password).

This JMAP proxy exists because Stalwart **does not send CORS headers** and the app is cross-origin.
Enable it on the backend with `APP_ORIGINS` (and `COOKIE_SECURE=true` in HTTPS production):
```
APP_ORIGINS=tauri://localhost,https://tauri.localhost
COOKIE_SECURE=true
```
Without `APP_ORIGINS`, the proxy and CORS are off (the web uses Caddy/Vite same-origin).

## Run / package
```
cd frontend
npm run tauri dev            # desktop, points to Vite (http://localhost:5173)
npm run tauri build          # desktop installers

# Mobile (once, to initialize each platform):
npm run tauri android init
npm run tauri ios init
npm run tauri android dev    # / ios dev
```

## PRODUCTION build — checklist

1. **Build variables** (public, non-secret — the only `VITE_` variables allowed):
   ```
   VITE_API_BASE=https://app.suaempresa.com \
   VITE_PUBLIC_BASE=https://app.suaempresa.com \
   npm run tauri build
   ```
   `VITE_PUBLIC_BASE` is the origin used in **shareable links** generated in the app (meeting
   invite `/j/<room>`, links in emails, shares) — on desktop the local origin is `tauri://…`,
   useless to third parties. Without it, it falls back to `VITE_API_BASE`.

2. **CSP** (`tauri.conf.json` → `app.security.csp`). The committed policy is strict and covers
   API/LiveKit over `https:`/`wss:`, but **OnlyOffice must be added explicitly** (the editor loads
   `api.js` from the Document Server), otherwise Office files do not open in the app:
   - `script-src 'self' 'wasm-unsafe-eval' https://docs.suaempresa.com`
   - `frame-src 'self' data: blob: https://docs.suaempresa.com`
   Optionally, tighten `connect-src` to the exact hosts (API, LiveKit `wss://`, OnlyOffice)
   instead of the generic `https: wss:`.
   Note: local test builds temporarily inject `localhost` origins into the CSP (the build scripts
   revert it); the committed CSP must stay the production one.

3. **LiveKit in production** must be served over **`wss://`** (TLS). A `ws://` to a non-localhost
   host is blocked by the webview as mixed content.

4. **Signing/notarization** (macOS): without a Developer ID, Gatekeeper warns on other machines and
   Notification Center may register the app inconsistently across reinstalls.

## Security — no secrets in the package

The desktop package is distributable and inspectable, so it **must not contain secrets**.
Guarantees:

- **All secrets live only in the Go backend** (AI keys, Stalwart admin, database, private VAPID
  key, OnlyOffice/LiveKit JWT…). The frontend never receives them.
- **Login uses the user's own credential** (JMAP Basic auth against Stalwart), typed at runtime —
  there is no embedded credential.
- **Environment variables**: Vite injects any `VITE_*` in plain text. Only `VITE_API_BASE` (a
  public, non-secret URL) is allowed — `vite.config.ts` **fails the build** if any other `VITE_*`
  appears.
- **No sourcemaps** in the build (`build.sourcemap: false`) — the source code is not shipped in the
  package.
- **Restrictive CSP** (`tauri.conf.json`): no inline scripts, no eval (except WASM), and
  `connect-src` limited to `https:`/`wss:`. Tightening `connect-src` to the exact backend host in
  production is recommended (e.g. `connect-src 'self' https://app.suaempresa.com wss://app.suaempresa.com`).
- **Audit**: `dist/` was checked — 0 secret values, 0 sourcemaps.

Golden rule: if a piece of data is sensitive, it **does not enter the frontend** — it becomes a
backend endpoint that serves or uses it under the authenticated session.

Note (runtime, not package): the user's session token is stored in the webview's `localStorage`.
On desktop, consider migrating it to the **OS keychain** (`tauri-plugin-stronghold` or `keyring`
plugin) so the token is not left on disk in plain text — a hardening improvement, separate from
"no secrets in the package".

## Structure
- `tauri.conf.json` — config (window, bundle, devUrl=Vite, frontendDist=../dist).
- `Cargo.toml` / `build.rs` / `src/` — the Rust "shell" (shared by desktop and mobile via `lib.rs`).
- `capabilities/default.json` — window permissions (Tauri 2 security model).
