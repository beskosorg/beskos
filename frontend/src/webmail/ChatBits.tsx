import { useTranslation } from "react-i18next";
import { openRoom } from "./MeetRoom";
import { IcVideo, IcPhone } from "../panel/icons";
import { quickTimes } from "./chatUtils";

export function TimeMenu({ onPick, onClose }: { onPick: (d: Date) => void; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="time-menu" onMouseLeave={onClose}>
      {quickTimes(t).map((o) => (
        <button key={o.label} onMouseDown={(e) => { e.preventDefault(); onPick(o.date); }}>{o.label}</button>
      ))}
    </div>
  );
}

export function BookmarkIcon({ filled, size = 13 }: { filled?: boolean; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill={filled ? "currentColor" : "none"} stroke="currentColor" strokeWidth="1.9" strokeLinejoin="round" aria-hidden><path d="M6 3h12a1 1 0 0 1 1 1v17l-7-4-7 4V4a1 1 0 0 1 1-1Z" /></svg>;
}

// Call card: shows who started it and a button to join the Meet room (LiveKit).
export function CallCard({ call, author }: { call: { mode: "video" | "audio"; room: string }; author: string }) {
  const { t } = useTranslation();
  return (
    <div className="call-card">
      <span className="call-ic">{call.mode === "video" ? <IcVideo size={18} /> : <IcPhone size={17} />}</span>
      <div className="call-info">
        <b>{t(call.mode === "video" ? "chat.videoCallStarted" : "chat.audioCallStarted")}</b>
        <span>{t("chat.callBy", { name: author })}</span>
      </div>
      <button className="call-join" onClick={() => openRoom(call.room, call.mode === "audio")}>{t("chat.joinCall")}</button>
    </div>
  );
}
