import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocalParticipant } from "@livekit/components-react";
import type { LocalVideoTrack } from "livekit-client";
import { BackgroundProcessor, supportsBg, preloadBgModel, bgEngine, type BgMode } from "./bgProcessor";
import { toast } from "./toast";
import { IcSparkle, IcUpload } from "../panel/icons";

// Generated backgrounds (soft bokeh) as data URL — lightweight and always available, in addition to custom upload.
function bokeh(base: string, dots: string): string {
  const c = document.createElement("canvas"); c.width = 1280; c.height = 720;
  const ctx = c.getContext("2d")!;
  const g = ctx.createLinearGradient(0, 0, 1280, 720);
  g.addColorStop(0, base); g.addColorStop(1, dots);
  ctx.fillStyle = g; ctx.fillRect(0, 0, 1280, 720);
  for (let i = 0; i < 40; i++) {
    const x = ((i * 97) % 128) * 10, y = ((i * 53) % 72) * 10, r = 20 + (i % 5) * 22;
    ctx.globalAlpha = 0.06 + (i % 4) * 0.03; ctx.fillStyle = "#ffffff";
    ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill();
  }
  ctx.globalAlpha = 1;
  return c.toDataURL("image/jpeg", 0.85);
}
const BGS = [
  { key: "teal", url: bokeh("#0d3b3d", "#116466") },
  { key: "indigo", url: bokeh("#1b2450", "#3B6FB0") },
  { key: "plum", url: bokeh("#2a1b3d", "#8257C7") },
  { key: "graphite", url: bokeh("#14161a", "#2c3038") },
];

// Meet camera effects using OUR processor (confidence mask + soft edge).
export function MeetEffects() {
  const { t } = useTranslation();
  const { cameraTrack } = useLocalParticipant();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState("none");
  const [busy, setBusy] = useState(false);
  const [custom, setCustom] = useState<string | null>(null);
  const proc = useRef<BackgroundProcessor | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const track = cameraTrack?.track as LocalVideoTrack | undefined;

  // Preloads the segmenter (LOCAL, bundled in the app — no downloads) as soon as the menu opens.
  const [model, setModel] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [modelErr, setModelErr] = useState("");
  useEffect(() => {
    if (!open || model === "ready" || model === "loading") return;
    setModel("loading");
    preloadBgModel().then(() => setModel("ready")).catch((e) => {
      console.warn("[meet] effects: model failed:", e);
      setModelErr(String((e as Error)?.message ?? e).slice(0, 140)); // real error on screen (diagnostic)
      setModel("error");
    });
  }, [open, model]);

  async function apply(key: string, mode: BgMode) {
    if (!track || busy) return;
    setBusy(true);
    try {
      if (mode.kind === "none") {
        // We do NOT turn off the processor (track.stopProcessor doesn't always restore the video on
        // WKWebView — the camera would "disappear"). "None" becomes pass-through: the renderer draws the raw video.
        if (proc.current) await proc.current.setMode(mode);
      } else if (!proc.current) {
        proc.current = new BackgroundProcessor(mode);
        await track.setProcessor(proc.current);
      } else {
        await proc.current.setMode(mode);
      }
      setActive(key);
    } catch (e) {
      console.warn("[meet] effects: apply failed:", e);
      toast(t("meet.fxApplyFail"));
    }
    setBusy(false);
  }
  function onUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]; if (!f) return;
    const rd = new FileReader();
    rd.onload = () => { const url = rd.result as string; setCustom(url); apply("custom", { kind: "image", url }); };
    rd.readAsDataURL(f); e.target.value = "";
  }

  if (!supportsBg()) return null;

  return (
    <div className="meet-fx">
      {/* Icon only: the label goes through title/aria so accessibility and the hover hint aren't lost. */}
      <button className="fx-toggle" onClick={() => setOpen((v) => !v)} disabled={!track}
        title={t("meet.effects")} aria-label={t("meet.effects")}><IcSparkle size={17} /></button>
      {open && (
        <div className="fx-menu">
          {model === "loading" && <div className="fx-status">{t("meet.fxPreparing")}</div>}
          {model === "ready" && bgEngine() && <div className="fx-status">{t("meet.fxEngine", { engine: bgEngine() })}</div>}
          {model === "error" && (
            <button className="fx-status err" onClick={() => setModel("idle")}>
              {t("meet.fxLoadFail")}
              {modelErr && <i style={{ display: "block", opacity: 0.8, fontStyle: "normal", marginTop: 2 }}>{modelErr}</i>}
            </button>
          )}
          <div className="fx-row">
            <button className={"fx-item" + (active === "none" ? " on" : "")} onClick={() => apply("none", { kind: "none" })}>{t("meet.fxNone")}</button>
            <button className={"fx-item" + (active === "blur" ? " on" : "")} onClick={() => apply("blur", { kind: "blur", radius: 6 })}>{t("meet.fxBlur")}</button>
            <button className={"fx-item" + (active === "blurStrong" ? " on" : "")} onClick={() => apply("blurStrong", { kind: "blur", radius: 14 })}>{t("meet.fxBlurStrong")}</button>
          </div>
          <div className="fx-row">
            {BGS.map((b) => (
              <button key={b.key} className={"fx-bg" + (active === b.key ? " on" : "")} style={{ backgroundImage: `url(${b.url})` }} title={t("meet.fxBackground")} onClick={() => apply(b.key, { kind: "image", url: b.url })} />
            ))}
            {custom && <button className={"fx-bg" + (active === "custom" ? " on" : "")} style={{ backgroundImage: `url(${custom})` }} onClick={() => apply("custom", { kind: "image", url: custom })} />}
            <button className="fx-upload" title={t("meet.fxUpload")} onClick={() => fileRef.current?.click()}><IcUpload size={16} /></button>
            <input ref={fileRef} type="file" accept="image/*" hidden onChange={onUpload} />
          </div>
        </div>
      )}
    </div>
  );
}
