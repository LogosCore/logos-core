package modulelogs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.uber.org/zap"
)

type fakeRegistry map[string]models.Module

func (f fakeRegistry) FindByInstance(_ context.Context, instance string) (models.Module, error) {
	m, ok := f[instance]
	if !ok {
		return m, qmgo.ErrNoSuchDocuments
	}
	return m, nil
}

type call struct {
	queue, op string
	payload   any
}

type fakeRPC struct {
	mu    sync.Mutex
	calls []call
	fail  error // returned for every call while set
}

func (f *fakeRPC) Call(_ context.Context, queue, op string, payload any, _ time.Duration) (messaging.ReplyEnvelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{queue, op, payload})
	if f.fail != nil {
		return messaging.ReplyEnvelope{}, f.fail
	}
	return messaging.ReplyEnvelope{Status: messaging.StatusOK}, nil
}

func (f *fakeRPC) ops() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

type fakeSource struct {
	ch     chan []byte
	opened string
}

func (f *fakeSource) Open(_ context.Context, id string) (<-chan []byte, error) {
	f.opened = id
	return f.ch, nil
}

func (f *fakeSource) send(t *testing.T, b protocol.LogBatch) {
	t.Helper()
	body, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	f.ch <- body
}

func module(caps ...string) models.Module {
	return models.Module{
		Instance: "http-1", RPCQueue: "logos.channel.rpc.http-1", Status: models.ModuleStatusRegistered,
		Manifest: &models.ModuleManifest{ManagementManifest: protocol.ManagementManifest{
			SchemaVersion: protocol.ManagementSchemaV1, Capabilities: caps,
		}},
	}
}

func next(t *testing.T, ch <-chan *Event) *Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
		return nil
	}
}

func newRelay(reg fakeRegistry) (*Relay, *fakeRPC, *fakeSource) {
	rpc := &fakeRPC{}
	src := &fakeSource{ch: make(chan []byte, 8)}
	r := New(reg, rpc, src, zap.NewNop())
	r.newID = func() string { return "S1" }
	return r, rpc, src
}

func TestStreamRefusals(t *testing.T) {
	r, _, _ := newRelay(fakeRegistry{
		"plain": func() models.Module { m := module(); m.Instance = "plain"; return m }(),
		"dead": func() models.Module {
			m := module(protocol.CapabilityLogs)
			m.Status = models.ModuleStatusDead
			return m
		}(),
	})
	for instance, want := range map[string]error{
		"missing": ErrModuleNotFound,
		"plain":   ErrNotSupported,
		"dead":    ErrNotRegistered,
	} {
		if _, err := r.Stream(context.Background(), Request{Instance: instance}); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", instance, err, want)
		}
	}
}

func TestStreamRelaysBatchesAndStops(t *testing.T) {
	r, rpc, src := newRelay(fakeRegistry{"http-1": module(protocol.CapabilityLogs)})
	ctx, cancel := context.WithCancel(context.Background())
	events, err := r.Stream(ctx, Request{Instance: "http-1", MinLevel: protocol.LogDebug,
		Actor: models.ModuleActor{Type: "user", ID: "u1", Name: "admin"}})
	if err != nil {
		t.Fatal(err)
	}
	if src.opened != "S1" {
		t.Errorf("queue opened for %q", src.opened)
	}
	start := rpc.ops()[0]
	req := start.payload.(protocol.LogsStartRequest)
	if start.op != protocol.TypeMgmtLogsStart || start.queue != "logos.channel.rpc.http-1" ||
		req.StreamID != "S1" || req.MinLevel != protocol.LogDebug || req.Backlog != backlogRecords || req.Actor.ID != "u1" {
		t.Fatalf("start call = %+v", start)
	}
	if ev := next(t, events); ev.State != StateStreaming {
		t.Fatalf("first event = %+v", ev)
	}

	rec := protocol.LogRecord{Time: time.Now(), Level: protocol.LogInfo, Message: "hello"}
	src.send(t, protocol.LogBatch{StreamID: "S1", Seq: 1, Backlog: true, Records: []protocol.LogRecord{rec}})
	src.send(t, protocol.LogBatch{StreamID: "other", Seq: 2}) // not ours: ignored
	src.send(t, protocol.LogBatch{StreamID: "S1", Seq: 3, Dropped: 4})
	src.send(t, protocol.LogBatch{StreamID: "S1", Seq: 4, End: true, EndReason: protocol.LogEndShutdown})

	if ev := next(t, events); !ev.Backlog || len(ev.Records) != 1 || ev.Records[0].Message != "hello" || ev.Lost {
		t.Errorf("backlog event = %+v", ev)
	}
	if ev := next(t, events); !ev.Lost || ev.Dropped != 4 {
		t.Errorf("gap event = %+v", ev)
	}
	if ev := next(t, events); ev.State != StateEnded || ev.Reason != protocol.LogEndShutdown || ev.Lost {
		t.Errorf("end event = %+v", ev)
	}

	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops := rpc.ops()
		if last := ops[len(ops)-1]; last.op == protocol.TypeMgmtLogsStop {
			if last.payload.(protocol.LogsStopRequest).StreamID != "S1" {
				t.Errorf("stop = %+v", last)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("stream not stopped after the viewer left")
}

func TestStreamStartFailureClosesNothingLeaks(t *testing.T) {
	r, rpc, _ := newRelay(fakeRegistry{"http-1": module(protocol.CapabilityLogs)})
	rpc.fail = messaging.ErrRPCUnroutable
	if _, err := r.Stream(context.Background(), Request{Instance: "http-1"}); !errors.Is(err, messaging.ErrRPCUnroutable) {
		t.Fatalf("err = %v", err)
	}
}

func TestEmitCountsWhatTheViewerMissed(t *testing.T) {
	s := &stream{out: make(chan *Event, 1)}
	s.emit(&Event{Records: make([]protocol.LogRecord, 2)})
	s.emit(&Event{Records: make([]protocol.LogRecord, 3), Dropped: 1}) // buffer full
	<-s.out
	s.emit(&Event{})
	if ev := <-s.out; ev.Dropped != 4 {
		t.Errorf("dropped = %d, want 4 (3 records + 1 module drop)", ev.Dropped)
	}
}
