-- +goose Up
-- Co-hosts: other users the organizer promotes to host. They get host powers (mute, remove,
-- lock, record) and join with the full policy. The owner still manages the list.
CREATE TABLE meeting_cohosts (
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (meeting_id, user_id)
);

-- +goose Down
DROP TABLE meeting_cohosts;
