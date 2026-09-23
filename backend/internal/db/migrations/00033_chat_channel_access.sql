-- +goose Up
-- Channel access: "listed" controls whether the channel appears in discovery (unlisted = closed,
-- invite only); "add_policy" controls who can add members (open = any member; restricted = channel
-- managers only). The per-channel role (channel_members.role) now accepts 'manager' besides
-- 'owner'/'member'. It is a CHANNEL permission, independent of the organization role.
ALTER TABLE channels
    ADD COLUMN listed     BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN add_policy TEXT    NOT NULL DEFAULT 'open'; -- open | restricted

-- +goose Down
ALTER TABLE channels DROP COLUMN listed, DROP COLUMN add_policy;
