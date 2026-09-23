// Low-level JMAP client. Environment-agnostic: in the browser it uses baseUrl "" (same origin
// via reverse proxy — DIVERGENCES §D7); in tests it points straight at Stalwart.
//
// Auth (brokering): in production the browser never sends the mailbox credential. It talks to
// OUR proxy, authenticated via the session -- a same-origin cookie (web) or a session-token
// Bearer (native app) -- and the proxy injects the mailbox's Basic auth server-to-server.
// Integration tests still use Basic directly against Stalwart (the `basic` option).

import { notifyUnauthorized } from "../apiBase";
import type { JmapSession } from "./types";

const MAIL = "urn:ietf:params:jmap:mail";
const CORE = "urn:ietf:params:jmap:core";
const SUBMISSION = "urn:ietf:params:jmap:submission";

export type MethodCall = [string, Record<string, unknown>, string];

export class JmapError extends Error {}

export class JmapClient {
  private baseUrl: string;
  private authHeader: string;
  private sessionAuth: boolean; // true = auth via OUR session (web/native); 401 → back to login

  // Production: pass { bearer } (native app) or nothing (web uses the cookie). Tests: { basic: base64(email:password) }.
  constructor(baseUrl = "", opts: { basic?: string; bearer?: string } = {}) {
    this.baseUrl = baseUrl;
    this.authHeader = opts.basic ? "Basic " + opts.basic : opts.bearer ? "Bearer " + opts.bearer : "";
    this.sessionAuth = !opts.basic; // Basic = integration tests; doesn't touch the login screen
  }

  // guard401: when the proxy returns 401 the session has died (expired, MFA forced, password
  // changed) -- JMAP is the main path for email, so it MUST trigger the same global logout as
  // /api/* calls (otherwise the native app would open into an empty app with no way back to login).
  private guard401(status: number): void {
    if (status === 401 && this.sessionAuth) notifyUnauthorized("/jmap");
  }

  static encodeAuth(email: string, password: string): string {
    return btoa(`${email}:${password}`);
  }

  // h builds the headers: Authorization only when there is an explicit credential (test Basic /
  // native Bearer); on the web it stays empty and the same-origin session cookie authenticates
  // against the proxy.
  private h(extra: Record<string, string> = {}): Record<string, string> {
    return this.authHeader ? { Authorization: this.authHeader, ...extra } : extra;
  }

  async session(): Promise<JmapSession> {
    const res = await fetch(`${this.baseUrl}/jmap/session`, {
      headers: this.h(),
      credentials: "include",
    });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`JMAP session: HTTP ${res.status}`); }
    const s = await res.json();
    const accountId = s.primaryAccounts?.[MAIL];
    if (!accountId) throw new JmapError("mail account not found in session");
    return {
      accountId,
      identityId: s.primaryAccounts?.[SUBMISSION],
      capabilities: s.capabilities ?? {},
      downloadUrlTemplate: s.downloadUrl ?? "",
    };
  }

  // request executes a batch of JMAP calls and returns the payloads in the order sent.
  async request(using: string[], calls: MethodCall[]): Promise<Record<string, unknown>[]> {
    const res = await fetch(`${this.baseUrl}/jmap/`, {
      method: "POST",
      headers: this.h({ "Content-Type": "application/json" }),
      credentials: "include",
      body: JSON.stringify({ using: [CORE, ...using], methodCalls: calls }),
    });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`JMAP: HTTP ${res.status}`); }
    const body = await res.json();
    if (!Array.isArray(body.methodResponses)) throw new JmapError("invalid JMAP response");
    return body.methodResponses.map((mr: [string, Record<string, unknown>, string]) => {
      if (mr[0] === "error") throw new JmapError(`JMAP method: ${JSON.stringify(mr[1])}`);
      return mr[1];
    });
  }

  // downloadBlob downloads an authenticated attachment (Basic via header) and returns the Blob.
  // Needed because <img>/<iframe>/<a download> don't send Authorization → would get a 401 on the direct path.
  async downloadBlob(accountId: string, blobId: string, name: string, type: string): Promise<Blob> {
    const url = `${this.baseUrl}/jmap/download/${encodeURIComponent(accountId)}/` +
      `${encodeURIComponent(blobId)}/${encodeURIComponent(name)}?accept=${encodeURIComponent(type)}`;
    const res = await fetch(url, { headers: this.h(), credentials: "include" });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`download: HTTP ${res.status}`); }
    return res.blob();
  }

  // streamEvents consumes JMAP's EventSource (StateChange push, RFC 8620 §7.3). The native
  // EventSource doesn't let us send Authorization, so we read the SSE via fetch streaming.
  // Calls onState() on every event that isn't a ping. Returns when the connection closes.
  async streamEvents(onState: () => void, signal: AbortSignal): Promise<void> {
    const url = `${this.baseUrl}/jmap/eventsource?types=*&closeafter=no&ping=30`;
    const res = await fetch(url, {
      headers: this.h({ Accept: "text/event-stream" }),
      credentials: "include",
      signal,
    });
    if (!res.ok || !res.body) { this.guard401(res.status); throw new JmapError(`eventsource: HTTP ${res.status}`); }
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) return;
      buf += dec.decode(value, { stream: true });
      let idx: number;
      while ((idx = buf.indexOf("\n\n")) >= 0) {
        const frame = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        const lines = frame.split("\n");
        const ev = lines.find((l) => l.startsWith("event:"))?.slice(6).trim();
        const hasData = lines.some((l) => l.startsWith("data:"));
        if (hasData && ev !== "ping") onState();
      }
    }
  }

  // upload sends a blob to the account and returns {blobId, type, size}. Same-origin path via
  // proxy (/jmap/upload/{accountId}/ — Stalwart's own pattern, DIVERGENCES §D7).
  async upload(accountId: string, file: File): Promise<{ blobId: string; type: string; size: number }> {
    const res = await fetch(`${this.baseUrl}/jmap/upload/${encodeURIComponent(accountId)}/`, {
      method: "POST",
      headers: this.h({ "Content-Type": file.type || "application/octet-stream" }),
      credentials: "include",
      body: file,
    });
    if (!res.ok) { this.guard401(res.status); throw new JmapError(`upload: HTTP ${res.status}`); }
    const b = await res.json();
    if (!b.blobId) throw new JmapError("upload without blobId");
    return { blobId: b.blobId, type: b.type ?? "", size: b.size ?? file.size };
  }

  // downloadUrl builds a blob's (attachment) download URL from the session's template path,
  // rewritten to the current origin (not the mail server's own hostname, which the browser
  // cannot reach directly).
  downloadUrl(session: JmapSession, accountId: string, blobId: string, name: string, type: string): string {
    const path = new URL(session.downloadUrlTemplate, "http://x").pathname; // grab just the path
    return (
      this.baseUrl +
      path
        .replace("{accountId}", encodeURIComponent(accountId))
        .replace("{blobId}", encodeURIComponent(blobId))
        .replace("{name}", encodeURIComponent(name)) +
      `?accept=${encodeURIComponent(type)}`
    );
  }
}

export const USING_MAIL = [MAIL];
export const USING_SEND = [MAIL, SUBMISSION];
