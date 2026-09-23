package assistant

import (
	"context"
	"sync"
)

// runState is what a run needs to remember BETWEEN tool calls. Today just one thing: which files
// were read, and at what version. This is what backs the hard rule of §5.1 — `docs.edit` requires
// having read the file WITHIN THIS RUN, and writing over the version it read.
//
// It lives in the context, not in a signature: that way the tools that need it fetch it, and the
// ones that don't need it never even see it. It's created per run — never shared between runs, or
// a read from yesterday would authorize a write today.
type runState struct {
	mu   sync.Mutex
	read map[string]ReadMark
}

// ReadMark is what's known about a file at the moment it was read: the version and a fingerprint
// of the content. Writes are validated against this — if the file changed in the meantime, the
// version no longer matches and the write is refused.
type ReadMark struct {
	Version int32
	Hash    string
}

type runStateKey struct{}

// withRunState returns a context with a fresh read log. The engine calls it once per run.
func withRunState(ctx context.Context) context.Context {
	return context.WithValue(ctx, runStateKey{}, &runState{read: map[string]ReadMark{}})
}

// MarkRead records that a file was read within this run. Called by the read tool. A no-op when
// there's no state in the context: a simple synchronous run doesn't need to carry it.
func MarkRead(ctx context.Context, fileID string, mark ReadMark) {
	st, ok := ctx.Value(runStateKey{}).(*runState)
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.read[fileID] = mark
}

// ReadMarkFor returns what was read of a file within this run, if anything was read at all. The
// second value is false when the file was NOT read — and then the write has to be refused, not
// guessed.
func ReadMarkFor(ctx context.Context, fileID string) (ReadMark, bool) {
	st, ok := ctx.Value(runStateKey{}).(*runState)
	if !ok {
		return ReadMark{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	m, seen := st.read[fileID]
	return m, seen
}
