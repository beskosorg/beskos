import "../webmail/webmail.css";
import { useEffect, useRef, useState } from "react";
import { Navigate, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { MeetRoom } from "../webmail/MeetRoom";
import { applyBrandColor, cachedBrand } from "../webmail/branding";
import { closeSelfWindow } from "../webmail/nativeWindow";
import { ToastHost } from "../webmail/toast";
import { backend, type MeetSession } from "../webmail/backend";

// Route /room/:room — the room's dedicated window: ONLY the call, no app rail or topbar.
// This is where the webmail and chat open a new window when joining a meeting.
export function RoomPage() {
  const { room } = useParams<{ room: string }>();
  const [params] = useSearchParams();
  const audioOnly = params.get("audio") === "1";
  const { session } = useWebmail();
  const { t } = useTranslation();
  const [call, setCall] = useState<MeetSession | null>(null);
  const joined = useRef(false);
  // The room's dedicated window: applies the organization's brand color (from cache) — Meet
  // follows the branding like the rest of the app.
  useEffect(() => { applyBrandColor(cachedBrand()?.color ?? ""); }, []);

  const join = useMutation({
    mutationFn: () => backend.meetJoin(room),
    onSuccess: setCall,
  });

  useEffect(() => {
    if (session && room && !joined.current) { joined.current = true; join.mutate(); }
  }, [session, room, join]);

  // Room locked ("host joins first") or full (plan limit): keeps retrying on its own until a spot
  // opens up. Without this the guest would have to reload to find out a spot became available.
  useEffect(() => {
    if (!call?.locked && !call?.full) return;
    const id = setInterval(() => join.mutate(), 5000);
    return () => clearInterval(id);
  }, [call?.locked, call?.full, join]);

  // No session: this is the URL people copy from the address bar to share a meeting, so treat it
  // as the guest link instead of asking outsiders to log in. Members sign in from the guest page.
  if (!session) return room ? <Navigate to={`/j/${encodeURIComponent(room)}`} replace /> : <WebmailLogin />;

  return (
    <div className="wm room-only">
      {call?.locked
        ? <div className="room-wait lobby">
            <b>{t("meet.waitingHostTitle")}</b>
            <span>{t("meet.waitingHost", { title: call.title ?? "" })}</span>
          </div>
        : call?.full
          ? <div className="room-wait lobby">
              <b>{t("meet.fullTitle")}</b>
              <span>{t("meet.fullBody", { limit: call.limit ?? 0 })}</span>
            </div>
          : call
            ? <MeetRoom session={call} audioOnly={audioOnly} onLeave={() => closeSelfWindow()} />
            : <div className="room-wait">{join.isError ? (join.error as Error).message : t("meet.joining")}</div>}
      <ToastHost />
    </div>
  );
}
