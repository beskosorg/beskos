-- +goose Up
-- Repairs the boards that were born without a prefix.
--
-- 00027 gave a prefix to every board that existed back then, but the INSERT for a NEW board never
-- filled it in -- it stayed on the column default of ''. The result is that every task on those
-- boards shows its key as "-1", "-2" instead of "QUA-1", and has done since. CreateBoard now
-- derives it; this fixes what is already stored.
--
-- SAME RULE as 00027, deliberately: first 3 alphanumerics of the name, uppercased, and 'TSK' when
-- the name has none (a board called "🚀" would land on '' and bring the bug straight back).
--
-- Only touches prefix = ''. A board whose prefix was chosen by hand is left alone.
UPDATE boards SET prefix = COALESCE(
    NULLIF(upper(substring(regexp_replace(name, '[^a-zA-Z0-9]', '', 'g') from 1 for 3)), ''),
    'TSK')
WHERE prefix = '';

-- +goose Down
-- No rollback: there is no way to tell which prefixes came from here and which were already set,
-- and putting '' back would reintroduce the defect. The Up is idempotent, so re-running is safe.
SELECT 1;
