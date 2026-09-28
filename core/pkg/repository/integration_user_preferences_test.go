package repository

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// Picker history writes, against a real server.
//
// Both lists are rewritten by one update-with-pipeline each, and none of what
// matters about them — the ranking, the cap, the dedupe, that concurrent writes
// all land — is visible in the bson a builder returns. A pipeline with the sort
// keys swapped, or a read-modify-write in its place, returns plausible lists.

func seedPrefsUser(t *testing.T, users IUserRepository) uuid.UUID {
	t.Helper()
	u := &models.User{UserID: uuid.New(), Username: "prefs-" + uuid.NewString()[:8], Active: true}
	if err := users.Create(testCtx(t), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.UserID
}

func iconNames(icons []models.IconUsage) []string {
	out := make([]string, len(icons))
	for i, ic := range icons {
		out[i] = ic.Name
	}
	return out
}

func TestIntegrationRecordIconUseRanksByCountThenRecency(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	users, prefs := NewUserRepository(db), NewUserPreferencesRepository(db)
	uid := seedPrefsUser(t, users)

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// Shield twice, then Bug and Flag once each, Flag last. Expected ranking:
	// Shield (2), then Flag before Bug — equal counts, Flag more recent.
	for i, name := range []string{"Shield", "Bug", "Shield", "Flag"} {
		if err := prefs.RecordIconUse(ctx, uid, name, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}

	u, err := users.FindByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(iconNames(u.FrequentIcons)), "[Shield Flag Bug]"; got != want {
		t.Fatalf("ranking = %s, want %s", got, want)
	}
	if u.FrequentIcons[0].Count != 2 {
		t.Errorf("Shield count = %d, want 2", u.FrequentIcons[0].Count)
	}
	if !u.FrequentIcons[0].LastUsed.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("Shield last_used = %v, want its second use", u.FrequentIcons[0].LastUsed)
	}
}

// A name starting with "$" would be read as a field path if it reached the
// pipeline unquoted, and store whatever that path resolves to.
func TestIntegrationRecordIconUseStoresNamesLiterally(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	users, prefs := NewUserRepository(db), NewUserPreferencesRepository(db)
	uid := seedPrefsUser(t, users)

	if err := prefs.RecordIconUse(ctx, uid, "$username", time.Now()); err != nil {
		t.Fatal(err)
	}
	u, _ := users.FindByID(ctx, uid)
	if len(u.FrequentIcons) != 1 || u.FrequentIcons[0].Name != "$username" {
		t.Fatalf("stored %+v, want the literal name", u.FrequentIcons)
	}
}

func TestIntegrationRecordIconUseCapsTheList(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	users, prefs := NewUserRepository(db), NewUserPreferencesRepository(db)
	uid := seedPrefsUser(t, users)

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// A favourite with a high count, then more one-off icons than the cap.
	for range 3 {
		if err := prefs.RecordIconUse(ctx, uid, "Favourite", base); err != nil {
			t.Fatal(err)
		}
	}
	extra := models.MaxFrequentIcons + 5
	for i := range extra {
		if err := prefs.RecordIconUse(ctx, uid, fmt.Sprintf("Icon%02d", i), base.Add(time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	u, _ := users.FindByID(ctx, uid)
	if len(u.FrequentIcons) != models.MaxFrequentIcons {
		t.Fatalf("len = %d, want %d", len(u.FrequentIcons), models.MaxFrequentIcons)
	}
	if u.FrequentIcons[0].Name != "Favourite" {
		t.Errorf("first = %s, want the high-count icon to survive the churn", u.FrequentIcons[0].Name)
	}
	// The one-offs that were evicted are the oldest: the newest one-off must be
	// second, and the list ends at the oldest one still inside the cap.
	if got, want := u.FrequentIcons[1].Name, fmt.Sprintf("Icon%02d", extra-1); got != want {
		t.Errorf("second = %s, want the newest one-off %s", got, want)
	}
	if got, want := u.FrequentIcons[len(u.FrequentIcons)-1].Name, fmt.Sprintf("Icon%02d", extra-(models.MaxFrequentIcons-1)); got != want {
		t.Errorf("last = %s, want %s", got, want)
	}
}

// The reason these writes are pipelines: a read-modify-write loses increments
// under concurrency. Swap RecordIconUse for one and this fails.
func TestIntegrationRecordIconUseConcurrentUsesAllLand(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	users, prefs := NewUserRepository(db), NewUserPreferencesRepository(db)
	uid := seedPrefsUser(t, users)

	const n = 25
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			errs <- prefs.RecordIconUse(ctx, uid, "Shield", time.Now())
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	u, _ := users.FindByID(ctx, uid)
	if len(u.FrequentIcons) != 1 || u.FrequentIcons[0].Count != n {
		t.Fatalf("stored %+v, want one entry with count %d", u.FrequentIcons, n)
	}
}

func TestIntegrationRecordIconUseUnknownUser(t *testing.T) {
	db := integrationDB(t)
	prefs := NewUserPreferencesRepository(db)
	if err := prefs.RecordIconUse(testCtx(t), uuid.New(), "Shield", time.Now()); !IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestIntegrationTouchRecentOperationMovesToFrontAndCaps(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	users, prefs := NewUserRepository(db), NewUserPreferencesRepository(db)
	uid := seedPrefsUser(t, users)

	ops := make([]uuid.UUID, models.MaxRecentOperations+2)
	for i := range ops {
		ops[i] = uuid.New()
		if err := prefs.TouchRecentOperation(ctx, uid, ops[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Re-touch one that is still in the list: it moves, it does not duplicate.
	again := ops[len(ops)-4]
	if err := prefs.TouchRecentOperation(ctx, uid, again); err != nil {
		t.Fatal(err)
	}

	u, _ := users.FindByID(ctx, uid)
	got := u.RecentOperations
	if len(got) != models.MaxRecentOperations {
		t.Fatalf("len = %d, want %d", len(got), models.MaxRecentOperations)
	}
	want := []uuid.UUID{again}
	for i := len(ops) - 1; len(want) < models.MaxRecentOperations; i-- {
		if ops[i] != again {
			want = append(want, ops[i])
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("recent = %v\nwant     %v", got, want)
	}
}

func TestIntegrationSeedWritesOnlyAnEmptyList(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	users, prefs := NewUserRepository(db), NewUserPreferencesRepository(db)
	uid := seedPrefsUser(t, users)

	now := time.Now().UTC().Truncate(time.Millisecond)
	first := []models.IconUsage{{Name: "Bug", Count: 4, LastUsed: now}}
	if err := prefs.SeedFrequentIcons(ctx, uid, first); err != nil {
		t.Fatal(err)
	}
	// A second import — another tab, or a stale localStorage — must not
	// overwrite what the first wrote.
	if err := prefs.SeedFrequentIcons(ctx, uid, []models.IconUsage{{Name: "Flag", Count: 9, LastUsed: now}}); err != nil {
		t.Fatalf("second seed should be a silent no-op, got %v", err)
	}
	opA, opB := uuid.New(), uuid.New()
	if err := prefs.SeedRecentOperations(ctx, uid, []uuid.UUID{opA}); err != nil {
		t.Fatal(err)
	}
	if err := prefs.SeedRecentOperations(ctx, uid, []uuid.UUID{opB}); err != nil {
		t.Fatal(err)
	}

	u, _ := users.FindByID(ctx, uid)
	if len(u.FrequentIcons) != 1 || u.FrequentIcons[0].Name != "Bug" || u.FrequentIcons[0].Count != 4 {
		t.Errorf("icons = %+v, want the first seed only", u.FrequentIcons)
	}
	if len(u.RecentOperations) != 1 || u.RecentOperations[0] != opA {
		t.Errorf("recent = %v, want [%s]", u.RecentOperations, opA)
	}

	// Once a real use has populated the list, a seed is a no-op too.
	uid2 := seedPrefsUser(t, users)
	if err := prefs.RecordIconUse(ctx, uid2, "Shield", now); err != nil {
		t.Fatal(err)
	}
	if err := prefs.SeedFrequentIcons(ctx, uid2, first); err != nil {
		t.Fatal(err)
	}
	u2, _ := users.FindByID(ctx, uid2)
	if got := fmt.Sprint(iconNames(u2.FrequentIcons)); got != "[Shield]" {
		t.Errorf("icons after seed over a used list = %s, want [Shield]", got)
	}
}
