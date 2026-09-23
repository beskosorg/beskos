import { useState, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { LiveKitRoom, VideoConference, PreJoin, useRoomContext, type LocalUserChoices } from "@livekit/components-react";
import { RoomEvent } from "livekit-client";
import "@livekit/components-styles";
import { playSound } from "./sounds";
import { MeetEffects } from "./MeetEffects";
import { MeetReactions } from "./MeetReactions";
import { PresentationLayer } from "./MeetPresentation";
import { ControlBarSlot } from "./MeetBarSlot";
import { NotesPanel } from "./MeetNotes";
import { Avatar } from "./Avatar";
import { useUserSettings } from "./useSettings";
import { backend, type MeetSession, type Recording, type MeetControls, type BreakoutSession } from "./backend";
import { toast } from "./toast";
import { openInWindow } from "./nativeWindow";
import { isTauri, onTauriEvent } from "./tauriNotify";
import { publicUrl } from "../apiBase";
import { IcUsers, IcLink, IcGear, IcX, IcPencilBox, IcLogout, IcMicOff, IcStar } from "../panel/icons";
import { Switch } from "./Switch";

// openRoom opens the room in a dedicated window (/room/:room). The name/label is per room, so clicking
// again focuses the existing window instead of opening another one. On the web it uses window.open; on the
// native app (Tauri) window.open doesn't create a window — the native WebviewWindow API is needed.
export function openRoom(room: string, audio = false) {
  const path = `/room/${encodeURIComponent(room)}${audio ? "?audio=1" : ""}`;
  if (openInWindow(`room-${room}`, path, "Beskos — Sala")) return; // native app: dedicated window
  const w = Math.min(1280, screen.availWidth), h = Math.min(800, screen.availHeight);
  const feats = `popup=yes,width=${w},height=${h},left=${Math.max(0, (screen.availWidth - w) / 2)},top=${Math.max(0, (screen.availHeight - h) / 2)}`;
  const win = window.open(path, `mailin-room-${room}`, feats);
  win?.focus();
}

type Panel = "notes" | "host" | null;

// The call (dedicated window /room/:room). Layout follows the Meet pattern: media controls stay
// in the LiveKit bar (bottom-center) and our buttons in a bottom-right bar; the panels
// open anchored to the right side, one at a time (so nothing else overlaps).
export function MeetRoom({ session, audioOnly, onLeave }: {
  session: MeetSession; audioOnly?: boolean; onLeave: () => void;
}) {
  const { t } = useTranslation();
  const settings = useUserSettings(); // mic/camera defaults (settings.meet)
  const [panel, setPanel] = useState<Panel>(null);
  const toggle = (p: Panel) => setPanel((cur) => (cur === p ? null : p));
  const isHost = !!session.is_host && !!session.meeting_id;
  // Chat turned off by the host: hides the LiveKit chat UI for non-hosts (the data channel stays
  // open for presentation/laser — the permission is never cut). Applied on entry.
  const chatOff = session.controls?.allow_chat === false && !isHost;

  // Pre-join: camera/microphone setup screen before connecting. The user picks the
  // devices, sees the preview and the mic level, and only joins upon confirming (Meet pattern). While
  // `choices` is null, we don't connect to the room — no media sent until "Join".
  const [choices, setChoices] = useState<LocalUserChoices | null>(null);

  // Breakout: when `bk` is set, we connect to the breakout room instead of the main one (same
  // meeting, different LiveKit room). Switching rooms remounts the LiveKitRoom (via key); the
  // `switching` guard prevents the remount's onDisconnected from closing the window.
  const [bk, setBk] = useState<BreakoutSession | null>(null);
  const switching = useRef(false);
  const enterBreakout = useCallback((b: BreakoutSession) => { switching.current = true; setBk(b); }, []);
  const returnToMain = useCallback(() => { switching.current = true; setBk(null); }, []);
  const active = bk ?? { url: session.url, token: session.token, room: session.room };

  if (!choices) {
    return (
      <div className="meet-prejoin" data-lk-theme="default">
        <PreJoin
          defaults={{
            username: session.name ?? "",
            // Respects "join with camera/mic off" from Settings (settings.meet).
            videoEnabled: !audioOnly && settings.meet?.camOff !== true,
            audioEnabled: settings.meet?.enterMuted !== true,
          }}
          camLabel={t("meet.prejoin.cam")}
          micLabel={t("meet.prejoin.mic")}
          userLabel={t("meet.prejoin.name")}
          joinLabel={t("meet.prejoin.join")}
          onSubmit={setChoices}
          persistUserChoices={false}
        />
      </div>
    );
  }

  return (
    <div className={"meet-call" + (chatOff ? " chat-off" : "")}>
      <LiveKitRoom key={active.room} serverUrl={active.url} token={active.token} connect
        video={choices.videoEnabled} audio={choices.audioEnabled}
        options={{
          videoCaptureDefaults: { deviceId: choices.videoDeviceId || undefined },
          audioCaptureDefaults: { deviceId: choices.audioDeviceId || undefined },
        }}
        onDisconnected={() => { if (switching.current) { switching.current = false; return; } onLeave(); }}
        data-lk-theme="default" style={{ height: "100%" }}>
        <VideoConference />
        <LeaveOnWindowClose />
        <RoomSounds />

        {/* Breakout bar (top): join your room / return to main; disappears when there are no rooms. */}
        {session.meeting_id && session.identity && (
          <BreakoutBar meetingId={session.meeting_id} myId={session.identity} inBreakout={bk}
            onJoin={enterBreakout} onReturn={returnToMain} />
        )}

        {/* Entry requests (approval): card in the top-left corner, away from the panels. */}
        {isHost && session.access === "approval" && <HostLobby meetingId={session.meeting_id!} />}

        {/* Our buttons go INSIDE the LiveKit bar: one row, one style. */}
        <ControlBarSlot>
          <MeetEffects />
          {session.meeting_id && (
            <ActionBtn label={t("meet.notes")} on={panel === "notes"} onClick={() => toggle("notes")}>
              <IcPencilBox size={19} />
            </ActionBtn>
          )}
          {isHost && (
            <ActionBtn label={t("meet.hostControls")} on={panel === "host"} onClick={() => toggle("host")}>
              <IcGear size={19} />
            </ActionBtn>
          )}
          <MeetReactions />
          <PresentationLayer room={session.room} />
          <PipButton />
          <RecordButton room={session.room} />
          <CopyLinkButton room={session.room} />
        </ControlBarSlot>

        {panel && (
          <aside className="meet-side">
            <header>
              <b>{panel === "notes" ? t("meet.notes") : t("meet.hostControls")}</b>
              <div className="grow" />
              <button className="ib" title={t("common.close")} onClick={() => setPanel(null)}><IcX size={15} /></button>
            </header>
            <div className="ms-body">
              {panel === "notes" && session.meeting_id && <NotesPanel meetingId={session.meeting_id} title={session.title || session.room} isHost={session.is_host} />}
              {panel === "host" && isHost && (
                <HostPanel meetingId={session.meeting_id!} initial={session.controls} initialAccess={session.access ?? "public"} isOwner={!!session.is_owner} />
              )}
            </div>
          </aside>
        )}
      </LiveKitRoom>
    </div>
  );
}

// Closing the window = leaving the meeting. On the desktop app neither path ran React's cleanup,
// and the person stayed in the room with a frozen camera for everyone else:
//
//  (a) MAIN window: closing doesn't close the app — hides it in the tray and the webview stays alive. The
//      native side notifies ("window-hidden").
//  (b) The room's DEDICATED window (that's how Meet opens on desktop, see openRoom): closing it destroys
//      the webview abruptly, with no goodbye to the server. We hold the close, leave the room, and only then
//      destroy the window — with a time cap, so it never traps the user's window.
//
// In both cases onDisconnected handles the rest (closes the view and ends the presentation).
function LeaveOnWindowClose() {
  const room = useRoomContext();
  useEffect(() => {
    const offHidden = onTauriEvent("window-hidden", () => { void room.disconnect(); });
    let offClose: (() => void) | null = null;
    let gone = false;
    if (isTauri()) {
      void (async () => {
        try {
          const { getCurrentWindow } = await import("@tauri-apps/api/window");
          const win = getCurrentWindow();
          if (win.label === "main") return; // main is case (a)
          // Registering an onCloseRequested makes Tauri hold the native close and pass the decision to
          // JS: the API itself destroys the window when this handler finishes without preventDefault.
          // So, no preventDefault here — we only delay the close for the time it takes to leave the room (with a
          // cap, so it never traps the window) and let the API close it. This requires
          // `core:window:allow-destroy` in the capabilities; without it the close was denied and the window
          // stayed stuck (no click would close it).
          const un = await win.onCloseRequested(async () => {
            await Promise.race([room.disconnect().catch(() => {}), new Promise((r) => setTimeout(r, 1500))]);
          });
          if (gone) un(); else offClose = un;
        } catch { /* API unavailable: only path (a) remains */ }
      })();
    }
    return () => { offHidden(); gone = true; offClose?.(); };
  }, [room]);
  return null;
}

// Room sounds: onJoined, and someone joined.
//
// WHY VIA ROOM EVENTS and not <LiveKitRoom>'s onConnected: switching to a breakout and back
// remounts the room, and onConnected would fire the join sound again on every hop. The room's
// Connected event is the same in both cases, but here we have somewhere to store that it already played.
//
// The one for others joining plays once PER PERSON. Does someone arriving at an ongoing meeting get the
// ParticipantConnected event for people already there? No — that event is only for whoever arrives AFTER us,
// which is exactly what we want to announce.
function RoomSounds() {
  const room = useRoomContext();
  // Entering plays ONCE. There are two ways to learn we are in -- being connected already when this
  // mounts, and the Connected event -- and both fire in the ordinary case, which chimed twice.
  const announced = useRef(false);
  useEffect(() => {
    const onJoined = () => {
      if (announced.current) return;
      announced.current = true;
      playSound("meetJoin");
    };
    const onPeerJoined = () => playSound("meetPeer");
    // Already connected when this mounts (the ordinary case: LiveKitRoom connects before its
    // children mount). Without this the join sound would never play, the event having passed.
    if (room.state === "connected") onJoined();
    room.on(RoomEvent.Connected, onJoined);
    room.on(RoomEvent.ParticipantConnected, onPeerJoined);
    return () => { room.off(RoomEvent.Connected, onJoined); room.off(RoomEvent.ParticipantConnected, onPeerJoined); };
  }, [room]);
  return null;
}

// Round button in the action bar (same visual language as LiveKit's controls).
function ActionBtn({ label, on, onClick, danger, children }: {
  label: string; on?: boolean; onClick: () => void; danger?: boolean; children: React.ReactNode;
}) {
  return (
    <button className={"meet-abtn" + (on ? " on" : "") + (danger ? " danger" : "")} title={label} aria-label={label} onClick={onClick}>
      {children}
    </button>
  );
}

// Organizer controls (panel content). Only the owner sees it — and the backend also requires the owner
// (the query filters by owner_user_id), so hiding the button isn't the only defense.
function HostPanel({ meetingId, initial, initialAccess, isOwner }: {
  meetingId: string; initial?: MeetControls; initialAccess: string; isOwner: boolean;
}) {
  const { t } = useTranslation();
  const [c, setC] = useState<MeetControls>(initial ?? { allow_mic: true, allow_cam: true, allow_share: true, allow_chat: true, lock_until_host: false, locked: false });
  const [access, setAccess] = useState(initialAccess);

  const save = useMutation({
    mutationFn: (next: MeetControls) => backend.meetControls(meetingId, next),
    onSuccess: (r) => setC(r),
    onError: (e) => { setC(c); toast((e as Error).message); },
  });
  const setAcc = useMutation({
    mutationFn: (a: string) => backend.meetAccess(meetingId, a),
    onSuccess: (r) => { setAccess(r.access); toast(t("meet.accessSaved")); },
    onError: (e) => toast((e as Error).message),
  });
  const toggle = (k: keyof MeetControls) => { const next = { ...c, [k]: !c[k] }; setC(next); save.mutate(next); };

  const row = (k: keyof MeetControls, label: string, hint?: string) => (
    <label className="hc-row">
      <span className="hc-label">{label}{hint && <i>{hint}</i>}</span>
      <span className="switch">
        <input type="checkbox" checked={c[k]} onChange={() => toggle(k)} />
        <span className="track" />
      </span>
    </label>
  );

  // Logical order: 1) how one ENTERS (access + lock), 2) what one CAN do (media permissions),
  // 3) who is in the room, 4) splitting into breakouts. Room/access options belong to the OWNER;
  // a co-host only moderates people.
  return (
    <>
      {isOwner && <>
        <div className="hc-sec first">{t("meet.accessSection")}</div>
        <div className="hc-access">
          {(["public", "approval"] as const).map((a) => (
            <button key={a} className={access === a ? "on" : ""} disabled={setAcc.isPending} onClick={() => setAcc.mutate(a)}>
              <b>{t(a === "public" ? "meet.accessPublic" : "meet.accessApproval")}</b>
              <i>{t(a === "public" ? "meet.accessPublicHint" : "meet.accessApprovalHint")}</i>
            </button>
          ))}
        </div>
        {row("lock_until_host", t("meet.lockUntilHost"), t("meet.lockUntilHostHint"))}
        {row("locked", t("meet.locked"), t("meet.lockedHint"))}

        <div className="hc-sec">{t("meet.permissions")}</div>
        {row("allow_mic", t("meet.allowMic"))}
        {row("allow_cam", t("meet.allowCam"))}
        {row("allow_share", t("meet.allowShare"))}
        {row("allow_chat", t("meet.allowChat"), t("meet.allowChatHint"))}
      </>}

      <div className={"hc-sec" + (isOwner ? "" : " first")}>{t("meet.people")}</div>
      <Participants meetingId={meetingId} isOwner={isOwner} />

      <div className="hc-sec">{t("meet.breakouts")}</div>
      <BreakoutPanel meetingId={meetingId} />
    </>
  );
}

// Who is IN THE ROOM now (LiveKit is the one who knows) + kick. The host doesn't get a kick
// button: leaving your own room with the lock on would leave everyone at the door.
function Participants({ meetingId, isOwner }: { meetingId: string; isOwner: boolean }) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["meet-participants", meetingId],
    queryFn: () => backend.meetParticipants(meetingId),
    refetchInterval: 5000, retry: false,
  });
  const kick = useMutation({
    mutationFn: (identity: string) => backend.meetKick(meetingId, identity),
    onSuccess: () => { void q.refetch(); toast(t("meet.kicked")); },
    onError: (e) => toast((e as Error).message),
  });
  const mute = useMutation({
    mutationFn: (identity: string) => backend.meetMute(meetingId, identity),
    onSuccess: () => toast(t("meet.muted")),
    onError: (e) => toast((e as Error).message),
  });
  // Promote/demote co-host — owner only. The LiveKit identity is the user's id.
  const promote = useMutation({
    mutationFn: (identity: string) => backend.meetAddCohost(meetingId, identity),
    onSuccess: () => { void q.refetch(); toast(t("meet.cohostAdded")); },
    onError: (e) => toast((e as Error).message),
  });
  const demote = useMutation({
    mutationFn: (identity: string) => backend.meetRemoveCohost(meetingId, identity),
    onSuccess: () => { void q.refetch(); toast(t("meet.cohostRemoved")); },
    onError: (e) => toast((e as Error).message),
  });
  const list = q.data ?? [];
  if (list.length === 0) return <p className="hc-intro">{t("meet.noOneHere")}</p>;
  return (
    <div className="hc-people">
      {list.map((p) => (
        <div className="hc-person" key={p.identity}>
          <Avatar name={p.name} userId={p.identity} size={30} />
          <span className="nm">{p.name || p.identity.slice(0, 8)}</span>
          {p.is_host && <span className="pnl-pill muted">{t("meet.host")}</span>}
          {p.is_cohost && <span className="pnl-pill">{t("meet.cohost")}</span>}
          {!p.is_host && <>
            <button className="ib" title={t("meet.muteOne")} disabled={mute.isPending} onClick={() => mute.mutate(p.identity)}><IcMicOff size={14} /></button>
            <button className="ib" title={t("meet.kick")} disabled={kick.isPending} onClick={() => kick.mutate(p.identity)}><IcLogout size={14} /></button>
            {isOwner && (p.is_cohost
              ? <button className="ib on" title={t("meet.removeCohost")} disabled={demote.isPending} onClick={() => demote.mutate(p.identity)}><IcStar size={14} filled /></button>
              : <button className="ib" title={t("meet.makeCohost")} disabled={promote.isPending} onClick={() => promote.mutate(p.identity)}><IcStar size={14} /></button>)}
          </>}
        </div>
      ))}
    </div>
  );
}

// BreakoutBar: banner at the top of the call. Whoever was assigned to a room sees "Join your room";
// whoever is already in a breakout sees "Return to main room". If the host closes everything (empty list)
// while the user is in a breakout, they return to the main room automatically.
function BreakoutBar({ meetingId, myId, inBreakout, onJoin, onReturn }: {
  meetingId: string; myId: string; inBreakout: BreakoutSession | null;
  onJoin: (b: BreakoutSession) => void; onReturn: () => void;
}) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["meet-breakouts", meetingId],
    queryFn: () => backend.meetBreakouts(meetingId),
    refetchInterval: 4000, retry: false,
  });
  const rooms = q.data?.breakouts ?? [];
  const mine = rooms.find((b) => b.members.includes(myId));

  // Host closed the breakouts while I was in one -> I return to the main room automatically.
  useEffect(() => {
    if (inBreakout && rooms.length === 0 && q.isFetched) onReturn();
  }, [inBreakout, rooms.length, q.isFetched, onReturn]);

  const join = useMutation({
    mutationFn: (bid: string) => backend.meetBreakoutToken(meetingId, bid),
    onSuccess: (s) => onJoin(s),
    onError: (e) => toast((e as Error).message),
  });

  if (inBreakout) {
    return (
      <div className="breakout-bar">
        <span className="bb-label">{t("meet.inBreakout", { name: inBreakout.name })}</span>
        <button className="bb-btn" onClick={onReturn}>{t("meet.returnMain")}</button>
      </div>
    );
  }
  if (!mine) return null;
  return (
    <div className="breakout-bar invite">
      <span className="bb-label">{t("meet.breakoutInvite", { name: mine.name })}</span>
      <button className="bb-btn" disabled={join.isPending} onClick={() => join.mutate(mine.id)}>{t("meet.joinBreakout")}</button>
    </div>
  );
}

// BreakoutPanel (host): creates N rooms (with round-robin auto-distribution of those present), lists the
// rooms + member count, and closes everything. (Manual assignment exists in the backend; the UI uses auto.)
function BreakoutPanel({ meetingId }: { meetingId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [count, setCount] = useState(2);
  const [auto, setAuto] = useState(true);
  const q = useQuery({ queryKey: ["meet-breakouts", meetingId], queryFn: () => backend.meetBreakouts(meetingId), retry: false });
  const rooms = q.data?.breakouts ?? [];
  const refresh = () => qc.invalidateQueries({ queryKey: ["meet-breakouts", meetingId] });
  const create = useMutation({
    mutationFn: () => backend.meetCreateBreakouts(meetingId, count, auto),
    onSuccess: () => { refresh(); toast(t("meet.breakoutsCreated")); },
    onError: (e) => toast((e as Error).message),
  });
  const close = useMutation({
    mutationFn: () => backend.meetCloseBreakouts(meetingId),
    onSuccess: () => { refresh(); toast(t("meet.breakoutsClosed")); },
    onError: (e) => toast((e as Error).message),
  });
  return (
    <div className="breakout-panel">
      {rooms.length === 0 ? (
        <div className="bp-create">
          <p className="bp-hint">{t("meet.breakoutsHint")}</p>
          <div className="bp-row">
            <span>{t("meet.breakoutCount")}</span>
            <input type="number" min={1} max={20} value={count} onChange={(e) => setCount(Math.max(1, Math.min(20, Number(e.target.value) || 1)))} />
          </div>
          <div className="bp-row">
            <span>{t("meet.breakoutAuto")}</span>
            <Switch checked={auto} onChange={setAuto} label={t("meet.breakoutAuto")} />
          </div>
          <button className="bp-btn" disabled={create.isPending} onClick={() => create.mutate()}>{t("meet.createBreakouts")}</button>
        </div>
      ) : (
        <>
          <div className="bp-rooms">
            {rooms.map((b) => (
              <div key={b.id} className="bp-room">
                <b>{b.name}</b>
                <span className="bp-count">{t("meet.breakoutMembers", { n: b.members.length })}</span>
              </div>
            ))}
          </div>
          <button className="bp-btn danger" disabled={close.isPending} onClick={() => close.mutate()}>{t("meet.closeBreakouts")}</button>
        </>
      )}
    </div>
  );
}

// Record (toggle) — round button with a red dot while recording. Recording uses the tracks
// pipeline (offline GRID composition); composition layouts (speaker) would only exist in the live
// RoomComposite, which no plan uses — so no layout selector is offered that would have no effect.
function RecordButton({ room }: { room: string }) {
  const { t } = useTranslation();
  const [on, setOn] = useState(false);
  const checkFailed = async (id: string) => {
    for (let i = 0; i < 5; i++) {
      await new Promise((r) => setTimeout(r, 2500));
      const rec = (await backend.meetRecordings().catch(() => null))?.recordings.find((x: Recording) => x.id === id);
      if (rec?.status === "failed") { setOn(false); toast(t("meet.recFailed")); return; }
      if (rec?.status === "ready") return;
    }
  };
  const start = useMutation({ mutationFn: () => backend.meetRecordStart(room), onSuccess: (d) => { setOn(true); toast(t("meet.recStarted")); void checkFailed(d.recording_id); }, onError: (e) => toast((e as Error).message) });
  const stop = useMutation({ mutationFn: () => backend.meetRecordStop(room), onSuccess: () => { setOn(false); toast(t("meet.recStopped")); }, onError: (e) => toast((e as Error).message) });
  const busy = start.isPending || stop.isPending;
  return (
    <button className={"meet-abtn rec" + (on ? " on" : "")} disabled={busy} title={on ? t("meet.stopRec") : t("meet.record")}
      aria-label={on ? t("meet.stopRec") : t("meet.record")} onClick={() => (on ? stop : start).mutate()}>
      <span className="rec-dot" />
    </button>
  );
}

// Picture-in-Picture: pops the focused video (screen share or speaker) into a floating OS
// window, to follow the meeting while switching tabs/apps. Feature-detected: disappears where the browser doesn't support it
// (e.g. iOS's WKWebView). Picks the largest <video> playing inside the LiveKit stage.
function PipButton() {
  const { t } = useTranslation();
  const [on, setOn] = useState(false);
  const supported = typeof document !== "undefined" && (document as Document & { pictureInPictureEnabled?: boolean }).pictureInPictureEnabled;
  if (!supported) return null;

  const pickVideo = (): HTMLVideoElement | null => {
    const vids = Array.from(document.querySelectorAll<HTMLVideoElement>(".lk-room-container video, .meet-room video"))
      .filter((v) => v.readyState >= 2 && v.videoWidth > 0 && !v.disablePictureInPicture);
    // Largest visible area ≈ stage/share; with no candidates, null (the click becomes a no-op).
    return vids.sort((a, b) => b.videoWidth * b.videoHeight - a.videoWidth * a.videoHeight)[0] ?? null;
  };

  const toggle = async () => {
    try {
      if (document.pictureInPictureElement) { await document.exitPictureInPicture(); setOn(false); return; }
      const v = pickVideo();
      if (!v) { toast(t("meet.pipNoVideo")); return; }
      v.addEventListener("leavepictureinpicture", () => setOn(false), { once: true });
      await v.requestPictureInPicture();
      setOn(true);
    } catch { toast(t("meet.pipFailed")); }
  };

  return (
    <button className={"meet-abtn" + (on ? " on" : "")} title={on ? t("meet.pipExit") : t("meet.pip")}
      aria-label={on ? t("meet.pipExit") : t("meet.pip")} onClick={() => void toggle()}>
      <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <rect x="2" y="4" width="20" height="16" rx="2" />
        <rect x="12" y="12" width="8" height="6" rx="1" fill="currentColor" stroke="none" />
      </svg>
    </button>
  );
}

// Copy the room's public link (for external guests).
function CopyLinkButton({ room }: { room: string }) {
  const { t } = useTranslation();
  const link = publicUrl(`/j/${room}`);
  return (
    <button className="meet-abtn" title={link} aria-label={t("meet.copyLink")}
      onClick={() => { navigator.clipboard?.writeText(link); toast(t("meet.linkCopied")); }}>
      <IcLink size={18} />
    </button>
  );
}

// Host's waiting room: pending guests (approval). Polling + admit/deny.
function HostLobby({ meetingId }: { meetingId: string }) {
  const { t } = useTranslation();
  // refetchIntervalInBackground: without it TanStack Query STOPS polling once the window loses
  // focus -- which is exactly when this matters. A host working in another window only learned
  // about someone waiting at the door on coming back, and the knock arrived with all of the delay
  // at once.
  const q = useQuery({
    queryKey: ["lobby", meetingId], queryFn: () => backend.meetLobby(meetingId),
    refetchInterval: 3000, refetchIntervalInBackground: true, retry: false,
  });
  const admit = useMutation({ mutationFn: (id: string) => backend.meetAdmit(id), onSuccess: () => q.refetch() });
  const deny = useMutation({ mutationFn: (id: string) => backend.meetDeny(id), onSuccess: () => q.refetch() });
  const pending = q.data?.pending ?? [];

  // Knocks once PER PERSON. The list is re-polled every 3 seconds and returns the same
  // requests as long as no one dismisses them — playing on every poll would give an alarm of 20 knocks
  // per minute until the host reacts, which is the surest way to make them mute the sound.
  const announced = useRef<Set<string>>(new Set());
  useEffect(() => {
    const fresh = pending.filter((k) => !announced.current.has(k.id));
    if (!fresh.length) return;
    fresh.forEach((k) => announced.current.add(k.id));
    playSound("meetKnock");
  }, [pending]);

  if (!pending.length) return null;
  return (
    <div className="host-lobby">
      <div className="hl-title"><IcUsers size={14} /> {t("meet.lobbyTitle")}</div>
      {pending.map((k) => (
        <div className="hl-item" key={k.id}>
          <span className="grow">{k.name}</span>
          <button className="hl-admit" onClick={() => admit.mutate(k.id)}>{t("meet.admit")}</button>
          <button className="hl-deny" onClick={() => deny.mutate(k.id)}>{t("meet.deny")}</button>
        </div>
      ))}
    </div>
  );
}
