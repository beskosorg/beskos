import { pipeline, RawImage, env, type BackgroundRemovalPipeline } from "@huggingface/transformers";
import { FilesetResolver, ImageSegmenter } from "@mediapipe/tasks-vision";
import { Track } from "livekit-client";
import type { TrackProcessor, ProcessorOptions } from "livekit-client";

// Background processor with a HYBRID engine (Jitsi/Meet pattern), 100% bundled (no network at runtime):
//  - MODNet (matting, top quality) via WebGPU — Chrome/Edge on the web.
//  - MediaPipe Selfie Segmentation (WebGL, light and fast) — WebKit/WKWebView (desktop Tauri and
//    Safari), where the ONNX Runtime is unviable: JSEP has a serious bug in WebKit 26 (CPU 400%+, memory
//    up to 14GB — onnxruntime#26827) and WASM falls back to single-thread without SharedArrayBuffer (11-40x slower).
// Inference runs decoupled from render → smooth video even if the mask runs at a lower fps.
export type BgMode =
  | { kind: "none" }
  | { kind: "blur"; radius: number }
  | { kind: "image"; url: string };

const MODEL = "Xenova/modnet";
const INFER_GAP_MS = 40; // gap between inferences (~≤12fps of mask) — don't choke the rest
const ALPHA_FLOOR = 34; // (MODNet) alpha below this becomes 0 → cuts the light fringe/halo
const RAMP_LO = 0.25, RAMP_HI = 0.7; // (MediaPipe) confidence ramp → alpha
const MASK_BLUR = 2; // (MediaPipe) box blur of the alpha at low resolution → soft edge

// LOCAL MODEL (transformers.js): served by the app itself. allowLocalModels defaults to FALSE
// in the browser — without enabling it, with remote blocked, no source would be allowed at all.
env.allowLocalModels = true;
env.allowRemoteModels = false;
env.localModelPath = "/models/";
// ONNX runtime (wasm/jsep) is also LOCAL — by default it would go to the jsdelivr CDN.
if (env.backends.onnx?.wasm) env.backends.onnx.wasm.wasmPaths = "/ort/";

// "Pure" WebKit (Safari and Tauri's WKWebView; Chrome's UA also contains AppleWebKit).
const isWebKitOnly = typeof navigator !== "undefined" &&
  /AppleWebKit/i.test(navigator.userAgent) && !/Chrome|Chromium|Edg\//i.test(navigator.userAgent);

type Matter =
  | { kind: "modnet"; run: BackgroundRemovalPipeline }
  | { kind: "mediapipe"; seg: ImageSegmenter };

let matterPromise: Promise<Matter> | null = null;
let inferSize = 256; // input size, decided by the loaded engine
let engineName = ""; // shown in the effects UI (diagnostic)
export function bgEngine(): string { return engineName; }

function withIdleTimeout<T>(run: (bump: () => void) => Promise<T>, idleMs: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    let timer: ReturnType<typeof setTimeout>;
    const arm = () => { clearTimeout(timer); timer = setTimeout(() => reject(new Error("model load stalled")), idleMs); };
    arm();
    run(arm).then((v) => { clearTimeout(timer); resolve(v); }, (e) => { clearTimeout(timer); reject(e); });
  });
}

async function getMatter(): Promise<Matter> {
  if (!matterPromise) {
    matterPromise = (async () => {
      // 1) MODNet via WebGPU — only outside WebKit (see note at the top).
      if ("gpu" in navigator && !isWebKitOnly) {
        try {
          const run = await withIdleTimeout<BackgroundRemovalPipeline>(
            (bump) => pipeline("background-removal", MODEL, {
              device: "webgpu", dtype: "fp16", progress_callback: () => bump(),
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
            } as any) as Promise<BackgroundRemovalPipeline>,
            15000,
          );
          // Reduces the model's internal resolution (default 512) → faster, less latency.
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const ip: any = (run.processor as any)?.image_processor ?? (run.processor as any)?.feature_extractor ?? run.processor;
          if (ip?.size) ip.size = { shortest_edge: 256 };
          inferSize = 256; engineName = "GPU · MODNet";
          console.info("[meet] matting: MODNet WebGPU/fp16 (local)");
          return { kind: "modnet", run } as Matter;
        } catch (e) { console.warn("[meet] MODNet WebGPU unavailable, using MediaPipe:", e); }
      }
      // 2) MediaPipe (WebGL) — fast on WKWebView/Safari; CPU as a last resort.
      const vision = await FilesetResolver.forVisionTasks("/mediapipe/wasm");
      const opts = {
        baseOptions: { modelAssetPath: "/mediapipe/models/selfie_segmenter.tflite", delegate: "GPU" as const },
        runningMode: "VIDEO" as const,
        outputConfidenceMasks: true,
        outputCategoryMask: false,
      };
      let seg: ImageSegmenter; let dev = "GPU";
      try {
        seg = await ImageSegmenter.createFromOptions(vision, opts);
      } catch (e) {
        console.warn("[meet] MediaPipe GPU unavailable, using CPU:", e);
        seg = await ImageSegmenter.createFromOptions(vision, { ...opts, baseOptions: { ...opts.baseOptions, delegate: "CPU" } });
        dev = "CPU";
      }
      inferSize = 320; engineName = dev + " · MediaPipe";
      console.info(`[meet] matting: MediaPipe ${dev} (local)`);
      return { kind: "mediapipe", seg } as Matter;
    })().catch((e) => { matterPromise = null; throw e; });
  }
  return matterPromise;
}

// preloadBgModel warms up the engine (called when the effects menu opens) — it's local, loads fast.
export function preloadBgModel(): Promise<void> { return getMatter().then(() => undefined); }

export function supportsBg(): boolean {
  return !!document.createElement("canvas").captureStream;
}

// Separable box blur of the alpha at low resolution (MediaPipe: confidence is nearly binary; without
// this the edge turns into steps when scaled up).
function blurAlpha(src: Float32Array, tmp: Float32Array, out: Float32Array, w: number, h: number, r: number) {
  const n = 2 * r + 1;
  for (let y = 0; y < h; y++) {
    const row = y * w;
    let sum = 0;
    for (let i = -r; i <= r; i++) sum += src[row + Math.min(w - 1, Math.max(0, i))];
    for (let x = 0; x < w; x++) {
      tmp[row + x] = sum / n;
      sum += src[row + Math.min(w - 1, x + r + 1)] - src[row + Math.max(0, x - r)];
    }
  }
  for (let x = 0; x < w; x++) {
    let sum = 0;
    for (let i = -r; i <= r; i++) sum += tmp[Math.min(h - 1, Math.max(0, i)) * w + x];
    for (let y = 0; y < h; y++) {
      out[y * w + x] = sum / n;
      sum += tmp[Math.min(h - 1, y + r + 1) * w + x] - tmp[Math.max(0, y - r) * w + x];
    }
  }
}

export class BackgroundProcessor implements TrackProcessor<Track.Kind.Video> {
  name = "mailin-background";
  processedTrack?: MediaStreamTrack;

  private matter?: Matter;
  private video = document.createElement("video");
  private out = document.createElement("canvas");
  private outCtx = this.out.getContext("2d")!;
  private person = document.createElement("canvas");
  private personCtx = this.person.getContext("2d")!;
  private mask = document.createElement("canvas");
  private maskCtx = this.mask.getContext("2d")!;
  private frame = document.createElement("canvas"); // video snapshot matching the current mask
  private frameCtx = this.frame.getContext("2d")!;
  private pending = document.createElement("canvas"); // full-res snapshot being processed
  private pendingCtx = this.pending.getContext("2d")!;
  private smallIn = document.createElement("canvas"); // DOWNSCALED input for the model
  private smallInCtx = this.smallIn.getContext("2d", { willReadFrequently: true })!;
  private blurA = document.createElement("canvas"); // downscale-blur scratch
  private blurB = document.createElement("canvas");
  private bgImg?: HTMLImageElement;
  private mode: BgMode;
  private raf = 0;
  private running = false;
  private inferring = false;
  private haveMask = false;
  private lastAttach = 0; // engine attach backoff
  private reqFrame: (() => void) | null = null; // manual per-frame capture (WebKit)
  private aBuf?: Float32Array; // reused buffers (MediaPipe)
  private aTmp?: Float32Array;
  private aOut?: Float32Array;

  constructor(mode: BgMode = { kind: "blur", radius: 6 }) { this.mode = mode; }

  async init(opts: ProcessorOptions<Track.Kind.Video>): Promise<void> {
    this.video.autoplay = true; this.video.muted = true; this.video.playsInline = true;
    this.video.srcObject = new MediaStream([opts.track]);
    await this.video.play();
    if (!this.video.videoWidth) await new Promise<void>((r) => { this.video.onloadedmetadata = () => r(); });
    const w = this.video.videoWidth || 640, h = this.video.videoHeight || 480;
    for (const c of [this.out, this.person, this.frame, this.pending]) { c.width = w; c.height = h; }
    this.personCtx.imageSmoothingEnabled = true; this.personCtx.imageSmoothingQuality = "high";
    this.outCtx.imageSmoothingEnabled = true; this.outCtx.imageSmoothingQuality = "high";
    if (this.mode.kind === "image") await this.loadImage(this.mode.url);
    const stream = this.out.captureStream(30);
    this.processedTrack = stream.getVideoTracks()[0];
    // Hint to the encoder: prioritize smoothness (fps) over resolution when allocating bitrate.
    try { (this.processedTrack as MediaStreamTrack & { contentHint?: string }).contentHint = "motion"; } catch { /* optional */ }
    // WebKit: a canvas OUTSIDE the DOM is barely composited → automatic capture delivers ~0-1fps
    // to the WebRTC encoder (the remote side sees "very slow" video; the local preview is misleading
    // because the local <video> pulls frames through a different path). Fix: (a) a tiny canvas in the
    // DOM so compositing happens, (b) an explicit requestFrame() on every render — guaranteed 30fps cadence.
    const t = this.processedTrack as unknown as { requestFrame?: () => void };
    const s = stream as unknown as { requestFrame?: () => void };
    this.reqFrame = t.requestFrame ? t.requestFrame.bind(this.processedTrack)
      : s.requestFrame ? s.requestFrame.bind(stream) : null;
    if (isWebKitOnly && !this.out.parentNode) {
      Object.assign(this.out.style, {
        position: "fixed", right: "0", bottom: "0", width: "2px", height: "2px",
        opacity: "0.01", pointerEvents: "none",
      });
      document.body.appendChild(this.out);
    }
    this.running = true;
    getMatter().then((m) => { this.matter = m; }).catch((e) => console.warn("[meet] matting failed:", e));
    this.renderLoop();
    this.inferLoop();
  }

  async restart(opts: ProcessorOptions<Track.Kind.Video>): Promise<void> {
    this.stopLoops();
    this.haveMask = false;
    this.video.srcObject = new MediaStream([opts.track]);
    await this.video.play();
    this.running = true;
    this.renderLoop(); this.inferLoop();
  }

  // We do NOT stop the source track (it belongs to LiveKit's camera, not ours) — we only clean up our own.
  async destroy(): Promise<void> {
    this.stopLoops();
    this.haveMask = false;
    this.video.srcObject = null;
    if (this.out.parentNode) this.out.remove(); // the WebKit compositing canvas leaves the DOM
  }

  async setMode(mode: BgMode): Promise<void> {
    if (mode.kind === "image") await this.loadImage(mode.url);
    this.mode = mode;
  }

  private stopLoops() { this.running = false; if (this.raf) cancelAnimationFrame(this.raf); this.raf = 0; }

  private loadImage(url: string): Promise<void> {
    return new Promise((resolve) => {
      const img = new Image(); img.crossOrigin = "anonymous";
      img.onload = () => { this.bgImg = img; resolve(); };
      img.onerror = () => resolve();
      img.src = url;
    });
  }

  // Prepares the model's downscaled input. The resize happens on the GPU (drawImage) and the
  // readback stays tiny — feeding the full-res frame cost dozens of ms of readback per inference.
  private drawSmall(): void {
    const vw = this.video.videoWidth || 640, vh = this.video.videoHeight || 480;
    const scale = inferSize / Math.min(vw, vh);
    const sw = Math.max(2, Math.round(vw * scale)), sh = Math.max(2, Math.round(vh * scale));
    if (this.smallIn.width !== sw || this.smallIn.height !== sh) { this.smallIn.width = sw; this.smallIn.height = sh; }
    this.smallInCtx.drawImage(this.video, 0, 0, sw, sh);
  }

  private inferLoop = async () => {
    if (!this.running) return;
    if (!this.matter && this.mode.kind !== "none" && Date.now() - this.lastAttach > 4000) {
      this.lastAttach = Date.now();
      getMatter().then((m) => { this.matter = m; }).catch(() => { /* retries next cycle */ });
    }
    if (this.matter && !this.inferring && this.video.readyState >= 2 && this.mode.kind !== "none") {
      this.inferring = true;
      try {
        // Full-res snapshot only for COMPOSITING (drawImage, no readback) + the model's downscaled input.
        this.pendingCtx.drawImage(this.video, 0, 0, this.pending.width, this.pending.height);
        this.drawSmall();
        if (this.matter.kind === "modnet") await this.inferModnet(this.matter.run);
        else await this.inferMediapipe(this.matter.seg);
      } catch { /* dropped frame */ }
      this.inferring = false;
    }
    setTimeout(this.inferLoop, INFER_GAP_MS);
  };

  private async inferModnet(run: BackgroundRemovalPipeline): Promise<void> {
    const input = await RawImage.fromCanvas(this.smallIn);
    const res = await run(input);
    const out = (Array.isArray(res) ? res[0] : res) as RawImage;
    if (!out?.data) return;
    const mw = out.width, mh = out.height, ch = out.channels;
    if (this.mask.width !== mw) { this.mask.width = mw; this.mask.height = mh; }
    const id = this.maskCtx.createImageData(mw, mh);
    for (let i = 0; i < mw * mh; i++) {
      let a = out.data[i * ch + (ch - 1)]; // alpha channel (matte — already smooth, handles hair)
      if (a < ALPHA_FLOOR) a = 0;
      id.data[i * 4] = 255; id.data[i * 4 + 1] = 255; id.data[i * 4 + 2] = 255; id.data[i * 4 + 3] = a;
    }
    this.maskCtx.putImageData(id, 0, 0);
    this.frameCtx.drawImage(this.pending, 0, 0); // atomic frame<->mask commit
    this.haveMask = true;
  }

  private inferMediapipe(seg: ImageSegmenter): Promise<void> {
    return new Promise((resolve) => {
      seg.segmentForVideo(this.smallIn, performance.now(), (res) => {
        try {
          const masks = res.confidenceMasks;
          const m = masks && masks[masks.length - 1]; // last = "person" confidence
          if (m) {
            const data = m.getAsFloat32Array();
            const mw = m.width, mh = m.height, count = mw * mh;
            if (!this.aBuf || this.aBuf.length !== count) {
              this.aBuf = new Float32Array(count); this.aTmp = new Float32Array(count); this.aOut = new Float32Array(count);
            }
            const span = RAMP_HI - RAMP_LO;
            for (let i = 0; i < count; i++) {
              let c = (data[i] - RAMP_LO) / span; // ramp: kills the halo without eating into the face
              c = c < 0 ? 0 : c > 1 ? 1 : c;
              this.aBuf[i] = c * c * (3 - 2 * c); // smoothstep
            }
            blurAlpha(this.aBuf, this.aTmp!, this.aOut!, mw, mh, MASK_BLUR); // soft edge
            if (this.mask.width !== mw) { this.mask.width = mw; this.mask.height = mh; }
            const id = this.maskCtx.createImageData(mw, mh);
            for (let i = 0; i < count; i++) {
              id.data[i * 4] = 255; id.data[i * 4 + 1] = 255; id.data[i * 4 + 2] = 255;
              id.data[i * 4 + 3] = (this.aOut![i] * 255) | 0;
            }
            this.maskCtx.putImageData(id, 0, 0);
            this.frameCtx.drawImage(this.pending, 0, 0); // atomic frame<->mask commit
            this.haveMask = true;
            m.close();
          }
        } finally { resolve(); }
      });
    });
  }

  // Render: composites the video cut out by the latest mask over the background. Capped at ~30fps —
  // capture is 30fps; rendering at 60 would just burn CPU/GPU needed elsewhere.
  private lastRender = 0;
  private renderLoop = () => {
    if (!this.running) return;
    const now = performance.now();
    if (now - this.lastRender >= 30) {
      this.lastRender = now;
      try {
        this.render();
        this.reqFrame?.(); // WebKit: forces capture of this frame for the encoder (see init)
      } catch { /* noop */ }
    }
    this.raf = requestAnimationFrame(this.renderLoop);
  };

  private render() {
    const w = this.out.width, h = this.out.height;
    if (this.video.readyState < 2) return;
    // No effect or before the 1st mask: live video (sharp, real time).
    if (this.mode.kind === "none" || !this.haveMask) { this.outCtx.drawImage(this.video, 0, 0, w, h); return; }

    // IMPORTANT: uses the SNAPSHOT that matches the mask (not the live video) → no ghosting.
    const src = this.frame;

    // person = frame cut out by the mask (soft edge via ramp/matte + bilinear upscale —
    // NO ctx.filter, which WKWebView silently ignores).
    this.personCtx.clearRect(0, 0, w, h);
    this.personCtx.drawImage(src, 0, 0, w, h);
    this.personCtx.globalCompositeOperation = "destination-in";
    this.personCtx.drawImage(this.mask, 0, 0, w, h);
    this.personCtx.globalCompositeOperation = "source-over";

    // background + person on top (background derived from the SAME frame → coherent)
    this.outCtx.clearRect(0, 0, w, h);
    if (this.mode.kind === "blur") {
      this.drawBlurred(src, w, h, this.mode.radius);
    } else if (this.mode.kind === "image" && this.bgImg) {
      drawCover(this.outCtx, this.bgImg, w, h);
    }
    this.outCtx.drawImage(this.person, 0, 0, w, h);
  }

  // Blur via progressive DOWNSCALE (½ per step) + bilinear upscale — a gaussian approximation
  // that works in every webview (ctx.filter blur doesn't exist in WKWebView).
  private drawBlurred(src: CanvasImageSource, w: number, h: number, radius: number) {
    const steps = radius >= 10 ? 4 : 3;
    let cur: CanvasImageSource = src;
    let cw = w, ch = h;
    let target = this.blurA;
    for (let i = 0; i < steps; i++) {
      const nw = Math.max(2, cw >> 1), nh = Math.max(2, ch >> 1);
      if (target.width !== nw || target.height !== nh) { target.width = nw; target.height = nh; }
      const tctx = target.getContext("2d")!;
      tctx.imageSmoothingEnabled = true;
      tctx.drawImage(cur, 0, 0, cw, ch, 0, 0, nw, nh);
      cur = target; cw = nw; ch = nh;
      target = target === this.blurA ? this.blurB : this.blurA;
    }
    this.outCtx.drawImage(cur, 0, 0, cw, ch, 0, 0, w, h);
  }
}

function drawCover(ctx: CanvasRenderingContext2D, img: HTMLImageElement, w: number, h: number) {
  const r = Math.max(w / img.width, h / img.height);
  const iw = img.width * r, ih = img.height * r;
  ctx.drawImage(img, (w - iw) / 2, (h - ih) / 2, iw, ih);
}
