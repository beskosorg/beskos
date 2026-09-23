-- +goose Up
-- Tasks: comments + an assignee who is a REAL person (2026-07-15).
--
-- Problem: `cards.assignee` was TEXT holding a name. The old picker suggested contacts from the
-- ADDRESS BOOK, so tasks were assigned to people with no account in the system — impossible to notify.
-- Now the assignee is an FK to mail_users.
--
-- `assignee` (text) does NOT go away: it keeps whatever does not match any user, so the information
-- about who was already there is not erased. Rule: assignee_id XOR assignee — the migration clears the
-- text of those that matched, and the UI only writes assignee_id from now on.

ALTER TABLE cards ADD COLUMN assignee_id UUID REFERENCES mail_users(id) ON DELETE SET NULL;
CREATE INDEX idx_cards_assignee ON cards(assignee_id);

-- Who created the task — to notify them when it changes or gets a comment. It stays NULL on the cards
-- that already exist: nobody recorded the author, and guessing (e.g. "the board owner") would email
-- someone who never created anything. Null = there is simply no one to notify.
ALTER TABLE cards ADD COLUMN created_by UUID REFERENCES mail_users(id) ON DELETE SET NULL;

-- 1) Direct match with a user (name or email).
UPDATE cards c SET assignee_id = u.id
FROM mail_users u
WHERE c.assignee <> '' AND u.tenant_id = c.tenant_id
  AND (lower(u.name) = lower(c.assignee) OR lower(u.email) = lower(c.assignee));

-- 2) Match via CONTACT: the name was an address-book contact; if their email belongs to a user,
--    it is the same person. This covers exactly the damage done by the old picker.
UPDATE cards c SET assignee_id = u.id
FROM contacts ct
JOIN mail_users u ON u.tenant_id = ct.tenant_id AND lower(u.email) = lower(ct.email)
WHERE c.assignee_id IS NULL AND c.assignee <> ''
  AND ct.tenant_id = c.tenant_id AND lower(ct.name) = lower(c.assignee) AND ct.email <> '';

-- 3) Those that matched no longer need the text (one source of truth per card).
UPDATE cards SET assignee = '' WHERE assignee_id IS NOT NULL;

-- Task comments. `parent_id` = reply to another comment (shallow thread, 1 level).
CREATE TABLE card_comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_id    UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES card_comments(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    -- Who was @mentioned in this comment. Stored on send: recomputing later from the text
    -- would notify again those already notified, and would be wrong if the person changed their name.
    mentions   UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_card_comments ON card_comments(card_id, created_at);

-- Notification preferences per user. Default: receive everything that concerns them, but
-- never about what they did themselves (that is a code rule, not configuration).
ALTER TABLE mail_users ADD COLUMN notify_email BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN notify_email;
DROP TABLE IF EXISTS card_comments;
DROP INDEX IF EXISTS idx_cards_assignee;
ALTER TABLE cards DROP COLUMN created_by;
ALTER TABLE cards DROP COLUMN assignee_id;
