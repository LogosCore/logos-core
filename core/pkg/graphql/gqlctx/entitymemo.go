package gqlctx

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
)

// Request-scoped identity maps.
//
// gqlgen invokes a field resolver once per row, so a list response resolves
// `createdBy` N times, `operation` N times, and so on — almost always over a
// handful of distinct ids. These memos make the repeated ids free within one
// operation, and Prime lets a list resolver that already knows which ids it
// will need fetch them in a single batch up front.
//
// Scope is one GraphQL operation, attached by the handler's AroundOperations
// hook. Nothing is cached between operations, so a membership or profile change
// is visible on the next call exactly as it was before any of this existed.
// Subscriptions deliberately get no memo: they are long-lived, and a cache that
// lives as long as the socket would keep serving what it saw at connect time.

// entityMemo is the identity map for one entity type.
type entityMemo[T any] struct {
	mu    sync.Mutex
	items map[uuid.UUID]*memoEntry[T]
}

// memoEntry is one id's slot. It is created before the fetch starts and done
// closes once v and err are final, so concurrent loads of an id wait on the
// first fetch instead of each issuing their own. That is the common case, not
// an edge: gqlgen resolves list rows in parallel, so every row of a page asks
// for its createdBy at the same moment, before any of them could fill a slot.
type memoEntry[T any] struct {
	done chan struct{}
	v    T
	err  error
}

// errFetchPanicked is what callers waiting on a fetch see if it panicked, so
// they fail instead of reading the zero value as a result. The panic itself
// still propagates from the fetching call.
var errFetchPanicked = errors.New("memo: fetch panicked")

// memoKey is generic so that each entity type gets its own context key without
// a registry of key types: memoKey[models.User] and memoKey[models.Operation]
// instantiate to distinct types, so they cannot collide in one context.
type memoKey[T any] struct{}

// withMemo attaches a fresh memo for T. Calling it twice for the same T
// replaces the memo, which is why it belongs at a request boundary only.
func withMemo[T any](ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKey[T]{}, &entityMemo[T]{items: map[uuid.UUID]*memoEntry[T]{}})
}

// loadEntity returns the memoised value for id, fetching it on a miss. A load
// that arrives while another is fetching the same id waits for that result
// rather than fetching again. With no memo on the context it degrades to a
// plain fetch, so a caller is never wrong to use it — it just gains nothing
// outside a request.
//
// Errors are never memoised: a transient failure must not poison the rest of
// the operation, and a not-found must not be cached as if it were a fact about
// a row that may appear later in the same request. Loads already waiting on a
// failed fetch share its error; the next one after it fetches again.
func loadEntity[T any](ctx context.Context, id uuid.UUID, fetch func(context.Context, uuid.UUID) (T, error)) (T, error) {
	memo, _ := ctx.Value(memoKey[T]{}).(*entityMemo[T])
	if memo == nil {
		return fetch(ctx, id)
	}

	memo.mu.Lock()
	e, inFlight := memo.items[id]
	if !inFlight {
		e = &memoEntry[T]{done: make(chan struct{})}
		memo.items[id] = e
	}
	memo.mu.Unlock()

	if inFlight {
		select {
		case <-e.done:
			return e.v, e.err
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}

	fetched := false
	defer func() {
		if !fetched {
			e.err = errFetchPanicked
		}
		if e.err != nil {
			memo.mu.Lock()
			if memo.items[id] == e { // not replaced by a prime meanwhile
				delete(memo.items, id)
			}
			memo.mu.Unlock()
		}
		close(e.done)
	}()
	e.v, e.err = fetch(ctx, id)
	fetched = true
	return e.v, e.err
}

// primeEntity seeds one value, so a batch fetch can fill the memo before the
// field resolvers run. Existing entries are overwritten: a fresher read of the
// same row within one operation is not a conflict worth preserving. A load
// already waiting on an in-flight fetch keeps waiting for that fetch.
func primeEntity[T any](ctx context.Context, id uuid.UUID, v T) {
	memo, _ := ctx.Value(memoKey[T]{}).(*entityMemo[T])
	if memo == nil {
		return
	}
	e := &memoEntry[T]{done: make(chan struct{}), v: v}
	close(e.done)
	memo.mu.Lock()
	memo.items[id] = e
	memo.mu.Unlock()
}

// memoMissing returns the subset of ids the memo has no entry for, so a
// preloader can skip a round trip when everything is already cached or being
// fetched. With no memo attached — a resolver called outside a GraphQL
// operation, as the MCP tools do — there is nothing to warm, so nothing is
// reported missing and the preloader makes no query at all.
func memoMissing[T any](ctx context.Context, ids []uuid.UUID) []uuid.UUID {
	memo, _ := ctx.Value(memoKey[T]{}).(*entityMemo[T])
	if memo == nil {
		return nil
	}

	memo.mu.Lock()
	defer memo.mu.Unlock()
	missing := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if _, hit := memo.items[id]; !hit {
			missing = append(missing, id)
		}
	}
	return missing
}
