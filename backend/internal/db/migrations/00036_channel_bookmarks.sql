-- +goose Up
-- Channel bookmarks: a curated collection of LINKS and FILES (Slack "Files & links" style). Members
-- add and remove them. A file references a storage_file; download is authorized by channel
-- membership (not by file ownership).
CREATE TABLE channel_bookmarks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,                 -- 'link' | 'file'
    title      TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL DEFAULT '',
    file_id    UUID REFERENCES storage_files(id) ON DELETE CASCADE,
    file_name  TEXT NOT NULL DEFAULT '',
    file_size  BIGINT NOT NULL DEFAULT 0,
    added_by   UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bookmarks_channel ON channel_bookmarks (channel_id, created_at DESC);

-- +goose Down
DROP TABLE channel_bookmarks;
