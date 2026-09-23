import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "./Switch";
import { toast } from "./toast";
import {
  notifSupported, notifPermission, requestNotifPermission, notifPrefs, setNotifPref, testNotification, type NotifDomain,
} from "./desktopNotify";
import { soundPrefs, setSoundPref, playSound } from "./sounds";
import { enableWebPush, disableWebPush, pushSupported } from "./webpush";
import { isTauri } from "./tauriNotify";

// Desktop notifications: browser permission + per-app choice. Per-device preferences
// (localStorage) — the permission belongs to the browser, so enabling it here doesn't affect other devices.
const NOTIF_DOMAINS: NotifDomain[] = ["email", "chat", "meet", "cal", "files", "tasks"];
const WEBPUSH_FLAG = "mailin.webpush.on";
export function NotificationsSection() {
  const { t } = useTranslation();
  const [perm, setPerm] = useState(notifPermission());
  const [prefs, setPrefs] = useState(notifPrefs());
  const [closedOn, setClosedOn] = useState(() => { try { return localStorage.getItem(WEBPUSH_FLAG) === "1"; } catch { return false; } });

  // Upon granting permission, also tries to subscribe to Web Push (to notify with the app closed).
  // If the server has no VAPID, the subscription fails silently and only the notifications
  // with the tab open remain — in both cases the open-tab experience works.
  const ask = async () => {
    const p = await requestNotifPermission();
    setPerm(p);
    if (p === "granted") { const ok = await enableWebPush(); setClosed(ok); }
  };
  const setClosed = (on: boolean) => { setClosedOn(on); try { localStorage.setItem(WEBPUSH_FLAG, on ? "1" : "0"); } catch { /* private mode */ } };
  const toggleClosed = async (on: boolean) => {
    if (on) { const ok = await enableWebPush(); setClosed(ok); }
    else { await disableWebPush(); setClosed(false); }
  };
  const toggle = (d: NotifDomain, on: boolean) => { setNotifPref(d, on); setPrefs(notifPrefs()); };
  const [sounds, setSounds] = useState(soundPrefs());
  const toggleSound = (k: "notify" | "meet", on: boolean) => {
    setSoundPref(k, on);
    setSounds(soundPrefs());
    if (on) playSound(k === "meet" ? "meetJoin" : "notify"); // turning it on plays immediately what was just turned on
  };

  if (!notifSupported()) {
    return (
      <div className="set-page">
        <h2>{t("settings.notifications")}</h2>
        <div className="set-card"><p className="desc">{t("settings.notifUnsupported")}</p></div>
      </div>
    );
  }

  return (
    <div className="set-page">
      <h2>{t("settings.notifications")}</h2>
      <div className="set-card">
        {perm !== "granted" ? (
          <div className="set-field col">
            <span>{t("settings.notifEnable")}</span>
            <p className="desc">{perm === "denied" ? t("settings.notifDenied") : t("settings.notifAskHint")}</p>
            {perm !== "denied" && (
              <button className="send" style={{ alignSelf: "flex-start" }} onClick={() => void ask()}>
                {t("settings.notifAllow")}
              </button>
            )}
          </div>
        ) : (
          <>
            <div className="ios-group-label">{t("settings.notifPerApp")}</div>
            <div className="ios-list">
              {NOTIF_DOMAINS.map((d) => (
                <div key={d} className="ios-row">
                  <span className="ios-row-label">{t(`settings.notifDomain.${d}`)}</span>
                  <Switch checked={prefs[d]} onChange={(on) => toggle(d, on)} label={t(`settings.notifDomain.${d}`)} />
                </div>
              ))}
            </div>
            {/* Native app (Tauri): notifies via the OS whenever it's running (even minimized),
                so it shows a note instead of Web Push (which WKWebView doesn't support). On the web,
                the toggle only appears when the browser supports Push; otherwise it explains why. */}
            {isTauri() ? (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifNativeTitle")}</span>
                    <span className="ios-row-sub">{t("settings.notifNativeHint")}</span>
                  </div>
                </div>
              </div>
            ) : pushSupported() ? (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifClosed")}</span>
                    <span className="ios-row-sub">{t("settings.notifTabHint")}</span>
                  </div>
                  <Switch checked={closedOn} onChange={(on) => void toggleClosed(on)} label={t("settings.notifClosed")} />
                </div>
              </div>
            ) : (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifClosed")}</span>
                    <span className="ios-row-sub">{t("settings.notifPushUnsupported")}</span>
                  </div>
                </div>
              </div>
            )}
            <button className="chipbtn" style={{ alignSelf: "flex-start" }}
              onClick={() => { void testNotification(t("settings.notifTestTitle"), t("settings.notifTestBody")).then((ok) => { if (!ok) toast(t("settings.notifTestFail")); }); }}>
              {t("settings.notifTest")}
            </button>
          </>
        )}
      </div>

      {/* Sounds: OUTSIDE the permission block, on purpose. Playing a sound needs no system
          authorization at all — someone who declined OS notifications might still want to hear that
          a message arrived, and hiding it here would leave that person with nothing. */}
      <div className="set-card">
        <div className="ios-group-label">{t("settings.soundGroup")}</div>
        <div className="ios-list">
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.soundNotify")}</span>
              <span className="ios-row-sub">{t(isTauri() ? "settings.soundNotifyHintApp" : "settings.soundNotifyHintWeb")}</span>
            </div>
            <Switch checked={sounds.notify} onChange={(on) => toggleSound("notify", on)} label={t("settings.soundNotify")} />
          </div>
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.soundMeet")}</span>
              <span className="ios-row-sub">{t("settings.soundMeetHint")}</span>
            </div>
            <Switch checked={sounds.meet} onChange={(on) => toggleSound("meet", on)} label={t("settings.soundMeet")} />
          </div>
        </div>
        {/* Hear it before deciding: a sound toggle with no way to try it forces you to
            wait for a real meeting to find out if it's any good. */}
        <div className="set-field" style={{ gap: 8 }}>
          <button className="chipbtn" onClick={() => playSound("notify")}>{t("settings.soundTryNotify")}</button>
          <button className="chipbtn" onClick={() => playSound("meetKnock")}>{t("settings.soundTryKnock")}</button>
        </div>
      </div>
    </div>
  );
}
