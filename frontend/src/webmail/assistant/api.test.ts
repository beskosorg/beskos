import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { assistantApi, type ChatEvent } from "./api";
import i18n from "../../i18n";

// Returns a fake Response whose body delivers the chunks exactly as given — including chunks
// that cut an event in half, which is what the network actually does.
function fakeStream(chunks: string[]): Response {
  const enc = new TextEncoder();
  let i = 0;
  const body = new ReadableStream<Uint8Array>({
    pull(ctrl) {
      if (i >= chunks.length) { ctrl.close(); return; }
      ctrl.enqueue(enc.encode(chunks[i++]));
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

// LanguageDetector picks up whatever locale the machine running the tests reports (this CI
// runner resolves to "en", a contributor's laptop might resolve to "pt-BR") -- a test asserting
// translated TEXT has to pin the language itself, or it passes and fails depending on who runs
// it (law 4.6: the frontend translates the error_code; which language it lands in is not this
// test's business).
beforeEach(async () => { await i18n.changeLanguage("pt-BR"); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("assistantApi.chat", () => {
  // An event split across two chunks HAS to wait for the rest. Processing half an event would
  // produce truncated JSON on every read, and the response would show up with gaps.
  it("joins events split across network chunks", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      'data: {"kind":"conversa',
      'tion","conversation_id":"c1","title":"hello","agent":"assistant"}\n\n',
      'data: {"kind":"delta","text":"Hi"}\n\ndata: {"kind":"delta","text":" there"}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "hello" }, (ev) => got.push(ev));

    expect(got.map((e) => e.kind)).toEqual(["conversation", "delta", "delta"]);
    expect(got[0]).toMatchObject({ conversation_id: "c1" });
    expect(got.filter((e) => e.kind === "delta").map((e) => (e as { text: string }).text).join(""))
      .toBe("Hi there");
  });

  // A broken event can't throw out the whole thread: losing everything over one bad line would be worse.
  it("ignores an unreadable event and continues", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      "data: {this is not json}\n\n",
      'data: {"kind":"delta","text":"ok"}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "hello" }, (ev) => got.push(ev));
    expect(got).toEqual([{ kind: "delta", text: "ok" }]);
  });

  // An action waiting for approval arrives as its own event, with the EXACT arguments that will
  // run — that's what the panel shows to whoever decides (§6.3).
  it("delivers the confirmation request with the exact arguments", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      'data: {"kind":"confirm","confirm":{"id":"a1","tool":"mail.send","args":{"to":["ana@exemplo.pt"],"text":"Here is the invoice."}}}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "send" }, (ev) => got.push(ev));

    expect(got).toHaveLength(1);
    const ev = got[0] as { kind: string; confirm: { id: string; args: Record<string, unknown> } };
    expect(ev.kind).toBe("confirm");
    expect(ev.confirm.id).toBe("a1");
    expect(ev.confirm.args).toEqual({ to: ["ana@exemplo.pt"], text: "Here is the invoice." });
  });

  // Before the thread opens, errors are plain HTTP, and error_code is what the frontend
  // translates (law 4.4/4.6) — raw server text is never shown.
  it("error before the thread opens rejects with the translated message", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ error_code: "assistant.rate_limited" }),
      { status: 429, headers: { "Content-Type": "application/json" } },
    )));

    // Language is pinned to pt-BR in beforeEach, and this asserts the frontend actually
    // translated error_code (law 4.4/4.6) -- the regex matches the pt-BR string on purpose.
    await expect(assistantApi.chat({ message: "hello" }, () => {})).rejects.toThrow(/assistente/i);
  });
});

describe("escolha de agente", () => {
  // The agent pinned by the user travels in the request: a manual choice outranks automatic
  // routing (§3.2). With no agent in the body, the server decides.
  it("sends the pinned agent, and omits it when automatic", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("fetch", vi.fn(async (_url: string, init: RequestInit) => {
      bodies.push(JSON.parse(String(init.body)));
      return fakeStream(['data: {"kind":"delta","text":"ok"}\n\n']);
    }));

    await assistantApi.chat({ message: "hello", agent: "inbox" }, () => {});
    await assistantApi.chat({ message: "hello" }, () => {});

    expect(bodies[0]).toMatchObject({ agent: "inbox" });
    expect(bodies[1]).not.toHaveProperty("agent");
  });

  // Who answered arrives in the first event, before any tool runs — the choice is always
  // visible, never silent.
  it("announces the chosen agent before anything else", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      'data: {"kind":"conversation","conversation_id":"c1","title":"t","agent":"inbox","pinned_agent":""}\n\n',
      'data: {"kind":"step","step":{"tool":"mail.search","args":{},"summary":"3 messages"}}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "summarize the emails" }, (ev) => got.push(ev));

    expect(got[0].kind).toBe("conversation");
    expect((got[0] as { agent: string }).agent).toBe("inbox");
  });
});

describe("assistantApi.confirm", () => {
  // One decision per request, with the action id in the path: there's no endpoint that approves
  // a batch (§6.3). The body carries the explicit decision — approval is never assumed.
  it("sends one decision at a time, with the explicit decision", async () => {
    const calls: Array<[string, RequestInit]> = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string, init: RequestInit) => {
      calls.push([url, init]);
      return new Response(JSON.stringify({ status: "ok", approved: true }), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    }));

    await assistantApi.confirm("a1", true);
    await assistantApi.confirm("a2", false);

    expect(calls).toHaveLength(2);
    expect(calls[0][0]).toContain("/api/assistant/confirm/a1");
    expect(JSON.parse(String(calls[0][1].body))).toEqual({ approve: true });
    expect(JSON.parse(String(calls[1][1].body))).toEqual({ approve: false });
  });
});
