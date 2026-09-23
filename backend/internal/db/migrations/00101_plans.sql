-- +goose Up
CREATE TABLE plans (
    key                      TEXT PRIMARY KEY,
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    price_cents_per_user     INT NOT NULL DEFAULT 0,
    min_users                INT NOT NULL DEFAULT 0,
    max_users                INT NOT NULL DEFAULT 0,
    apps_enabled             TEXT[] NOT NULL DEFAULT '{}',
    max_storage_gb           INT NOT NULL DEFAULT 0,
    ai_tokens_per_window     INT NOT NULL DEFAULT 0,
    meet_max_participants    INT NOT NULL DEFAULT 0,
    meet_max_minutes         INT NOT NULL DEFAULT 0,
    recording                TEXT NOT NULL DEFAULT 'none'
        CHECK (recording IN ('none', 'offline', 'offline_priority')),
    recording_retention_days INT NOT NULL DEFAULT 30,
    max_daily_send           INT NOT NULL DEFAULT 0,
    onlyoffice_edit          BOOLEAN NOT NULL DEFAULT true,
    wiki_public_links        BOOLEAN NOT NULL DEFAULT true,
    active                   BOOLEAN NOT NULL DEFAULT true,
    sort                     INT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);


INSERT INTO plans (key, name) VALUES ('self_hosted', 'Community');

-- +goose Down
DROP TABLE IF EXISTS plans;
