package resolver

// BacklinkCounter lets a list resolver hand a page of credentials or hashes
// to the per-row backlinkCount field resolvers, so the page is counted in one
// aggregation instead of one per row.
//
// Registering is free: the list resolver only records which ids it returned.
// The aggregation runs the first time one of those rows asks for its count,
// with the query the asking resolver supplies — credential rows are only ever
// counted by CredentialBacklinkCount, hash rows by HashBacklinkCount — and the
// rest of the page waits for that one result. A query that never selects
// backlinkCount never pays for it.
//
// One instance per GraphQL operation, attached by the handler's
// AroundOperations hook. Without it (subscriptions, single-entity queries,
// rows no list registered) the field resolvers count one row at a time.

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type backlinkCounterKey struct{}

// countReferrersFunc counts, per id, the active wiki documents in opID that
// reference it — the shape of the repository's Count*ReferrersBatch methods.
type countReferrersFunc func(ctx context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]int64, error)

// BacklinkCounter maps each registered id to the batch it was registered in.
type BacklinkCounter struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*backlinkBatch
}

// backlinkBatch is one page's worth of ids from one operation, counted
// together at most once.
type backlinkBatch struct {
	opID   uuid.UUID
	ids    []uuid.UUID
	once   sync.Once
	counts map[uuid.UUID]int64
	err    error
}

// NewBacklinkCounter returns an empty counter ready to be attached to a
// context.
func NewBacklinkCounter() *BacklinkCounter {
	return &BacklinkCounter{byID: make(map[uuid.UUID]*backlinkBatch)}
}

// WithBacklinkCounter attaches c to ctx.
func WithBacklinkCounter(ctx context.Context, c *BacklinkCounter) context.Context {
	return context.WithValue(ctx, backlinkCounterKey{}, c)
}

// BacklinkCounterFromContext returns the request's counter, or nil. A nil
// counter is safe to use: it registers nothing and counts nothing.
func BacklinkCounterFromContext(ctx context.Context) *BacklinkCounter {
	c, _ := ctx.Value(backlinkCounterKey{}).(*BacklinkCounter)
	return c
}

// register records one operation's rows of a page to be counted together.
func (c *BacklinkCounter) register(opID uuid.UUID, ids []uuid.UUID) {
	if c == nil || len(ids) == 0 {
		return
	}
	b := &backlinkBatch{opID: opID, ids: ids}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		c.byID[id] = b
	}
}

// count returns id's count from its page's batch, running countBatch for the
// whole batch on first use. ok is false when no list registered id; the
// caller then counts it alone.
func (c *BacklinkCounter) count(ctx context.Context, id uuid.UUID, countBatch countReferrersFunc) (n int64, ok bool, err error) {
	if c == nil {
		return 0, false, nil
	}
	c.mu.Lock()
	b := c.byID[id]
	c.mu.Unlock()
	if b == nil {
		return 0, false, nil
	}
	b.once.Do(func() { b.counts, b.err = countBatch(ctx, b.opID, b.ids) })
	if b.err != nil {
		return 0, true, b.err
	}
	return b.counts[id], true, nil
}

// registerBacklinkPage hands a page of rows to the request's counter, one
// batch per operation on the page — MyCredentials and MyHashes pages span
// several.
func registerBacklinkPage[T any](ctx context.Context, rows []T, key func(*T) (opID, id uuid.UUID)) {
	c := BacklinkCounterFromContext(ctx)
	if c == nil || len(rows) == 0 {
		return
	}
	byOp := make(map[uuid.UUID][]uuid.UUID)
	for i := range rows {
		opID, id := key(&rows[i])
		byOp[opID] = append(byOp[opID], id)
	}
	for opID, ids := range byOp {
		c.register(opID, ids)
	}
}
