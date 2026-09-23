import { describe, it, expect } from "vitest";
import { URL_RE, URL_TOKEN, splitTrailing, href } from "./links";

// A link detector fails in two directions and only one of them gets reported. Missing a link is
// visible -- someone pastes an address and it stays grey. Inventing one is not: it quietly turns a
// filename in a sentence into something clickable, and nobody files that as a bug. Both are here.

const all = (s: string) => [...s.matchAll(new RegExp(URL_TOKEN, "gi"))].map((m) => m[0]);

describe("finds real links", () => {
  it("a bare host with a path -- the way people actually paste", () => {
    expect(all("meet.google.com/naf-xpvx-wah")).toEqual(["meet.google.com/naf-xpvx-wah"]);
  });

  it("with a scheme, taken whole and not split at the host", () => {
    expect(all("https://meet.google.com/naf?x=1#f")).toEqual(["https://meet.google.com/naf?x=1#f"]);
  });

  it("www with no scheme", () => {
    expect(all("www.beskos.com")).toEqual(["www.beskos.com"]);
  });

  it("inside a sentence", () => {
    expect(all("open beskos.com/app now")).toEqual(["beskos.com/app"]);
  });
});

describe("leaves prose alone", () => {
  // Each of these appears in ordinary chat. Linking any of them is worse than linking none.
  it.each([
    ["a filename", "check report.txt again"],
    ["a source file", "package.json changed"],
    ["a version number", "release v1.2.3 shipped"],
    ["an e-mail address", "write to ana@beskos.com"],
    ["a sentence with a dot", "done. next one"],
  ])("%s", (_label, text) => {
    expect(all(text)).toEqual([]);
  });
});

describe("trailing punctuation belongs to the sentence", () => {
  it("a full stop is not part of the host", () => {
    const [url, tail] = splitTrailing(all("see beskos.com.")[0] ?? "");
    expect(url).toBe("beskos.com");
    expect(tail).toBe("");
  });

  it("a full stop after a path is trimmed and handed back", () => {
    expect(splitTrailing("https://x.com/a.")).toEqual(["https://x.com/a", "."]);
  });

  // Wikipedia-style addresses carry their own brackets; cutting them breaks the link.
  it("a closing bracket that the URL itself opened is kept", () => {
    expect(splitTrailing("https://x.com/a_(b)")).toEqual(["https://x.com/a_(b)", ""]);
  });
});

describe("href", () => {
  // Without this an <a> reads a bare host as a RELATIVE path: the link would stay inside Beskos and
  // land on a 404 instead of leaving for the site.
  it("adds a scheme to a bare host", () => {
    expect(href("beskos.com/a")).toBe("https://beskos.com/a");
  });

  it("leaves an explicit scheme alone, including http", () => {
    expect(href("http://x.io")).toBe("http://x.io");
    expect(href("https://x.io")).toBe("https://x.io");
  });
});

describe("URL_RE", () => {
  it("finds the first link in a message, for the preview card", () => {
    expect("hi meet.google.com/x and beskos.com".match(URL_RE)?.[0]).toBe("meet.google.com/x");
  });
});
