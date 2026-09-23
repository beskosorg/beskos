// JMAP types used by the webmail (a subset of RFC 8620/8621 plus what Stalwart actually returns
// in practice).

export interface JmapSession {
  accountId: string; // primaryAccounts["urn:ietf:params:jmap:mail"]
  identityId?: string;
  capabilities: Record<string, unknown>;
  // Paths (not hosts — hosts come with the configured serverHostname, DIVERGENCES §D7).
  downloadUrlTemplate: string;
}

export interface Mailbox {
  id: string;
  name: string;
  role: string | null; // inbox | drafts | sent | trash | junk | archive | null
  parentId?: string | null; // parent folder (nesting); null = root
  totalEmails: number;
  unreadEmails: number;
  sortOrder?: number;
}

export interface EmailAddress {
  name?: string | null;
  email: string;
}

export interface EmailSummary {
  id: string;
  from: EmailAddress[] | null;
  to: EmailAddress[] | null;
  subject: string;
  receivedAt: string;
  preview: string;
  hasAttachment: boolean;
  keywords?: Record<string, boolean>;
  mailboxIds?: Record<string, boolean>; // folders the message is in — for the folder chip (Starred/search)
  threadId?: string;
  threadCount?: number; // number of messages in the conversation (only when the list is collapsed and >1)
  /** Some message in the conversation is unread (the collapsed row shows only the most recent one). */
  threadUnread?: boolean;
}

export interface EmailAttachment {
  partId?: string;
  blobId: string;
  type: string;
  name?: string | null;
  size: number;
  cid?: string | null;   // Content-ID: image embedded in the HTML (src="cid:...")
}

// A page of results: the messages + the total, for "1-50 of 35,547" and the arrows.
export interface EmailPage { items: EmailSummary[]; total: number; position: number }

export interface EmailFull extends EmailSummary {
  threadId?: string;            // conversation id (related messages share it)
  messageId?: string[];         // RFC822 Message-ID; the SAME message in different folders shares it
  sentAt?: string;               // date stamped by the sender (can differ from delivery)
  cc?: EmailAddress[];
  replyTo?: EmailAddress[];
  textBody: { partId: string; type?: string }[];
  htmlBody: { partId: string; type?: string }[];
  bodyValues: Record<string, { value: string; isTruncated?: boolean }>;
  attachments: EmailAttachment[];
}

export interface UploadedBlob {
  blobId: string;
  type: string;
  name: string;
  size: number;
}

export interface SendDraft {
  from: EmailAddress;
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  subject: string;
  textBody: string;
  htmlBody?: string;          // when present, sends text/html (with text/plain as the alternative)
  attachments?: UploadedBlob[];
}
