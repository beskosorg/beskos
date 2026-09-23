import "../webmail/webmail.css";
import { LanguageSelect } from "../webmail/LanguageSelect";
import "./guest.css";
import { useState, useEffect, useRef } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { LiveKitRoom, VideoConference } from "@livekit/components-react";
import "@livekit/components-styles";
import { backend, type GuestJoin } from "../webmail/backend";
import { MeetEffects } from "../webmail/MeetEffects";
import { PresentationLayer } from "../webmail/MeetPresentation";
import { applyBrandColor } from "../webmail/branding";
import { ToastHost, toast } from "../webmail/toast";
import { IcVideo } from "../panel/icons";
import { useBrandingCapability } from "../useEdition";

// Public meeting entry page (external guest, no login). A public room joins right away; an
// approval room waits for the host to admit them. Wears the organization's branding, detects the
// browser's language (i18next LanguageDetector) and lets it be switched; signs with "Powered by
// Beskos" unless the instance has the Pro branding capability.
export function GuestPage() {
  const { t, i18n } = useTranslation();
  const { room } = useParams<{ room: string }>();
  const [name, setName] = useState("");
  const [session, setSession] = useState<GuestJoin | null>(null);
  const [waiting, setWaiting] = useState<string | null>(null); // lobby_id
  const [denied, setDenied] = useState(false);
  const [busy, setBusy] = useState(false);
  const poll = useRef<number | null>(null);

  const info = useQuery({ queryKey: ["public-meet", room], queryFn: () => backend.publicMeetInfo(room!), retry: false, enabled: !!room });
  const branded = useBrandingCapability(); // Pro: no "Powered by" signature
  // Organization brand (color) applied to the page; reverts to the default on exit.
  useEffect(() => { if (info.data?.brand?.color) applyBrandColor(info.data.brand.color); return () => applyBrandColor(""); }, [info.data?.brand?.color]);

  async function join() {
    if (!name.trim() || !room) return;
    setBusy(true);
    try {
      const r = await backend.publicMeetJoin(room, name.trim());
      if (r.mode === "joined") setSession(r);
      else if (r.mode === "lobby" && r.lobby_id) setWaiting(r.lobby_id);
    } catch (e) { toast((e as Error).message); }
    setBusy(false);
  }

  // Waiting for approval: polls the lobby until admitted (or denied).
  useEffect(() => {
    if (!waiting) return;
    const tick = async () => {
      try {
        const r = await backend.publicLobbyPoll(waiting);
        if (r.status === "admitted" && r.token) { setSession(r); setWaiting(null); }
        else if (r.status === "denied") { setDenied(true); setWaiting(null); }
      } catch { /* keep trying */ }
    };
    poll.current = window.setInterval(tick, 3000);
    return () => { if (poll.current) clearInterval(poll.current); };
  }, [waiting]);

  if (session?.token && session.url) {
    return (
      <div className="wm">
        <div className="meet-call" style={{ position: "fixed", inset: 0 }}>
          <LiveKitRoom serverUrl={session.url} token={session.token} connect video audio
            onDisconnected={() => setSession(null)} data-lk-theme="default" style={{ height: "100%" }}>
            <VideoConference />
            <MeetEffects />
            <PresentationLayer room={room!} viewerOnly />
          </LiveKitRoom>
        </div>
        <ToastHost />
      </div>
    );
  }

  const brand = info.data?.brand;
  const title = info.data?.title || t("meet.title");
  return (
    <div className="wm">
      <div className="gp">
        <div className="gp-card">
          <LangSwitch />
          {brand?.logo
            ? <img className={"gp-logo" + (brand.logo_white_dark ? " white-dark" : "")} src={brand.logo} alt={info.data?.org || ""} />
            : <span className="gp-badge"><IcVideo size={26} /></span>}
          {info.data?.org && <div className="gp-org">{info.data.org}</div>}
          <h1 className="gp-title">{title}</h1>

          {denied ? (
            <p className="gp-msg">{t("meet.denied")}</p>
          ) : waiting ? (
            <div className="gp-waiting">
              <span className="gp-spin" />
              <p className="gp-msg">{t("meet.waitingHost", { title })}</p>
            </div>
          ) : (
            <>
              <p className="gp-sub">{info.data?.access === "approval" ? t("meet.approvalHint") : t("meet.publicHint")}</p>
              <input className="gp-input" autoFocus value={name} placeholder={t("meet.yourName")}
                onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") join(); }} />
              <button className="gp-join" disabled={!name.trim() || busy} onClick={join}>
                {busy ? "…" : <><IcVideo size={17} /> {t("meet.enter")}</>}
              </button>
            </>
          )}
          {/* Members who land here from a shared room URL (/room or /mail/meet redirect here
              without a session) still have a way into their own account. */}
          {!waiting && !denied && <a className="gp-signin" href="/mail">{t("meet.memberSignIn")}</a>}
          {!branded && (
            <a className="gp-powered" href="https://beskos.com" target="_blank" rel="noreferrer">
              <Trans i18nKey="public.poweredBy" values={{ brand: "Beskos" }} components={{ b: <b /> }} />
            </a>
          )}
        </div>
      </div>
      <ToastHost />
    </div>
  );

  function LangSwitch() {
    return (
      <LanguageSelect className="gp-lang" value={i18n.resolvedLanguage ?? i18n.language}
        onChange={(lng) => void i18n.changeLanguage(lng)} />
    );
  }
}
