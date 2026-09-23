# Mobile apps (Tauri 2 — Android + iOS)

The native projects are already initialized (`tauri android init` / `tauri ios init`) and live in
`src-tauri/gen/android` and `src-tauri/gen/apple`. The shared entry point is
`src-tauri/src/lib.rs` (`#[cfg_attr(mobile, tauri::mobile_entry_point)] pub fn run()`).

## Prerequisites

- Rust with the mobile targets: `rustup target add aarch64-linux-android armv7-linux-androideabi \
  i686-linux-android x86_64-linux-android aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios`
- Android: Android Studio + SDK + NDK. Export these before running the CLI:
  ```sh
  export ANDROID_HOME="$HOME/Library/Android/sdk"
  export NDK_HOME="$ANDROID_HOME/android-ndk-<versão>"
  ```
- iOS: Xcode + `xcodegen` (`tauri ios init` installs it via Homebrew).

## Run / build

```sh
cd frontend
./node_modules/.bin/tauri android dev      # Android emulator/device
./node_modules/.bin/tauri android build    # APK/AAB
./node_modules/.bin/tauri ios dev          # iOS simulator/device
./node_modules/.bin/tauri ios build        # IPA (requires an Apple Developer account to sign)
```

## Already configured

- **Meet permissions (WebRTC):** camera and microphone declared —
  `gen/android/app/src/main/AndroidManifest.xml` (CAMERA/RECORD_AUDIO/MODIFY_AUDIO_SETTINGS) and
  `gen/apple/mailin_iOS/Info.plist` (NSCameraUsageDescription/NSMicrophoneUsageDescription). Without
  the usage descriptions, the App Store rejects the app.
- `POST_NOTIFICATIONS` (Android 13+) for notifications.
- The UI is already responsive at 380–430px (see `webmail.css`), so it fits a phone screen.

## Still to do (requires external accounts/services)

1. **Native push (app closed):** `tauri-plugin-notification` only does **local** notifications.
   Real push requires **FCM** (Android) and **APNs** (iOS):
   - add a push plugin (e.g. `tauri-plugin-push` or a native FCM/APNs integration);
   - register the device token with our API, and add an FCM/APNs channel to the backend
     (`internal/notify`) alongside the current Web Push;
   - Firebase (google-services.json) and an APNs certificate/*key* — these require the accounts.
2. **WebRTC spike on WKWebView (iOS)** — recorded decision: **validate first** before committing
   mobile Meet to Tauri. `getUserMedia`/WebRTC on iOS WKWebView has historically been fragile; if it
   does not work, the fallback is to open Meet in Safari via `tauri-plugin-opener`. How to test:
   `tauri ios dev`, join a Meet room, confirm camera + microphone and media publishing.
3. **Deep links** (`tauri-plugin-deep-link`) to open meeting/email invites in the app.
4. **Signing and stores:** Apple Developer account (iOS) and a release keystore (Android); a
   privacy policy (mandatory in the stores, and required under the LGPD).

## Maintenance notes

- `gen/android` and `gen/apple` are generated projects; the `.gitignore` files inside them already
  exclude build artifacts (`.gradle`, `build/`, `xcuserdata/`). The project files (manifest,
  Info.plist, Gradle) are versioned and can be edited — rerunning `init` preserves them.
