// Email rules: visual model ⇄ Sieve script.
//
// Sieve is the single source of truth: the script stored in Stalwart carries its own definition in
// a `# mailin:rules {...}` comment. No table of our own, no sync, no drift — and it works from any
// device, even after a restore of our database.
//
// DECLARED LIMIT: Sieve runs at DELIVERY. Outbound rules ("when I send") are not Sieve; they are
// applied by the webmail at send time, and therefore do NOT apply when sending from another
// client.

export type Trigger = "receive" | "send";
export type Field = "from" | "to" | "subject" | "body" | "cc";
export type Op = "contains" | "is" | "starts" | "ends" | "domain";
export type ActionKind = "move" | "flag" | "read" | "reply" | "forward" | "delete" | "keyword" | "stop";

export interface Condition { field: Field; op: Op; value: string }
export interface Action { kind: ActionKind; value?: string; extra?: string }

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  match: "all" | "any";   // AND / OR between the conditions
  conditions: Condition[];
  actions: Action[];
}

// Vacation reply: auto-response with a date window and scope. Lives in the SAME Sieve script as
// the rules (its own comment), to coexist with them without a second source of truth.
export interface VacationConfig {
  enabled: boolean;
  subject: string;
  message: string;
  start?: string;          // YYYY-MM-DD (optional)
  end?: string;            // YYYY-MM-DD (optional)
  scope: "all" | "internal"; // internal = only replies to senders from the user's own domain
  domain?: string;         // the user's domain (for the internal scope)
}

export const emptyVacation = (): VacationConfig => ({ enabled: false, subject: "", message: "", scope: "all" });

export const newRule = (): Rule => ({
  id: Math.random().toString(36).slice(2, 10),
  name: "",
  enabled: true,
  trigger: "receive",
  match: "all",
  conditions: [{ field: "from", op: "contains", value: "" }],
  actions: [{ kind: "move", value: "" }],
});

// --- Sieve: escaping strings -------------------------------------------------

// In a Sieve string only \ and " are special. Without this, a subject with quotes would break the
// entire script — and the user would see a Stalwart error instead of their own typing mistake.
const q = (s: string) => `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;

const HEADER: Record<Field, string> = { from: "from", to: "to", cc: "cc", subject: "subject", body: "" };

// testFor translates a condition. `address :domain` compares only the address's domain — that's
// what makes "from anyone at acme.com" work without depending on the name's text.
function testFor(c: Condition): string {
  const v = q(c.value);
  if (c.field === "body") return `body :text :contains ${v}`;
  const h = q(HEADER[c.field]);
  switch (c.op) {
    case "domain": return `address :domain :is ${h} ${v}`;
    case "is": return `header :is ${h} ${v}`;
    case "starts": return `header :matches ${h} ${q(escapeGlob(c.value) + "*")}`;
    case "ends": return `header :matches ${h} ${q("*" + escapeGlob(c.value))}`;
    default: return `header :contains ${h} ${v}`;
  }
}

// In :matches, * and ? are wildcards. Whoever writes "50% *promo*" wants the literal.
const escapeGlob = (s: string) => s.replace(/([*?\\])/g, "\\$1");

// requiresFor: only the extensions actually used. Requesting everything would make Stalwart reject
// the script if any extension weren't compiled into it.
function requiresFor(rules: Rule[]): string[] {
  const need = new Set<string>();
  for (const r of rules) {
    if (r.conditions.some((c) => c.field === "body")) need.add("body");
    for (const a of r.actions) {
      if (a.kind === "move") need.add("fileinto");
      if (a.kind === "flag" || a.kind === "read" || a.kind === "keyword") need.add("imap4flags");
      if (a.kind === "reply") need.add("vacation");
    }
  }
  return [...need].sort();
}

// actionsFor: the ORDER is not cosmetic. Flags (addflag/setflag) must come BEFORE fileinto — after
// filing, the message is already gone and the flag doesn't stick. Verified against Stalwart
// 0.16.4.
function actionsFor(actions: Action[]): string[] {
  const out: string[] = [];
  const flags = actions.filter((a) => a.kind === "flag" || a.kind === "read" || a.kind === "keyword");
  const rest = actions.filter((a) => !flags.includes(a));

  for (const a of flags) {
    if (a.kind === "flag") out.push(`addflag "\\\\Flagged";`);
    if (a.kind === "read") out.push(`addflag "\\\\Seen";`);
    if (a.kind === "keyword" && a.value) out.push(`addflag ${q(a.value)};`);
  }
  for (const a of rest) {
    switch (a.kind) {
      case "move": if (a.value) out.push(`fileinto ${q(a.value)};`); break;
      case "forward": if (a.value) out.push(`redirect ${q(a.value)};`); break;
      case "reply":
        // :days 1 avoids replying to the same person in a loop on the same day — an auto-response
        // without this is how a storm of emails between two autoresponders gets created.
        out.push(`vacation :days 1 ${a.extra ? `:subject ${q(a.extra)} ` : ""}${q(a.value ?? "")};`);
        break;
      case "delete": out.push("discard;"); break;
      case "stop": out.push("stop;"); break;
    }
  }
  return out;
}

// compile: visual flow → Sieve script. The JSON goes in a comment so we can reopen the editor.
export function compile(rules: Rule[], vacation?: VacationConfig): string {
  const inbound = rules.filter((r) => r.trigger === "receive" && r.enabled);
  const req = new Set(requiresFor(inbound));
  if (vacation?.enabled) {
    req.add("vacation");
    if (vacation.start || vacation.end) { req.add("date"); req.add("relational"); }
  }
  const lines: string[] = [
    "# Generated by Beskos — do not edit by hand: the editor rewrites this file.",
    `# mailin:rules ${JSON.stringify(rules)}`,
  ];
  if (vacation) lines.push(`# mailin:vacation ${JSON.stringify(vacation)}`);
  lines.push("");
  if (req.size) lines.push(`require [${[...req].sort().map(q).join(", ")}];`, "");

  // Vacation first: replies regardless of what the rules do with the message.
  if (vacation?.enabled) {
    const guards: string[] = [];
    if (vacation.start) guards.push(`currentdate :value "ge" "date" ${q(vacation.start)}`);
    if (vacation.end) guards.push(`currentdate :value "le" "date" ${q(vacation.end)}`);
    if (vacation.scope === "internal" && vacation.domain) guards.push(`address :domain :is "from" ${q(vacation.domain)}`);
    const vac = `vacation :days 1 ${vacation.subject ? `:subject ${q(vacation.subject)} ` : ""}${q(vacation.message)};`;
    lines.push("# Vacation reply (Beskos)");
    if (guards.length) {
      const cond = guards.length === 1 ? guards[0] : `allof(${guards.join(", ")})`;
      lines.push(`if ${cond} {`, "  " + vac, "}", "");
    } else {
      lines.push(vac, "");
    }
  }

  for (const r of inbound) {
    const tests = r.conditions.filter((c) => c.value.trim()).map(testFor);
    lines.push(`# ${r.name || "(unnamed)"}`);
    if (tests.length === 0) {
      // With no condition, the rule applies to everything — say so in the script, don't hide it.
      lines.push("# (no conditions: applies to all messages)");
      lines.push(...actionsFor(r.actions).map((a) => a));
    } else {
      const join = r.match === "any" ? "anyof" : "allof";
      const cond = tests.length === 1 ? tests[0] : `${join}(${tests.join(", ")})`;
      lines.push(`if ${cond} {`);
      lines.push(...actionsFor(r.actions).map((a) => "  " + a));
      lines.push("}");
    }
    lines.push("");
  }
  return lines.join("\n");
}

// parse: reads the JSON back from the comment. If the script exists without our marker (someone
// wrote it by hand), returns null — better to say "I can't read this" than to erase the person's
// work.
export function parse(script: string): Rule[] | null {
  const m = script.match(/^# mailin:rules (.+)$/m);
  if (!m) return null;
  try {
    const parsed = JSON.parse(m[1]);
    return Array.isArray(parsed) ? (parsed as Rule[]) : null;
  } catch {
    return null;
  }
}

// parseVacation: reads the vacation config from the same script (its own comment). Absent → null.
export function parseVacation(script: string): VacationConfig | null {
  const m = script.match(/^# mailin:vacation (.+)$/m);
  if (!m) return null;
  try {
    return JSON.parse(m[1]) as VacationConfig;
  } catch {
    return null;
  }
}

// --- Outbound rules: applied by the webmail at send time --------------------

export interface Draft { to?: string; cc?: string; subject?: string; body?: string }

// matchesOutbound tests a draft against an outbound rule, with the same semantics as Sieve.
export function matchesOutbound(r: Rule, d: Draft): boolean {
  const val = (f: Field): string =>
    f === "to" ? d.to ?? "" : f === "cc" ? d.cc ?? "" : f === "subject" ? d.subject ?? "" : f === "body" ? d.body ?? "" : "";
  const one = (c: Condition) => {
    const v = val(c.field).toLowerCase();
    const n = c.value.trim().toLowerCase();
    if (!n) return false;
    switch (c.op) {
      case "is": return v === n;
      case "starts": return v.startsWith(n);
      case "ends": return v.endsWith(n);
      case "domain": return v.split("@").pop()?.trim() === n.replace(/^@/, "");
      default: return v.includes(n);
    }
  };
  const tests = r.conditions.filter((c) => c.value.trim());
  if (tests.length === 0) return true;
  return r.match === "any" ? tests.some(one) : tests.every(one);
}

// outboundBcc evaluates a draft's OUTBOUND rules at send time and returns the addresses to include
// in Bcc (the "forward" action of matching rules). Honors "stop". This is what the webmail can
// apply at send time (the other actions only apply at delivery, via Sieve). Before this, no
// outbound rule was ever evaluated.
export function outboundBcc(rules: Rule[], d: Draft): string[] {
  const bcc: string[] = [];
  for (const r of rules) {
    if (r.trigger !== "send" || !r.enabled) continue;
    if (!matchesOutbound(r, d)) continue;
    for (const a of r.actions) {
      if (a.kind === "forward" && a.value?.trim()) bcc.push(a.value.trim());
      if (a.kind === "stop") return bcc;
    }
  }
  return bcc;
}
