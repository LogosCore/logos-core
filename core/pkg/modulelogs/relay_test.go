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
	mu     sync.Mutex
	ch     chan []byte
	opened string
	opens  int
}

func (f *fakeSource) Open(_ context.Context, id string) (<-chan []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = id
	f.opens++
	if f.opens > 1 {
		f.ch = make(chan []byte, 8) // a fresh queue after the old one closed
	}
	return f.ch, nil
}

func (f *fakeSource) send(t *testing.T, b protocol.LogBatch) {
	t.Helper()
	body, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	ch := f.ch
	f.mu.Unlock()
	ch <- body
}

// lose closes the current queue, as a dropped broker connection does.
func (f *fakeSource) lose() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.ch)
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

func TestStreamRecreatedAfterModuleEndedIt(t *testing.T) {
	r, rpc, src := newRelay(fakeRegistry{"http-1": module(protocol.CapabilityLogs)})
	r.renew = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := r.Stream(ctx, Request{Instance: "http-1"})
	if err != nil {
		t.Fatal(err)
	}
	next(t, events) // STREAMING
	src.send(t, protocol.LogBatch{StreamID: "S1", Seq: 1, End: true, EndReason: protocol.LogEndShutdown})
	if ev := next(t, events); ev.State != StateEnded {
		t.Fatalf("end event = %+v", ev)
	}
	// The next renewal recreates the stream with a backlog and says so.
	if ev := next(t, events); ev.State != StateStreaming {
		t.Fatalf("after renewal = %+v", ev)
	}
	var backlogs []int
	for _, c := range rpc.ops() {
		if c.op == protocol.TypeMgmtLogsStart {
			backlogs = append(backlogs, c.payload.(protocol.LogsStartRequest).Backlog)
		}
	}
	if len(backlogs) < 2 || backlogs[1] != backlogRecords {
		t.Errorf("start backlogs = %v, want the recreating renewal to ask for %d", backlogs, backlogRecords)
	}
	for _, b := range backlogs[2:] {
		if b != 0 {
			t.Errorf("plain renewal asked for a backlog: %v", backlogs)
		}
	}
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

func TestReplacedStreamEndsForGood(t *testing.T) {
	r, rpc, src := newRelay(fakeRegistry{"http-1": module(protocol.CapabilityLogs)})
	r.renew = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := r.Stream(ctx, Request{Instance: "http-1"})
	if err != nil {
		t.Fatal(err)
	}
	next(t, events) // STREAMING
	src.send(t, protocol.LogBatch{StreamID: "S1", Seq: 1, End: true, EndReason: protocol.LogEndReplaced})
	if ev := next(t, events); ev.State != StateEnded || ev.Reason != protocol.LogEndReplaced {
		t.Fatalf("end event = %+v", ev)
	}
	time.Sleep(60 * time.Millisecond) // several renew periods
	starts := 0
	for _, c := range rpc.ops() {
		if c.op == protocol.TypeMgmtLogsStart {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("starts = %d, want 1: a replaced stream must not reclaim its slot", starts)
	}
	select {
	case ev := <-events:
		t.Errorf("event after replacement: %+v", ev)
	default:
	}
}

func TestQueueLossReopens(t *testing.T) {
	r, _, src := newRelay(fakeRegistry{"http-1": module(protocol.CapabilityLogs)})
	r.renew = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := r.Stream(ctx, Request{Instance: "http-1"})
	if err != nil {
		t.Fatal(err)
	}
	next(t, events) // STREAMING
	src.lose()
	if ev := next(t, events); ev.State != StateDisconnected {
		t.Fatalf("after queue loss = %+v", ev)
	}
	if ev := next(t, events); ev.State != StateStreaming {
		t.Fatalf("after reopen = %+v", ev)
	}
	src.send(t, protocol.LogBatch{StreamID: "S1", Seq: 1, Records: []protocol.LogRecord{{Message: "back"}}})
	if ev := next(t, events); len(ev.Records) != 1 || ev.Records[0].Message != "back" {
		t.Errorf("record after reopen = %+v", ev)
	}
}

func TestEmitKeepsLatestStateForSlowViewer(t *testing.T) {
	s := &stream{out: make(chan *Event, 1)}
	s.emit(&Event{Records: make([]protocol.LogRecord, 1)})         // fills the buffer
	s.emit(&Event{State: StateDisconnected, Reason: "unroutable"}) // dropped, remembered
	<-s.out
	s.emit(&Event{Records: make([]protocol.LogRecord, 2)})
	if ev := <-s.out; ev.State != StateDisconnected || ev.Reason != "unroutable" {
		t.Errorf("state not carried on the next event: %+v", ev)
	}
}
