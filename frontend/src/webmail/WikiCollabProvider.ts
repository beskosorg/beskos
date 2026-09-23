import * as Y from "@y/y";
import * as encoding from "lib0/encoding";
import * as decoding from "lib0/decoding";
import * as syncProtocol from "@y/protocols/sync";
import * as awarenessProtocol from "@y/protocols/awareness";

import { apiBase, getSessionToken } from "../apiBase";

// Wiki's Yjs provider (B10): talks to the Go RELAY (/api/wiki/pages/:id/collab/ws) using the
// classic y-websocket protocol — a type varuint (0=sync, 1=awareness) + payload. The server only
// relays; it is the clients that respond to each other's sync steps. The server's 1st frame is
// text {"type":"hello","peers":N}: peers===0 → whoever joined seeds the doc from body_json; peers>0 →
// starts empty and syncs from the peers (seeding twice would duplicate the content).

const MSG_SYNC = 0;
const MSG_AWARENESS = 1;

export class WikiCollabProvider {
  readonly doc: Y.Doc;
  readonly awareness: awarenessProtocol.Awareness;
  // The hello can arrive BEFORE the editor registers the callback (the WS connects in the constructor):
  // the value is stored and fired on registration — without this, the template seed would lose the
  // race and the page would open empty in the editor.
  private helloPeers: number | null = null;
  private helloCb?: (peers: number) => void;
  /** Registers the hello callback; if the hello has already arrived, fires immediately. */
  set onHello(cb: ((peers: number) => void) | undefined) {
    this.helloCb = cb;
    if (cb && this.helloPeers !== null) cb(this.helloPeers);
  }

  private ws: WebSocket | null = null;
  private closed = false;
  private retry = 1000;
  private readonly url: string;

  constructor(pageId: string, doc: Y.Doc) {
    this.doc = doc;
    this.awareness = new awarenessProtocol.Awareness(doc);
    const base = apiBase() || location.origin;
    const u = new URL(base);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    u.pathname = `/api/wiki/pages/${pageId}/collab/ws`;
    const t = getSessionToken();
    if (t) u.searchParams.set("access_token", t);
    this.url = u.toString();

    this.doc.on("update", this.handleDocUpdate);
    this.awareness.on("update", this.handleAwarenessUpdate);
    // Does NOT connect here: in dev, StrictMode invokes the state initializer TWICE and the orphan
    // provider would occupy the room (the real one would receive peers=1 and never seed). It connects
    // in the useEffect via ensureConnected() — only the instance React adopts opens the WS.
  }

  private started = false;
  private destroyTimer: ReturnType<typeof setTimeout> | null = null;
  /** Opens (or reopens) the connection. Called on editor mount; idempotent and cancels a deferred destroy. */
  ensureConnected() {
    if (this.destroyTimer) { clearTimeout(this.destroyTimer); this.destroyTimer = null; }
    if (this.started && !this.closed) return;
    this.started = true;
    this.closed = false;
    this.retry = 1000;
    if (!this.ws) this.connect();
  }

  /** DEFERRED destroy (150ms): StrictMode mounts→unmounts→mounts in dev; closing the WS in the
   *  fake mount's cleanup would open-and-close a socket for every edit (EPIPE on the proxy). The remount cancels it. */
  scheduleDestroy() {
    if (this.destroyTimer) clearTimeout(this.destroyTimer);
    this.destroyTimer = setTimeout(() => this.destroy(), 150);
  }

  destroy() {
    this.closed = true;
    this.doc.off("update", this.handleDocUpdate);
    this.awareness.off("update", this.handleAwarenessUpdate);
    awarenessProtocol.removeAwarenessStates(this.awareness, [this.doc.clientID], "destroy");
    this.ws?.close();
    this.ws = null;
  }

  private connect() {
    if (this.closed) return;
    let sock: WebSocket;
    try {
      sock = new WebSocket(this.url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    sock.binaryType = "arraybuffer";
    this.ws = sock;
    sock.onopen = () => {
      this.retry = 1000;
      // SyncStep1: requests state from the peers (via the relay).
      const enc = encoding.createEncoder();
      encoding.writeVarUint(enc, MSG_SYNC);
      syncProtocol.writeSyncStep1(enc, this.doc);
      sock.send(encoding.toUint8Array(enc));
      // Announces its own awareness state (cursor/user).
      const aw = encoding.createEncoder();
      encoding.writeVarUint(aw, MSG_AWARENESS);
      encoding.writeVarUint8Array(aw, awarenessProtocol.encodeAwarenessUpdate(this.awareness, [this.doc.clientID]));
      sock.send(encoding.toUint8Array(aw));
    };
    sock.onmessage = (e) => {
      if (typeof e.data === "string") {
        try {
          const hello = JSON.parse(e.data) as { type?: string; peers?: number };
          if (hello.type === "hello") {
            this.helloPeers = hello.peers ?? 0;
            this.helloCb?.(this.helloPeers);
          }
        } catch { /* invalid control frame: ignore */ }
        return;
      }
      const dec = decoding.createDecoder(new Uint8Array(e.data as ArrayBuffer));
      const kind = decoding.readVarUint(dec);
      if (kind === MSG_SYNC) {
        const reply = encoding.createEncoder();
        encoding.writeVarUint(reply, MSG_SYNC);
        syncProtocol.readSyncMessage(dec, reply, this.doc, this);
        if (encoding.length(reply) > 1) sock.send(encoding.toUint8Array(reply));
      } else if (kind === MSG_AWARENESS) {
        awarenessProtocol.applyAwarenessUpdate(this.awareness, decoding.readVarUint8Array(dec), this);
      }
    };
    sock.onerror = () => sock.close();
    sock.onclose = () => {
      this.ws = null;
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect() {
    if (this.closed) return;
    setTimeout(() => this.connect(), this.retry);
    this.retry = Math.min(this.retry * 2, 15000);
  }

  private send(data: Uint8Array) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) this.ws.send(data);
  }

  // Local doc update → relays to the peers (origin === this → came from the network, don't re-emit).
  private handleDocUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin === this) return;
    const enc = encoding.createEncoder();
    encoding.writeVarUint(enc, MSG_SYNC);
    syncProtocol.writeUpdate(enc, update);
    this.send(encoding.toUint8Array(enc));
  };

  private handleAwarenessUpdate = ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }) => {
    const changed = [...added, ...updated, ...removed];
    if (changed.length === 0) return;
    const enc = encoding.createEncoder();
    encoding.writeVarUint(enc, MSG_AWARENESS);
    encoding.writeVarUint8Array(enc, awarenessProtocol.encodeAwarenessUpdate(this.awareness, changed));
    this.send(encoding.toUint8Array(enc));
  };
}
