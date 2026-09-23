import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { overlayRoot } from "./MeetBarSlot";
import { useTranslation } from "react-i18next";
import { useLocalParticipant, useRoomContext } from "@livekit/components-react";
import { ConnectionState, RoomEvent } from "livekit-client";
import { IcHandRaise, IcEmoji } from "../panel/icons";

// Reactions and raised hand over the LiveKit data channel ("react" topic), separate from the
// presentation's "pres". Emoji float up and disappear; raised hands stay in a panel until their
// owner lowers them. Everything is ephemeral and P2P — nothing touches the backend. Meet pattern: emoji palette + hand button in the action bar.

const enc = new TextEncoder();
const dec = new TextDecoder();
const PALETTE = ["👍", "❤️", "😂", "🎉", "👏", "😮"];

type RMsg =
  | { t: "emoji"; e: string; by: string; name?: string }
  | { t: "hand"; up: boolean; by: string; name?: string };

type Flying = { id: number; e: string; left: number };

export function MeetReactions() {
  const { t } = useTranslation();
  const roomCtx = useRoomContext();
  const { localParticipant } = useLocalParticipant();
  const myId = localParticipant?.identity ?? "";
  const myName = localParticipant?.name || myId;

  const [flying, setFlying] = useState<Flying[]>([]);
  const [hands, setHands] = useState<{ id: string; name: string }[]>([]);
  const [myHand, setMyHand] = useState(false);
  const [picker, setPicker] = useState(false);
  const seq = useRef(0);

  const post = useCallback((m: RMsg, reliable = true) => {
    const room = roomCtx;
    if (!room || room.state !== ConnectionState.Connected) return; // ephemeral: queuing doesn't make sense
    room.localParticipant.publishData(enc.encode(JSON.stringify(m)), { reliable, topic: "react" })
      .catch(() => { /* a lost frame is acceptable for a reaction */ });
  }, [roomCtx]);

  const showEmoji = useCallback((e: string) => {
    const id = ++seq.current;
    const left = 8 + Math.floor(Math.random() * 84); // 8%..92% of the width
    setFlying((f) => [...f, { id, e, left }]);
    setTimeout(() => setFlying((f) => f.filter((x) => x.id !== id)), 3200);
  }, []);

  const sendEmoji = (e: string) => {
    showEmoji(e);
    post({ t: "emoji", e, by: myId, name: myName });
    setPicker(false);
  };

  const setHand = useCallback((up: boolean) => {
    setMyHand(up);
    setHands((h) => {
      const rest = h.filter((x) => x.id !== myId);
      return up ? [...rest, { id: myId, name: myName }] : rest;
    });
    post({ t: "hand", up, by: myId, name: myName });
  }, [myId, myName, post]);

  // Receives reactions from others. Kept in a ref so the handler sees the current state without resubscribing.
  const onMsgRef = useRef<(m: RMsg) => void>(() => {});
  onMsgRef.current = (m) => {
    if (m.by === myId) return; // already shown locally
    if (m.t === "emoji") { showEmoji(m.e); return; }
    setHands((h) => {
      const rest = h.filter((x) => x.id !== m.by);
      return m.up ? [...rest, { id: m.by, name: m.name || m.by }] : rest;
    });
  };
  const handRef = useRef(myHand);
  handRef.current = myHand;

  useEffect(() => {
    if (!roomCtx) return;
    const onData = (payload: Uint8Array, _p?: unknown, _k?: unknown, topic?: string) => {
      if (topic && topic !== "react") return;
      try { onMsgRef.current(JSON.parse(dec.decode(payload)) as RMsg); } catch { /* invalid */ }
    };
    // A newcomer joined: if my hand is raised, re-announce it so they see it.
    const onJoin = () => { if (handRef.current) setTimeout(() => post({ t: "hand", up: true, by: myId, name: myName }), 900); };
    // Someone left: lower their hand locally.
    const onLeft = (p: { identity?: string }) => { if (p?.identity) setHands((h) => h.filter((x) => x.id !== p.identity)); };
    roomCtx.on(RoomEvent.DataReceived, onData);
    roomCtx.on(RoomEvent.ParticipantConnected, onJoin);
    roomCtx.on(RoomEvent.ParticipantDisconnected, onLeft);
    return () => {
      roomCtx.off(RoomEvent.DataReceived, onData);
      roomCtx.off(RoomEvent.ParticipantConnected, onJoin);
      roomCtx.off(RoomEvent.ParticipantDisconnected, onLeft);
    };
  }, [roomCtx, post, myId, myName]);

  const overlay = (
    <>
      <div className="react-fly" aria-hidden>
        {flying.map((f) => (
          <span key={f.id} className="react-fly-item" style={{ left: f.left + "%" }}>{f.e}</span>
        ))}
      </div>
      {hands.length > 0 && (
        <div className="react-hands" role="status">
          <b>✋ {t("meet.react.raised")}</b>
          <ul>{hands.map((h) => <li key={h.id}>{h.id === myId ? t("meet.react.you") : h.name}</li>)}</ul>
        </div>
      )}
    </>
  );

  return (
    <>
      <div className="react-ctl">
        {picker && (
          <div className="react-palette" role="menu">
            {PALETTE.map((e) => (
              <button key={e} className="react-emoji" onClick={() => sendEmoji(e)} aria-label={e}>{e}</button>
            ))}
          </div>
        )}
        <button className={"meet-abtn" + (myHand ? " on" : "")} title={myHand ? t("meet.react.lower") : t("meet.react.raise")}
          onClick={() => setHand(!myHand)}>
          <IcHandRaise size={19} />
        </button>
        <button className={"meet-abtn" + (picker ? " on" : "")} title={t("meet.react.emoji")}
          onClick={() => setPicker((v) => !v)}>
          <IcEmoji size={19} />
        </button>
      </div>
      {createPortal(overlay, overlayRoot())}
    </>
  );
}
