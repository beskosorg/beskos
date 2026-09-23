import { useEffect, useRef } from "react";
import { apiBase, getSessionToken } from "../apiBase";

export interface ChatSocketEvent { type: string; channel_id: string; payload: Record<string, unknown> }

// Chat WebSocket URL. On the web it uses the same origin (same-origin proxy). In the native app
// (Tauri) the origin is tauri://localhost — no backend there — so it derives the host/protocol
// from API_BASE instead. A WebSocket cannot send an Authorization header, so the session token
// travels in the query string (?access_token=); the backend only accepts it on this route (see
// requireWebmailSession).
function chatWsUrl(): string {
  const base = apiBase() || location.origin; // apiBase() empty on the web
  const u = new URL(base);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  u.pathname = "/api/chat/ws";
  const t = getSessionToken();
  if (t) u.searchParams.set("access_token", t);
  return u.toString();
}

// Chat WebSocket connection (Redis hub on the backend). Reconnects with backoff. Sends only "typing";
// everything else travels via REST + push events.
export function useChatSocket(onEvent: (ev: ChatSocketEvent) => void): { sendTyping: (channelId: string) => void } {
  const cb = useRef(onEvent);
  cb.current = onEvent;
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let closed = false;
    let retry = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const connect = () => {
      if (closed) return;
      // new WebSocket can throw synchronously (invalid URL or CSP block). Isolating it in a
      // try/catch keeps an error here from taking down the whole app (blank screen in Tauri).
      let sock: WebSocket;
      try {
        sock = new WebSocket(chatWsUrl());
      } catch {
        if (!closed) { timer = setTimeout(connect, retry); retry = Math.min(retry * 2, 15000); }
        return;
      }
      wsRef.current = sock;
      sock.onopen = () => { retry = 1000; };
      sock.onmessage = (e) => { try { cb.current(JSON.parse(e.data)); } catch { /* ignore invalid frame */ } };
      sock.onerror = () => sock.close();
      sock.onclose = () => {
        wsRef.current = null;
        if (!closed) { timer = setTimeout(connect, retry); retry = Math.min(retry * 2, 15000); }
      };
    };
    connect();
    return () => { closed = true; if (timer) clearTimeout(timer); wsRef.current?.close(); };
  }, []);

  const sendTyping = (channelId: string) => {
    const s = wsRef.current;
    if (s && s.readyState === WebSocket.OPEN) s.send(JSON.stringify({ type: "typing", channel_id: channelId }));
  };
  return { sendTyping };
}
