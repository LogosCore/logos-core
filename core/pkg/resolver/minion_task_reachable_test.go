package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

type fakeTaskTargets map[uuid.UUID]*models.MinionTarget

func (f fakeTaskTargets) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	if t, ok := f[id]; ok {
		return t, nil
	}
	return nil, qmgo.ErrNoSuchDocuments
}

// A bind-call session takes tasks only while it has a target: without one
// nothing calls its page. Any other session takes them, as before.
func TestReachable(t *testing.T) {
	ctx := context.Background()
	withTarget, without := uuid.New(), uuid.New()
	r := &minionTaskResolver{targets: fakeTaskTargets{withTarget: {State: models.TargetUnreachable}}}
	bind := func(id uuid.UUID) *models.MinionSession {
		return &models.MinionSession{SessionID: id, Number: 12, ConnKind: protocol.KindBindCall}
	}
	if err := r.reachable(ctx, bind(withTarget)); err != nil {
		t.Errorf("a target, unreachable: %v", err)
	}
	err := r.reachable(ctx, bind(without))
	if err == nil || !strings.Contains(err.Error(), "session #12 has no target") {
		t.Errorf("no target: %v", err)
	}
	if err := r.reachable(ctx, &models.MinionSession{SessionID: without, ConnKind: protocol.KindReverseCall}); err != nil {
		t.Errorf("a reverse session: %v", err)
	}
	if err := (&minionTaskResolver{}).reachable(ctx, bind(without)); err != nil {
		t.Errorf("no targets served: %v", err)
	}
	broken := &minionTaskResolver{targets: failingTargets{}}
	if err := broken.reachable(ctx, bind(without)); err == nil || strings.Contains(err.Error(), "has no target") {
		t.Errorf("a failed lookup is not a missing target: %v", err)
	}
}

type failingTargets struct{}

func (failingTargets) FindBySessionID(context.Context, uuid.UUID) (*models.MinionTarget, error) {
	return nil, errors.New("mongo down")
}
