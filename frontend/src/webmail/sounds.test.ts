import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";

// A sound that doesn't play is an invisible defect: nobody files a bug for "I didn't hear
// anything," they assume they muted it by accident. That's why what's tested here is what decides
// whether it plays or not.
//
// Each test reimports the module (vi.resetModules) because it caches the AudioContext — a single
// context per page, which is correct in production but would make a test inherit the fake audio
// from another.

// The vitest environment here is node, with no DOM. We simulate the minimum instead of pulling in
// jsdom just for this: it's four localStorage methods and an addEventListener, versus a whole
// dependency for the same effect (law 2.5).
function memoryStorage(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => { m.set(k, String(v)); },
    removeItem: (k: string) => { m.delete(k); },
    clear: () => m.clear(),
  } as unknown as Storage;
}

// Fake AudioContext that records when each oscillator starts — that's what proves a sound was made.
function stage() {
  const started: number[] = [];
  const wire = { connect: (n: unknown) => n };
  const ctx = {
    state: "running",
    currentTime: 0,
    resume: () => Promise.resolve(),
    createOscillator: () => ({
      type: "", frequency: { value: 0 }, connect: wire.connect,
      start: (t: number) => started.push(t), stop: () => {},
    }),
    createGain: () => ({
      gain: { setValueAtTime: () => {}, exponentialRampToValueAtTime: () => {} },
      connect: wire.connect,
    }),
    destination: {},
  };
  return { started, AudioContext: function () { return ctx; } as unknown as typeof AudioContext };
}

// Sets up the environment and returns the fresh module. `withAudio: false` simulates an
// environment without Web Audio.
async function setup(withAudio = true) {
  const p = stage();
  const listeners: string[] = [];
  vi.stubGlobal("localStorage", memoryStorage());
  vi.stubGlobal("window", {
    AudioContext: withAudio ? p.AudioContext : undefined,
    addEventListener: (n: string, _f: unknown, o?: { once?: boolean }) => { listeners.push(n); if (o) expect(o.once).toBe(true); },
  });
  vi.resetModules();
  const mod = await import("./sounds");
  return { ...mod, started: p.started, listeners };
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("preferences", () => {
  it("are on by default — a product that arrives mute looks broken", async () => {
    const { soundPrefs } = await setup();
    expect(soundPrefs()).toEqual({ notify: true, meet: true });
  });

  it("save and read back", async () => {
    const { soundPrefs, setSoundPref } = await setup();
    setSoundPref("notify", false);
    expect(soundPrefs()).toEqual({ notify: false, meet: true });
  });

  it("turning one off does not turn the other off — that is why there are two switches", async () => {
    const { soundPrefs, setSoundPref } = await setup();
    setSoundPref("notify", false);
    expect(soundPrefs().meet).toBe(true);
  });

  it("corrupted localStorage falls back to the default instead of throwing", async () => {
    const { soundPrefs } = await setup();
    localStorage.setItem("mailin.sound.prefs", "{this is not json");
    expect(soundPrefs()).toEqual({ notify: true, meet: true });
  });
});

describe("playSound", () => {
  it("plays when the matching switch is on", async () => {
    const { playSound, started } = await setup();
    playSound("notify");
    expect(started.length).toBeGreaterThan(0);
  });

  it("does not play with the switch off", async () => {
    const { playSound, setSoundPref, started } = await setup();
    setSoundPref("notify", false);
    playSound("notify");
    expect(started).toHaveLength(0);
  });

  // Mail is governed by the notify switch, not a third one of its own. If this were to drift
  // apart by accident, turning notifications off would leave mail still sounding -- the opposite
  // of what was asked.
  it("mail obeys the notify switch", async () => {
    const { playSound, setSoundPref, started } = await setup();
    setSoundPref("notify", false);
    playSound("email");
    expect(started).toHaveLength(0);
  });

  it("Meet sounds obey the Meet switch, not the notify one", async () => {
    const { playSound, setSoundPref, started } = await setup();
    setSoundPref("notify", false);
    playSound("meetKnock");
    expect(started.length).toBeGreaterThan(0);
    setSoundPref("meet", false);
    started.length = 0;
    playSound("meetKnock");
    expect(started).toHaveLength(0);
  });

  // The knock is the one sound the host HAS to hear: if it slips past them, someone is left
  // waiting without knowing why. Two beats set it apart from any ordinary chime.
  it("the knock request has two beats", async () => {
    const { playSound, started } = await setup();
    playSound("meetKnock");
    expect(started).toHaveLength(2);
    expect(started[1]).toBeGreaterThan(started[0]);
  });

  it("with no Web Audio in the environment, it does not throw — a failed chime can't take down whoever triggered it", async () => {
    const { playSound } = await setup(false);
    expect(() => playSound("notify")).not.toThrow();
  });
});

describe("unlockSounds", () => {
  it("waits for the gesture on all three input types, once each", async () => {
    const { unlockSounds, listeners } = await setup();
    unlockSounds();
    expect(listeners).toEqual(["pointerdown", "keydown", "touchstart"]);
  });
});

// A suspended context is the normal state of a page the browser has put in the background, and it
// is also where every context starts before the first gesture. currentTime does not advance while
// suspended, so scheduling "now + 10ms" and calling resume() in the same breath puts the whole gain
// envelope in the past: the oscillator starts, the envelope has already finished, nothing is heard.
// This is what made a Meet join sound silent while the window was not focused.
describe("a suspended context", () => {
  it("schedules only after the context is running again", async () => {
    const started: number[] = [];
    const gains: number[] = [];
    let state = "suspended";
    let now = 5;
    const wire = { connect: (n: unknown) => n };
    const ctx = {
      get state() { return state; },
      get currentTime() { return now; },
      // Real resume() is ASYNC: the state and the clock only move when the promise settles. A fake
      // that mutates synchronously hides exactly the bug this test exists for.
      resume: () => Promise.resolve().then(() => { state = "running"; now = 9; }),
      createOscillator: () => ({
        type: "", frequency: { value: 0 }, connect: wire.connect,
        start: (t: number) => started.push(t), stop: () => {},
      }),
      createGain: () => ({
        gain: { setValueAtTime: (_v: number, t: number) => gains.push(t), exponentialRampToValueAtTime: () => {} },
        connect: wire.connect,
      }),
      destination: {},
    };
    vi.stubGlobal("localStorage", memoryStorageForTest());
    vi.stubGlobal("window", { AudioContext: function () { return ctx; } as unknown as typeof AudioContext, addEventListener: () => {} });
    vi.resetModules();
    const { playSound } = await import("./sounds");

    playSound("meetPeer");
    await Promise.resolve();
    await Promise.resolve();

    expect(started.length).toBeGreaterThan(0);
    // The envelope must start at or after the time the context reports once awake -- never before.
    expect(Math.min(...gains)).toBeGreaterThanOrEqual(9);
    expect(Math.min(...started)).toBeGreaterThanOrEqual(9);
  });
});

function memoryStorageForTest(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => { m.set(k, String(v)); },
    removeItem: (k: string) => { m.delete(k); },
    clear: () => m.clear(),
  } as unknown as Storage;
}
