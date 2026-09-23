import { describe, it, expect } from "vitest";
import { measure } from "./passwordStrength";

// The server (Stalwart's zxcvbn) is the authority; this meter exists to warn BEFORE and is
// intentionally stricter. The cases below are the ones we caught in production or that the user
// naturally tries.
describe("password strength meter", () => {
  it("rejects what Stalwart rejected (repetitions)", () => {
    expect(measure("abcabcabcabc").ok).toBe(false);
    expect(measure("noreply2026noreply2026").ok).toBe(false);
  });

  it("rejects short, common, and sequential passwords", () => {
    expect(measure("Ab1!xY").ok).toBe(false);            // short
    expect(measure("password12345").ok).toBe(false);     // common
    expect(measure("qwertyuiop123").ok).toBe(false);     // keyboard sequence
    expect(measure("abcdefghijkl").ok).toBe(false);      // alphabet sequence
  });

  it("rejects passwords made from the user's own address", () => {
    expect(measure("dfernandes2026!", ["dfernandes@beskos.com"]).ok).toBe(false);
    expect(measure("beskos-forte-2026", ["ana@beskos.com"]).ok).toBe(false);
  });

  it("accepts long passphrases and varied passwords", () => {
    expect(measure("cavalo bateria grampo correto").ok).toBe(true);
    expect(measure("Tr0vo4da-Lilas!Verde").ok).toBe(true);
    expect(measure("xJ9#pQ2vLm74Kd", ["ana@acme.pt"]).ok).toBe(true);
  });

  it("empty shows nothing (hasn't typed yet)", () => {
    const s = measure("");
    expect(s.score).toBe(0);
    expect(s.hint).toBe("");
  });
});
