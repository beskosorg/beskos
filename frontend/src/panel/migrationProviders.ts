// The providers people are actually leaving, and what their IMAP server is called.
//
// WHY A LIST AND NOT A TEXT BOX. "imap.gmail.com" is not knowledge a ten-person company has, and
// asking for it is asking them to search the web mid-signup and paste something plausible. Every
// entry here removes one chance to get it wrong.
//
// `notice` is a translation key, not a sentence, and it is shown BEFORE the password field. Two of
// these providers refuse an ordinary password over IMAP; finding that out after typing it is the
// support request this list exists to prevent.

export interface Provider {
  id: string;
  host: string;
  port: number;
  starttls: boolean;
  // Translation key under migration.notice.*, or "" when the provider needs no warning.
  notice: "" | "appPassword" | "oauthOnly";
  // Domains that select this provider automatically once the address is typed. The person who knows
  // their address but not their mail server is the person this whole file is for.
  domains: string[];
}

// Ordered by who is actually being left, not alphabetically.
export const PROVIDERS: Provider[] = [
  {
    id: "google",
    host: "imap.gmail.com",
    port: 993,
    starttls: false,
    // Google refuses the account password over IMAP once two-step verification is on, and wants an
    // application password instead. That is the single most common reason an import fails to start.
    notice: "appPassword",
    domains: ["gmail.com", "googlemail.com"],
  },
  {
    id: "microsoft365",
    host: "outlook.office365.com",
    port: 993,
    starttls: false,
    // Microsoft withdrew password authentication for IMAP on business tenants; only OAuth is
    // accepted. Saying so here is the difference between a customer who plans around it and one who
    // concludes our import is broken.
    notice: "oauthOnly",
    domains: [],
  },
  {
    id: "outlookcom",
    host: "outlook.office365.com",
    port: 993,
    starttls: false,
    notice: "oauthOnly",
    domains: ["outlook.com", "hotmail.com", "live.com", "msn.com"],
  },
  {
    id: "yahoo",
    host: "imap.mail.yahoo.com",
    port: 993,
    starttls: false,
    notice: "appPassword",
    domains: ["yahoo.com", "yahoo.co.uk", "ymail.com"],
  },
  {
    id: "icloud",
    host: "imap.mail.me.com",
    port: 993,
    starttls: false,
    notice: "appPassword",
    domains: ["icloud.com", "me.com", "mac.com"],
  },
  {
    id: "zoho",
    host: "imap.zoho.com",
    port: 993,
    starttls: false,
    notice: "appPassword",
    domains: ["zoho.com", "zohomail.com"],
  },
  {
    id: "fastmail",
    host: "imap.fastmail.com",
    port: 993,
    starttls: false,
    notice: "appPassword",
    domains: ["fastmail.com", "fastmail.fm"],
  },
  // Everything else, including whatever a Thunderbird or Outlook installation was pointed at. The
  // host stays empty so the field is asked for rather than guessed.
  { id: "other", host: "", port: 993, starttls: false, notice: "", domains: [] },
];

export const OTHER_PROVIDER = PROVIDERS[PROVIDERS.length - 1];

export function providerById(id: string): Provider {
  return PROVIDERS.find((p) => p.id === id) ?? OTHER_PROVIDER;
}

// guessProvider picks the provider from the address the person just typed. A company on Google
// Workspace has its own domain, so this only fires for the consumer domains -- which is exactly the
// case where somebody is least likely to know what an IMAP server is.
export function guessProvider(address: string): Provider | null {
  const at = address.lastIndexOf("@");
  if (at < 0) return null;
  const domain = address.slice(at + 1).trim().toLowerCase();
  if (!domain) return null;
  return PROVIDERS.find((p) => p.domains.includes(domain)) ?? null;
}
