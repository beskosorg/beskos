-- The Documents app: a VIEW over the files that already exist, not a second store.
--
-- Same files, same sharing, same versions, same trash. The only thing this adds is a way of looking
-- at them: by what somebody was working on, across every folder, instead of by where it was filed.
-- If this ever grows folders of its own, it stopped being a view and became a second file manager.

-- name: ListRecentDocuments :many
-- The office documents this person can open, newest first.
--
-- The visibility rule is copied VERBATIM from the file sync query (queries.sql, ListFilesForSync):
-- mine, or shared with me by e-mail. A second rule written here is how a listing ends up showing
-- somebody a document they were never given -- and the bug would live in a SELECT nobody re-reads.
--
-- ONE predicate, no top-level OR. The first draft of this query ended in `... AND ext IN (...) OR
-- (tenant = ... )`, which SQL reads as `(everything) OR (the second half)` -- and the second half
-- had its own tenant check only by luck. That is precisely how one organisation's documents end up
-- in another's list.
SELECT f.*, (f.owner_user_id = @user_id) AS owned
FROM storage_files f
WHERE f.tenant_id = @tenant_id
  AND f.deleted_at IS NULL
  AND (
        f.owner_user_id = @user_id
        OR EXISTS (SELECT 1 FROM storage_shares sh
                   WHERE sh.file_id = f.id AND sh.tenant_id = @tenant_id AND sh.principal = @email)
      )
  -- Only what the editor can actually open: a list that offers to edit a PDF is a list that lies.
  AND lower(substring(f.name from '\.[^.]+$')) = ANY (ARRAY[
        '.docx', '.doc', '.odt', '.rtf',
        '.xlsx', '.xls', '.ods', '.csv',
        '.pptx', '.ppt', '.odp'
      ])
ORDER BY f.updated_at DESC
LIMIT @lim;
