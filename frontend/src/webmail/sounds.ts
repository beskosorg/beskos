// Alert sounds: notification arrival, and the three moments of a meeting.
//
// WHY SYNTHESIZED and not files: these are five alerts under half a second. As WAV they would be
// hundreds of KiB in the bundle and five network requests on the first play — the worst possible
// moment, because that's exactly when you want the sound NOW. Generated this way, they live in
// this file, play offline, and get tuned by changing a number instead of re-exporting audio.
//
// PER-DEVICE PREFERENCE (localStorage), like the notifications in desktopNotify.ts: turning the
// sound on on the laptop shouldn't turn it on on the phone. Same reasoning, same place.

export type SoundKind = "notify" | "email" | "meetJoin" | "meetKnock" | "meetPeer";

const LS = "mailin.sound.prefs";

export type SoundPrefs = {
  notify: boolean; // chat, task, file, calendar, and email alerts
  meet: boolean;   // joining the room, someone at the door, someone joined
};

const DEF: SoundPrefs = { notify: true, meet: true };

export function soundPrefs(): SoundPrefs {
  try {
    const raw = localStorage.getItem(LS);
    return raw ? { ...DEF, ...JSON.parse(raw) } : DEF;
  } catch { return DEF; }
}

export function setSoundPref(key: keyof SoundPrefs, on: boolean): void {
  try { localStorage.setItem(LS, JSON.stringify({ ...soundPrefs(), [key]: on })); }
  catch { /* private mode: only applies to this session */ }
}

// Which switch controls each sound.
function prefOf(kind: SoundKind): keyof SoundPrefs {
  return kind === "notify" || kind === "email" ? "notify" : "meet";
}

// --- synthesis ---------------------------------------------------------------

// Each alert is a sequence of notes: frequency (Hz), when it starts, and how long it lasts
// (seconds). The frequencies are real notes so the pairs sound in tune instead of just "two beeps".
type Note = { hz: number; at: number; dur: number; gain?: number };

const VOICES: Record<SoundKind, Note[]> = {
  // Generic alert: two rising tones (A5 -> C#6). Short and clear, it's the one that plays most.
  notify: [
    { hz: 880, at: 0, dur: 0.09 },
    { hz: 1108.73, at: 0.085, dur: 0.14 },
  ],
  // Email: a single tone, lower and softer. A lot of email arrives; a discreet alert keeps the
  // person from turning off the sound entirely out of fatigue.
  email: [{ hz: 587.33, at: 0, dur: 0.18, gain: 0.5 }],
  // Joined the room: three rising notes (C6-E6-G6), the chord that says "connected".
  meetJoin: [
    { hz: 1046.5, at: 0, dur: 0.08 },
    { hz: 1318.51, at: 0.075, dur: 0.08 },
    { hz: 1567.98, at: 0.15, dur: 0.16 },
  ],
  // Someone is asking to join: two low knocks, like knocking on a door. This is the only one the
  // host HAS to hear — if it's missed, someone is left waiting without knowing why. That's why it's
  // the most insistent: low-pitched (cuts through a speaking voice better) and with two knocks
  // instead of one.
  meetKnock: [
    { hz: 392, at: 0, dur: 0.11, gain: 1.1 },
    { hz: 392, at: 0.17, dur: 0.13, gain: 1.1 },
  ],
  // Someone joined: a single, discreet note. In a ten-person meeting, this plays ten times.
  meetPeer: [{ hz: 783.99, at: 0, dur: 0.1, gain: 0.45 }],
};

let ctx: AudioContext | null = null;

function audioContext(): AudioContext | null {
  if (ctx) return ctx;
  try {
    const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctor) return null;
    ctx = new Ctor();
    return ctx;
  } catch { return null; }
}

// The browser creates the AudioContext SUSPENDED until there is a user gesture — autoplay policy.
// Without this the first alert would be swallowed in silence and only the second would be heard.
// We unlock it on the first click/keypress/tap in the app, and the listener removes itself.
export function unlockSounds(): void {
  const unlock = () => {
    const c = audioContext();
    if (c && c.state === "suspended") void c.resume().catch(() => {});
  };
  const opts = { once: true, passive: true } as const;
  window.addEventListener("pointerdown", unlock, opts);
  window.addEventListener("keydown", unlock, opts);
  window.addEventListener("touchstart", unlock, opts);
}

// Plays an alert. Never throws: a sound that fails must not break what triggered it.
export function playSound(kind: SoundKind): void {
  if (!soundPrefs()[prefOf(kind)]) return;
  const c = audioContext();
  if (!c) return;
  // DO NOT SCHEDULE WHILE SUSPENDED. This is the whole point of the function.
  //
  // The browser suspends the AudioContext of a backgrounded page, and every context is born
  // suspended before the first gesture. While suspended, currentTime DOES NOT ADVANCE. Calling
  // resume() -- which is asynchronous -- and scheduling right after marks the envelope at
  // "now + 10ms" by a stopped clock; by the time the context wakes, seconds later, that instant is
  // in the past. The oscillator still starts, but the gain has already run its attack and decay
  // back there: nothing is heard.
  //
  // This is what made the "someone joined" alert silent whenever the Meet window was not focused --
  // and it is general, not a Meet problem: it applies to any alert while the page is in the
  // background, which is exactly when an alert is worth having.
  if (c.state !== "running") {
    void c.resume().then(() => emit(c, kind)).catch(() => {});
    return;
  }
  emit(c, kind);
}

// emit writes the notes into the context. Only ever called with the context running.
function emit(c: AudioContext, kind: SoundKind): void {
  const t0 = c.currentTime + 0.01;
  for (const n of VOICES[kind]) {
    try {
      const osc = c.createOscillator();
      const g = c.createGain();
      // Sine wave: no harsh harmonics. A square wave would get grating by the end of the day.
      osc.type = "sine";
      osc.frequency.value = n.hz;
      // Envelope with attack and decay: gain switched on/off abruptly gives an audible "click",
      // which is the sound of a speaker popping, not of an alert.
      const startAt = t0 + n.at;
      const peak = 0.16 * (n.gain ?? 1);
      g.gain.setValueAtTime(0.0001, startAt);
      g.gain.exponentialRampToValueAtTime(peak, startAt + 0.012);
      g.gain.exponentialRampToValueAtTime(0.0001, startAt + n.dur);
      osc.connect(g).connect(c.destination);
      osc.start(startAt);
      osc.stop(startAt + n.dur + 0.02);
    } catch { /* a failed alert must not break whoever triggered it */ }
  }
}
