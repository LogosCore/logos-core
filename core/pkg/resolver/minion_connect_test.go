package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeConnector leaves the target in after's state.
type fakeConnector struct {
	after             models.MinionTarget
	connects, discons int
}

func (f *fakeConnector) ConnectTarget(context.Context, uuid.UUID) (*models.MinionTarget, error) {
	f.connects++
	t := f.after
	return &t, nil
}

func (f *fakeConnector) DisconnectTarget(context.Context, uuid.UUID) (*models.MinionTarget, error) {
	f.discons++
	t := f.after
	return &t, nil
}

// connect and disconnect say what came of them, and do nothing when the
// connection is already as asked.
func TestConnectOutcome(t *testing.T) {
	ctx := context.Background()
	notConnected := &models.MinionTarget{Name: "10.0.0.5", State: models.TargetNotConnected}
	connected := &models.MinionTarget{Name: "10.0.0.5", State: models.TargetConnected}
	for _, c := range []struct {
		name, command string
		before        *models.MinionTarget
		after         models.MinionTarget
		ok            bool
		out, err      string
		calls         int
	}{
		{"connect", protocol.CommandConnect, notConnected, models.MinionTarget{Name: "10.0.0.5", State: models.TargetConnected}, true, "connected (10.0.0.5)", "", 1},
		{"connect, unreachable", protocol.CommandConnect, notConnected,
			models.MinionTarget{State: models.TargetUnreachable, StateDetail: "unreachable: connection refused"}, false, "", "unreachable: unreachable: connection refused", 1},
		{"connect, connected", protocol.CommandConnect, connected, models.MinionTarget{}, true, "already connected", "", 0},
		{"disconnect", protocol.CommandDisconnect, connected, models.MinionTarget{Name: "10.0.0.5", State: models.TargetNotConnected}, true, "disconnected (10.0.0.5)", "", 1},
		{"disconnect, always", protocol.CommandDisconnect, connected,
			models.MinionTarget{State: models.TargetNotConnected, Policy: models.PolicyAlways}, true, "opens it again", "", 1},
		{"disconnect, not connected", protocol.CommandDisconnect, notConnected, models.MinionTarget{}, true, "not connected", "", 0},
	} {
		f := &fakeConnector{after: c.after}
		r := &minionTaskResolver{connector: f}
		ok, out, why := r.connectOutcome(ctx, c.command, c.before)
		if ok != c.ok || !strings.Contains(out, c.out) || why != c.err || f.connects+f.discons != c.calls {
			t.Errorf("%s: %v %q %q, %d call(s)", c.name, ok, out, why, f.connects+f.discons)
		}
	}
}

// Only a bind-persistent session's console has connect and disconnect.
func TestOffersConnect(t *testing.T) {
	r := &minionTaskResolver{connector: &fakeConnector{}, targets: fakeTaskTargets{}}
	if !r.offersConnect(&models.MinionSession{ConnKind: protocol.KindBindPersistent}) {
		t.Error("a bind-persistent session")
	}
	for _, kind := range []string{protocol.KindBindCall, protocol.KindReversePersistent, ""} {
		if r.offersConnect(&models.MinionSession{ConnKind: kind}) {
			t.Errorf("a %q session", kind)
		}
	}
	if (&minionTaskResolver{targets: fakeTaskTargets{}}).offersConnect(&models.MinionSession{ConnKind: protocol.KindBindPersistent}) {
		t.Error("no connector")
	}
}
