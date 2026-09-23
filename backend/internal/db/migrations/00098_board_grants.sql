-- +goose Up
-- Kanban board permissions: per person, team or organization, with roles
-- viewer|editor|manager. Visibility rules (enforced in SQL, same pattern as the wiki):
--  - the board owner = manager;
--  - LEGACY board (owner NULL and no grants) = the whole org can edit (compatible with the previous
--    behavior, where everyone saw every board);
--  - anyone with a card ASSIGNED on the board automatically gets editor access.
ALTER TABLE boards ADD COLUMN owner_user_id UUID REFERENCES mail_users(id) ON DELETE SET NULL;

CREATE TABLE board_grants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    board_id     UUID NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group', 'org')),
    subject_id   UUID,
    role         TEXT NOT NULL DEFAULT 'editor' CHECK (role IN ('viewer', 'editor', 'manager')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((subject_type = 'org') = (subject_id IS NULL))
);
CREATE INDEX idx_board_grants_board ON board_grants(tenant_id, board_id);
CREATE UNIQUE INDEX ux_board_grants_subject ON board_grants(board_id, subject_type, subject_id)
    WHERE subject_id IS NOT NULL;
CREATE UNIQUE INDEX ux_board_grants_org ON board_grants(board_id) WHERE subject_type = 'org';

-- +goose Down
DROP TABLE IF EXISTS board_grants;
ALTER TABLE boards DROP COLUMN IF EXISTS owner_user_id;
