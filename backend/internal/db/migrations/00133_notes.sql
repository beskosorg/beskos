-- +goose Up
-- Notes: personal notes and meeting notes, in one place.
--
-- ONE TABLE for both. A meeting note is a note that happens to carry a meeting_id -- it is the same
-- object with the same sharing, the same pinning and the same "turn this into tasks". Keeping the
-- old meeting_notes table alongside would mean two models, two screens and two places to fix every
-- future bug about notes.
--
-- PRIVATE BY DEFAULT is the absence of grants, not a flag. A note nobody was given is visible to its
-- owner and to nobody else; there is no state to forget to set, and no way for a bug in a boolean to
-- expose someone's notes. Sharing is always something added.
--
-- The grant shape is copied from wiki_page_grants deliberately (user | group | org), so the sharing
-- UI, the mental model and the permission query all read the same as the wiki's. Two roles instead
-- of the wiki's four: there is nothing to comment on yet, and a 'commenter' that behaves exactly
-- like a viewer is a promise the product does not keep.

CREATE TABLE notes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    title         TEXT NOT NULL DEFAULT '',
    -- Same rich format as the wiki (BlockNote block array), so the editor is literally the same
    -- component. Raw JSON in the response, never base64 -- see the sqlc override for wiki_pages.
    body_json     JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- Plain-text mirror of the body. The AI task extraction and the search both want text, and
    -- walking the block tree in three different places to get it would drift.
    body_text     TEXT NOT NULL DEFAULT '',
    -- Set when the note belongs to a meeting. NULL for an ordinary note.
    meeting_id    UUID REFERENCES meetings(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    UUID REFERENCES mail_users(id) ON DELETE SET NULL
);
CREATE INDEX idx_notes_owner ON notes (tenant_id, owner_user_id, updated_at DESC);
-- One note per meeting: the meeting's notes are a single shared document, as they are today.
CREATE UNIQUE INDEX idx_notes_meeting ON notes (meeting_id) WHERE meeting_id IS NOT NULL;

CREATE TABLE note_grants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    note_id      UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group', 'org')),
    subject_id   UUID,
    role         TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('viewer', 'editor')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((subject_type = 'org') = (subject_id IS NULL))
);
CREATE INDEX idx_note_grants_note ON note_grants (tenant_id, note_id);
CREATE INDEX idx_note_grants_subject ON note_grants (subject_type, subject_id);
-- Granting the same subject twice is a no-op, not two rows: the sharing UI upserts.
CREATE UNIQUE INDEX idx_note_grants_uq ON note_grants (note_id, subject_type, COALESCE(subject_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- Pinning is PER PERSON, not a column on the note. A note shared with eight people, pinned by one
-- of them, must not jump to the top for the other seven -- pinning says "this matters to me now",
-- which is not a property of the note.
CREATE TABLE note_pins (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    note_id   UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    pinned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (note_id, user_id)
);
CREATE INDEX idx_note_pins_user ON note_pins (tenant_id, user_id, pinned_at DESC);

-- Carry the existing meeting notes over. The owner is whoever last edited them, falling back to the
-- meeting's owner: the old table has no author, only updated_by, and a note has to belong to
-- someone. Body arrives as a single paragraph block -- the old notes were a plain textarea.
INSERT INTO notes (tenant_id, owner_user_id, title, body_json, body_text, meeting_id, updated_at, updated_by)
SELECT n.tenant_id,
       COALESCE(n.updated_by, m.owner_user_id),
       -- The meeting's own title, or empty. Deliberately NOT a written-out fallback like
       -- "Meeting notes": that is display copy, it belongs to i18n, and a product served in eight
       -- languages must not have one of them frozen into a row.
       m.title,
       CASE WHEN n.body = '' THEN '[]'::jsonb ELSE
           jsonb_build_array(jsonb_build_object(
               'type', 'paragraph',
               'content', jsonb_build_array(jsonb_build_object('type', 'text', 'text', n.body, 'styles', '{}'::jsonb))))
       END,
       n.body,
       n.meeting_id,
       n.updated_at,
       n.updated_by
FROM meeting_notes n
JOIN meetings m ON m.id = n.meeting_id
WHERE COALESCE(n.updated_by, m.owner_user_id) IS NOT NULL;

-- meeting_notes stays for now, read by nobody. Dropping a table in the same migration that copies
-- out of it leaves no way back if the copy turns out wrong; it goes in a later one, once the app
-- has been running on the new table.

-- +goose Down
DROP TABLE note_pins;
DROP TABLE note_grants;
DROP TABLE notes;
