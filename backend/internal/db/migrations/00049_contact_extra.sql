-- +goose Up
-- Multiple emails/phones per contact. The primary email/phone (email/phone columns) remain the
-- source for autocomplete, history, dedup and distribution lists; changing that would touch half
-- the system. These arrays hold the SECONDARY ones (e.g. personal + work email). Same pattern as tags[].
ALTER TABLE contacts ADD COLUMN extra_emails TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN extra_phones TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE contacts DROP COLUMN extra_emails;
ALTER TABLE contacts DROP COLUMN extra_phones;
