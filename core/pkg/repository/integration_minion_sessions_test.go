package repository

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
)

// The Sessions table's sort and filters, against a real server: keyset pages
// over every sortable column, with missing values (null to Mongo, first going
// ascending, last going descending) and ties broken by _id, and each filter
// operation's answer, missing values included.
func TestIntegrationMinionSessionQueries(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("session indexes: %v", err)
	}

	op := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ago := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	rows := []models.MinionSession{
		{Hello: models.MinionHello{Host: "web01", User: "root", PID: 10, OS: "linux", Arch: "amd64", Addrs: []string{"10.0.0.5"}, Integrity: "root"}, LastSeenAt: ago(time.Minute), Staged: true},
		{Hello: models.MinionHello{Host: "web02", User: "www", PID: 20, OS: "linux", Arch: "386", Addrs: []string{"10.0.0.6", "172.17.0.1"}}, LastSeenAt: ago(3 * time.Hour)},
		{Hello: models.MinionHello{PID: 20, OS: "windows", Arch: "amd64"}, Staged: true}, // no host, never seen
		{Hello: models.MinionHello{Host: "DB01", User: "CORP\\svc", PID: 5, OS: "windows", Arch: "amd64", Integrity: "high"}, LastSeenAt: ago(time.Minute)},
		{Hello: models.MinionHello{Host: "web01", PID: 30, OS: "linux", Arch: "amd64"}, LastSeenAt: ago(48 * time.Hour), Staged: true},
		{Hello: models.MinionHello{PID: 1, OS: "darwin", Arch: "arm64"}}, // no host, never seen
		{Hello: models.MinionHello{Host: "mail", PID: 7, OS: "linux", Arch: "amd64", Addrs: []string{"192.168.1.9"}}, LastSeenAt: ago(10 * time.Second)},
	}
	soon, past, later, long := now.Add(time.Minute), now.Add(-time.Minute), now.Add(time.Hour), now.Add(-time.Hour)
	// 1 on time, 2 late, 4 lost.
	rows[0].LateAt, rows[0].LostAt = &soon, &later
	rows[1].LateAt, rows[1].LostAt = &past, &later
	rows[3].LateAt, rows[3].LostAt = &long, &past
	for i := range rows {
		r := &rows[i]
		r.SessionID, r.OperationID, r.BuildID = uuid.New(), op, uuid.New()
		r.Number, r.ModuleName, r.Target, r.Platform = int64(i+1), "bob", "linux-elf", "linux/amd64"
		r.CreatedAt = now.Add(time.Duration(i) * time.Second)
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	// Another operation's session is never in the list.
	if err := repo.Insert(ctx, &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), Number: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	// expected is rows in sort's order: nil first ascending, _id on ties,
	// the whole order reversed descending.
	expected := func(sort models.MinionSessionSort) []int64 {
		got := slices.Clone(rows)
		slices.SortStableFunc(got, func(a, b models.MinionSession) int {
			c := compareSortValues(sessionSortValue(&a, sort.Column), sessionSortValue(&b, sort.Column))
			if c == 0 {
				c = bytes.Compare(a.Id[:], b.Id[:])
			}
			if sort.Desc {
				return -c
			}
			return c
		})
		var out []int64
		for _, s := range got {
			out = append(out, s.Number)
		}
		return out
	}

	// Page through every sortable column both ways, two rows a page.
	for column, col := range models.MinionSessionColumns {
		if !col.Sortable {
			continue
		}
		for _, desc := range []bool{false, true} {
			sort := models.MinionSessionSort{Column: column, Desc: desc}
			q := MinionSessionQuery{Sort: sort}
			var got []int64
			var all []models.MinionSession
			var after *MinionSessionKeyset
			for range 10 {
				page, err := repo.FindByOperationIDWithCursor(ctx, op, q, after, 2, true)
				if err != nil {
					t.Fatalf("%v: %v", sort, err)
				}
				if len(page) == 0 {
					break
				}
				for _, s := range page {
					got = append(got, s.Number)
				}
				all = append(all, page...)
				if after, err = DecodeMinionSessionCursor(MinionSessionCursor(&page[len(page)-1], sort), sort); err != nil {
					t.Fatal(err)
				}
			}
			if want := expected(sort); !slices.Equal(got, want) {
				t.Errorf("%s desc=%v: %v, want %v", column, desc, got, want)
				continue
			}
			// Back from the last row: the two before it, in display order.
			before, _ := DecodeMinionSessionCursor(MinionSessionCursor(&all[len(all)-1], sort), sort)
			back, err := repo.FindByOperationIDWithCursor(ctx, op, q, before, 2, false)
			if err != nil || len(back) != 2 || back[0].Number != got[len(got)-3] || back[1].Number != got[len(got)-2] {
				t.Errorf("%s desc=%v back: %v %v, want %v", column, desc, numbers(back), err, got[len(got)-3:len(got)-1])
			}
		}
	}

	// A cursor of one sort does not page another.
	c := MinionSessionCursor(&rows[0], models.MinionSessionSort{Column: "host"})
	if _, err := DecodeMinionSessionCursor(c, models.MinionSessionSort{Column: "host", Desc: true}); err == nil {
		t.Error("a host-ascending cursor read for host descending")
	}

	secs := func(n int64) *int64 { return &n }
	num := func(n float64) *float64 { return &n }
	byNumber := models.MinionSessionSort{Column: "number"}
	for name, c := range map[string]struct {
		filters []models.MinionSessionFilter
		search  string
		want    []int64
	}{
		"host contains, case aside":                    {filters: []models.MinionSessionFilter{{Column: "host", Op: "CONTAINS", Text: "WEB"}}, want: []int64{1, 2, 5}},
		"host does not contain keeps none":             {filters: []models.MinionSessionFilter{{Column: "host", Op: "NOT_CONTAINS", Text: "web"}}, want: []int64{3, 4, 6, 7}},
		"host empty":                                   {filters: []models.MinionSessionFilter{{Column: "host", Op: "EMPTY"}}, want: []int64{3, 6}},
		"user not empty":                               {filters: []models.MinionSessionFilter{{Column: "user", Op: "NOT_EMPTY"}}, want: []int64{1, 2, 4}},
		"user with a dot of regex":                     {filters: []models.MinionSessionFilter{{Column: "user", Op: "CONTAINS", Text: "\\svc"}}, want: []int64{4}},
		"address contains":                             {filters: []models.MinionSessionFilter{{Column: "addrs", Op: "CONTAINS", Text: "172.17"}}, want: []int64{2}},
		"addresses empty":                              {filters: []models.MinionSessionFilter{{Column: "addrs", Op: "EMPTY"}}, want: []int64{3, 4, 5, 6}},
		"os in":                                        {filters: []models.MinionSessionFilter{{Column: "os", Op: "IN", Values: []string{"windows", "darwin"}}}, want: []int64{3, 4, 6}},
		"user contains the integrity":                  {filters: []models.MinionSessionFilter{{Column: "user", Op: "CONTAINS", Text: "high"}}, want: []int64{4}},
		"user does not contain root, nor runs as root": {filters: []models.MinionSessionFilter{{Column: "user", Op: "NOT_CONTAINS", Text: "root"}}, want: []int64{2, 3, 4, 5, 6, 7}},
		"staged":            {filters: []models.MinionSessionFilter{{Column: "staged", Op: "IN", Values: []string{"true"}}}, want: []int64{1, 3, 5}},
		"pid range":         {filters: []models.MinionSessionFilter{{Column: "pid", Op: "RANGE", Min: num(5), Max: num(20)}}, want: []int64{1, 2, 3, 4, 7}},
		"pid at least":      {filters: []models.MinionSessionFilter{{Column: "pid", Op: "RANGE", Min: num(21)}}, want: []int64{5}},
		"seen within 5m":    {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "WITHIN", Seconds: secs(300)}}, want: []int64{1, 4, 7}},
		"seen over 1h ago":  {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "OLDER_THAN", Seconds: secs(3600)}}, want: []int64{2, 5}},
		"live":              {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "LIVE"}}, want: []int64{1}},
		"late":              {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "LATE"}}, want: []int64{2}},
		"lost":              {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "LOST"}}, want: []int64{4}},
		"never seen":        {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "NEVER"}}, want: []int64{3, 6}},
		"seen in a range":   {filters: []models.MinionSessionFilter{{Column: "lastSeen", Op: "RANGE", From: ago(4 * time.Hour), To: ago(time.Hour)}}, want: []int64{2}},
		"session id":        {filters: []models.MinionSessionFilter{{Column: "sessionId", Op: "EQUALS", Text: rows[3].SessionID.String()}}, want: []int64{4}},
		"two filters":       {filters: []models.MinionSessionFilter{{Column: "os", Op: "IN", Values: []string{"linux"}}, {Column: "host", Op: "CONTAINS", Text: "web"}}, want: []int64{1, 2, 5}},
		"filter and search": {filters: []models.MinionSessionFilter{{Column: "os", Op: "IN", Values: []string{"linux"}}}, search: "mail", want: []int64{7}},
	} {
		q := MinionSessionQuery{Search: c.search, Filters: c.filters, Sort: byNumber}
		if err := q.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		page, err := repo.FindByOperationIDWithCursor(ctx, op, q, nil, 50, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		n, _ := repo.CountByOperationID(ctx, op, q)
		if got := numbers(page); !slices.Equal(got, c.want) || n != int64(len(c.want)) {
			t.Errorf("%s: %v (count %d), want %v", name, got, n, c.want)
		}
	}

	values, err := repo.Values(ctx, op, "os")
	if err != nil || !slices.Equal(values, []string{"darwin", "linux", "windows"}) {
		t.Errorf("os values %v %v", values, err)
	}
	if values, _ := repo.Values(ctx, op, "arch"); !slices.Equal(values, []string{"386", "amd64", "arm64"}) {
		t.Errorf("arch values %v", values)
	}
	if values, _ := repo.Values(ctx, op, "staged"); !slices.Equal(values, []string{"false", "true"}) {
		t.Errorf("staged values %v", values)
	}
	if _, err := repo.Values(ctx, op, "host"); err == nil {
		t.Error("values of a text column")
	}
}

func numbers(rows []models.MinionSession) []int64 {
	out := []int64{}
	for _, s := range rows {
		out = append(out, s.Number)
	}
	return out
}

// compareSortValues orders sort values as Mongo does for one column: nil
// first.
func compareSortValues(a, b any) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	switch a := a.(type) {
	case string:
		return cmp.Compare(a, b.(string))
	case int64:
		return cmp.Compare(a, b.(int64))
	case time.Time:
		return a.Compare(b.(time.Time))
	case bool:
		switch {
		case a == b.(bool):
			return 0
		case !a:
			return -1
		}
		return 1
	}
	panic("unknown sort value")
}

// A check-in records where it came from, in one update inside Mongo: the
// current address, and the history, most recent first, counted, capped.
func TestIntegrationMinionSessionPeers(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	op, sid := uuid.New(), uuid.New()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	expires := t0.Add(time.Hour)
	if err := repo.Insert(ctx, &models.MinionSession{SessionID: sid, OperationID: op, Number: 1, CreatedAt: t0, PurgeAt: &expires}); err != nil {
		t.Fatal(err)
	}
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	for i, peer := range []string{"203.0.113.7", "203.0.113.7", "198.51.100.2", ""} {
		if err := repo.CheckIn(ctx, sid, MinionSessionCheckIn{At: at(i + 1), Peer: peer}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := repo.FindBySessionID(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if s.PeerAddress != "198.51.100.2" || s.PurgeAt != nil || !s.LastSeenAt.Equal(at(4)) {
		t.Fatalf("session %q expires=%v last seen %v", s.PeerAddress, s.PurgeAt, s.LastSeenAt)
	}
	if len(s.Peers) != 2 {
		t.Fatalf("peers %+v", s.Peers)
	}
	b, a := s.Peers[0], s.Peers[1]
	if b.Address != "198.51.100.2" || b.Count != 1 || a.Address != "203.0.113.7" || a.Count != 2 ||
		!a.FirstSeen.Equal(at(1)) || !a.LastSeen.Equal(at(2)) {
		t.Fatalf("peers %+v", s.Peers)
	}

	// Capped, the oldest dropped.
	for i := range models.MaxSessionPeers + 1 {
		if err := repo.CheckIn(ctx, sid, MinionSessionCheckIn{At: at(10 + i), Peer: fmt.Sprintf("10.9.0.%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	s, _ = repo.FindBySessionID(ctx, sid)
	if len(s.Peers) != models.MaxSessionPeers || s.Peers[0].Address != fmt.Sprintf("10.9.0.%d", models.MaxSessionPeers) {
		t.Fatalf("%d peers, newest %+v", len(s.Peers), s.Peers[0])
	}

	// The column filters on any address it was seen from.
	q := MinionSessionQuery{Sort: models.MinionSessionSort{Column: "peer"}, Filters: []models.MinionSessionFilter{{Column: "peer", Op: "CONTAINS", Text: "10.9.0.5"}}}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.CountByOperationID(ctx, op, q); n != 1 {
		t.Errorf("filter on the history: %d", n)
	}
}

// The list holds open sessions only; a closed one stays as a tombstone, its
// keys there for the minion to be told, until MongoDB purges it.
func TestIntegrationMinionSessionTombstones(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	op := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	seen := now.Add(-time.Hour)
	insert := func(n int64, created time.Time, lastSeen *time.Time, os string) uuid.UUID {
		t.Helper()
		s := &models.MinionSession{SessionID: uuid.New(), OperationID: op, Number: n, CreatedAt: created, LastSeenAt: lastSeen,
			Hello: models.MinionHello{OS: os}}
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatal(err)
		}
		return s.SessionID
	}
	insert(1, now.Add(-48*time.Hour), &seen, "linux") // checked in long ago: open
	insert(2, now.Add(-48*time.Hour), nil, "windows") // expired waiting
	insert(3, now.Add(-time.Hour), nil, "darwin")     // still waiting: open
	q := MinionSessionQuery{Sort: models.MinionSessionSort{Column: "number"}}
	list := func() []int64 {
		page, err := repo.FindByOperationIDWithCursor(ctx, op, q, nil, 50, true)
		if err != nil {
			t.Fatal(err)
		}
		return numbers(page)
	}
	if got := list(); !slices.Equal(got, []int64{1, 3}) {
		t.Fatalf("open sessions %v", got)
	}
	if values, _ := repo.Values(ctx, op, "os"); !slices.Equal(values, []string{"darwin", "linux"}) {
		t.Errorf("values of open sessions %v", values)
	}

	if err := repo.CloseByOperationID(ctx, op, models.MinionSessionClosedOperationDeleted, now); err != nil {
		t.Fatal(err)
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("after closing: %v", got)
	}
	tomb, err := repo.FindBySessionID(ctx, insert(4, now, &seen, "linux"))
	if err != nil || tomb.Closed(now) {
		t.Fatalf("a new session after the close: %+v %v", tomb, err)
	}
	page, _ := repo.FindByOperationIDWithCursor(ctx, op, MinionSessionQuery{Sort: q.Sort}, nil, 50, true)
	if len(page) != 1 {
		t.Fatalf("listed %v", numbers(page))
	}
	n, _ := repo.CountByOperationID(ctx, op, q)
	if n != 1 {
		t.Errorf("count %d", n)
	}
	raw, _ := db.Collection(minionSessionCollection).(*database.QmgoCollection).RawCollection()
	var closed models.MinionSession
	if err := raw.FindOne(ctx, bson.M{"operation_id": op, "number": 1}).Decode(&closed); err != nil {
		t.Fatal(err)
	}
	if closed.ClosedAt == nil || closed.CloseReason != models.MinionSessionClosedOperationDeleted || closed.PurgeAt == nil ||
		!closed.PurgeAt.Equal(now.Add(models.MinionSessionTombstone)) || !closed.Closed(now) {
		t.Errorf("tombstone %+v", closed)
	}
}

// A claim, in one update inside Mongo: the claim's keys in, the old ones
// kept as replaced, the restart counted, what the minion sent stored as it
// is (a "$" in it is no field path), and a closed session not claimed.
func TestIntegrationMinionSessionClaim(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	op, sid, b2 := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	k1 := protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: []byte(`{"k":1}`)}
	k2 := protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: []byte(`{"k":2}`)}
	if err := repo.Insert(ctx, &models.MinionSession{SessionID: sid, OperationID: op, Number: 1, CreatedAt: now, LastSeenAt: &now, Crypto: k1}); err != nil {
		t.Fatal(err)
	}
	claim := MinionSessionClaim{At: now, BuildID: b2, ModuleName: "bob", Target: "linux-elf", Platform: "linux/amd64", Crypto: k2,
		Hello: models.MinionHello{PID: 4242, OS: "linux", Arch: "amd64", Host: "$host"}, Address: "$addr"}
	for range 2 {
		if err := repo.Claim(ctx, sid, claim); err != nil {
			t.Fatal(err)
		}
	}
	s, err := repo.FindBySessionID(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if string(s.Crypto.Material) != `{"k":2}` || s.BuildID != b2 || s.Hello.Host != "$host" || s.Address != "$addr" ||
		!s.Persisted || s.RestartCount != 2 || len(s.Restarts) != 2 || s.Restarts[0].PID != 4242 {
		t.Fatalf("claimed %+v", s)
	}
	if len(s.ReplacedKeys) != 2 || string(s.ReplacedKeys[1].Crypto.Material) != `{"k":1}` {
		t.Fatalf("replaced keys %+v", s.ReplacedKeys)
	}
	for range models.MaxSessionReplacedKeys + 2 {
		_ = repo.Claim(ctx, sid, claim)
	}
	if s, _ = repo.FindBySessionID(ctx, sid); len(s.ReplacedKeys) != models.MaxSessionReplacedKeys {
		t.Fatalf("%d replaced keys kept", len(s.ReplacedKeys))
	}

	if err := repo.CloseByOperationID(ctx, op, models.MinionSessionClosedOperationDeleted, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Claim(ctx, sid, claim); !IsNotFound(err) {
		t.Fatalf("a closed session claimed: %v", err)
	}
}

// Reservations: one claim takes one, and unclaimed ones go with their build
// or operation.
func TestIntegrationMinionSessionReservations(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionReservationRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("reservation indexes: %v", err)
	}
	op, b1, b2 := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, b := range []uuid.UUID{b1, b1, b2} {
		if err := repo.Insert(ctx, &models.MinionSessionReservation{SessionID: ids[i], OperationID: op, BuildID: b, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Insert(ctx, &models.MinionSessionReservation{SessionID: ids[0], OperationID: op, BuildID: b2}); err == nil {
		t.Fatal("a session id reserved twice")
	}
	if err := repo.Delete(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, ids[0]); !IsNotFound(err) {
		t.Fatalf("a second claim took it too: %v", err)
	}
	if err := repo.DeleteByBuildID(ctx, b1); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBySessionID(ctx, ids[1]); !IsNotFound(err) {
		t.Fatalf("build b1's reservation stayed: %v", err)
	}
	if r, err := repo.FindBySessionID(ctx, ids[2]); err != nil || r.BuildID != b2 {
		t.Fatalf("build b2's reservation: %+v %v", r, err)
	}
	if err := repo.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBySessionID(ctx, ids[2]); !IsNotFound(err) {
		t.Fatal("the operation's reservation stayed")
	}
}

// A check-in stores a schedule and late_at, with a peer address (the
// pipeline) or without; a claim whose hello says none drops them.
func TestIntegrationMinionSessionSchedule(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	op, sid := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Insert(ctx, &models.MinionSession{SessionID: sid, OperationID: op, Number: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	sleep := &models.MinionSessionSleep{Interval: 60_000, Jitter: 20}
	for _, peer := range []string{"203.0.113.7", ""} {
		late, lost := sleep.LateAtFrom(now), sleep.LostAtFrom(now)
		if err := repo.CheckIn(ctx, sid, MinionSessionCheckIn{At: now, Peer: peer, Sleep: sleep, LateAt: &late, LostAt: &lost}); err != nil {
			t.Fatal(err)
		}
		s, _ := repo.FindBySessionID(ctx, sid)
		if s.Sleep == nil || *s.Sleep != *sleep || s.LateAt == nil || !s.LateAt.Equal(late) || s.LostAt == nil || !s.LostAt.Equal(lost) {
			t.Fatalf("peer %q: %+v late %v", peer, s.Sleep, s.LateAt)
		}
		sleep = &models.MinionSessionSleep{Interval: 3_600_000}
	}

	if err := repo.Claim(ctx, sid, MinionSessionClaim{At: now, BuildID: uuid.New()}); err != nil {
		t.Fatal(err)
	}
	if s, _ := repo.FindBySessionID(ctx, sid); s.Sleep != nil || s.LateAt != nil || s.LostAt != nil {
		t.Fatalf("after a claim without one: %+v %v", s.Sleep, s.LateAt)
	}
}

// Sessions from before lost_at get theirs from late_at and their schedule.
func TestIntegrationMinionSessionBackfillLostAt(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	sleep := &models.MinionSessionSleep{Interval: 60_000, Jitter: 20}
	late := sleep.LateAtFrom(now)
	old := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), Number: 1, CreatedAt: now, Sleep: sleep, LateAt: &late}
	bare := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), Number: 1, CreatedAt: now}
	for _, s := range []*models.MinionSession{old, bare} {
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	n, err := repo.BackfillLostAt(ctx)
	if err != nil || n != 1 {
		t.Fatalf("backfilled %d: %v", n, err)
	}
	s, _ := repo.FindBySessionID(ctx, old.SessionID)
	if s.LostAt == nil || !s.LostAt.Equal(sleep.LostAtFrom(now)) {
		t.Fatalf("lost at %v, want %v", s.LostAt, sleep.LostAtFrom(now))
	}
	if s, _ := repo.FindBySessionID(ctx, bare.SessionID); s.LostAt != nil {
		t.Fatal("a session without a schedule got lost_at")
	}
	if n, _ := repo.BackfillLostAt(ctx); n != 0 {
		t.Fatalf("backfilled %d again", n)
	}
}

// Archiving hides a session from the list, the archived list holds it, and a
// check-in (with or without a peer address) brings it back.
func TestIntegrationMinionSessionArchive(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	op := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range ids {
		if err := repo.Insert(ctx, &models.MinionSession{SessionID: id, OperationID: op, Number: int64(i + 1), CreatedAt: now, LastSeenAt: &now}); err != nil {
			t.Fatal(err)
		}
	}
	list := func(archived bool) []int64 {
		page, err := repo.FindByOperationIDWithCursor(ctx, op, MinionSessionQuery{Archived: archived, Sort: models.MinionSessionSort{Column: "number"}}, nil, 10, true)
		if err != nil {
			t.Fatal(err)
		}
		return numbers(page)
	}
	if err := repo.SetArchived(ctx, ids[0], true); err != nil {
		t.Fatal(err)
	}
	if got, arch := list(false), list(true); !slices.Equal(got, []int64{2}) || !slices.Equal(arch, []int64{1}) {
		t.Fatalf("listed %v, archived %v", got, arch)
	}
	for _, peer := range []string{"203.0.113.7", ""} {
		if err := repo.SetArchived(ctx, ids[0], true); err != nil {
			t.Fatal(err)
		}
		if err := repo.CheckIn(ctx, ids[0], MinionSessionCheckIn{At: now, Peer: peer}); err != nil {
			t.Fatal(err)
		}
		if got := list(false); !slices.Equal(got, []int64{1, 2}) {
			t.Fatalf("after a check-in (peer %q): %v", peer, got)
		}
	}
	if err := repo.SetArchived(ctx, ids[1], true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetArchived(ctx, ids[1], false); err != nil {
		t.Fatal(err)
	}
	if got := list(true); len(got) != 0 {
		t.Fatalf("restored but archived: %v", got)
	}
}

// Bulk archiving: by ids, and by what a list shows (its view and filters),
// in one write each, never past its operation.
func TestIntegrationMinionSessionArchiveMany(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	op, other := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.New()
		os := "linux"
		if i >= 3 {
			os = "windows"
		}
		if err := repo.Insert(ctx, &models.MinionSession{SessionID: ids[i], OperationID: op, Number: int64(i + 1), CreatedAt: now,
			LastSeenAt: &now, Hello: models.MinionHello{OS: os}}); err != nil {
			t.Fatal(err)
		}
	}
	foreign := uuid.New()
	if err := repo.Insert(ctx, &models.MinionSession{SessionID: foreign, OperationID: other, Number: 1, CreatedAt: now, LastSeenAt: &now}); err != nil {
		t.Fatal(err)
	}
	list := func(archived bool) []int64 {
		page, _ := repo.FindByOperationIDWithCursor(ctx, op, MinionSessionQuery{Archived: archived, Sort: models.MinionSessionSort{Column: "number"}}, nil, 10, true)
		return numbers(page)
	}

	// Another operation's id in the list changes nothing there.
	n, err := repo.SetArchivedMany(ctx, op, []uuid.UUID{ids[0], ids[1], foreign}, true)
	if err != nil || n != 2 {
		t.Fatalf("by ids: %d %v", n, err)
	}
	if f, _ := repo.FindBySessionID(ctx, foreign); f.Archived {
		t.Fatal("another operation's session was archived")
	}
	// Every live windows session.
	q := MinionSessionQuery{Filters: []models.MinionSessionFilter{{Column: "os", Op: "IN", Values: []string{"windows"}}}, Sort: models.DefaultMinionSessionSort}
	if n, err = repo.SetArchivedMatching(ctx, op, q, true); err != nil || n != 2 {
		t.Fatalf("matching: %d %v", n, err)
	}
	if got, arch := list(false), list(true); !slices.Equal(got, []int64{3}) || !slices.Equal(arch, []int64{1, 2, 4, 5}) {
		t.Fatalf("live %v, archived %v", got, arch)
	}
	// Restore every archived one.
	if n, err = repo.SetArchivedMatching(ctx, op, MinionSessionQuery{Archived: true, Sort: models.DefaultMinionSessionSort}, false); err != nil || n != 4 {
		t.Fatalf("restore matching: %d %v", n, err)
	}
	if got := list(false); len(got) != 5 {
		t.Fatalf("after restoring: %v", got)
	}
}

// A session's description: an operator's replaces it, clearing included, and
// the backfill fills only sessions that never had one given.
func TestIntegrationMinionSessionDescription(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	now := time.Now().UTC()
	op := uuid.New()
	old, cleared, fresh := uuid.New(), uuid.New(), uuid.New()
	for i, s := range []*models.MinionSession{
		{SessionID: old, BuildDescription: "from the build"},
		{SessionID: cleared, BuildDescription: "from the build"},
		{SessionID: fresh, BuildDescription: "from the build", Description: "kept", DescriptionSet: true},
	} {
		s.OperationID, s.Number, s.CreatedAt = op, int64(i+1), now
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SetDescription(ctx, cleared, ""); err != nil {
		t.Fatal(err)
	}
	n, err := repo.BackfillDescriptions(ctx)
	if err != nil || n != 1 {
		t.Fatalf("backfilled %d: %v", n, err)
	}
	for id, want := range map[uuid.UUID]string{old: "from the build", cleared: "", fresh: "kept"} {
		if s, _ := repo.FindBySessionID(ctx, id); s.Description != want {
			t.Errorf("%s: %q, want %q", id, s.Description, want)
		}
	}
	if err := repo.SetDescription(ctx, old, "the DC's backup box"); err != nil {
		t.Fatal(err)
	}
	if s, _ := repo.FindBySessionID(ctx, old); s.Description != "the DC's backup box" {
		t.Fatalf("edited: %q", s.Description)
	}
	q := MinionSessionQuery{Filters: []models.MinionSessionFilter{{Column: "description", Op: "CONTAINS", Text: "backup"}}, Sort: models.DefaultMinionSessionSort}
	if n, _ := repo.CountByOperationID(ctx, op, q); n != 1 {
		t.Fatalf("filter on description: %d", n)
	}
}

// Marks are each operator's own; marking again replaces the fields, an
// empty mark removes it, and a bulk mark marks many alike.
func TestIntegrationMinionSessionMarks(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	marks := NewMinionSessionMarkRepository(db)
	sessions := NewMinionSessionRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("mark indexes: %v", err)
	}
	op, alice, bob := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		if err := sessions.Insert(ctx, &models.MinionSession{SessionID: id, OperationID: op, Number: int64(i + 1), CreatedAt: now, LastSeenAt: &now}); err != nil {
			t.Fatal(err)
		}
	}

	if err := marks.SetMany(ctx, alice, op, ids[:1], "teal-medium", "Crosshair", ""); err != nil {
		t.Fatal(err)
	}
	// Again, an emoji instead of the icon: the icon goes.
	if err := marks.SetMany(ctx, alice, op, ids[:1], "teal-medium", "", "🎯"); err != nil {
		t.Fatal(err)
	}
	got, _ := marks.ForSessions(ctx, alice, ids)
	if m := got[ids[0]]; len(got) != 1 || m.Color != "teal-medium" || m.Icon != "" || m.Emoji != "🎯" {
		t.Fatalf("alice's marks %+v", got)
	}
	if theirs, _ := marks.ForSessions(ctx, bob, ids); len(theirs) != 0 {
		t.Fatalf("bob sees %+v", theirs)
	}

	// Bulk, then cleared.
	if err := marks.SetMany(ctx, bob, op, ids, "red-strong", "", ""); err != nil {
		t.Fatal(err)
	}
	if theirs, _ := marks.ForSessions(ctx, bob, ids); len(theirs) != 3 {
		t.Fatalf("bob's bulk marks %d", len(theirs))
	}
	if err := marks.SetMany(ctx, bob, op, ids[1:], "", "", ""); err != nil {
		t.Fatal(err)
	}
	if theirs, _ := marks.ForSessions(ctx, bob, ids); len(theirs) != 1 {
		t.Fatalf("after clearing two: %d", len(theirs))
	}

	// What a bulk change names: the operation's own ids, or what a list matches.
	own, _ := sessions.OwnSessionIDs(ctx, op, []uuid.UUID{ids[0], uuid.New()})
	if len(own) != 1 || own[0] != ids[0] {
		t.Fatalf("own %v", own)
	}
	all, _ := sessions.SessionIDs(ctx, op, MinionSessionQuery{Sort: models.DefaultMinionSessionSort}, 2)
	if len(all) != 2 {
		t.Fatalf("matching ids, at most 2: %v", all)
	}

	if err := marks.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if left, _ := marks.ForSessions(ctx, alice, ids); len(left) != 0 {
		t.Fatal("marks outlived their operation")
	}

	// Orphans: a mark on a session gone (purged by its TTL), and one by an
	// operator gone (bob has no user row here); alice's on a live session
	// stays.
	if _, err := db.Collection(userCollection).InsertOne(ctx, bson.M{"user_id": alice}); err != nil {
		t.Fatal(err)
	}
	gone := uuid.New()
	for _, m := range []struct {
		user    uuid.UUID
		session uuid.UUID
	}{{alice, ids[0]}, {alice, gone}, {bob, ids[0]}} {
		if err := marks.SetMany(ctx, m.user, op, []uuid.UUID{m.session}, "red-soft", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := marks.RemoveOrphans(ctx); err != nil || n != 2 {
		t.Fatalf("orphans removed: %d, %v", n, err)
	}
	if left, _ := marks.ForSessions(ctx, alice, []uuid.UUID{ids[0], gone}); len(left) != 1 || left[ids[0]].Color != "red-soft" {
		t.Fatalf("alice's marks after the sweep: %+v", left)
	}
	if n, _ := marks.RemoveOrphans(ctx); n != 0 {
		t.Fatalf("a second sweep removed %d", n)
	}
}

// Process paths stored before core kept a hello's proc as a name become
// names, in either OS's form; a name, or nothing, stays.
func TestIntegrationMinionSessionProcessNames(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionSessionRepository(db)
	now := time.Now().UTC()
	op := uuid.New()
	want := map[string]string{
		"/home/user/Downloads/bob-linux-elf": "bob-linux-elf",
		`C:\Users\svc\AppData\updater.exe`:   "updater.exe",
		"svchost.exe":                        "svchost.exe",
		"":                                   "",
	}
	ids := map[uuid.UUID]string{}
	i := 0
	for proc := range want {
		id := uuid.New()
		ids[id] = proc
		i++
		s := &models.MinionSession{SessionID: id, OperationID: op, Number: int64(i), CreatedAt: now, Hello: models.MinionHello{Proc: proc}}
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := repo.BackfillProcessNames(ctx); err != nil || n != 2 {
		t.Fatalf("backfilled %d: %v", n, err)
	}
	for id, proc := range ids {
		if s, _ := repo.FindBySessionID(ctx, id); s.Hello.Proc != want[proc] {
			t.Errorf("%q became %q, want %q", proc, s.Hello.Proc, want[proc])
		}
	}
	if n, _ := repo.BackfillProcessNames(ctx); n != 0 {
		t.Fatalf("a second run changed %d", n)
	}
}
