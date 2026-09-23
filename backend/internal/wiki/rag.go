package wiki

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Semantic RAG (docs/wiki-features.md §7.2): published pages of spaces with ai_indexed are
// split into chunks and embedded by an idempotent worker; search is cosine kNN ALWAYS
// filtered by the asker's permission (SearchWikiChunks applies the same visibility surface, §7.4).

// Embedder is the embeddings client (implemented by ai.Embedder). Local interface so the wiki
// package does not depend on the ai package (the wiring happens in main/httpapi).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// chunkSize/chunkOverlap: ~500 tokens per chunk with a one-paragraph overlap (§7.2).
const (
	chunkSize    = 2000 // characters per chunk (approx. 500 tokens)
	maxChunks    = 64   // cap per page (giant pages don't blow up the index)
	embedBatch   = 32   // chunks per call to the provider
	searchLimit  = 8    // default number of search results
	minChunkText = 20   // chunks shorter than this aren't worth embedding
)

// chunkText splits the text into ~chunkSize-character chunks at paragraph boundaries, repeating
// the last paragraph as overlap. Deterministic and pure (testable).
func chunkText(text string) []string {
	paras := strings.Split(text, "\n")
	var chunks []string
	var cur strings.Builder
	lastPara := ""
	flush := func() {
		if s := strings.TrimSpace(cur.String()); len(s) >= minChunkText {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Paragraph bigger than a chunk: hard-cut it into chunkSize-sized slices.
		for len(p) > chunkSize {
			flush()
			cut := p[:chunkSize]
			chunks = append(chunks, cut)
			p = p[chunkSize:]
		}
		if cur.Len() > 0 && cur.Len()+len(p)+1 > chunkSize {
			flush()
			// Overlap: the new chunk starts with the previous one's last paragraph.
			if lastPara != "" && len(lastPara) < chunkSize/2 {
				cur.WriteString(lastPara)
				cur.WriteByte('\n')
			}
		}
		cur.WriteString(p)
		cur.WriteByte('\n')
		lastPara = p
		if len(chunks) >= maxChunks {
			return chunks
		}
	}
	flush()
	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
	}
	return chunks
}

// vecLiteral formats an embedding as a pgvector literal "[0.1,0.2,...]" (the SQL casts ::vector).
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// EmbedPending (re)embeds up to maxPages stale pages and clears obsolete chunks. Idempotent:
// it can run on any cadence; a failure on one page does not block the rest. Returns how many pages
// were processed.
func (s *Service) EmbedPending(ctx context.Context, e Embedder, maxPages int32) (int, error) {
	if _, err := s.q.DeleteStaleWikiChunks(ctx); err != nil {
		return 0, fmt.Errorf("wiki embed: cleanup stale chunks: %w", err)
	}
	pages, err := s.q.ListWikiPagesNeedingEmbedding(ctx, maxPages)
	if err != nil {
		return 0, fmt.Errorf("wiki embed: list pending pages: %w", err)
	}
	done := 0
	var firstErr error
	for _, p := range pages {
		if err := s.embedPage(ctx, e, p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done++
	}
	return done, firstErr
}

func (s *Service) embedPage(ctx context.Context, e Embedder, p db.ListWikiPagesNeedingEmbeddingRow) error {
	chunks := chunkText(p.BodyText)
	// Non-empty page but below the minimum chunk size: index it anyway as a single chunk —
	// otherwise it would stay forever in the pending list (no fresh chunk) and out of RAG.
	if len(chunks) == 0 {
		if t := strings.TrimSpace(p.BodyText); t != "" {
			chunks = []string{t}
		}
	}
	if len(chunks) == 0 {
		// Nothing embeddable: make sure no stale index is left for this page.
		if err := s.q.DeleteWikiChunksFrom(ctx, db.DeleteWikiChunksFromParams{PageID: p.ID, FromIndex: 0}); err != nil {
			return fmt.Errorf("wiki embed: clear page %s: %w", p.ID, err)
		}
		return nil
	}
	for start := 0; start < len(chunks); start += embedBatch {
		end := min(start+embedBatch, len(chunks))
		batch := chunks[start:end]
		// The title goes into the embedding input (context), but the stored chunk_text is the raw chunk.
		inputs := make([]string, len(batch))
		for i, c := range batch {
			inputs[i] = p.Title + "\n\n" + c
		}
		vecs, err := e.Embed(ctx, inputs)
		if err != nil {
			return fmt.Errorf("wiki embed: page %s: %w", p.ID, err)
		}
		for i, v := range vecs {
			if err := s.q.UpsertWikiChunk(ctx, db.UpsertWikiChunkParams{
				TenantID: p.TenantID, PageID: p.ID, SpaceID: p.SpaceID,
				ChunkIndex: int32(start + i), ChunkText: batch[i], Embedding: vecLiteral(v),
			}); err != nil {
				return fmt.Errorf("wiki embed: upsert chunk %d of page %s: %w", start+i, p.ID, err)
			}
		}
	}
	// Trims whatever is left over from a previous, larger version.
	if err := s.q.DeleteWikiChunksFrom(ctx, db.DeleteWikiChunksFromParams{
		PageID: p.ID, FromIndex: int32(len(chunks)),
	}); err != nil {
		return fmt.Errorf("wiki embed: trim page %s: %w", p.ID, err)
	}
	return nil
}

// SearchChunks embeds the question and returns the closest chunks THE USER IS ALLOWED TO SEE
// (the query applies tenant + visibility + ai_indexed + published). limit<=0 uses the default.
func (s *Service) SearchChunks(ctx context.Context, e Embedder, tenantID, userID uuid.UUID, query string, limit int32) ([]db.SearchWikiChunksRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalid
	}
	if limit <= 0 {
		limit = searchLimit
	}
	vecs, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("wiki search: embed query: %w", err)
	}
	// B9: hybrid search — 1st pass with an FTS pre-filter (cheap, GIN); if it returns nothing
	// (vocabulary different from the text, typical of questions), retry with semantic kNN only (recall).
	params := db.SearchWikiChunksParams{
		QueryEmbedding: vecLiteral(vecs[0]), TenantID: tenantID, UserID: userID,
		MaxResults: limit, UseFts: true, QueryText: query,
	}
	rows, err := s.q.SearchWikiChunks(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}
	params.UseFts = false
	return s.q.SearchWikiChunks(ctx, params)
}

// SearchPagesText is the LEXICAL search, direct against pages and with no embeddings. Serves
// installs without EMBEDDINGS_API_KEY: there, chunks don't exist (they're only born in embedPage),
// so the fallback can't search them. Matches words rather than meaning — enough for exact terms.
func (s *Service) SearchPagesText(ctx context.Context, tenantID, userID uuid.UUID, query string, limit int32) ([]db.SearchWikiPagesTextRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalid
	}
	if limit <= 0 {
		limit = searchLimit
	}
	return s.q.SearchWikiPagesText(ctx, db.SearchWikiPagesTextParams{
		TenantID: tenantID, UserID: userID, QueryText: query, MaxResults: limit,
	})
}
