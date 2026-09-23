import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import "./index.css";
import { i18nReady } from "./i18n";
import { ResetPage } from "./pages/Reset";
import { MailPage } from "./pages/Mail";
import { EditorPage } from "./pages/Editor";
import { PublicFilePage } from "./pages/PublicFile";
import { PublicWikiPage } from "./pages/PublicWiki";
import { GuestPage } from "./pages/Guest";
import { RoomPage } from "./pages/Room";
import { AcceptInvitePage } from "./pages/AcceptInvite";
import { BookingPage } from "./pages/Booking";
import { hostedPages } from "./edition";
import { isTauri, initTauriNotify } from "./webmail/tauriNotify";
import { needsServer } from "./apiBase";
import { ServerSetup } from "./webmail/ServerSetup";
import { unlockSounds } from "./webmail/sounds";
import { GlobalTooltip } from "./webmail/GlobalTooltip";

const qc = new QueryClient();

// In the native app (Tauri), reads the OS's notification permission into the cache (the UI reads
// it synchronously). On the web it's a no-op.
if (isTauri()) void initTauriNotify();

// Unlocks audio on the app's first gesture. The browser keeps the AudioContext suspended until
// there is interaction -- without this, the session's first notification sound would be
// swallowed in silence.
unlockSounds();

// The room's dedicated window (Tauri): opens at index.html?wpath=/room/:room (the asset server
// doesn't do SPA fallback). Rewrites the URL to the real route BEFORE BrowserRouter mounts, so it
// renders the room instead of the /mail fallback.
const wpath = new URLSearchParams(window.location.search).get("wpath");
if (wpath && wpath.startsWith("/")) {
  // Marks this as a dedicated window (room/editor): the close button should close the native
  // window instead of navigating to Drive. See isDedicatedWindow()/closeSelfWindow() in nativeWindow.ts.
  (window as unknown as { __beskosDedicated?: boolean }).__beskosDedicated = true;
  window.history.replaceState(null, "", wpath);
}

// The first screen waits for its dictionary, so it paints in the right language instead of the
// fallback and then switching. `finally`, not `then`: a dictionary that fails to load must still
// leave the app usable in the fallback, not a blank page.
void i18nReady.finally(() => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <QueryClientProvider client={qc}>
        <GlobalTooltip />
        {needsServer() ? (
          // The packaged app with no server yet: every route below would call an address that does
          // not exist, so none of them are mounted until this is answered.
          <ServerSetup />
        ) : (
        <BrowserRouter>
          <Routes>
            <Route path="/mail/files/edit/:id" element={<EditorPage />} />
            <Route path="/reset" element={<ResetPage />} />
            <Route path="/mail" element={<MailPage />} />
            <Route path="/mail/:view" element={<MailPage />} />
            <Route path="/mail/:view/:sub" element={<MailPage />} />
            <Route path="/mail/:view/:sub/:sub2" element={<MailPage />} />
            {/* Room in a dedicated window: just the call, no webmail menus. */}
            <Route path="/room/:room" element={<RoomPage />} />
            <Route path="/j/:room" element={<GuestPage />} />
            {/* File shared via a public link (no login): opens in anonymous OnlyOffice or downloads. */}
            <Route path="/p/:token" element={<PublicFilePage />} />
            {/* Read-only public wiki (no login): published page tree + reader. */}
            <Route path="/w/:slug" element={<PublicWikiPage />} />
            <Route path="/w/:slug/:pageSlug" element={<PublicWikiPage />} />
            {/* Self-service signup (this is where the site's "Get started" points to). */}
            {hostedPages.signup && <Route path="/signup" element={<hostedPages.signup />} />}
            <Route path="/invite/:token" element={<AcceptInvitePage />} />
            {/* Public booking (Calendly-style): outsiders schedule a meeting with a team member. */}
            <Route path="/b/:org/:user" element={<BookingPage />} />
            {/* Single screen: the webmail. Administration is a view of it (/app was removed). */}
            <Route path="*" element={<Navigate to="/mail" replace />} />
          </Routes>
        </BrowserRouter>
        )}
      </QueryClientProvider>
    </React.StrictMode>
  );
});
