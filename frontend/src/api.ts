import { apiUrl, authHeaders, notifyUnauthorized } from "./apiBase";
// Client for OUR Go API (control plane). The webmail talks JMAP directly to Stalwart; this file
// is only for panel/admin/billing/storage. No Stalwart admin credential ever passes through here.
// Session via HttpOnly cookie (same-origin).

// Capabilities this instance has unlocked -- Community (all false) or a Pro license key.
// Gates Pro-only UI; unauthenticated on purpose (see GET /api/edition on the backend).
export interface Edition {
  edition: "community" | "pro";
  org?: string;
  seats?: number;
  expires?: string;
  recording: boolean;
  branding: boolean;
  audit_log: boolean;
  org_policies: boolean;
  agents: boolean;
  // Build metadata (internal/version, stamped by GoReleaser's ldflags): "dev" outside a release.
  version: string;
  commit: string;
  build_date: string;
}

export interface Me {
  id: string;
  email: string;
  role: string;
  tenant_id: { Bytes?: string; Valid?: boolean } | string;
  mfa_setup_required?: boolean; // the org's policy requires 2FA and the user hasn't set it up yet
  org_mfa_required?: boolean; // the organization's 2FA policy is turned on
  mfa_required_method?: string; // any | totp | sms
  language: string; // "" = automatic (browser); otherwise the preference saved on the account
  permissions: string[]; // UI gates (see internal/rbac)
  mfa: { totp: boolean; sms: boolean };
  disabled_apps?: string[]; // apps the admin turned off for the org — the rail hides them
  assistant_enabled?: boolean; // false if the admin turned off the assistant (or AI isn't configured) — the bubble disappears
  // The organisation's billing state and whether its own domain is live. Both ride on /api/me so
  // the warning bar is on screen at the first render -- see AccountBanner.
  account?: { state: "ok" | "ending" | "past_due" | "send_blocked" | "locked"; blocked: boolean; until?: string; days_left?: number };
  domain?: { domain: string; published: boolean };
}

export interface Member { id: string; email: string; name?: string; role: string; created_at: string; has_mailbox?: boolean }
export interface MemberPermissions {
  role: string; all: string[]; from_role: string[]; effective: string[];
  allow: string[]; deny: string[]; editable: string[];
}
export interface Group { id: string; name: string; channel_id: string; members: number }
export interface GroupMember { id: string; email: string; name: string; role: string }
export interface Invite { id: string; email: string; role: string; token: string; created_at: string }
export interface Plan {
  id: string; name: string; mailbox_range: string;
  max_mailboxes: number; max_storage_bytes: number; max_daily_send: number; price_cents_per_user: number;
  // Plan catalog entitlements -- the plans table is the source of truth for all of them.
  min_users: number; apps_enabled: string[]; ai_tokens_per_window: number;
  max_meet_participants: number; meet_max_minutes: number;
  recording: string; recording_retention_days: number;
  onlyoffice_edit: boolean; wiki_public_links: boolean; active: boolean; currency?: string;
}
export interface BillingInfo {
  provider: string; current_plan: Plan; status: string; plans: Plan[];
  currency: string; min_paid_users: number; seats: number;
  subscription?: { manageable: boolean; status: string; cancel_at_period_end: boolean; current_period_end: number };
}
export interface Usage {
  plan: Plan;
  usage: { mailboxes: number; storage_bytes: number; mail_sent_today: number };
  limits: { max_mailboxes: number; max_storage_bytes: number; max_daily_send: number };
}

// Languages supported in the UI (mirrors the backend). "" = automatic via the browser.

export interface Domain {
  id: string;
  domain: string;
  provision_state: string;
}

export interface DNSRecord {
  host: string;
  type: string;
  value: string;
  purpose: string; // mx | spf | dkim | dkim_ses | dmarc | other
}

export interface DomainDetail {
  id: string;
  domain: string;
  provision_state: string;
  dkim_status: string;
  dns_status: string;
  dns_records: DNSRecord[];
}

import i18n from "./i18n";

// Translates the error via the backend's stable KEY (error_code — law §4.4), falling back to the
// server's message and then to a generic one. Never shows raw HTTP to the user.
function translateError(code: string | undefined, serverMsg: string | undefined, status: number): string {
  if (code) {
    return i18n.t(`server.${code}`, {
      defaultValue: serverMsg || i18n.t("errors.http", { status }),
    });
  }
  return serverMsg || i18n.t("errors.http", { status });
}

// Reasons the server takes access away mid-session. They are not 401 on purpose: the session was
// valid, the answer is that this account may no longer use it, and the status code says which.
const LOGGED_OUT_CODES = ["auth.account_suspended", "account.locked"];

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...opts,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...authHeaders(), ...(opts.headers || {}) },
  });
  if (!res.ok) {
    if (res.status === 401) notifyUnauthorized(path); // dead session → back to login (global)
    const body = await res.json().catch(() => ({}));
    // Access withdrawn while signed in: an admin suspended this person, or the organisation's grace
    // period ran out. The server has already destroyed the session; without this the app carries on
    // showing a screen full of data it can no longer refresh, and every click fails one by one.
    if (LOGGED_OUT_CODES.includes(body.error_code)) notifyUnauthorized(path);
    throw new Error(translateError(body.error_code, body.error, res.status));
  }
  return res.json() as Promise<T>;
}

// A DNS record's state in the real world: what we asked for, what's published, and the verdict.
export interface EmailAlias {
  id: string; address: string; domain?: string;
  owner_id: string; owner_email?: string; owner_name?: string; created_at?: string;
}
export interface DNSCheck {
  host: string; type: string; expected: string; purpose: string;
  found: string[]; status: "ok" | "missing" | "mismatch"; required: boolean;
}
export interface DNSCheckResult {
  records: DNSCheck[]; checked_at: string | null; cached?: boolean;
  summary: { total: number; ok: number; missing: number; mismatch: number; required_pending: number };
}

export type Mailbox = {
  id: string; email: string; name: string; role: string; created_at: string;
  status: "active" | "suspended" | "deleted";
  successor_email?: string; deleted_at?: string; purge_after?: string;
};

export const api = {
  me: () => req<Me>("/api/me"),
  edition: () => req<Edition>("/api/edition"),
  // Email aliases (an extra address that delivers to another mailbox). Not members: just mail.
  aliases: () => req<{ aliases: EmailAlias[] }>("/api/aliases"),
  createAlias: (local: string, domain_id: string, owner_id: string) =>
    req<EmailAlias>("/api/aliases", { method: "POST", body: JSON.stringify({ local, domain_id, owner_id }) }),
  deleteAlias: (id: string) => req<{ status: string }>(`/api/aliases/${id}`, { method: "DELETE" }),
  // On-demand DNS check (informational: it's the worker that promotes the domain).
  dnsCheck: (id: string) => req<DNSCheckResult>(`/api/domains/${id}/dns/check`),
  // Exports the zone: downloads it via fetch (with the session) and saves it as a file -- this
  // way the token never ends up in a URL inside an href.
  dnsExport: async (id: string, format: "zone" | "csv", domain: string): Promise<void> => {
    const res = await fetch(apiUrl(`/api/domains/${id}/dns/export?format=${format}`), {
      credentials: "include", headers: authHeaders(),
    });
    if (!res.ok) throw new Error(translateError(undefined, undefined, res.status));
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = format === "csv" ? `${domain}-dns.csv` : `${domain}.zone`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  },
  // Organization apps: list of toggleable + disabled ones; PUT saves the disabled ones (admin/owner only).
  appsGet: () => req<{ apps: string[]; disabled: string[] }>("/api/apps"),
  appsSet: (disabled: string[]) => req<{ disabled: string[] }>("/api/apps", { method: "PUT", body: JSON.stringify({ disabled }) }),
  login: (email: string, password: string) =>
    req<{ user_id?: string; mfa_required?: boolean; challenge?: string; methods?: string[] }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  logout: () => req<{ status: string }>("/api/webmail/logout", { method: "POST" }),
  domains: () => req<Domain[]>("/api/domains"),
  deleteDomain: (id: string) =>
    req<{ status: string }>(`/api/domains/${id}`, { method: "DELETE" }),
  createDomain: (domain: string) =>
    req<Domain>("/api/domains", { method: "POST", body: JSON.stringify({ domain }) }),
  getDomain: (id: string) => req<DomainDetail>(`/api/domains/${id}`),
  createMailbox: (domainId: string, login: string, name: string, password: string) =>
    req<{ email: string }>(`/api/domains/${domainId}/mailboxes`, {
      method: "POST",
      body: JSON.stringify({ login, name, password }),
    }),

  // Members / RBAC
  members: () => req<Member[]>("/api/members"),
  // User groups (creating a group creates the private channel in chat).
  groups: () => req<Group[]>("/api/groups"),
  createGroup: (name: string, members: string[] = []) => req<Group>("/api/groups", { method: "POST", body: JSON.stringify({ name, members }) }),
  renameGroup: (id: string, name: string) => req<Group>(`/api/groups/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  deleteGroup: (id: string) => req<{ status: string }>(`/api/groups/${id}`, { method: "DELETE" }),
  groupMembers: (id: string) => req<GroupMember[]>(`/api/groups/${id}/members`),
  addGroupMember: (id: string, user_id: string) => req<{ status: string }>(`/api/groups/${id}/members`, { method: "POST", body: JSON.stringify({ user_id }) }),
  removeGroupMember: (id: string, userId: string) => req<{ status: string }>(`/api/groups/${id}/members/${userId}`, { method: "DELETE" }),
  // The user's OWN groups — a user can be in several.
  userGroups: (id: string) => req<{ id: string; name: string }[]>(`/api/members/${id}/groups`),
  setUserGroups: (id: string, group_ids: string[]) => req<{ id: string; name: string }[]>(`/api/members/${id}/groups`, { method: "PUT", body: JSON.stringify({ group_ids }) }),
  invites: () => req<Invite[]>("/api/invites"),
  createInvite: (email: string, role: string) =>
    req<Invite>("/api/invites", { method: "POST", body: JSON.stringify({ email, role }) }),
  revokeInvite: (id: string) => req<unknown>(`/api/invites/${id}`, { method: "DELETE" }),
  // A member's effective permissions: what comes from the role, what was adjusted by hand, and
  // what I myself am allowed to change (you can't grant what you don't have).
  memberPermissions: (id: string) => req<MemberPermissions>(`/api/members/${id}/permissions`),
  setMemberPermissions: (id: string, allow: string[], deny: string[]) =>
    req<{ effective: string[]; allow: string[]; deny: string[] }>(`/api/members/${id}/permissions`, { method: "PUT", body: JSON.stringify({ allow, deny }) }),
  changeRole: (id: string, role: string) =>
    req<Member>(`/api/members/${id}/role`, { method: "PATCH", body: JSON.stringify({ role }) }),
  removeMember: (id: string) => req<unknown>(`/api/members/${id}`, { method: "DELETE" }),

  // MFA
  securityPolicyGet: () => req<{ mfa_required: boolean; mfa_method: string }>("/api/org/security"),
  securityPolicySet: (on: boolean, method: string) =>
    req<{ mfa_required: boolean; mfa_method: string }>("/api/org/security", { method: "PUT", body: JSON.stringify({ mfa_required: on, mfa_method: method }) }),
  totpStart: () => req<{ secret: string; otpauth_uri: string }>("/api/mfa/totp/start", { method: "POST" }),
  totpEnable: (code: string) =>
    req<{ recovery_codes: string[] }>("/api/mfa/totp/enable", { method: "POST", body: JSON.stringify({ code }) }),
  smsStart: (phone: string) =>
    req<{ challenge: string }>("/api/mfa/sms/start", { method: "POST", body: JSON.stringify({ phone }) }),
  smsEnable: (challenge: string, code: string) =>
    req<{ recovery_codes: string[] }>("/api/mfa/sms/enable", { method: "POST", body: JSON.stringify({ challenge, code }) }),
  mfaDisable: (password: string) => req<unknown>("/api/mfa/disable", { method: "POST", body: JSON.stringify({ password }) }),
  mfaVerify: (challenge: string, code: string) =>
    req<{ user_id: string }>("/api/auth/mfa/verify", { method: "POST", body: JSON.stringify({ challenge, code }) }),

  // Mailboxes (managed from the panel)
  mailboxes: () => req<Mailbox[]>("/api/mailboxes"),
  resetMailboxPassword: (id: string, password: string) =>
    req<{ status: string }>(`/api/mailboxes/${id}/password`, { method: "POST", body: JSON.stringify({ password }) }),

  // Mailbox lifecycle. Deleting destroys nothing today -- see docs/data-retention.md.
  setMailboxStatus: (id: string, status: "active" | "suspended") =>
    req<Mailbox>(`/api/mailboxes/${id}/status`, { method: "PUT", body: JSON.stringify({ status }) }),
  deleteMailbox: (id: string, successor_email: string) =>
    req<Mailbox>(`/api/mailboxes/${id}`, { method: "DELETE", body: JSON.stringify({ successor_email, confirm: true }) }),
  restoreMailbox: (id: string) => req<Mailbox>(`/api/mailboxes/${id}/restore`, { method: "POST" }),
  // The export is a download, not JSON: the browser handles it from the address.
  mailboxExportURL: (id: string) => apiUrl(`/api/mailboxes/${id}/export`),

  // Moving in from the old provider. `check` copies nothing -- it exists so a wrong password is
  // found on the form, in seconds, rather than by a job that fails an hour later.
  migrationCheck: (s: MigrationSource) =>
    req<MigrationCheck>("/api/me/migration/check", { method: "POST", body: JSON.stringify(s) }),
  migrationStart: (s: MigrationSource) =>
    req<Migration>("/api/me/migration", { method: "POST", body: JSON.stringify(s) }),
  migration: () => req<{ migration: Migration | null; prompt?: boolean }>("/api/me/migration"),
  // Records that the first-access offer was made, whatever the answer. Against the account, not
  // the browser: one person asked once, on every device.
  migrationDismiss: () => req<{ status: string }>("/api/me/migration/dismiss", { method: "POST" }),
  migrationCancel: (id: string) => req<{ status: string }>(`/api/me/migration/${id}`, { method: "DELETE" }),
  migrations: () => req<{ migrations: Migration[] }>("/api/migrations"),

  // Uploading a mailbox from the customer's own disk.
  //
  // XHR and not fetch, for one reason: fetch cannot report how far an upload has got. A mail
  // archive is measured in gigabytes, and a browser that shows nothing for eleven minutes is a
  // browser somebody closes. The body is the file itself -- no multipart envelope around several
  // gigabytes just to carry a filename, which rides in the query instead.
  migrationUploadFile: (file: File, onProgress: (pct: number) => void) =>
    new Promise<Migration>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", apiUrl(`/api/me/migration/file?name=${encodeURIComponent(file.name)}`));
      xhr.withCredentials = true;
      for (const [k, v] of Object.entries(authHeaders())) xhr.setRequestHeader(k, v);
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(Math.floor((e.loaded / e.total) * 100));
      };
      xhr.onload = () => {
        let body: { error_code?: string; error?: string } = {};
        try { body = JSON.parse(xhr.responseText); } catch { /* not JSON: fall through to the generic */ }
        if (xhr.status >= 200 && xhr.status < 300) { resolve(body as unknown as Migration); return; }
        if (xhr.status === 401) notifyUnauthorized("/api/me/migration/file");
        reject(new Error(translateError(body.error_code, body.error, xhr.status)));
      };
      xhr.onerror = () => reject(new Error(i18n.t("errors.network", { defaultValue: "Network error" })));
      xhr.send(file);
    }),

  // Unified users = mailbox + role. Creating one provisions both together.
  createUser: (domain_id: string, login: string, name: string, password: string, role: string, groups: string[] = []) =>
    req<{ id: string; email: string; role: string }>("/api/users", { method: "POST", body: JSON.stringify({ domain_id, login, name, password, role, quota_gb: 5, groups }) }),
  resetUserPassword: (email: string, password: string) =>
    req<{ status: string }>("/api/users/reset-password", { method: "POST", body: JSON.stringify({ email, password }) }),

  // Billing / uso
  billing: () => req<BillingInfo>("/api/billing"),
  usage: () => req<Usage>("/api/usage"),
  checkout: (plan: string, cycle: BillingCycle = "month") =>
    req<{ url?: string; applied: boolean }>("/api/billing/checkout", { method: "POST", body: JSON.stringify({ plan, cycle }) }),
  // Billing data (who's subscribing, where the invoice goes) and a VAT simulation.
  billingProfile: () => req<BillingProfile>("/api/billing/profile"),
  saveBillingProfile: (p: BillingProfile) => req<{ status: string }>("/api/billing/profile", { method: "PUT", body: JSON.stringify(p) }),
  billingQuote: (q: { plan: string; cycle: BillingCycle; kind: string; country: string; tax_id: string }) =>
    req<BillingQuote>("/api/billing/quote", { method: "POST", body: JSON.stringify(q) }),
  // Self-service signup: catalog and quote WITHOUT a session (the organisation does not exist yet).
  publicPlans: () => req<{ plans: Plan[]; currency: string; trial_days: number; months_charged: number; seller_country: string; signups_open: boolean }>("/api/public/plans"),
  publicQuote: (q: { plan: string; cycle: BillingCycle; kind: string; country: string; tax_id: string; seats: number }) =>
    req<BillingQuote>("/api/public/quote", { method: "POST", body: JSON.stringify(q) }),
  publicSignup: (b: {
    org_name: string; domain: string; login: string; name: string; password: string;
    plan: string; cycle: BillingCycle; billing: BillingProfile;
  }) => req<{ tenant_id: string; email: string; token: string; trial_days: number; checkout_url?: string }>(
    "/api/public/signup", { method: "POST", body: JSON.stringify(b) }),
  // The waiting list. Reached from the closed-signup screen, which is the highest-intent surface
  // there is: somebody who navigated to /signup has already decided to try the product.
  joinWaitlist: (b: { email: string; name: string; company?: string; note?: string; locale?: string; source?: string }) =>
    req<{ status: string }>("/api/public/waitlist", { method: "POST", body: JSON.stringify(b) }),
  billingCancel: () => req<{ status: string }>("/api/billing/cancel", { method: "POST", body: "{}" }),
  billingResume: () => req<{ status: string }>("/api/billing/resume", { method: "POST", body: "{}" }),
  billingPortal: () => req<{ url: string }>("/api/billing/portal", { method: "POST", body: "{}" }),
};


// --- Billing: customer profile and VAT simulation (the rule lives in the backend) ---
export type BillingCycle = "month" | "year";
export interface BillingProfile {
  kind: "company" | "individual";
  legal_name?: string; tax_id?: string; country?: string; city?: string; postal_code?: string;
  address_line1?: string; address_line2?: string; invoice_email?: string; invoice_name?: string; phone?: string;
  filled?: boolean;
}
export interface BillingQuote {
  quote: {
    currency: string; seats: number; unit_cents: number; subtotal_cents: number;
    vat_rate_bps: number; vat_cents: number; total_cents: number;
    reverse_charge: boolean; tax_id_valid: boolean; country: string;
  };
  cycle: BillingCycle; plan: string; plan_name: string;
  monthly_cents: number; months_charged: number; seller_country: string; vat_rate_percent: number;
}

export function hasPerm(me: Me | undefined, perm: string): boolean {
  return !!me?.permissions?.includes(perm);
}

// --- Moving a mailbox in from the provider the customer is leaving ---

export interface MigrationSource {
  host: string; port?: number; starttls?: boolean; username: string; password: string;
}
export interface MigrationFolder {
  remote_name: string; local_name: string; local_role: string;
  // Empty means the folder is copied. Set means it is not, and the panel says why -- a folder that
  // vanished without a stated reason looks exactly like data loss to whoever counts them afterwards.
  skip_reason: "" | "virtual" | "trash" | "junk";
  status?: string;
  messages?: number;
  total_messages?: number; copied_messages?: number; skipped_messages?: number; failed_messages?: number;
}
export interface MigrationCheck { folders: MigrationFolder[]; total_messages: number }
export interface Migration {
  id: string;
  // "imap" = an account on a server; "file" = something the customer uploaded.
  kind?: "imap" | "file";
  source_file?: string;
  status: "pending" | "running" | "done" | "failed" | "cancelled";
  error_code: string;
  source_host: string; source_username: string;
  total_messages: number; copied_messages: number; skipped_messages: number; failed_messages: number;
  created_at: string; started_at?: string | null; finished_at?: string | null;
  folders?: MigrationFolder[];
  mailbox_email?: string;
}
