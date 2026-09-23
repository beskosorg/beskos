-- +goose Up
-- Fix: boards created between 00098 and this fix got an owner but NO grant (they became
-- invisible to colleagues). Backfill: every board with an owner and no grants receives the grant
-- org=editor (the intended collaborative default; managers can remove it to restrict access).
INSERT INTO board_grants (tenant_id, board_id, subject_type, subject_id, role)
SELECT b.tenant_id, b.id, 'org', NULL, 'editor'
FROM boards b
WHERE b.owner_user_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM board_grants g WHERE g.board_id = b.id);

-- +goose Down
-- No down: grants created here cannot be told apart from those created by users.
