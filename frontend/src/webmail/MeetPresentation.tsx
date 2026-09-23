import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { overlayRoot } from "./MeetBarSlot";
import { useTranslation } from "react-i18next";
import { useLocalParticipant, useRoomContext } from "@livekit/components-react";
import { ConnectionState, RoomEvent } from "livekit-client";
import { backend, type Presentation } from "./backend";
import { loadPdf, type PDFDocument } from "./pdfjs";
import { toast } from "./toast";
import { Spinner } from "./Spinner";
import { IcX, IcTrash, IcPencil, IcPresent, IcChevron, IcExternal, IcMaximize, IcRestoreWin } from "../panel/icons";
import { onTauriEvent } from "./tauriNotify";
import { GrantMenu } from "./MeetPresentationGrantMenu";
import { Picker } from "./MeetPresentationPicker";

// Presentation in Meet (BBB style). The presenter picks a PDF/PPTX (upload or Drive); everyone
// renders the PDF locally (pdf.js) and the slide/pen/laser sync over the LiveKit data channel.
// Works on web and desktop (same code; the PDF comes via a public URL with a token).

type Stroke = { pts: number[]; color: string; w: number }; // normalized pts (0..1), x,y pairs
type Msg =
  | { t: "hello" }
  | { t: "start"; id: string; token: string; by: string; slide: number; granted: string[] }
  | { t: "slide"; n: number }
  | { t: "stroke"; page: number; s: Stroke }
  /** Snapshot of a page's strokes, for someone joining mid-way. `reset` marks the first batch:
   *  replaces whatever is there; the following ones append (a long drawing doesn't fit in a single packet). */
  | { t: "strokes"; page: number; list: Stroke[]; reset: boolean }
  | { t: "clear"; page: number }
  | { t: "laser"; x: number; y: number }
  | { t: "laserOff" }
  | { t: "grant"; id: string }
  | { t: "revoke"; id: string }
  | { t: "stop" };

const COLORS = ["#e53935", "#1e88e5", "#43a047", "#fdd835", "#111"];
/** Cap per packet when resending strokes. LiveKit cuts off near 15 KB; we keep margin. */
const MAX_SNAPSHOT_BYTES = 8000;
const enc = new TextEncoder();
const dec = new TextDecoder();
const DEBUG = false; // presentation diagnostic panel (turn on only to debug)

export function PresentationLayer({ room, viewerOnly }: { room: string; viewerOnly?: boolean }) {
  const { t } = useTranslation();
  const roomCtx = useRoomContext();
  const { localParticipant } = useLocalParticipant();
  const myId = localParticipant?.identity ?? "";

  // Temporary diagnostic: counts received messages and shows the state.
  const dbg = useRef({ rx: 0, tx: 0, topic: "-", lastT: "-", err: "" });
  const [, setDbgTick] = useState(0);
  useEffect(() => {
    if (!DEBUG) return;
    const i = setInterval(() => setDbgTick((v) => v + 1), 700);
    return () => clearInterval(i);
  }, []);

  const [pres, setPres] = useState<{ id: string; token: string; by: string } | null>(null);
  const [picker, setPicker] = useState(false);
  const [busy, setBusy] = useState(false);
  const [hidden, setHidden] = useState(false); // hide locally (doesn't end it; shows the cameras)
  const [barCollapsed, setBarCollapsed] = useState(false); // collapse the control bar (tab)
  const [full, setFull] = useState(false); // presentation occupying the full screen (hides the cameras)
  const isPresenter = !!pres && pres.by === myId;

  // Sending over the LiveKit data channel ("pres" topic). publishData ONLY works with the room
  // CONNECTED — before that LiveKit rejects it ("PC manager is closed") and the message is lost (that was
  // why hello/start never arrived). Messages sent too early go to the queue and are
  // dispatched on RoomEvent.Connected. Laser goes lossy (fast); everything else reliable.
  const pending = useRef<{ m: Msg; reliable: boolean }[]>([]);
  /** `to` limits delivery to specific participants (used to bring a newcomer up to date without
   *  resending the state to the whole room). Without `to`, it goes to everyone. */
  const post = useCallback((m: Msg, reliable = true, to?: string[]) => {
    const room = roomCtx;
    if (!room) { if (DEBUG) dbg.current.err = "no-room"; return; }
    if (room.state !== ConnectionState.Connected) { pending.current.push({ m, reliable }); return; }
    room.localParticipant.publishData(enc.encode(JSON.stringify(m)), { reliable, topic: "pres", destinationIdentities: to })
      .then(() => { if (DEBUG) dbg.current.tx++; })
      .catch((e) => { if (DEBUG) dbg.current.err = "tx:" + String((e as Error).message).slice(0, 24); });
  }, [roomCtx]);

  // Shared annotation state (refs so it doesn't re-render on every stroke).
  const strokes = useRef<Map<number, Stroke[]>>(new Map());
  const [granted, setGranted] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [numPages, setNumPages] = useState(0);
  const [redraw, setRedraw] = useState(0);
  const pageRef = useRef(1); pageRef.current = page;

  const docRef = useRef<PDFDocument | null>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const pdfCanvas = useRef<HTMLCanvasElement>(null);
  const annoCanvas = useRef<HTMLCanvasElement>(null);

  // IMPERATIVE laser: moving an element via ref doesn't re-render the component. Positioned via
  // TRANSFORM (compositor-only) — animating left/top forced layout on every pixel and "froze" the mouse.
  // slideSize is updated when the page renders (avoids reading getBoundingClientRect on every move).
  const laserRef = useRef<HTMLSpanElement>(null);
  const slideSize = useRef({ w: 0, h: 0 });
  const laserFade = useRef<ReturnType<typeof setTimeout>>();
  const showLaser = useCallback((x: number, y: number) => {
    const el = laserRef.current; if (!el || !slideSize.current.w) return;
    el.style.transform = `translate(${x * slideSize.current.w - 9}px, ${y * slideSize.current.h - 9}px)`;
    el.style.opacity = "1";
    clearTimeout(laserFade.current);
    laserFade.current = setTimeout(() => { if (laserRef.current) laserRef.current.style.opacity = "0"; }, 2500);
  }, []);
  const hideLaser = useCallback(() => { if (laserRef.current) laserRef.current.style.opacity = "0"; clearTimeout(laserFade.current); }, []);

  // --- message reception ---
  function onMsg(m: Msg, from?: string) {
    switch (m.t) {
      case "hello":
        // Only the presenter responds, re-emitting the state to whoever just joined (includes guests).
        // Directed to whoever asked: re-emitting to the whole room would make everyone else redraw for nothing.
        if (pres && pres.by === myId) resendRef.current(from ? [from] : undefined);
        break;
      case "start":
        // Idempotent: only (re)loads when it's a NEW presentation (different id). The presenter
        // re-emits "start" as a heartbeat (see below) — without this guard, every re-emission would recreate the
        // `pres` object and the effect would refetch the PDF, plus force setHidden(false) on whoever had
        // hidden it locally. `pres` here is the current one (onMsgRef reassigns onMsg on every render).
        if (!pres || pres.id !== m.id) {
          setPres({ id: m.id, token: m.token, by: m.by });
          setPage(m.slide || 1);
          setHidden(false);
        }
        setGranted(new Set(m.granted));
        break;
      case "slide": setPage(m.n); break;
      case "stroke": {
        const arr = strokes.current.get(m.page) ?? [];
        arr.push(m.s); strokes.current.set(m.page, arr);
        if (m.page === pageRef.current) setRedraw((v) => v + 1);
        break;
      }
      case "strokes": {
        const arr = m.reset ? [] : (strokes.current.get(m.page) ?? []);
        arr.push(...m.list); strokes.current.set(m.page, arr);
        if (m.page === pageRef.current) setRedraw((v) => v + 1);
        break;
      }
      case "clear": strokes.current.set(m.page, []); if (m.page === pageRef.current) setRedraw((v) => v + 1); break;
      case "laser": showLaser(m.x, m.y); break;
      case "laserOff": hideLaser(); break;
      case "grant": setGranted((g) => new Set(g).add(m.id)); break;
      case "revoke": setGranted((g) => { const n = new Set(g); n.delete(m.id); return n; }); break;
      case "stop": setPres(null); strokes.current.clear(); break;
    }
  }

  // Receives "pres" messages directly from the room. onMsgRef guarantees the current state in the handler (without
  // closing over a stale `pres`/`granted`).
  const onMsgRef = useRef(onMsg);
  onMsgRef.current = onMsg;
  // Current presenter's identity (ref so the exit handler reads the current value).
  const presByRef = useRef<string | undefined>(undefined);
  presByRef.current = pres?.by;
  // If I AM the presenter and I leave the room (disconnect/unmount), end the presentation on the
  // server — it doesn't stay "active" as an orphan waiting for the webhook. The webhook (participant_left) is the backstop.
  const selfStopRef = useRef<() => void>(() => {});
  selfStopRef.current = () => {
    if (pres && pres.by === myId) backend.presentationStop(pres.id).catch(() => {});
  };
  useEffect(() => () => selfStopRef.current(), []);
  // "Hard" exit (closing the window/tab): React's cleanup does NOT run. On the web it fires `pagehide`; on
  // desktop, closing the main window only HIDES it (stays in the tray) and the webview stays alive — that's
  // why the native side emits "window-hidden". In both cases, whoever is presenting ends it: notifies the room via the
  // data channel and deactivates it on the server (the request goes keepalive, survives the page closing).
  useEffect(() => {
    const onHardExit = () => {
      if (!myId || presByRef.current !== myId) return; // only the presenter ends it
      post({ t: "stop" });
      selfStopRef.current();
    };
    window.addEventListener("pagehide", onHardExit);
    const offNative = onTauriEvent("window-hidden", onHardExit);
    return () => { window.removeEventListener("pagehide", onHardExit); offNative(); };
  }, [post, myId]);
  // resendState: the presenter re-emits the current state (used when someone new joins).
  const resendRef = useRef<(to?: string[]) => void>(() => {});
  useEffect(() => {
    if (!roomCtx) return;
    const onData = (payload: Uint8Array, p?: { identity?: string }, _k?: unknown, topic?: string) => {
      if (DEBUG) { dbg.current.rx++; dbg.current.topic = topic ?? "(none)"; }
      if (topic && topic !== "pres") return;
      try {
        const m = JSON.parse(dec.decode(payload)) as Msg;
        if (DEBUG) dbg.current.lastT = m.t;
        onMsgRef.current(m, p?.identity);
      } catch { /* invalid frame */ }
    };
    // State only for whoever joined (not the whole room). The delay gives the newcomer time to mount the listener.
    const onJoin = (p?: { identity?: string }) => {
      const to = p?.identity ? [p.identity] : undefined;
      setTimeout(() => resendRef.current(to), 900);
    };
    // Presenter left the room => the presentation is over for everyone (the server also deactivates it on the
    // participant_left webhook; this is the immediate, live effect).
    const onLeft = (p?: { identity?: string }) => {
      if (p?.identity && p.identity === presByRef.current) {
        setPres(null);
        strokes.current.clear();
      }
    };
    // Room connected: dispatches the queued messages (e.g. the mount's hello, done before connect).
    const onConnected = () => { const q = pending.current.splice(0); q.forEach(({ m, reliable }) => post(m, reliable)); };
    // Reconnection: asks for the state again and, if we're the presenter, re-emits it.
    const onReconnected = () => { post({ t: "hello" }); setTimeout(() => resendRef.current(), 600); };
    const onSelfDisconnect = () => selfStopRef.current();
    roomCtx.on(RoomEvent.DataReceived, onData);
    roomCtx.on(RoomEvent.ParticipantConnected, onJoin);
    roomCtx.on(RoomEvent.ParticipantDisconnected, onLeft);
    roomCtx.on(RoomEvent.Disconnected, onSelfDisconnect);
    roomCtx.on(RoomEvent.Connected, onConnected);
    roomCtx.on(RoomEvent.Reconnected, onReconnected);
    if (roomCtx.state === ConnectionState.Connected) onConnected(); // already connected on mount
    return () => {
      roomCtx.off(RoomEvent.DataReceived, onData);
      roomCtx.off(RoomEvent.ParticipantConnected, onJoin);
      roomCtx.off(RoomEvent.ParticipantDisconnected, onLeft);
      roomCtx.off(RoomEvent.Disconnected, onSelfDisconnect);
      roomCtx.off(RoomEvent.Connected, onConnected);
      roomCtx.off(RoomEvent.Reconnected, onReconnected);
    };
  }, [roomCtx, post]);

  resendRef.current = (to?: string[]) => {
    if (!pres || pres.by !== myId) return;
    post({ t: "start", id: pres.id, token: pres.token, by: myId, slide: pageRef.current, granted: [...granted] }, true, to);
    // ...and the strokes already drawn. Without this, someone joining midway sees a blank presentation: the
    // annotations only existed as live events, never as state.
    for (const [pg, list] of strokes.current) {
      if (!list.length) continue;
      let batch: Stroke[] = [];
      let bytes = 0;
      let reset = true;
      const flush = () => {
        if (!batch.length) return;
        post({ t: "strokes", page: pg, list: batch, reset }, true, to);
        reset = false; batch = []; bytes = 0;
      };
      for (const s of list) {
        // A LiveKit data packet is limited (~15 KB); it gets split before reaching that.
        const size = s.pts.length * 8 + 40;
        if (bytes + size > MAX_SNAPSHOT_BYTES) flush();
        batch.push(s); bytes += size;
      }
      flush();
    }
  };

  // On mount/reconnect, asks for the state (to join mid-presentation).
  useEffect(() => { post({ t: "hello" }); }, [post]);

  // Presenter HEARTBEAT: re-broadcasts "start" to EVERYONE every 4s. Without this, a participant
  // who misses the single initial "start" broadcast is left blank — especially a GUEST, who has
  // no REST fallback (handleGetPresentation requires a session → 401). The "start" handler is
  // idempotent (same id does not re-fetch the PDF), so re-broadcasting is cheap and self-heals.
  useEffect(() => {
    if (!pres || pres.by !== myId) return;
    const iv = setInterval(() => {
      post({ t: "start", id: pres.id, token: pres.token, by: myId, slide: pageRef.current, granted: [...granted] });
    }, 4000);
    return () => clearInterval(iv);
  }, [pres, myId, granted, post]);

  // REST recovery only ONCE, on joining (in case no one answers the hello in time). It can't
  // run every time `pres` becomes null, or right after "Stop" it would fetch again and reopen it.
  const askedRef = useRef(false);
  useEffect(() => {
    if (viewerOnly) return; // guest has no session: this endpoint would give 401 (and the global 401 handler would send them to login)
    if (askedRef.current) return;
    askedRef.current = true;
    let stop = false;
    backend.presentationGet(room).then((p) => {
      if (stop || !p.active) return;
      // by = presenter's identity (backend). If it's us (we rejoined the room), we recover
      // the role: controls back and we answer the hellos from whoever arrives.
      setPres({ id: p.id, token: p.token, by: p.by || "" }); setPage(p.current_slide || 1);
    }).catch(() => { /* no presentation, or guest without a session: ignore */ });
    return () => { stop = true; };
  }, [room]);

  // Loads the PDF when the presentation becomes active. The state (loading / failed) is shown ON
  // THE STAGE: before, whoever wasn't the presenter just saw a white rectangle while the file downloaded
  // — or forever, if it failed (the toast went unnoticed).
  const [pdfState, setPdfState] = useState<"idle" | "loading" | "error">("idle");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!pres) { docRef.current = null; setNumPages(0); setPdfState("idle"); return; }
    let stop = false;
    setPdfState("loading");
    loadPdf(backend.presentationPdfUrl(pres.id, pres.token)).then((doc) => {
      if (stop) return;
      docRef.current = doc; setNumPages(doc.numPages); setRedraw((v) => v + 1); setPdfState("idle");
      if (DEBUG) dbg.current.err = "pdf ok " + doc.numPages;
    }).catch((e) => {
      if (stop) return;
      setPdfState("error");
      if (DEBUG) dbg.current.err = "pdf ERR: " + String((e as Error).message).slice(0, 40);
      // The real reason (network, CORS, corrupted PDF) only lives here: without the log, a
      // presentation failure was impossible to diagnose (the user only saw the generic message).
      console.error("meet: failed to load the presentation PDF", e);
      toast(t("meet.presLoadFail"));
    });
    return () => { stop = true; };
  }, [pres, t, retry]);

  // Renders the current page on the PDF canvas and adjusts the annotation canvas. Depends on `hidden`
  // because when hiding/showing the canvases get remounted (new and blank) — without re-rendering,
  // a white square would reappear.
  useEffect(() => {
    if (hidden) return;
    const doc = docRef.current, stage = stageRef.current, pc = pdfCanvas.current, ac = annoCanvas.current;
    if (!doc || !stage || !pc || !ac) return;
    let cancelled = false;
    const dpr = window.devicePixelRatio || 1;
    doc.getPage(page).then((pg) => {
      if (cancelled) return;
      const base = pg.getViewport({ scale: 1 });
      const scale = Math.min(stage.clientWidth / base.width, stage.clientHeight / base.height) || 1;
      const vp = pg.getViewport({ scale: scale * dpr });
      const cssW = vp.width / dpr, cssH = vp.height / dpr;
      slideSize.current = { w: cssW, h: cssH }; // so the laser can position via transform without measuring the DOM
      for (const c of [pc, ac]) { c.width = vp.width; c.height = vp.height; c.style.width = cssW + "px"; c.style.height = cssH + "px"; }
      const ctx = pc.getContext("2d")!;
      pg.render({ canvasContext: ctx, viewport: vp });
      drawStrokes();
    });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, numPages, redraw, hidden]);

  // The slide was measured ONCE and never again. When the stage changes size — opening the camera
  // column, resizing the window, entering/exiting full screen — the canvas kept the old
  // measurement and overflowed. Observing is safe: `.pres-canvas-wrap` is `flex:1`, sized
  // by the stage and not by the content, so changing the canvas doesn't feed back into the observer.
  useEffect(() => {
    const stage = stageRef.current;
    if (!stage || hidden || !pres) return;
    const ro = new ResizeObserver(() => setRedraw((v) => v + 1));
    ro.observe(stage);
    return () => ro.disconnect();
  }, [hidden, pres]);

  // Draws the current page's strokes on the annotation canvas.
  function drawStrokes() {
    const ac = annoCanvas.current; if (!ac) return;
    const ctx = ac.getContext("2d")!;
    ctx.clearRect(0, 0, ac.width, ac.height);
    ctx.lineCap = "round"; ctx.lineJoin = "round";
    for (const s of strokes.current.get(page) ?? []) drawStroke(ctx, s, ac.width, ac.height);
  }
  function drawStroke(ctx: CanvasRenderingContext2D, s: Stroke, w: number, h: number) {
    if (s.pts.length < 2) return;
    ctx.strokeStyle = s.color; ctx.lineWidth = s.w * w;
    ctx.beginPath(); ctx.moveTo(s.pts[0] * w, s.pts[1] * h);
    for (let i = 2; i < s.pts.length; i += 2) ctx.lineTo(s.pts[i] * w, s.pts[i + 1] * h);
    ctx.stroke();
  }

  const canDraw = isPresenter || granted.has(myId);
  const [tool, setTool] = useState<"pen" | null>(null); // null = laser mode (automatic on mouse hover)
  const [color, setColor] = useState(COLORS[0]);
  const cur = useRef<number[] | null>(null);
  const lastLaser = useRef(0);

  const relPos = (e: React.PointerEvent) => {
    const r = annoCanvas.current!.getBoundingClientRect();
    return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height };
  };
  // AUTOMATIC laser: while the mouse hovers over the presentation (and no one is drawing), the
  // pointer's position becomes a laser for everyone. Moves via ref (no re-render) and the sending is throttled.
  const sendLaser = (x: number, y: number) => {
    showLaser(x, y);
    const now = performance.now();
    if (now - lastLaser.current > 33) { lastLaser.current = now; post({ t: "laser", x, y }, false); }
  };
  const onDown = (e: React.PointerEvent) => {
    if (!canDraw || tool !== "pen") return;
    const { x, y } = relPos(e);
    cur.current = [x, y]; annoCanvas.current?.setPointerCapture(e.pointerId);
  };
  const onMove = (e: React.PointerEvent) => {
    if (!canDraw) return;
    const { x, y } = relPos(e);
    // With the PEN selected the laser never comes into play. Before, moving the mouse without pressing fell into
    // the automatic laser and the red dot kept following the pointer — it looked like the tool
    // hadn't changed.
    if (tool === "pen") {
      if (cur.current) {
        cur.current.push(x, y);
        const ac = annoCanvas.current!; drawStroke(ac.getContext("2d")!, { pts: cur.current, color, w: 0.004 }, ac.width, ac.height);
      }
      return;
    }
    sendLaser(x, y); // automatic laser (laser mode only)
  };
  const onUp = () => {
    if (cur.current && cur.current.length >= 4) {
      const s: Stroke = { pts: cur.current, color, w: 0.004 };
      const arr = strokes.current.get(page) ?? []; arr.push(s); strokes.current.set(page, arr);
      post({ t: "stroke", page, s });
    }
    cur.current = null;
  };
  const onLeave = () => { onUp(); hideLaser(); post({ t: "laserOff" }, false); };

  // --- presenter actions ---
  const gotoPage = (n: number) => {
    const p = Math.max(1, Math.min(numPages || 1, n));
    setPage(p); post({ t: "slide", n: p });
    if (pres) backend.presentationSetSlide(pres.id, p).catch(() => {});
  };
  const clearPage = () => { strokes.current.set(page, []); setRedraw((v) => v + 1); post({ t: "clear", page }); };
  const stop = () => { if (pres) backend.presentationStop(pres.id).catch(() => {}); post({ t: "stop" }); setPres(null); strokes.current.clear(); };
  const startWith = async (fn: () => Promise<Presentation>) => {
    setPicker(false); setBusy(true); // closes the picker and shows the "preparing" overlay
    try {
      const p = await fn();
      strokes.current.clear();
      setPres({ id: p.id, token: p.token, by: myId }); setGranted(new Set()); setPage(p.current_slide || 1); setHidden(false);
      post({ t: "start", id: p.id, token: p.token, by: myId, slide: p.current_slide || 1, granted: [] });
    } catch (e) { toast((e as Error).message); }
    setBusy(false);
  };

  // Grant/revoke the pen. The network send stays OUTSIDE the setState updater — putting it inside
  // made React re-run the updater (StrictMode/dev) and send both grant+revoke, canceling the
  // grant out. The next state is computed separately and sent as a single message.
  const toggleGrant = (id: string) => {
    const has = granted.has(id);
    const next = new Set(granted);
    if (has) next.delete(id); else next.add(id);
    setGranted(next);
    post({ t: has ? "revoke" : "grant", id });
  };

  // Remote participants (to grant annotation).
  const peers = roomCtx ? [...roomCtx.remoteParticipants.values()] : [];

  // The BUTTON stays in the control bar; everything else (stage, modals, warnings) is a full-screen
  // overlay and goes to the <body>. If it stayed inside the bar, the bar would only need to gain
  // `backdrop-filter`/`transform`/`filter` to become the containing block for `position:fixed`
  // — and the stage would shrink into it, on top of the icons. It's happened before; hence the portal.
  const overlays = (
    <>
      {DEBUG && (
        <div style={{ position: "fixed", top: 6, left: 6, zIndex: 300, background: "rgba(0,0,0,.78)", color: "#3f6", font: "11px monospace", padding: "4px 7px", borderRadius: 6, pointerEvents: "none", whiteSpace: "nowrap" }}>
          {`id:${myId.slice(0, 6)} st:${roomCtx?.state ?? "?"} rp:${roomCtx?.remoteParticipants?.size ?? "?"} tx:${dbg.current.tx} rx:${dbg.current.rx} last:${dbg.current.lastT} pres:${pres ? 1 : 0} by:${pres?.by?.slice(0, 6) || "-"} pg:${numPages} ${dbg.current.err}`}
        </div>
      )}
      {picker && <Picker onClose={() => setPicker(false)} onUpload={(f) => startWith(() => backend.presentationStartUpload(room, f))} onDrive={(id) => startWith(() => backend.presentationStartDrive(room, id))} />}

      {busy && <div className="pres-loading"><Spinner size={22} /><span>{t("meet.presConverting")}</span></div>}

      {/* Hidden (local): shows only a button to bring the presentation back; the cameras return. */}
      {pres && hidden && (
        <button className="pres-show" onClick={() => setHidden(false)}><IcPresent size={16} /> {t("meet.presShow")}</button>
      )}

      {pres && !hidden && (
        <div className={"pres-stage" + (full ? " full" : "")}>
          <div className="pres-canvas-wrap" ref={stageRef}>
            <div className="pres-slide" style={pdfState === "idle" ? undefined : { visibility: "hidden" }}>
              <canvas ref={pdfCanvas} className="pres-pdf" />
              <canvas ref={annoCanvas} className="pres-anno"
                style={{ cursor: canDraw ? (tool === "pen" ? "crosshair" : "none") : "default", pointerEvents: canDraw ? "auto" : "none" }}
                onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerLeave={onLeave} />
              <span className="pres-laser" ref={laserRef} />
            </div>
            {/* PDF state over the presentation area (and only it) — doesn't cover the cameras. */}
            {pdfState === "loading" && (
              <div className="pres-state"><Spinner size={22} /><span>{t("meet.presLoading")}</span></div>
            )}
            {pdfState === "error" && (
              <div className="pres-state">
                <span>{t("meet.presLoadFail")}</span>
                <button className="pres-retry" onClick={() => setRetry((v) => v + 1)}>{t("common.retry")}</button>
              </div>
            )}
          </div>

          {barCollapsed ? (
            <button className="pres-bar-tab" title={t("meet.presBarShow")} onClick={() => setBarCollapsed(false)}>
              <IcChevron size={16} className="chev-up" />
            </button>
          ) : (
          <div className="pres-bar">
            <button className="ib" title={t("meet.presBarHide")} onClick={() => setBarCollapsed(true)}><IcChevron size={16} /></button>
            <span className="pres-sep" />
            {isPresenter && <>
              <button className="ib" title={t("meet.presPrev")} onClick={() => gotoPage(page - 1)} disabled={page <= 1}><IcChevron size={16} className="chev-l" /></button>
              <span className="pres-count">{page} / {numPages || "…"}</span>
              <button className="ib" title={t("meet.presNext")} onClick={() => gotoPage(page + 1)} disabled={page >= numPages}><IcChevron size={16} className="chev-r" /></button>
              <span className="pres-sep" />
            </>}
            {canDraw && <>
              <button className={"ib" + (tool === "pen" ? " on" : "")} title={t("meet.presPen")}
                onClick={() => {
                  const next = tool === "pen" ? null : "pen";
                  setTool(next);
                  // When switching to the pen, turns off the laser that was left on — on my screen and everyone else's.
                  if (next === "pen") { hideLaser(); post({ t: "laserOff" }); }
                }}><IcPencil size={15} /></button>
              {tool === "pen" && (
                <span className="pres-swatches">
                  {COLORS.map((c) => <button key={c} className={"pres-swatch" + (c === color ? " on" : "")} style={{ "--sw": c } as React.CSSProperties} onClick={() => setColor(c)} aria-label={c} />)}
                </span>
              )}
              <button className="ib" title={t("meet.presClear")} onClick={clearPage}><IcTrash size={15} /></button>
              <span className="pres-sep" />
            </>}
            {isPresenter && peers.length > 0 && <GrantMenu peers={peers} granted={granted} onToggle={toggleGrant} label={t("meet.presAllow")} />}
            {/* Full screen: the slide takes up everything and the cameras get out of the way (only for me). */}
            <button className="ib" title={full ? t("meet.presExitFull") : t("meet.presFull")} onClick={() => setFull((f) => !f)}>
              {full ? <IcRestoreWin size={15} /> : <IcMaximize size={15} />}
            </button>
            {/* Hiding is LOCAL (doesn't end it): brings the cameras back. */}
            <button className="ib" title={t("meet.presHide")} onClick={() => setHidden(true)}><IcExternal size={15} /></button>
            {isPresenter && <>
              <span className="pres-sep" />
              <button className="pres-stop" onClick={stop}><IcX size={14} /> {t("meet.presStop")}</button>
            </>}
          </div>
          )}
        </div>
      )}
    </>
  );

  return (
    <>
      {/* Guests (viewerOnly) can only watch — they can't start a presentation (no session on our API). */}
      {!viewerOnly && (
        <button className="meet-abtn" title={t("meet.present")} aria-label={t("meet.present")} onClick={() => (pres ? undefined : setPicker(true))} disabled={!!pres && !isPresenter}>
          <IcPresent size={19} />
        </button>
      )}
      {createPortal(overlays, overlayRoot())}
    </>
  );
}
