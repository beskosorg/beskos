-- +goose Up
-- Sharing a CONTACT GROUP with a specific USER (in addition to teams). Previously a group could
-- only be shared with a team (contact_group_shares); now also with a person. Contact visibility
-- now considers both paths (see ListContacts/GetContact).
CREATE TABLE contact_group_user_shares (
    group_id UUID NOT NULL REFERENCES contact_groups(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_cgus_user ON contact_group_user_shares(user_id);

-- +goose Down
DROP TABLE contact_group_user_shares;
