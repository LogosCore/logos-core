package miniontunnels

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

// fakeMemory keeps uses and heat as the repository does: a use by its
// session, purpose, target and port; heat by port.
type fakeMemory struct {
	mu      sync.Mutex
	uses    []models.OperationalPortUse
	heat    map[int]models.OperationalPortHeat
	heatErr error
}

func (f *fakeMemory) Record(_ context.Context, u models.OperationalPortUse, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.uses, func(x models.OperationalPortUse) bool {
		return x.SessionID == u.SessionID && x.Purpose == u.Purpose && x.Target == u.Target && x.Port == u.Port
	})
	if i < 0 {
		u.FirstAt = at
		f.uses = append(f.uses, u)
		i = len(f.uses) - 1
	}
	f.uses[i].Uses++
	f.uses[i].LastAt = at
	if f.heat == nil {
		f.heat = map[int]models.OperationalPortHeat{}
	}
	h := f.heat[u.Port]
	h.Port, h.Uses, h.LastAt = u.Port, h.Uses+1, at
	f.heat[u.Port] = h
	return nil
}

func (f *fakeMemory) BySession(_ context.Context, session uuid.UUID) ([]models.OperationalPortUse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.OperationalPortUse
	for _, u := range f.uses {
		if u.SessionID == session {
			out = append(out, u)
		}
	}
	slices.SortStableFunc(out, func(a, b models.OperationalPortUse) int { return b.LastAt.Compare(a.LastAt) })
	return out, nil
}

func (f *fakeMemory) Heat(context.Context) (map[int]models.OperationalPortHeat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.heatErr != nil {
		return nil, f.heatErr
	}
	return maps.Clone(f.heat), nil
}

func (f *fakeMemory) DeleteByOperationID(context.Context, uuid.UUID) error { return nil }

func heatOf(uses map[int]int64) map[int]models.OperationalPortHeat {
	out := map[int]models.OperationalPortHeat{}
	for p, n := range uses {
		out[p] = models.OperationalPortHeat{Port: p, Uses: n}
	}
	return out
}

// The ports an opening naming none tries: the session's, for the same use,
// the same purpose, any, the latest first (one out of the range skipped);
// then the rest, the least heated first, shuffled within a heat.
func TestOrderPrefersTheSessionsPortsThenTheLeastHeated(t *testing.T) {
	session, other := uuid.New(), uuid.New()
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	mem := &fakeMemory{heat: heatOf(map[int]int64{41000: 5, 41001: 2, 41004: 1, 41006: 9})}
	mem.uses = []models.OperationalPortUse{
		{SessionID: session, Port: 50000, Purpose: models.OperationalPurposeSOCKS, LastAt: t0.Add(4 * time.Hour)},
		{SessionID: session, Port: 41001, Purpose: models.OperationalPurposeForward, Target: "10.0.0.5:3389", LastAt: t0.Add(3 * time.Hour)},
		{SessionID: session, Port: 41004, Purpose: models.OperationalPurposeSOCKS, LastAt: t0.Add(2 * time.Hour)},
		{SessionID: session, Port: 41000, Purpose: models.OperationalPurposeHTTP, LastAt: t0.Add(time.Hour)},
		{SessionID: other, Port: 41006, Purpose: models.OperationalPurposeSOCKS, LastAt: t0},
	}
	svc := New(Config{Ports: []int{41000, 41001, 41002, 41003, 41004, 41005, 41006}}, nil, nil, nil, zap.NewNop()).WithPortMemory(mem)
	svc.shuffle = func([]int) {}
	ctx := context.Background()
	for _, c := range []struct {
		name string
		o    Opening
		want []int
	}{
		{"a SOCKS proxy", Opening{Session: session}, []int{41004, 41001, 41000, 41002, 41003, 41005, 41006}},
		{"the same forward", Opening{Session: session, Purpose: models.OperationalPurposeForward, Host: "10.0.0.5", HostPort: 3389},
			[]int{41001, 41004, 41000, 41002, 41003, 41005, 41006}},
		{"another forward", Opening{Session: session, Purpose: models.OperationalPurposeForward, Host: "10.0.0.6", HostPort: 22},
			[]int{41001, 41004, 41000, 41002, 41003, 41005, 41006}},
		{"a session's first", Opening{Session: uuid.New()}, []int{41002, 41003, 41005, 41004, 41001, 41000, 41006}},
	} {
		purpose := c.o.Purpose
		if purpose == "" {
			purpose = models.OperationalPurposeSOCKS
		}
		if got := svc.order(ctx, c.o, purpose); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}

	// Within a heat, at random: the shuffle orders the coldest.
	svc.shuffle = slices.Reverse[[]int]
	if got := svc.order(ctx, Opening{Session: uuid.New()}, models.OperationalPurposeSOCKS); !slices.Equal(got[:3], []int{41005, 41003, 41002}) {
		t.Errorf("the coldest, shuffled: %v", got)
	}
	// The default shuffle keeps each heat together.
	svc.shuffle = nil
	for range 20 {
		got := svc.order(ctx, Opening{Session: uuid.New()}, models.OperationalPurposeSOCKS)
		cold := slices.Clone(got[:3])
		slices.Sort(cold)
		if !slices.Equal(cold, []int{41002, 41003, 41005}) || !slices.Equal(got[3:], []int{41004, 41001, 41000, 41006}) {
			t.Fatalf("by heat: %v", got)
		}
	}

	// No heat to read: the range in order, as without a memory.
	mem.heatErr = errors.New("down")
	if got := svc.order(ctx, Opening{Session: session}, models.OperationalPurposeSOCKS); !slices.Equal(got, svc.cfg.Ports) {
		t.Errorf("no heat: %v", got)
	}
	plain := New(Config{Ports: []int{41000, 41001}}, nil, nil, nil, zap.NewNop())
	if got := plain.order(ctx, Opening{Session: session}, models.OperationalPurposeSOCKS); !slices.Equal(got, []int{41000, 41001}) {
		t.Errorf("no memory: %v", got)
	}
}

// Open takes the port the session last took for the same use, and another
// while that one is in use; each taking is counted, to the use and the heat.
func TestOpenTakesTheSessionsPortAgain(t *testing.T) {
	a, b := freePort(t), freePort(t)
	for b == a {
		b = freePort(t)
	}
	mem := &fakeMemory{}
	c := DefaultConfig()
	c.Ports, c.Bind = []int{a, b}, "127.0.0.1"
	svc := New(c, &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}, &recordingBus{}, zap.NewNop()).
		WithPortMemory(mem)
	svc.Start(context.Background())
	t.Cleanup(svc.Stop)
	ctx := context.Background()
	session, op := uuid.New(), uuid.New()
	// The session took b before, for a forward to 10.0.0.5:3389.
	if err := mem.Record(ctx, models.OperationalPortUse{SessionID: session, OperationID: op, Port: b,
		Purpose: models.OperationalPurposeForward, Target: "10.0.0.5:3389"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	forward := func() Opening {
		return Opening{Task: uuid.New(), Session: session, Operation: op, Purpose: models.OperationalPurposeForward, Host: "10.0.0.5", HostPort: 3389}
	}
	first := forward()
	if port, err := svc.Open(ctx, first); err != nil || port != b {
		t.Fatalf("the session's port again: %d %v, want %d", port, err, b)
	}
	if port, err := svc.Open(ctx, forward()); err != nil || port != a {
		t.Fatalf("its port in use: %d %v, want %d", port, err, a)
	}
	uses, _ := mem.BySession(ctx, session)
	byPort := map[int]int64{}
	for _, u := range uses {
		if u.Target != "10.0.0.5:3389" || u.Purpose != models.OperationalPurposeForward || u.OperationID != op {
			t.Errorf("a use: %+v", u)
		}
		byPort[u.Port] = u.Uses
	}
	if byPort[b] != 2 || byPort[a] != 1 {
		t.Errorf("uses by port: %v", byPort)
	}
	heat, _ := mem.Heat(ctx)
	if heat[b].Uses != 2 || heat[a].Uses != 1 {
		t.Errorf("heat: %v", heat)
	}
	// Freed, b is the session's again.
	svc.Close(ctx, first.Task)
	if port, err := svc.Open(ctx, forward()); err != nil || port != b {
		t.Fatalf("its port freed: %d %v, want %d", port, err, b)
	}
	if heat, _ := mem.Heat(ctx); heat[b].Uses != 3 {
		t.Errorf("heat after: %v", heat)
	}
}
