-- +goose Up
-- Embedding model switch: OpenAI `text-embedding-3-small` -> `qwen/qwen3-embedding-8b`
-- served in the EU (docs/landing-europa.md). Vectors from different models are NOT
-- comparable: the distance between an old vector and a new question is noise, and semantic search
-- would return wrong results without raising an error. So everything is cleared -- the worker
-- reindexes on its own (ListWikiPagesNeedingEmbedding picks up pages with no chunk newer than
-- updated_at).
--
-- The dimension stays 1536: Qwen3 is Matryoshka and truncates on request (EMBEDDINGS_DIMENSIONS),
-- which keeps the column and the ivfflat index as they are -- pgvector does not index above 2000
-- dimensions.
DELETE FROM wiki_chunks;

-- +goose Down
-- No way back: the old embeddings cannot be recovered. The worker reindexes with the configured model.
SELECT 1;
