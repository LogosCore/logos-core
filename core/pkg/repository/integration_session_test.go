package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
)

// Session log queries, against a real server.
//
// The activeOnly view lists the rows whose session_id is in the live set read
// from Redis. It used to fetch that set in one unpaginated query and cap it at
// the page size, so every active session past the first page was unreachable.
// It now shares the history view's cursor path, which this pins: paging the
// live set sees every live row exactly once, in the history view's order.
func TestIntegrationSessionFilterPagesTheLiveSet(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewSessionRepository(db)
	clock := newSeedClock(time.Minute)

	alice, bob := uuid.New(), uuid.New()
	const total = 12
	var live []uuid.UUID
	liveOwnedBy := map[uuid.UUID]int{}
	for i := range total {
		owner := alice
		if i%3 == 0 {
			owner = bob
		}
		s := &models.Session{
			SessionID:    uuid.New(),
			UserID:       owner,
			DefaultField: createdAt(clock.next()),
		}
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		if i%2 == 0 {
			live = append(live, s.SessionID)
			liveOwnedBy[owner]++
		}
	}

	activeOnly := SessionFilter{SessionIDs: live}
	if n, err := repo.Count(ctx, activeOnly); err != nil || n != int64(len(live)) {
		t.Fatalf("Count(live) = %d, %v; want %d", n, err, len(live))
	}

	want, err := repo.FindWithCursor(ctx, activeOnly, nil, total+5, true)
	if err != nil {
		t.Fatalf("reference fetch: %v", err)
	}
	got := pageAll(t, integrationPageSize, len(live),
		func(cursor *pagination.Cursor, limit int64) ([]models.Session, error) {
			return repo.FindWithCursor(ctx, activeOnly, cursor, limit, true)
		},
		func(s *models.Session) string { return pagination.EncodeCursor(s.CreateAt, s.Id) },
	)
	assertNoDuplicates(t, "paged live sessions", sessionIDsOf(got))
	assertSameOrder(t, "paged vs single fetch", sessionIDsOf(got), sessionIDsOf(want))
	if len(got) != len(live) {
		t.Errorf("paging the live set returned %d rows, want all %d", len(got), len(live))
	}

	// Both constraints apply together: one user's live sessions.
	aliceLive := SessionFilter{UserIDs: []uuid.UUID{alice}, SessionIDs: live}
	if n, err := repo.Count(ctx, aliceLive); err != nil || n != int64(liveOwnedBy[alice]) {
		t.Errorf("Count(alice, live) = %d, %v; want %d", n, err, liveOwnedBy[alice])
	}

	// An empty live set must match nothing — never widen into every row.
	none := SessionFilter{SessionIDs: []uuid.UUID{}}
	if n, err := repo.Count(ctx, none); err != nil || n != 0 {
		t.Errorf("Count(empty live set) = %d, %v; want 0", n, err)
	}
	if rows, err := repo.FindWithCursor(ctx, none, nil, total, true); err != nil || len(rows) != 0 {
		t.Errorf("FindWithCursor(empty live set) = %d rows, %v; want 0", len(rows), err)
	}
	if n, err := repo.Count(ctx, SessionFilter{}); err != nil || n != total {
		t.Errorf("Count(zero filter) = %d, %v; want every row (%d)", n, err, total)
	}
}

func sessionIDsOf(rows []models.Session) []string {
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].SessionID.String()
	}
	return out
}
