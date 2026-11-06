package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeTaskTunnels opens the ports asked for: 41000 for any, the one asked
// for otherwise; err refuses.
type fakeTaskTunnels struct {
	opened  []miniontunnels.Opening
	closed  []uuid.UUID
	err     error
	address string
	live    map[uuid.UUID]miniontunnels.Summary
	ended   miniontunnels.Summary
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

func (f *fakeTaskTunnels) Address() string { return f.address }

func (f *fakeTaskTunnels) View(_ context.Context, task uuid.UUID) (miniontunnels.Summary, bool, error) {
	if sum, ok := f.live[task]; ok {
		return sum, true, nil
	}
	return f.ended, false, nil
}

func (f *fakeTaskTunnels) Summary(task uuid.UUID) (miniontunnels.Summary, bool) {
	sum, ok := f.live[task]
	return sum, ok
}

func (f *fakeTaskTunnels) Streams(context.Context, uuid.UUID, repository.TunnelStreamFilter, uint64, int64) ([]models.MinionTunnelStream, error) {
	return nil, nil
}

func (f *fakeTaskTunnels) Port(context.Context, uuid.UUID) (*models.OperationalPort, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeTaskTunnels) Ports(context.Context, uuid.UUID, string, *pagination.Cursor, int64, bool) ([]models.OperationalPort, error) {
	return nil, nil
}

func (f *fakeTaskTunnels) CountPorts(context.Context, uuid.UUID, string) (int64, error) {
	return 0, nil
}

// A tunnel task's proxy is live while core listens for it (its port, its
// streams now); once it ended, its records' totals, the port its args say.
// The address is core's, null for the web app's host.
func TestTaskTunnel(t *testing.T) {
	ctx := context.Background()
	live := uuid.New()
	f := &fakeTaskTunnels{live: map[uuid.UUID]miniontunnels.Summary{live: {Port: 41003, Open: 2, Streams: 9, Up: 100, Down: 2000}},
		ended: miniontunnels.Summary{Streams: 4, Up: 7, Down: 8}}
	r := &minionTaskResolver{tunnels: f}
	got, err := r.Tunnel(ctx, &model.MinionTask{ID: live.String()})
	if err != nil || got.Port != 41003 || !got.Listening || got.Open != 2 || got.Streams != 9 || got.Up != 100 || got.Down != 2000 || got.Address != nil {
		t.Fatalf("live: %+v %v", got, err)
	}
	f.address = "ops.example.org"
	got, err = r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Args: map[string]any{"port": float64(41007)}})
	if err != nil || got.Port != 41007 || got.Listening || got.Open != 0 || got.Streams != 4 || got.Up != 7 || got.Down != 8 ||
		got.Address == nil || *got.Address != "ops.example.org" {
		t.Fatalf("ended: %+v %v", got, err)
	}
}

// A stream reads OPENING until the minion's reply, OPEN after it, ENDED
// once it ended; its result by name.
func TestTunnelStreamModel(t *testing.T) {
	at := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	s := models.MinionTunnelStream{Stream: 3, Client: "10.9.0.2:50122", Host: "dc01", Port: 445, OpenedAt: at, Up: 5, Down: 6}
	if m := tunnelStreamModel(&s); m.State != "OPENING" || m.Result != nil || m.Stream != 3 || m.OpenedAt != "2026-11-06T10:00:00Z" {
		t.Errorf("opening: %+v", m)
	}
	open := protocol.TunnelOpen
	s.Result, s.Bound = &open, "10.0.0.7:51234"
	if m := tunnelStreamModel(&s); m.State != "OPEN" || m.Result == nil || *m.Result != "open" || m.Bound == nil || *m.Bound != "10.0.0.7:51234" {
		t.Errorf("open: %+v", m)
	}
	refused := protocol.TunnelRefused
	s.Result, s.Bound, s.EndedAt, s.End = &refused, "", &at, models.TunnelEndNotOpened
	if m := tunnelStreamModel(&s); m.State != "ENDED" || *m.Result != "refused" || m.Bound != nil || m.End == nil || *m.End != "not_opened" {
		t.Errorf("refused: %+v", m)
	}
}

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
	port, err := r.openTunnel(ctx, s, uid, task, protocol.ActionSocksProxy, wire)
	if err != nil || port != 41000 || wire["port"] != int64(41000) {
		t.Fatalf("any port: %d %v, args %v", port, err, wire)
	}
	o := f.opened[0]
	if o.Task != task || o.Session != s.SessionID || o.Operation != s.OperationID || o.By != uid || o.Port != 0 ||
		o.MaxStreams != miniontunnels.DefaultMaxStreams || o.Timeout != miniontunnels.DefaultConnectTimeout {
		t.Errorf("opened %+v", o)
	}
	wire = map[string]any{"port": int64(41007), "max_streams": int64(8), "timeout": int64(3000)}
	if port, err = r.openTunnel(ctx, s, uid, uuid.New(), protocol.ActionSocksProxy, wire); err != nil || port != 41007 {
		t.Fatalf("the port asked for: %d %v", port, err)
	}
	if o := f.opened[1]; o.Port != 41007 || o.MaxStreams != 8 || o.Timeout != 3*time.Second {
		t.Errorf("opened %+v", o)
	}
	f.err = fmt.Errorf("%w: all 64 are in use", miniontunnels.ErrNoPort)
	if _, err := r.openTunnel(ctx, s, uid, uuid.New(), protocol.ActionSocksProxy, map[string]any{}); err == nil || !strings.Contains(err.Error(), "all 64 are in use") {
		t.Errorf("no port free: %v", err)
	}
	f.err = errors.New("mongo down")
	if _, err := r.openTunnel(ctx, s, uid, uuid.New(), protocol.ActionSocksProxy, map[string]any{}); err == nil || !strings.Contains(err.Error(), "mongo down") {
		t.Errorf("a store failing: %v", err)
	}
	if _, err := (&minionTaskResolver{}).openTunnel(ctx, s, uid, uuid.New(), protocol.ActionSocksProxy, map[string]any{}); err == nil || !strings.Contains(err.Error(), "core opens no tunnels") {
		t.Errorf("no tunnels: %v", err)
	}
}

// A port forward's task opens its port onto its destination, its host and
// to, checked as a stream's destination is; one that does not read is the
// operator's to fix.
func TestOpenForward(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), Number: 7}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	wire := map[string]any{"port": int64(0), "host": " 10.0.0.5 ", "to": int64(3389)}
	port, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), protocol.ActionPortForward, wire)
	if err != nil || port != 41000 {
		t.Fatalf("forward: %d %v", port, err)
	}
	if o := f.opened[0]; o.Purpose != models.OperationalPurposeForward || o.Host != "10.0.0.5" || o.HostPort != 3389 || o.SessionNumber != 7 {
		t.Errorf("opened %+v", o)
	}
	if wire["host"] != "10.0.0.5" {
		t.Errorf("args %v", wire)
	}
	for name, w := range map[string]map[string]any{
		"no host":    {"to": int64(22)},
		"port 0":     {"host": "dc01"},
		"port 65536": {"host": "dc01", "to": int64(65536)},
		"a control":  {"host": "dc\x0001", "to": int64(22)},
	} {
		if _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), protocol.ActionPortForward, w); err == nil || !strings.Contains(err.Error(), "destination") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if o := f.opened[0]; len(f.opened) != 1 {
		t.Errorf("a bad forward opened a port: %+v", o)
	}
	socks, _ := r.openTunnel(ctx, s, uuid.New(), uuid.New(), protocol.ActionSocksProxy, map[string]any{})
	if o := f.opened[1]; socks != 41000 || o.Purpose != models.OperationalPurposeSOCKS || o.Host != "" {
		t.Errorf("socks: %+v", o)
	}
}

// A forward's tunnel says what it serves and where to, from its task's
// args; a SOCKS proxy's names no destination.
func TestForwardTunnel(t *testing.T) {
	ctx := context.Background()
	r := &minionTaskResolver{tunnels: &fakeTaskTunnels{}}
	forward := protocol.ActionPortForward
	got, err := r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &forward,
		Args: map[string]any{"port": float64(41001), "host": "fe80::1", "to": float64(22)}})
	if err != nil || got.Purpose != models.OperationalPurposeForward || got.To == nil || *got.To != "[fe80::1]:22" || got.Port != 41001 {
		t.Fatalf("forward: %+v %v", got, err)
	}
	tunnel := protocol.ActionSocksProxy
	got, err = r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &tunnel, Args: map[string]any{"port": float64(41000)}})
	if err != nil || got.Purpose != models.OperationalPurposeSOCKS || got.To != nil {
		t.Fatalf("socks: %+v %v", got, err)
	}
}
