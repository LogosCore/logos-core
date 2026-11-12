package minionsync

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeTunnels notes what core hands it, answers with answers, and fills a
// message with fill.
type fakeTunnels struct {
	took       []int
	answers    []any
	bad        error
	fill       []any
	poll       time.Duration
	polls      bool
	room, most int
}

func (f *fakeTunnels) Fill(_ context.Context, _ uuid.UUID, room, most int, add func(cbor.RawMessage) bool) (int, time.Duration, bool) {
	f.room, f.most = room, most
	n := 0
	for _, it := range f.fill {
		b, _ := protocol.MarshalCBOR(it)
		if add(b) {
			n++
		}
	}
	f.fill = nil
	return n, f.poll, f.polls
}

func (f *fakeTunnels) Take(_ context.Context, _ uuid.UUID, raw cbor.RawMessage) ([]cbor.RawMessage, error) {
	f.took = append(f.took, protocol.ItemType(raw))
	if f.bad != nil {
		return nil, f.bad
	}
	var out []cbor.RawMessage
	for _, a := range f.answers {
		b, _ := protocol.MarshalCBOR(a)
		out = append(out, b)
	}
	return out, nil
}

func tunnelRig(t *testing.T) (*rig, uuid.UUID, *fakeTunnels) {
	t.Helper()
	r := newRig(t)
	sid, _ := openSession(t, r)
	tunnels := &fakeTunnels{}
	r.svc.WithTunnels(tunnels)
	return r, sid, tunnels
}

// The minion's tunnel items go to the tunnels, in order (its tunnel, a
// reverse port forward's stream, its tunnel_listen, and its tunnel_ctl: a
// process's exit), and what they answer goes back in the reply; one the
// tunnels find bad is a bad message.
func TestTunnelItemsGoToTheTunnels(t *testing.T) {
	r, sid, tunnels := tunnelRig(t)
	task := bytes.Repeat([]byte{4}, 16)
	room, exit := uint64(4096), int64(3)
	tunnels.answers = []any{protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: task, Stream: 7, Reason: protocol.TunnelCloseUnknown}}
	reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(),
		protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: task, Stream: 1, Result: protocol.TunnelOpen, Bound: "10.0.0.7:5000"},
		protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: task, Stream: 1, Data: []byte("hi")},
		protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: task, Stream: 1, Next: 3, Room: &room},
		protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: task, Stream: 2, Reason: protocol.TunnelCloseReset},
		protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: task, Stream: 3, Host: "10.0.0.53", Port: 53, Data: []byte{1}},
		protocol.TunnelItem{T: protocol.ItemTunnel, Task: task, Stream: 4, Host: "h", Port: 1},
		protocol.TunnelListenItem{T: protocol.ItemTunnelListen, Task: task, Bound: "0.0.0.0:8080"},
		protocol.TunnelCtlItem{T: protocol.ItemTunnelCtl, Task: task, Stream: 5, Exit: &exit})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{protocol.ItemTunnelReply, protocol.ItemTunnelData, protocol.ItemTunnelAck, protocol.ItemTunnelClose, protocol.ItemTunnelDatagram,
		protocol.ItemTunnel, protocol.ItemTunnelListen, protocol.ItemTunnelCtl}
	if len(tunnels.took) != len(want) {
		t.Fatalf("took %v, want %v", tunnels.took, want)
	}
	for i := range want {
		if tunnels.took[i] != want[i] {
			t.Fatalf("took %v, want %v", tunnels.took, want)
		}
	}
	msg := r.openUnder(t, sid, r.keys, reply)
	if closes := itemsOf(msg, protocol.ItemTunnelClose); len(closes) != len(want) {
		t.Errorf("answers %d, want one per item", len(closes))
	}
	if errs := itemsOf(msg, protocol.ItemErr); len(errs) != 0 {
		t.Fatalf("the minion's tunnel items: %d errs", len(errs))
	}
	tunnels.bad, tunnels.answers = errors.New("stream is 1 or more"), nil
	reply, err = r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: task, Next: 3})
	if err != nil {
		t.Fatal(err)
	}
	errs := itemsOf(r.openUnder(t, sid, r.keys, reply), protocol.ItemErr)
	var e protocol.ErrItem
	if len(errs) != 1 || protocol.UnmarshalCBOR(errs[0], &e) != nil || e.Code != protocol.ErrItemBadMessage || e.Msg != "stream is 1 or more" {
		t.Fatalf("a bad tunnel item: %d errs, %+v", len(errs), e)
	}
}

// Without tunnels, a tunnel item is one core does not know.
func TestTunnelItemsWithoutTunnels(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(),
		protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: bytes.Repeat([]byte{4}, 16), Stream: 1, Data: []byte("hi")})
	if err != nil {
		t.Fatal(err)
	}
	errs := itemsOf(r.openUnder(t, sid, r.keys, reply), protocol.ItemErr)
	var e protocol.ErrItem
	if len(errs) != 1 || protocol.UnmarshalCBOR(errs[0], &e) != nil || e.Code != protocol.ErrItemUnknownItem {
		t.Fatalf("%d errs, %+v", len(errs), e)
	}
}

// The tunnels' items go in the reply, within the message's room and its
// count of items; a minion that polls is told how soon to come back as
// the tunnels say (the sooner of theirs and a stream's), one over a
// socket is not.
func TestTunnelItemsInTheReply(t *testing.T) {
	r, sid, tunnels := tunnelRig(t)
	task := bytes.Repeat([]byte{4}, 16)
	checkIn := func(items ...any) protocol.CoreMessage {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), items...)
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return protocol.CoreMessage{}
		}
		return r.openUnder(t, sid, r.keys, reply)
	}
	within := func(msg protocol.CoreMessage) (uint64, bool) {
		t.Helper()
		polls := itemsOf(msg, protocol.ItemPoll)
		if len(polls) == 0 {
			return 0, false
		}
		var p protocol.PollItem
		if len(polls) != 1 || protocol.UnmarshalCBOR(polls[0], &p) != nil {
			t.Fatalf("polls %d", len(polls))
		}
		return p.Within, true
	}
	if _, ok := within(checkIn()); ok {
		t.Fatal("a poll with no tunnel")
	}
	tunnels.fill = []any{protocol.TunnelItem{T: protocol.ItemTunnel, Task: task, Stream: 1, Host: "dc01", Port: 445}}
	tunnels.poll, tunnels.polls = 0, true
	tunnels.answers = []any{protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: task, Stream: 9, Reason: protocol.TunnelCloseUnknown}}
	msg := checkIn(protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: task, Stream: 9, Next: 1})
	tunnels.answers = nil
	if opens := itemsOf(msg, protocol.ItemTunnel); len(opens) != 1 {
		t.Fatalf("opens %d", len(opens))
	}
	if w, ok := within(msg); !ok || w != 0 {
		t.Fatalf("within %d %v, want 0", w, ok)
	}
	// One answer, and a place kept for the poll.
	if tunnels.most != protocol.MaxMinionMessageItems-2 || tunnels.room <= 0 || tunnels.room > r.sessionRow(t, sid).MessageLimit() {
		t.Errorf("filled with room %d, most %d", tunnels.room, tunnels.most)
	}
	tunnels.poll = 250 * time.Millisecond
	if w, ok := within(checkIn()); !ok || w != 250 {
		t.Fatalf("within %d %v, want 250", w, ok)
	}
	streams := &fakeStreams{soon: true}
	r.svc.WithStreams(streams)
	tunnels.poll = 5 * time.Second
	if w, ok := within(checkIn()); !ok || w != uint64(pollSoon.Milliseconds()) {
		t.Fatalf("within %d %v with a stream held, want the stream's", w, ok)
	}
	c := newConnRig(t, r, sid, "c1")
	reply, err := c.exchange(t, protocol.NewMessageID(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg := r.openUnder(t, sid, r.keys, reply); len(itemsOf(msg, protocol.ItemPoll)) != 0 {
		t.Fatal("a poll over a socket")
	}
}

// A tunnel task holds no place in the minion's max_tasks: a standing
// allowance of 1 leaves room for a task beside a running SOCKS proxy, and
// for none beside a running shell.
func TestTunnelsHoldNoPlace(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	session := r.sessionRow(t, sid)
	one := uint64(1)
	now := time.Now()
	run := func(command, action string) {
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, Command: command, Action: action,
			State: models.MinionTaskRunning, AckedAt: &now})
	}
	run("tunnel", protocol.ActionTunnel)
	run("reverse_tunnel", protocol.ActionReverseTunnel)
	if n, err := r.svc.room(context.Background(), session, &one); err != nil || n != 1 {
		t.Fatalf("beside two tunnels: room %d %v", n, err)
	}
	run("shell", "")
	if n, err := r.svc.room(context.Background(), session, &one); err != nil || n != 0 {
		t.Fatalf("beside a shell: room %d %v", n, err)
	}
}
