package resolver

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/auth"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/qiniu/qmgo/field"
)

// fakeTokenStore serves a fixed live set and counts reads. Unused methods
// panic through the nil embed.
type fakeTokenStore struct {
	auth.TokenStore
	live        map[uuid.UUID][]auth.ActiveSession // by user
	listByUser  int
	listAllLive int
}

func (f *fakeTokenStore) ListByUser(_ context.Context, userID uuid.UUID) ([]auth.ActiveSession, error) {
	f.listByUser++
	return f.live[userID], nil
}

func (f *fakeTokenStore) ListAllActive(context.Context) ([]auth.ActiveSession, error) {
	f.listAllLive++
	var all []auth.ActiveSession
	for _, s := range f.live {
		all = append(all, s...)
	}
	return all, nil
}

// fakeSessionRepo applies the filter in memory, newest first, and records
// what the resolver asked for. It ignores the cursor: keyset semantics are
// pinned against a real server in TestIntegrationSessionFilterPagesTheLiveSet.
type fakeSessionRepo struct {
	repository.ISessionRepository
	rows       []models.Session
	lastFilter repository.SessionFilter
	lastLimit  int64
}

func (f *fakeSessionRepo) match(filter repository.SessionFilter) []models.Session {
	var out []models.Session
	for _, r := range f.rows {
		if len(filter.UserIDs) > 0 && !slices.Contains(filter.UserIDs, r.UserID) {
			continue
		}
		if filter.SessionIDs != nil && !slices.Contains(filter.SessionIDs, r.SessionID) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreateAt.After(out[j].CreateAt) })
	return out
}

func (f *fakeSessionRepo) Count(_ context.Context, filter repository.SessionFilter) (int64, error) {
	return int64(len(f.match(filter))), nil
}

func (f *fakeSessionRepo) FindWithCursor(_ context.Context, filter repository.SessionFilter, _ *pagination.Cursor, limit int64, _ bool) ([]models.Session, error) {
	f.lastFilter, f.lastLimit = filter, limit
	rows := f.match(filter)
	if int64(len(rows)) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// seedSessions gives user n sessions, the first `live` of which are active.
func seedSessions(repo *fakeSessionRepo, store *fakeTokenStore, user uuid.UUID, n, live int) {
	base := time.Now().Add(-time.Hour)
	for i := range n {
		s := models.Session{
			SessionID:    uuid.New(),
			UserID:       user,
			DefaultField: field.DefaultField{CreateAt: base.Add(time.Duration(len(repo.rows)) * time.Minute)},
		}
		repo.rows = append(repo.rows, s)
		if i < live {
			store.live[user] = append(store.live[user], auth.ActiveSession{
				SessionID:      s.SessionID,
				LastActivityAt: base.Add(time.Duration(i) * time.Second),
			})
		}
	}
}

// TestMySessions_ActiveOnlyPaginates is the bug this path had: activeOnly
// returned the first `first` live sessions with hasNextPage=false and
// totalCount equal to the page length, so the rest were unreachable.
func TestMySessions_ActiveOnlyPaginates(t *testing.T) {
	me := uuid.New()
	repo := &fakeSessionRepo{}
	store := &fakeTokenStore{live: map[uuid.UUID][]auth.ActiveSession{}}
	seedSessions(repo, store, me, 8, 5) // 5 live of 8
	seedSessions(repo, store, uuid.New(), 3, 3)
	r := &sessionResolver{sessionRepo: repo, tokenStore: store}

	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: me.String()})
	activeOnly, first := true, 2
	conn, err := r.MySessions(ctx, &activeOnly, &first, nil, nil, nil)
	if err != nil {
		t.Fatalf("MySessions: %v", err)
	}

	if conn.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5 (every live session, not the page length)", conn.TotalCount)
	}
	if len(conn.Edges) != 2 || !conn.PageInfo.HasNextPage || conn.PageInfo.EndCursor == nil {
		t.Errorf("page = %d edges, hasNextPage=%v, endCursor set=%v; want 2, true, true",
			len(conn.Edges), conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor != nil)
	}
	if repo.lastLimit != 3 {
		t.Errorf("fetched %d rows, want first+1 = 3 so the surplus row can signal a next page", repo.lastLimit)
	}
	wantLive := make([]uuid.UUID, 0, 5)
	for _, a := range store.live[me] {
		wantLive = append(wantLive, a.SessionID)
	}
	if !sameUUIDSet(repo.lastFilter.SessionIDs, wantLive) {
		t.Errorf("query scoped to %v, want the caller's live set %v", repo.lastFilter.SessionIDs, wantLive)
	}
	for _, e := range conn.Edges {
		if e.Node.Status != models.SessionStatusActive || e.Node.LastActivityAt == nil {
			t.Errorf("session %s: status %q, lastActivityAt set=%v; want active with a timestamp",
				e.Node.SessionID, e.Node.Status, e.Node.LastActivityAt != nil)
		}
	}
	if store.listByUser != 1 {
		t.Errorf("ListByUser called %d times, want 1: the page is decorated from the snapshot it was scoped by", store.listByUser)
	}
}

func TestSessions_ActiveOnlyGlobalReadsEveryUser(t *testing.T) {
	repo := &fakeSessionRepo{}
	store := &fakeTokenStore{live: map[uuid.UUID][]auth.ActiveSession{}}
	seedSessions(repo, store, uuid.New(), 4, 2)
	seedSessions(repo, store, uuid.New(), 4, 3)
	r := &sessionResolver{sessionRepo: repo, tokenStore: store}

	activeOnly, first := true, 10
	conn, err := r.Sessions(context.Background(), nil, nil, &activeOnly, &first, nil, nil, nil)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if store.listAllLive != 1 || store.listByUser != 0 {
		t.Errorf("ListAllActive=%d ListByUser=%d, want one global read and no per-user reads",
			store.listAllLive, store.listByUser)
	}
	if conn.TotalCount != 5 || len(conn.Edges) != 5 || conn.PageInfo.HasNextPage {
		t.Errorf("got total=%d edges=%d next=%v, want 5, 5, false",
			conn.TotalCount, len(conn.Edges), conn.PageInfo.HasNextPage)
	}
}

func TestMySessions_ActiveOnlyWithNothingLive(t *testing.T) {
	me := uuid.New()
	repo := &fakeSessionRepo{}
	store := &fakeTokenStore{live: map[uuid.UUID][]auth.ActiveSession{}}
	seedSessions(repo, store, me, 3, 0)
	r := &sessionResolver{sessionRepo: repo, tokenStore: store}

	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: me.String()})
	activeOnly := true
	conn, err := r.MySessions(ctx, &activeOnly, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("MySessions: %v", err)
	}
	if repo.lastFilter.SessionIDs == nil {
		t.Error("an empty live set must reach the query as an empty list, not nil — nil means every session")
	}
	if conn.TotalCount != 0 || len(conn.Edges) != 0 {
		t.Errorf("got total=%d edges=%d, want an empty page", conn.TotalCount, len(conn.Edges))
	}
}

func TestMySessions_HistoryDecoratesFromRedis(t *testing.T) {
	me := uuid.New()
	repo := &fakeSessionRepo{}
	store := &fakeTokenStore{live: map[uuid.UUID][]auth.ActiveSession{}}
	seedSessions(repo, store, me, 4, 1)
	r := &sessionResolver{sessionRepo: repo, tokenStore: store}

	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: me.String()})
	conn, err := r.MySessions(ctx, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("MySessions: %v", err)
	}
	if repo.lastFilter.SessionIDs != nil {
		t.Errorf("history view scoped to sessions %v, want no session constraint", repo.lastFilter.SessionIDs)
	}
	active := 0
	for _, e := range conn.Edges {
		if e.Node.Status == models.SessionStatusActive {
			active++
		}
	}
	if conn.TotalCount != 4 || active != 1 {
		t.Errorf("got total=%d active=%d, want 4 rows with 1 active", conn.TotalCount, active)
	}
}

func sameUUIDSet(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !slices.Contains(b, id) {
			return false
		}
	}
	return true
}
