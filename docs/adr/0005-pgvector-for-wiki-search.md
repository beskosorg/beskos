# 5. pgvector for wiki semantic search

## Status

Accepted.

## Context

The Wiki (`backend/internal/wiki`) needs semantic search over its pages -- finding a page by
meaning rather than keyword match, and feeding relevant page content to the AI assistant as
retrieval-augmented context (RAG). That requires storing an embedding vector per chunk of page
text and running approximate nearest-neighbor search over them, scoped to what the requesting
user is allowed to see.

The project already runs PostgreSQL for every other piece of relational and full-text data (the
Kanban board, chat search, contacts). Adding a second, separate database purely for vector search
would mean a second system to deploy, back up, and keep consistent with Postgres -- a second
source of truth for data that is otherwise scoped, permissioned, and transactionally tied to rows
that live in Postgres already.

## Decision

Use the `pgvector` Postgres extension and keep vectors in the same database as everything else.
The reference deployment (`deploy/docker-compose.yml`) runs
`pgvector/pgvector:pg16` instead of a plain `postgres:16` image specifically so the extension is
available without a manual install step. The schema
(`backend/internal/db/migrations/00093_wiki_chunks.sql`) is a `wiki_chunks` table -- one row per
chunk of page text, each carrying `tenant_id`, `page_id`, `space_id`, the chunk text, and its
`vector(1536)` embedding -- with an IVFFlat index (`vector_cosine_ops`) for approximate
nearest-neighbor search by cosine distance, and a plain b-tree index on `(tenant_id, page_id)`
for the non-semantic lookups.

Embeddings are generated through `backend/internal/ai.Embedder`, which talks to an
OpenAI-compatible `/embeddings` endpoint configured entirely through environment variables
(`EMBEDDINGS_API_KEY`, `EMBEDDINGS_BASE_URL`, `EMBEDDINGS_MODEL`, `EMBEDDINGS_DIMENSIONS`) --
the provider is swappable, but whatever model is configured has to produce vectors matching the
`vector(1536)` column width, or writes fail loudly rather than silently truncating or padding a
vector (see `embeddings_test.go`'s short-vector case). `EMBEDDINGS_API_KEY` unset turns the
feature off entirely: the wiki keeps working, only semantic search is unavailable, the same
"missing config disables the feature cleanly" pattern used for OnlyOffice and LiveKit.

Every query against `wiki_chunks` joins through the wiki's own permission CTE and filters by
`tenant_id` before it filters by vector distance -- law §5.1 applies to a semantic
search index exactly as it applies to any other table holding customer data.

## Consequences

- One database to operate, back up, and reason about the consistency of, instead of two. A
  wiki page's permission change and its chunk rows can be updated in the same transactional
  world.
- IVFFlat trades recall for speed and needs a `lists` tuning parameter (currently 100) chosen for
  the expected table size; a deployment with a very large wiki may need to revisit that number or
  move to HNSW indexing, which pgvector also supports.
- The embedding dimension is fixed at the schema level (`vector(1536)`). Switching to a provider
  or model with a different output dimension requires a migration that changes the column type
  and re-embeds every existing chunk -- there is no per-row dimension flexibility.
- Because the extension lives inside Postgres, self-hosting the project depends on the operator
  running `pgvector/pgvector:pg16` (or manually installing the extension on their own Postgres) --
  a slightly less common image than plain `postgres:16`, called out explicitly in
  `docker-compose.yml`'s comments so it isn't swapped out by habit.

## Alternatives considered

- **A dedicated vector database** (Qdrant, Weaviate, Milvus, Pinecone). Rejected: more
  operational surface for a self-hosted product, and a second place permission logic would need
  to be duplicated or synchronized with Postgres instead of enforced once, in one query.
- **Postgres full-text search (`tsvector`) alone, no semantic layer.** This is what backs
  keyword search elsewhere in the project (chat search, for instance) and remains available; it
  doesn't answer "find the page about this concept" when the concept isn't phrased the way the
  page is, which is the specific gap semantic search closes.
