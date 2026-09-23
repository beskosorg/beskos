-- name: DeactivateRoomPresentations :exec
UPDATE meet_presentations SET active = false WHERE room = $1 AND tenant_id = $2 AND active;

-- name: CreatePresentation :one
INSERT INTO meet_presentations (room, tenant_id, name, object_key, created_by)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetActivePresentation :one
SELECT * FROM meet_presentations
WHERE room = $1 AND tenant_id = $2 AND active
ORDER BY created_at DESC LIMIT 1;

-- name: GetPresentation :one
SELECT * FROM meet_presentations WHERE id = $1;

-- name: SetPresentationSlide :exec
UPDATE meet_presentations SET current_slide = $2 WHERE id = $1;

-- name: DeactivatePresentation :exec
UPDATE meet_presentations SET active = false WHERE id = $1 AND tenant_id = $2;

-- name: DeactivateRoomPresentationsByCreator :exec
-- Ends the active presentations a user created in a room (when they leave it).
-- No tenant filter: the room name (mailin-<rand>) is already unique, and created_by narrows it.
UPDATE meet_presentations SET active = false WHERE room = $1 AND created_by = $2 AND active;
