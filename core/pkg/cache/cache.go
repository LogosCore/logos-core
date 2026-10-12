package cache

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Cache provides key-value caching with tag-based invalidation.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	// SetNX sets key to value only if it does not already exist, returning true
	// when the key was newly set. Used for idempotency/dedup guards.
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	// IncrWithTTL atomically increments a counter and returns its new value,
	// applying expiration on the first increment so the window expires on its
	// own. The atomicity is the point: a read-then-write counter lets
	// concurrent callers each observe the same value and collectively exceed
	// whatever limit is being enforced.
	IncrWithTTL(ctx context.Context, key string, expiration time.Duration) (int64, error)
	// AppendAt appends chunk to the string at key, but only when the string is
	// exactly offset bytes long, and returns the new length. Resending the
	// chunk that ended at the current length is a no-op that succeeds, so a
	// retried append cannot duplicate data. A wrong offset fails with
	// *OffsetMismatchError, growing past maxBytes with ErrTooLarge. The TTL is
	// refreshed on every append.
	AppendAt(ctx context.Context, key string, offset int64, chunk string, maxBytes int64, expiration time.Duration) (int64, error)
	SetWithTags(ctx context.Context, key string, value interface{}, tags []string, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
	InvalidateCache(ctx context.Context, entityName string, entityID string) error
	Close() error
	IsEnabled() bool
}

// ErrTooLarge is returned by AppendAt when the chunk would grow the value past
// its cap.
var ErrTooLarge = errors.New("value would exceed its size cap")

// OffsetMismatchError is returned by AppendAt when the caller's offset is not
// the value's current length. Have is the length the caller should resume
// from.
type OffsetMismatchError struct {
	Have int64
}

func (e *OffsetMismatchError) Error() string {
	return fmt.Sprintf("offset does not match: the value is %d bytes long", e.Have)
}
