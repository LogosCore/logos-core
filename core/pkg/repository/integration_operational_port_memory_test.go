package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// What sessions took operational ports for: a use counted once a taking
// (its first and last kept), a session's the latest first, a port's heat
// summed over every session, and an operation's uses forgotten with it
// while the heat stays.
func TestIntegrationOperationalPortMemory(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewOperationalPortMemoryRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	op, other := uuid.New(), uuid.New()
	s1, s2 := uuid.New(), uuid.New()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	socks := models.OperationalPortUse{SessionID: s1, OperationID: op, Port: 41000, Purpose: models.OperationalPurposeSOCKS}
	fwd := models.OperationalPortUse{SessionID: s1, OperationID: op, Port: 41001, Purpose: models.OperationalPurposeForward, Target: "10.0.0.5:3389"}
	for i, step := range []struct {
		u  models.OperationalPortUse
		at time.Time
	}{
		{socks, t0}, {fwd, t0.Add(time.Minute)}, {socks, t0.Add(2 * time.Minute)},
		{models.OperationalPortUse{SessionID: s2, OperationID: other, Port: 41000, Purpose: models.OperationalPurposeHTTP}, t0.Add(3 * time.Minute)},
	} {
		if err := repo.Record(ctx, step.u, step.at); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	uses, err := repo.BySession(ctx, s1)
	if err != nil || len(uses) != 2 {
		t.Fatalf("s1's uses: %+v %v", uses, err)
	}
	if u := uses[0]; u.Port != 41000 || u.Uses != 2 || !u.FirstAt.Equal(t0) || !u.LastAt.Equal(t0.Add(2*time.Minute)) || u.Target != "" {
		t.Errorf("the latest first, counted: %+v", u)
	}
	if u := uses[1]; u.Port != 41001 || u.Uses != 1 || u.Target != "10.0.0.5:3389" || u.OperationID != op {
		t.Errorf("the forward's: %+v", u)
	}
	heat, err := repo.Heat(ctx)
	if err != nil || heat[41000].Uses != 3 || heat[41001].Uses != 1 || len(heat) != 2 || !heat[41000].LastAt.Equal(t0.Add(3*time.Minute)) {
		t.Fatalf("heat: %+v %v", heat, err)
	}
	if err := repo.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if uses, _ := repo.BySession(ctx, s1); len(uses) != 0 {
		t.Errorf("the operation's uses stay: %+v", uses)
	}
	if uses, _ := repo.BySession(ctx, s2); len(uses) != 1 {
		t.Errorf("another operation's went: %+v", uses)
	}
	if after, _ := repo.Heat(ctx); after[41000].Uses != 3 || after[41001].Uses != 1 {
		t.Errorf("the heat went with the operation: %+v", after)
	}
}
