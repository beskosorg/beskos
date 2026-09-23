import { describe, it, expect } from "vitest";
import { JmapClient } from "./client";
import { MailService } from "./mail";

// Integration test against a test Stalwart. Run with:
//   STALWART_TEST_URL=http://localhost:18091 STALWART_TEST_EMAIL=bob@wmtest.com \
//   STALWART_TEST_PWD=pw-bob-123 npm test
// Without the envs, it's skipped. (There's no CORS in node, so we talk to Stalwart directly.)
const url = process.env.STALWART_TEST_URL;
const email = process.env.STALWART_TEST_EMAIL ?? "";
const pwd = process.env.STALWART_TEST_PWD ?? "";

describe.skipIf(!url)("MailService (integration)", () => {
  const client = new JmapClient(url, { basic: JmapClient.encodeAuth(email, pwd) });

  it("reads session, lists folders, sends and receives", async () => {
    const session = await client.session();
    expect(session.accountId).toBeTruthy();

    const svc = new MailService(client, session.accountId);
    const boxes = await svc.mailboxes();
    const inbox = boxes.find((b) => b.role === "inbox");
    expect(inbox, "an Inbox should exist").toBeTruthy();

    const subject = "Webmail test " + Date.now();
    await svc.send({
      from: { email, name: "Test" },
      to: [{ email }],
      subject,
      textBody: "Webmail test body.\n",
    });

    // The email arrives asynchronously; do a short poll on the Inbox.
    let found: string | undefined;
    for (let i = 0; i < 10 && !found; i++) {
      const page = await svc.listEmails(inbox!.id, 0, 20);
      found = page.items.find((e) => e.subject === subject)?.id;
      if (!found) await new Promise((r) => setTimeout(r, 1000));
    }
    expect(found, "the sent email should appear in the Inbox").toBeTruthy();

    const full = await svc.getEmail(found!);
    const partId = full.textBody[0]?.partId;
    expect(full.bodyValues[partId]?.value).toContain("test body");

    // Full-text search finds the email.
    const hits = await svc.search(subject);
    expect(hits.items.some((e) => e.id === found)).toBe(true);
  }, 30000);
});
