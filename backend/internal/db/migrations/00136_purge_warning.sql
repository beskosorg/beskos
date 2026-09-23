-- +goose Up
-- When the administrators were told a mailbox is about to be destroyed.
--
-- The warning goes to the ADMINS, never to the person whose mailbox it is. They are usually leaving
-- or have left, and telling somebody "everything you have here disappears in seven days" is an
-- invitation to take what they can on the way out. The admins are the ones who can still act: the
-- export lives inside this window and nowhere else.
--
-- A column rather than a computed date because the alternative is sending it every ten minutes for
-- a week -- and an alert that arrives forty times is an alert nobody reads the fortieth time.
ALTER TABLE mail_users ADD COLUMN purge_warned_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE mail_users DROP COLUMN IF EXISTS purge_warned_at;
