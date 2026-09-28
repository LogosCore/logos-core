package resolver

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
)

// fakePrefsRepo records the writes a resolver asked for.
type fakePrefsRepo struct {
	icons      []string
	touched    []uuid.UUID
	seedIcons  []models.IconUsage
	seedOps    []uuid.UUID
	seedCalled bool
}

func (f *fakePrefsRepo) RecordIconUse(_ context.Context, _ uuid.UUID, name string, _ time.Time) error {
	f.icons = append(f.icons, name)
	return nil
}
func (f *fakePrefsRepo) TouchRecentOperation(_ context.Context, _, id uuid.UUID) error {
	f.touched = append(f.touched, id)
	return nil
}
func (f *fakePrefsRepo) SeedFrequentIcons(_ context.Context, _ uuid.UUID, icons []models.IconUsage) error {
	f.seedCalled, f.seedIcons = true, icons
	return nil
}
func (f *fakePrefsRepo) SeedRecentOperations(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	f.seedCalled, f.seedOps = true, ids
	return nil
}

// prefsHarness is a resolver for caller over the given operations; caller is
// a member (viewer) of exactly the ones in member.
type prefsHarness struct {
	r      IUserResolver
	prefs  *fakePrefsRepo
	caller models.User
	ctx    context.Context
}

func newPrefsHarness(t *testing.T, caller models.User, member []uuid.UUID, other []uuid.UUID) prefsHarness {
	t.Helper()
	ops := map[uuid.UUID]models.Operation{}
	for _, id := range member {
		ops[id] = memberOp(id, caller.UserID, models.OperationRoleViewer)
	}
	for _, id := range other {
		ops[id] = memberOp(id, uuid.New(), models.OperationRoleAdmin)
	}
	opRepo := &mockOpRepo{findByIDFn: func(_ context.Context, id uuid.UUID) (models.Operation, error) {
		if op, ok := ops[id]; ok {
			return op, nil
		}
		return models.Operation{}, qmgo.ErrNoSuchDocuments
	}}
	userRepo := &mockUserRepo{findByIDFn: func(context.Context, uuid.UUID) (models.User, error) {
		return caller, nil
	}}
	prefs := &fakePrefsRepo{}
	return prefsHarness{
		r:      NewUserResolver(userRepo, eventbus.NewNopEventBus(), prefs, opRepo),
		prefs:  prefs,
		caller: caller,
		ctx:    newCallerCtx(caller.UserID, "user"),
	}
}

func TestRecordIconUse_ValidatesName(t *testing.T) {
	h := newPrefsHarness(t, models.User{UserID: uuid.New()}, nil, nil)

	for _, ok := range []string{"Shield", "ShieldAlert", "si:github", "arrow-up_1"} {
		if _, err := h.r.RecordIconUse(h.ctx, ok); err != nil {
			t.Errorf("RecordIconUse(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "$where", "a b", "<svg>", string(make([]byte, 65))} {
		if _, err := h.r.RecordIconUse(h.ctx, bad); err == nil {
			t.Errorf("RecordIconUse(%q) accepted", bad)
		}
	}
	if len(h.prefs.icons) != 4 {
		t.Errorf("wrote %v, want only the four valid names", h.prefs.icons)
	}
}

// A recent entry for an operation the caller cannot view would let the list
// probe operation IDs, so the write is refused before it reaches the store.
func TestTouchRecentOperation_RefusesWhatTheCallerCannotView(t *testing.T) {
	mine, theirs, gone := uuid.New(), uuid.New(), uuid.New()
	h := newPrefsHarness(t, models.User{UserID: uuid.New()}, []uuid.UUID{mine}, []uuid.UUID{theirs})

	if _, err := h.r.TouchRecentOperation(h.ctx, mine.String()); err != nil {
		t.Fatalf("own operation: %v", err)
	}
	if _, err := h.r.TouchRecentOperation(h.ctx, theirs.String()); err == nil {
		t.Error("operation the caller is not a member of was accepted")
	}
	if _, err := h.r.TouchRecentOperation(h.ctx, gone.String()); err == nil {
		t.Error("missing operation was accepted")
	}
	if _, err := h.r.TouchRecentOperation(h.ctx, "not-a-uuid"); err == nil {
		t.Error("malformed id was accepted")
	}
	if !slices.Equal(h.prefs.touched, []uuid.UUID{mine}) {
		t.Errorf("touched %v, want only %s", h.prefs.touched, mine)
	}
}

func TestRecentOperations_DropsDeletedAndRevokedInOrder(t *testing.T) {
	a, b, revoked, deleted := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	caller := models.User{UserID: uuid.New(), RecentOperations: []uuid.UUID{b, revoked, deleted, a}}
	h := newPrefsHarness(t, caller, []uuid.UUID{a, b}, []uuid.UUID{revoked})

	ops, err := h.r.RecentOperations(h.ctx, &h.caller)
	if err != nil {
		t.Fatal(err)
	}
	var got []uuid.UUID
	for _, op := range ops {
		got = append(got, op.OperationID)
	}
	if !slices.Equal(got, []uuid.UUID{b, a}) {
		t.Fatalf("resolved %v, want [%s %s] in stored order", got, b, a)
	}
}

// A transient lookup failure drops that entry instead of failing the field:
// recentOperations is non-null on the non-null `me`, so an error would null
// the query the app shell boots from.
func TestRecentOperations_LookupErrorDropsTheEntry(t *testing.T) {
	a := uuid.New()
	caller := models.User{UserID: uuid.New(), RecentOperations: []uuid.UUID{uuid.New(), a}}
	h := newPrefsHarness(t, caller, []uuid.UUID{a}, nil)
	h.r.(*userResolver).operationRepo = &mockOpRepo{findByIDFn: func(_ context.Context, id uuid.UUID) (models.Operation, error) {
		if id == a {
			return memberOp(a, caller.UserID, models.OperationRoleViewer), nil
		}
		return models.Operation{}, errors.New("connection reset")
	}}

	ops, err := h.r.RecentOperations(h.ctx, &h.caller)
	if err != nil {
		t.Fatalf("field failed: %v", err)
	}
	if len(ops) != 1 || ops[0].OperationID != a {
		t.Fatalf("resolved %v, want only %s", ops, a)
	}
}

// Picker history is private: an admin reading another user sees none of it.
func TestPreferenceFields_EmptyOnSomeoneElse(t *testing.T) {
	op := uuid.New()
	other := models.User{
		UserID:           uuid.New(),
		FrequentIcons:    []models.IconUsage{{Name: "Bug", Count: 3}},
		RecentOperations: []uuid.UUID{op},
	}
	h := newPrefsHarness(t, models.User{UserID: uuid.New()}, []uuid.UUID{op}, nil)
	h.ctx = newCallerCtx(h.caller.UserID, "admin")

	icons, _ := h.r.FrequentIcons(h.ctx, &other)
	ops, _ := h.r.RecentOperations(h.ctx, &other)
	if icons == nil || len(icons) != 0 || ops == nil || len(ops) != 0 {
		t.Fatalf("icons=%v ops=%v, want both empty and non-nil", icons, ops)
	}

	own, _ := h.r.FrequentIcons(newCallerCtx(other.UserID, "user"), &other)
	if !slices.Equal(own, []string{"Bug"}) {
		t.Fatalf("own icons = %v, want [Bug]", own)
	}
}

func TestImportLocalPreferences_SanitizesBeforeSeeding(t *testing.T) {
	mine, theirs := uuid.New(), uuid.New()
	h := newPrefsHarness(t, models.User{UserID: uuid.New()}, []uuid.UUID{mine}, []uuid.UUID{theirs})

	future := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	old := "2026-01-01T00:00:00.000Z" // what JS toISOString produces
	_, err := h.r.ImportLocalPreferences(h.ctx, model.ImportLocalPreferencesInput{
		FrequentIcons: []*model.IconUsageInput{
			{Name: "Flag", Count: 1, LastUsedAt: old},
			{Name: "Bug", Count: 5, LastUsedAt: future},
			{Name: "Bug", Count: 99, LastUsedAt: old}, // duplicate: first wins
			{Name: "$bad", Count: 3, LastUsedAt: old},
			{Name: "Shield", Count: -4, LastUsedAt: old}, // clamped to 1
			{Name: "Map", Count: 2, LastUsedAt: "yesterday"},
			nil,
		},
		RecentOperationIds: []string{theirs.String(), mine.String(), "junk", mine.String()},
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, ic := range h.prefs.seedIcons {
		names = append(names, ic.Name)
	}
	if !slices.Equal(names, []string{"Bug", "Flag", "Shield"}) {
		t.Fatalf("seeded icons %v, want [Bug Flag Shield]", names)
	}
	if h.prefs.seedIcons[0].Count != 5 || h.prefs.seedIcons[0].LastUsed.After(time.Now()) {
		t.Errorf("Bug = %+v, want count 5 and a last-used clamped to now", h.prefs.seedIcons[0])
	}
	if h.prefs.seedIcons[2].Count != 1 {
		t.Errorf("Shield count = %d, want clamped to 1", h.prefs.seedIcons[2].Count)
	}
	if !slices.Equal(h.prefs.seedOps, []uuid.UUID{mine}) {
		t.Errorf("seeded ops %v, want only the viewable one, once", h.prefs.seedOps)
	}
}

func TestImportLocalPreferences_NothingValidWritesNothing(t *testing.T) {
	h := newPrefsHarness(t, models.User{UserID: uuid.New()}, nil, nil)
	_, err := h.r.ImportLocalPreferences(h.ctx, model.ImportLocalPreferencesInput{
		FrequentIcons:      []*model.IconUsageInput{{Name: "$x", Count: 1, LastUsedAt: "2026-01-01T00:00:00Z"}},
		RecentOperationIds: []string{uuid.NewString()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.prefs.seedCalled {
		t.Error("an import with nothing valid still wrote")
	}
}
