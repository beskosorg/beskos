-- +goose Up
-- Semantic RAG for the wiki (docs/wiki-features.md §7.2). Requires the pgvector extension (image
-- pgvector/pgvector:pg16). Each row is a chunk of a page, with its embedding.
-- Chosen embeddings provider: OpenAI text-embedding-3-small → dimension D = 1536.
-- A worker (re)computes the chunks when a page is saved/deleted (idempotent). Search is cosine kNN,
-- ALWAYS filtered by the asker's permission (vis CTE) + tenant_id + ai_indexed.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE wiki_chunks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    page_id     UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    space_id    UUID NOT NULL REFERENCES wiki_spaces(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    chunk_text  TEXT NOT NULL,
    embedding   vector(1536) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (page_id, chunk_index)
);
CREATE INDEX idx_wiki_chunks_page ON wiki_chunks(tenant_id, page_id);
-- Cosine ANN index (ivfflat). The permission filter is applied in the query (join with the vis CTE).
CREATE INDEX idx_wiki_chunks_embedding ON wiki_chunks
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- +goose Down
DROP TABLE IF EXISTS wiki_chunks;
-- Keeps the vector extension (other features may use it); remove it only if explicitly requested.
