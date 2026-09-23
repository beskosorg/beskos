// Email rules = a Sieve script on Stalwart, managed via the user's OWN session (JMAP).
//
// Why here and not in the Go backend: an account's Sieve script can only be written with that
// account's own credential, and the backend never holds it -- no password is stored; the webmail
// keeps Basic auth in memory only. It is the same path as the rest of the webmail -- direct JMAP.
//
// Rules run AT DELIVERY TIME, on the server: they apply with the browser closed, on the phone, in
// every client.
import type { JmapClient } from "./client";

export const SIEVE_CAP = "urn:ietf:params:jmap:sieve";
export const SCRIPT_NAME = "mailin-regras";

export interface SieveScript { id: string; name: string; isActive: boolean; blobId?: string }

export class SieveApi {
  constructor(private c: JmapClient, private accountId: string) {}

  // list returns the account's scripts. We only touch our own (SCRIPT_NAME): if someone created
  // another one outside of us, it's not ours to touch.
  async list(): Promise<SieveScript[]> {
    const r = await this.c.request([SIEVE_CAP], [["SieveScript/get", { accountId: this.accountId }, "g"]]);
    return ((r[0] as { list?: SieveScript[] }).list ?? []);
  }

  // download fetches the script's content by blobId (that's where the rules JSON lives, in the comment).
  async download(blobId: string): Promise<string> {
    const blob = await this.c.downloadBlob(this.accountId, blobId, "rules.sieve", "application/sieve");
    return blob.text();
  }

  // save uploads the script and activates it in a single request: if activation failed after a
  // separate upload, a script would be left saved but inert -- worse than failing outright.
  async save(content: string): Promise<void> {
    const existing = (await this.list()).find((s) => s.name === SCRIPT_NAME);
    const b64 = b64encode(content);
    const create: Record<string, unknown> = {};
    const update: Record<string, unknown> = {};
    if (existing) update[existing.id] = { blobId: "#s1" };
    else create["r1"] = { name: SCRIPT_NAME, blobId: "#s1" };

    const r = await this.c.request([SIEVE_CAP, "urn:ietf:params:jmap:blob"], [
      ["Blob/upload", {
        accountId: this.accountId,
        create: { s1: { data: [{ "data:asBase64": b64 }], type: "application/sieve" } },
      }, "u"],
      ["SieveScript/set", {
        accountId: this.accountId,
        ...(existing ? { update } : { create }),
        onSuccessActivateScript: existing ? existing.id : "#r1",
      }, "s"],
    ]);

    const res = r[1] as { notCreated?: Record<string, { description?: string }>; notUpdated?: Record<string, { description?: string }> };
    const err = Object.values(res.notCreated ?? {})[0] ?? Object.values(res.notUpdated ?? {})[0];
    if (err) throw new Error(err.description ?? "sieve");
  }

  // deactivate turns it off without deleting it: whoever turns off the rules usually wants to turn them back on.
  async deactivate(): Promise<void> {
    await this.c.request([SIEVE_CAP],
      [["SieveScript/set", { accountId: this.accountId, onSuccessActivateScript: null }, "s"]]);
  }
}

function b64encode(s: string): string {
  // btoa doesn't accept non-ASCII characters; TextEncoder handles it (accented text would break here).
  const bytes = new TextEncoder().encode(s);
  let bin = "";
  bytes.forEach((b) => { bin += String.fromCharCode(b); });
  return btoa(bin);
}
