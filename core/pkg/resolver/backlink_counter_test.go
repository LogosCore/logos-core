package resolver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// referrerCounts is a fake Count*ReferrersBatch: it answers from want and
// records each call's operation and ids.
type referrerCounts struct {
	t     *testing.T
	want  map[uuid.UUID]int64
	opOf  map[uuid.UUID]uuid.UUID
	calls atomic.Int32
	err   error
}

func (f *referrerCounts) count(_ context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[uuid.UUID]int64, len(ids))
	for _, id := range ids {
		if op, known := f.opOf[id]; known && op != opID {
			f.t.Errorf("id %s counted under operation %s, but it belongs to %s", id, opID, op)
		}
		out[id] = f.want[id]
	}
	return out, nil
}

func TestBacklinkCount_OneAggregationPerOperationOfAPage(t *testing.T) {
	// A MyCredentials page spans operations, and gqlgen resolves its rows'
	// backlinkCount in parallel. Each operation's rows share one aggregation.
	ctx := WithBacklinkCounter(context.Background(), NewBacklinkCounter())
	opA, opB := uuid.New(), uuid.New()
	f := &referrerCounts{t: t, want: map[uuid.UUID]int64{}, opOf: map[uuid.UUID]uuid.UUID{}}

	var page []models.Credential
	for i := range 20 {
		op := opA
		if i%4 == 0 {
			op = opB
		}
		c := models.Credential{CredentialID: uuid.New(), OperationID: op}
		f.want[c.CredentialID] = int64(i)
		f.opOf[c.CredentialID] = op
		page = append(page, c)
	}
	registerBacklinkPage(ctx, page, func(c *models.Credential) (uuid.UUID, uuid.UUID) {
		return c.OperationID, c.CredentialID
	})

	var wg sync.WaitGroup
	for i := range page {
		wg.Add(1)
		go func(c models.Credential) {
			defer wg.Done()
			n, err := backlinkCount(ctx, c.OperationID, c.CredentialID, f.count, "credential")
			if err != nil || int64(n) != f.want[c.CredentialID] {
				t.Errorf("backlinkCount(%s) = %d, %v; want %d", c.CredentialID, n, err, f.want[c.CredentialID])
			}
		}(page[i])
	}
	wg.Wait()
	if got := f.calls.Load(); got != 2 {
		t.Errorf("a page across 2 operations made %d aggregations, want 2", got)
	}

	// A row no list registered — a single credential — is counted alone.
	loner := uuid.New()
	f.want[loner] = 7
	if n, err := backlinkCount(ctx, opA, loner, f.count, "credential"); err != nil || n != 7 {
		t.Errorf("unregistered row = %d, %v; want 7", n, err)
	}
	if got := f.calls.Load(); got != 3 {
		t.Errorf("unregistered row made %d aggregations in total, want 3", got)
	}
}

func TestBacklinkCount_WithoutACounterCountsAlone(t *testing.T) {
	// Subscriptions and MCP calls carry no counter; nothing may break.
	f := &referrerCounts{t: t, want: map[uuid.UUID]int64{}}
	id := uuid.New()
	f.want[id] = 3
	n, err := backlinkCount(context.Background(), uuid.New(), id, f.count, "hash")
	if err != nil || n != 3 || f.calls.Load() != 1 {
		t.Errorf("got %d, %v after %d calls; want 3, nil after 1", n, err, f.calls.Load())
	}
}

func TestBacklinkCount_BatchErrorReachesEveryRow(t *testing.T) {
	ctx := WithBacklinkCounter(context.Background(), NewBacklinkCounter())
	opID := uuid.New()
	f := &referrerCounts{t: t, err: errors.New("mongo timeout")}
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	BacklinkCounterFromContext(ctx).register(opID, ids)
	for _, id := range ids {
		if _, err := backlinkCount(ctx, opID, id, f.count, "credential"); err == nil {
			t.Errorf("row %s: a failed aggregation must surface as an error, not a zero count", id)
		}
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("made %d aggregations, want 1: a failed batch is not retried per row", got)
	}
}

// countingReferrerRepo is the wiki repository as the credential list sees it:
// only the batch count is real.
type countingReferrerRepo struct {
	repository.IWikiDocumentRepository
	aggregations atomic.Int32
}

func (f *countingReferrerRepo) CountCredentialReferrersBatch(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	f.aggregations.Add(1)
	out := make(map[uuid.UUID]int64, len(ids))
	for _, id := range ids {
		out[id] = 1
	}
	return out, nil
}

// TestCredentials_PageWarmsItsFieldResolvers wires the pieces the way the
// handler does and resolves a page like gqlgen: every row's backlinkCount and
// createdBy in parallel. The page must cost one aggregation and one user
// query per distinct creator at most — not one of each per row.
func TestCredentials_PageWarmsItsFieldResolvers(t *testing.T) {
	caller, opID := uuid.New(), uuid.New()
	creators := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	var page []models.Credential
	for i := range 20 {
		page = append(page, models.Credential{
			CredentialID: uuid.New(),
			OperationID:  opID,
			CreatedByID:  creators[i%len(creators)],
		})
	}

	var userReads atomic.Int32
	docs := &countingReferrerRepo{}
	r := &credentialResolver{
		credRepo: &mockCredRepo{
			countByOperationIDFn: func(context.Context, uuid.UUID, repository.CredentialFilter) (int64, error) {
				return int64(len(page)), nil
			},
			findByOperationIDWithCursorFn: func(context.Context, uuid.UUID, repository.CredentialFilter, repository.CredentialSort, *pagination.Cursor, int64, bool) ([]models.Credential, error) {
				return page, nil
			},
		},
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) {
			return memberOp(opID, caller, models.OperationRoleViewer), nil
		}},
		userRepo: &mockUserRepo{findByIDFn: func(_ context.Context, id uuid.UUID) (models.User, error) {
			userReads.Add(1)
			time.Sleep(2 * time.Millisecond) // a real read takes long enough for rows to overlap
			return models.User{UserID: id}, nil
		}},
		wikiDocRes: &wikiDocumentResolver{docRepo: docs},
	}

	ctx := newCallerCtx(caller)
	ctx = gqlctx.WithUserMemo(gqlctx.WithOperationMemo(ctx))
	ctx = WithBacklinkCounter(ctx, NewBacklinkCounter())
	first := 50
	conn, err := r.Credentials(ctx, opID.String(), nil, nil, nil, nil, nil, nil, nil, &first, nil, nil, nil)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}

	var wg sync.WaitGroup
	for _, e := range conn.Edges {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if n, err := r.BacklinkCount(ctx, e.Node); err != nil || n != 1 {
				t.Errorf("BacklinkCount = %d, %v; want 1", n, err)
			}
		}()
		go func() {
			defer wg.Done()
			if u, err := r.CreatedBy(ctx, e.Node); err != nil || u == nil || u.UserID != e.Node.CreatedByID {
				t.Errorf("CreatedBy = %v, %v; want %s", u, err, e.Node.CreatedByID)
			}
		}()
	}
	wg.Wait()

	if got := docs.aggregations.Load(); got != 1 {
		t.Errorf("a page of %d rows made %d backlink aggregations, want 1", len(page), got)
	}
	if got := userReads.Load(); got > int32(len(creators)) {
		t.Errorf("a page by %d creators read %d users, want at most %d", len(creators), got, len(creators))
	}
}
