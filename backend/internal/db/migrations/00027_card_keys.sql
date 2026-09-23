-- +goose Up
-- Tasks with a Jira-style identifier: PREFIX-NUMBER (e.g. QUA-12), per board.
--
-- The number is per BOARD, not global: that is what makes "QUA-1" the first card of the board Quadro,
-- regardless of what happens in the others. Same sequence mechanism as the old tickets:
-- UPDATE ... RETURNING locks the board's row, so two cards created at the same time never
-- get the same number (MAX(number)+1 would tie).

ALTER TABLE boards ADD COLUMN prefix   TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN card_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE cards  ADD COLUMN number   BIGINT;

-- Default prefix: the first 3 letters/digits of the name, uppercased ("Quadro" → QUA).
-- A name with no letters at all (e.g. "🚀") would fall to '' — hence the COALESCE to 'TSK'.
UPDATE boards SET prefix = COALESCE(
    NULLIF(upper(substring(regexp_replace(name, '[^a-zA-Z0-9]', '', 'g') from 1 for 3)), ''),
    'TSK');

-- Number the existing cards in creation order, so the oldest is #1.
WITH numbered AS (
    SELECT id, row_number() OVER (PARTITION BY board_id ORDER BY created_at, id) AS n FROM cards
)
UPDATE cards c SET number = numbered.n FROM numbered WHERE numbered.id = c.id;

-- Each board's counter continues from where the numbering stopped.
UPDATE boards b SET card_seq = COALESCE((SELECT max(c.number) FROM cards c WHERE c.board_id = b.id), 0);

ALTER TABLE cards ALTER COLUMN number SET NOT NULL;
ALTER TABLE cards ADD CONSTRAINT cards_number_uq UNIQUE (board_id, number);

-- +goose Down
ALTER TABLE cards DROP CONSTRAINT cards_number_uq;
ALTER TABLE cards DROP COLUMN number;
ALTER TABLE boards DROP COLUMN card_seq;
ALTER TABLE boards DROP COLUMN prefix;
