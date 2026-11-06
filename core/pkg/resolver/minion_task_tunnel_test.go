package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// fakeTaskTunnels opens the ports asked for: 41000 for any, the one asked
// for otherwise; err refuses.
type fakeTaskTunnels struct {
	opened []miniontunnels.Opening
	closed []uuid.UUID
	err    error
}

func (f *fakeTaskTunnels) Open(_ context.Context, o miniontunnels.Opening) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.opened = append(f.opened, o)
	if o.Port != 0 {
		return o.Port, nil
	}
	return 41000, nil
}

func (f *fakeTaskTunnels) Close(_ context.Context, task uuid.UUID) { f.closed = append(f.closed, task) }

// A tunnel task takes its port before it is queued, for any free one (0)
// or the one asked for; its args then carry the port, and its max_streams
// and timeout (the command's defaults when it has none) bound the proxy.
// No port free, or no tunnels in core, is the operator's to fix.
func TestOpenTunnel(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	task, uid := uuid.New(), uuid.New()
	wire := map[string]any{"port": int64(0)}
	port, err := r.openTunnel(ctx, s, uid, task, wire)
	if err != nil || port != 41000 || wire["port"] != int64(41000) {
		t.Fatalf("any port: %d %v, args %v", port, err, wire)
	}
	o := f.opened[0]
	if o.Task != task || o.Session != s.SessionID || o.Operation != s.OperationID || o.By != uid || o.Port != 0 ||
		o.MaxStreams != miniontunnels.DefaultMaxStreams || o.Timeout != miniontunnels.DefaultConnectTimeout {
		t.Errorf("opened %+v", o)
	}
	wire = map[string]any{"port": int64(41007), "max_streams": int64(8), "timeout": int64(3000)}
	if port, err = r.openTunnel(ctx, s, uid, uuid.New(), wire); err != nil || port != 41007 {
		t.Fatalf("the port asked for: %d %v", port, err)
	}
	if o := f.opened[1]; o.Port != 41007 || o.MaxStreams != 8 || o.Timeout != 3*time.Second {
		t.Errorf("opened %+v", o)
	}
	f.err = fmt.Errorf("%w: all 64 are in use", miniontunnels.ErrNoPort)
	if _, err := r.openTunnel(ctx, s, uid, uuid.New(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "all 64 are in use") {
		t.Errorf("no port free: %v", err)
	}
	f.err = errors.New("mongo down")
	if _, err := r.openTunnel(ctx, s, uid, uuid.New(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "mongo down") {
		t.Errorf("a store failing: %v", err)
	}
	if _, err := (&minionTaskResolver{}).openTunnel(ctx, s, uid, uuid.New(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "core opens no tunnels") {
		t.Errorf("no tunnels: %v", err)
	}
}
