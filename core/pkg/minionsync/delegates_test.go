package minionsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Delegation (delegates.go): minion B, a delegate, carries minion A's
// frames to core in delegate items, and core's back. A is the rig's build,
// B a build of the same operation whose factory's commands serve channel
// kinds (relay: tcp bind-persistent and dumbo bind-call; relay_in: http
// reverse-call and reverse-persistent). B checks in through the sync
// call, so what core has for A waits for B's next message.

// delegateTasks are fakeTasks that also queue and cancel.
type delegateTasks struct {
	*fakeTasks
	cancelled []uuid.UUID
}

func (f *delegateTasks) Insert(_ context.Context, t *models.MinionTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, *t)
	return nil
}

func (f *delegateTasks) RequestCancelWhy(_ context.Context, id uuid.UUID, at time.Time, _ string) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.find(id)
	if t == nil {
		return nil, repository.ErrNotFound
	}
	t.CancelRequestedAt = &at
	f.cancelled = append(f.cancelled, id)
	c := *t
	return &c, nil
}

func (f *delegateTasks) set(id uuid.UUID, change func(t *models.MinionTask)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f.find(id))
}

type fakeDelegateSchemas map[string]*models.ModuleBuildSchema

func (f fakeDelegateSchemas) LatestBuildSchema(_ context.Context, name string) (*models.ModuleBuildSchema, error) {
	return f[name], nil
}

type delegateRig struct {
	*rig
	tasks    *delegateTasks
	bKeys    protocol.AES256CBCHMACSHA256Material
	bBuild   *models.Build
	bSession uuid.UUID
	// rev is B's reverse_delegate task, running; bind its delegate task,
	// running unless a test removes it.
	rev, bind uuid.UUID
}

func randomKeys(t *testing.T) (protocol.AES256CBCHMACSHA256Material, []byte) {
	t.Helper()
	k := protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}
	_, _ = rand.Read(k.EncKey)
	_, _ = rand.Read(k.MACKey)
	m, _ := json.Marshal(k)
	return k, m
}

// newDelegateRig is A's build with conn as its one connection, and B in
// session, its connection carrying limit bytes at most (0: the protocol's).
func newDelegateRig(t *testing.T, conn protocol.BuildConnection, limit int) *delegateRig {
	t.Helper()
	r := newRig(t)
	r.build.Connections.Items = []protocol.BuildConnection{conn}
	keys, material := randomKeys(t)
	bc := endpoint(protocol.KindReverseCall, "http")
	bc.MaxMessage = limit
	bb := &models.Build{BuildID: uuid.New(), OperationID: r.build.OperationID, ModuleName: "bob", Commands: []string{"relay", "relay_in"},
		Crypto:      &protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: material},
		Connections: models.BuildConnections{Items: []protocol.BuildConnection{bc}}}
	r.builds[bb.BuildID] = bb
	tasks := &delegateTasks{fakeTasks: &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}}
	kinds := []string{protocol.KindReverseCall, protocol.KindReversePersistent}
	r.svc.WithTasks(tasks).WithDelegates(fakeDelegateSchemas{"bob": {BuildSchema: protocol.BuildSchema{Commands: []protocol.BuildCommand{
		{ID: "relay", Op: 30, Action: protocol.ActionDelegate, Kinds: kinds, Serves: []protocol.ChannelVersions{
			{ModuleName: "tcp", Versions: []int{1}, Kinds: []string{protocol.KindBindPersistent}},
			{ModuleName: "dumbo", Versions: []int{1}, Kinds: []string{protocol.KindBindCall}}}},
		{ID: "relay_in", Op: 31, Action: protocol.ActionReverseDelegate, Kinds: kinds, Serves: []protocol.ChannelVersions{
			{ModuleName: "http", Versions: []int{1}, Kinds: kinds}}},
	}}}})
	reply, err := r.svc.Handle(context.Background(), bb.BuildID.String(), "203.0.113.9", sealedMinionMessage(t, keys, bb.BuildID, nil, hello("b")))
	if err != nil {
		t.Fatal(err)
	}
	d := &delegateRig{rig: r, tasks: tasks, bKeys: keys, bBuild: bb, bSession: sessionID(t, openCore(t, keys, bb.BuildID, reply))}
	now := time.Now()
	for _, c := range []struct {
		id      *uuid.UUID
		command string
		action  string
	}{{&d.rev, "relay_in", protocol.ActionReverseDelegate}, {&d.bind, "relay", protocol.ActionDelegate}} {
		*c.id = uuid.New()
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: *c.id, OperationID: bb.OperationID, SessionID: d.bSession,
			Command: c.command, Action: c.action, State: models.MinionTaskRunning, AckedAt: &now})
	}
	return d
}

// fromB sends a message of B's with items through the sync call, and
// returns the delegate items of core's reply, read as a delegate reads
// them, and whether it said to come back soon.
func (d *delegateRig) fromB(t *testing.T, items ...any) ([]protocol.DelegateItem, bool) {
	t.Helper()
	reply, err := d.svc.Handle(context.Background(), d.bSession.String(), "203.0.113.9", sealedMinionMessage(t, d.bKeys, d.bSession, nil, items...))
	if err != nil {
		t.Fatalf("B's message: %v", err)
	}
	if reply == nil {
		return nil, false
	}
	m := openCore(t, d.bKeys, d.bSession, reply)
	var out []protocol.DelegateItem
	polled := false
	for _, raw := range m.Items {
		switch protocol.ItemType(raw) {
		case protocol.ItemDelegate:
			it, err := protocol.ReadDelegate(raw, false)
			if err != nil {
				t.Fatalf("core's delegate item does not read: %v", err)
			}
			out = append(out, it)
		case protocol.ItemPoll:
			polled = true
		}
	}
	return out, polled
}

// step is the item of items of step, the test failing without one.
func step(t *testing.T, items []protocol.DelegateItem, s int) protocol.DelegateItem {
	t.Helper()
	for _, it := range items {
		if it.Step == s {
			return it
		}
	}
	t.Fatalf("no %s among %+v", protocol.DelegateStepName(s), items)
	return protocol.DelegateItem{}
}

// item is a delegate item of task.
func item(task uuid.UUID, s int, n uint64) protocol.DelegateItem {
	return protocol.DelegateItem{T: protocol.ItemDelegate, Task: task[:], Step: s, N: n}
}

// framed is it carrying A's frame under id.
func framed(it protocol.DelegateItem, id uuid.UUID, frame []byte) protocol.DelegateItem {
	it.ID, it.Frame = id[:], frame
	return it
}

func TestDelegatedSync(t *testing.T) {
	d := newDelegateRig(t, endpoint(protocol.KindReverseCall, "http"), 0)
	ctx := context.Background()
	// A's hello, as A sends it to the channel, in a sync of B's.
	sync := framed(item(d.rev, protocol.DelegateSync, 1), d.build.BuildID, sealedMinionMessage(t, d.keys, d.build.BuildID, nil, hello("a")))
	sync.Serve, sync.Peer = 1, []byte{10, 0, 0, 7}
	items, _ := d.fromB(t, sync)
	reply := step(t, items, protocol.DelegateReply)
	if reply.N != 1 || reply.Status != protocol.ReplyOK || !bytes.Equal(reply.ID, d.build.BuildID[:]) {
		t.Fatalf("the reply %+v", reply)
	}
	a := sessionID(t, openCore(t, d.keys, d.build.BuildID, reply.Frame))
	row := d.sessionRow(t, a)
	bNumber := d.sessionRow(t, d.bSession).Number
	if len(row.Via) != 1 || row.Via[0] != (models.MinionSessionVia{SessionID: d.bSession, Number: bNumber, TaskID: d.rev}) || row.ViaSince == nil {
		t.Fatalf("A's path %+v since %v", row.Via, row.ViaSince)
	}
	since := *row.ViaSince
	if row.ConnKind != protocol.KindReverseCall || row.PeerAddress != "10.0.0.7" || row.OperationID != d.build.OperationID {
		t.Fatalf("A's session %+v", row)
	}
	// A checks in with nothing to say: the sync call's 204.
	again := framed(item(d.rev, protocol.DelegateSync, 2), a, sealedMinionMessage(t, d.keys, a, nil))
	again.Serve = 1
	items, _ = d.fromB(t, again)
	if r := step(t, items, protocol.DelegateReply); r.Status != protocol.ReplyNoContent || r.Frame != nil {
		t.Fatalf("an empty check-in's reply %+v", r)
	}
	// The same way: since when it came that way stays.
	if row := d.sessionRow(t, a); len(row.Via) != 1 || row.ViaSince == nil || !row.ViaSince.Equal(since) {
		t.Fatalf("A's path after another message %+v since %v, not %v", row.Via, row.ViaSince, since)
	}
	// One straight from a channel: no delegates on its way.
	if _, err := d.svc.Handle(ctx, a.String(), "198.51.100.4", sealedMinionMessage(t, d.keys, a, nil)); err != nil {
		t.Fatal(err)
	}
	if row := d.sessionRow(t, a); row.Via != nil || row.ViaSince != nil {
		t.Fatalf("A's path after a message of its own %+v", row.Via)
	}
	// A message of another operation's minion, or of B itself (a loop),
	// is not taken: the sync call's 422.
	ok, okMat := randomKeys(t)
	other := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(), ModuleName: "bob",
		Crypto: &protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: okMat}}
	d.builds[other.BuildID] = other
	for i, f := range []protocol.DelegateItem{
		framed(item(d.rev, protocol.DelegateSync, 3), other.BuildID, sealedMinionMessage(t, ok, other.BuildID, nil, hello("x"))),
		framed(item(d.rev, protocol.DelegateSync, 4), d.bSession, sealedMinionMessage(t, d.bKeys, d.bSession, nil)),
	} {
		f.Serve = 1
		items, _ = d.fromB(t, f)
		if r := step(t, items, protocol.DelegateReply); r.Status != 422 {
			t.Fatalf("refusal %d: %+v", i, r)
		}
	}
	// An item of a task B does not run gets a close; a sync in a delegate
	// task (it serves bind kinds) an err.
	items, _ = d.fromB(t, framed(item(uuid.New(), protocol.DelegateFrame, 9), a, []byte{1}))
	if c := step(t, items, protocol.DelegateClose); c.Reason != protocol.DelegateTaskEnded || c.N != 9 {
		t.Fatalf("an unknown task's close %+v", c)
	}
	bad := framed(item(d.bind, protocol.DelegateSync, 5), a, []byte{1})
	bad.Serve = 1
	reply2, err := d.svc.Handle(ctx, d.bSession.String(), "", sealedMinionMessage(t, d.bKeys, d.bSession, nil, bad))
	if err != nil {
		t.Fatal(err)
	}
	m := openCore(t, d.bKeys, d.bSession, reply2)
	var e protocol.ErrItem
	if len(m.Items) == 0 || protocol.UnmarshalCBOR(m.Items[0], &e) != nil || e.Code != protocol.ErrItemBadMessage {
		t.Fatalf("a sync in a delegate task: %+v", m)
	}
}

// A connection through a delegate is A's, as a socket is: core's pushes go
// over it as frames for the delegate, and B's close ends it.
func TestDelegatedConnection(t *testing.T) {
	d := newDelegateRig(t, endpoint(protocol.KindReversePersistent, "http"), 0)
	ctx := context.Background()
	first := framed(item(d.rev, protocol.DelegateFrame, 1), d.build.BuildID, sealedMinionMessage(t, d.keys, d.build.BuildID, nil, hello("a")))
	first.Serve, first.Peer = 1, []byte{10, 0, 0, 7}
	items, _ := d.fromB(t, first)
	f := step(t, items, protocol.DelegateFrame)
	if f.N != 1 || f.Seq != 0 {
		t.Fatalf("core's first frame %+v", f)
	}
	a := sessionID(t, openCore(t, d.keys, d.build.BuildID, f.Frame))
	two := uint64(2)
	items, _ = d.fromB(t, framed(item(d.rev, protocol.DelegateFrame, 1), a,
		sealedMinionMessage(t, d.keys, a, nil, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &two})))
	if f := step(t, items, protocol.DelegateFrame); f.Seq != 1 {
		t.Fatalf("core's reply to its check-in %+v", f)
	}
	if !d.svc.Connected(a) || d.sessionRow(t, a).ConnKind != protocol.KindReversePersistent {
		t.Fatal("A is not connected through its delegate")
	}
	if via := d.sessionRow(t, a).Via; len(via) != 1 || via[0].SessionID != d.bSession || via[0].TaskID != d.rev {
		t.Fatalf("A's path %+v", via)
	}
	// A task for A goes as a frame in B's next reply, and B is told to come
	// back soon while one waits.
	task := uuid.New()
	d.tasks.mu.Lock()
	d.tasks.rows = append(d.tasks.rows, models.MinionTask{TaskID: task, SessionID: a, OperationID: d.build.OperationID, Command: "shell",
		Op: 11, State: models.MinionTaskQueued})
	d.tasks.mu.Unlock()
	d.svc.Notify(ctx, a)
	items, _ = d.fromB(t)
	push := step(t, items, protocol.DelegateFrame)
	m := openCore(t, d.keys, a, push.Frame)
	var ti protocol.TaskItem
	if push.Seq != 2 || len(m.ID) == 0 || len(m.Items) != 1 || protocol.UnmarshalCBOR(m.Items[0], &ti) != nil || !bytes.Equal(ti.Task, task[:]) {
		t.Fatalf("the push %+v: %+v", push, m)
	}
	if sum, ok := d.svc.Delegate(d.rev); !ok || len(sum.Connections) != 1 || sum.Connections[0].Session != a || sum.Connections[0].Peer != "10.0.0.7" {
		t.Fatalf("the delegate's view %+v", sum)
	}
	// B says it closed: A is not connected; a frame of it after gets a
	// close back.
	d.fromB(t, func() protocol.DelegateItem {
		c := item(d.rev, protocol.DelegateClose, 1)
		c.Reason = protocol.DelegateClosed
		return c
	}())
	if d.svc.Connected(a) {
		t.Fatal("A still connected")
	}
	items, _ = d.fromB(t, framed(item(d.rev, protocol.DelegateFrame, 1), a, sealedMinionMessage(t, d.keys, a, nil)))
	if c := step(t, items, protocol.DelegateClose); c.Reason != protocol.DelegateNoConnection {
		t.Fatalf("a frame of a closed connection: %+v", c)
	}
}

// What does not fit a delegate's messages goes in pieces, both ways.
func TestDelegatedPieces(t *testing.T) {
	const limit = 4096
	d := newDelegateRig(t, endpoint(protocol.KindReversePersistent, "http"), limit)
	ctx := context.Background()
	big := hello("a")
	big.X = map[string]any{"pad": strings.Repeat("x", 9000)}
	first := framed(item(d.rev, protocol.DelegateFrame, 1), d.build.BuildID, sealedMinionMessage(t, d.keys, d.build.BuildID, nil, big))
	first.Serve = 1
	pieces := first.Pieces(3000)
	if len(pieces) < 3 {
		t.Fatalf("%d pieces", len(pieces))
	}
	var items []protocol.DelegateItem
	for _, p := range pieces {
		items, _ = d.fromB(t, p)
	}
	a := sessionID(t, openCore(t, d.keys, d.build.BuildID, step(t, items, protocol.DelegateFrame).Frame))
	two := uint64(2)
	d.fromB(t, framed(item(d.rev, protocol.DelegateFrame, 1), a,
		sealedMinionMessage(t, d.keys, a, nil, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &two})))
	// A task larger than B's messages reaches B in pieces, each message
	// within B's limit, polled while the rest waits.
	args, _ := protocol.MarshalCBOR(protocol.Args{1: strings.Repeat("y", 10000)})
	task := uuid.New()
	d.tasks.mu.Lock()
	d.tasks.rows = append(d.tasks.rows, models.MinionTask{TaskID: task, SessionID: a, OperationID: d.build.OperationID, Command: "shell",
		Op: 11, WireArgs: args, State: models.MinionTaskQueued})
	d.tasks.mu.Unlock()
	d.svc.Notify(ctx, a)
	var j protocol.DelegateJoiner
	var whole []byte
	for i := 0; i < 10 && whole == nil; i++ {
		reply, err := d.svc.Handle(ctx, d.bSession.String(), "", sealedMinionMessage(t, d.bKeys, d.bSession, nil))
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := protocol.OpenFrame(d.bKeys, d.bSession, protocol.FrameCoreToMinion, reply)
		if len(plain) > limit {
			t.Fatalf("a message of %d bytes to B, its limit %d", len(plain), limit)
		}
		polled := false
		for _, raw := range openCore(t, d.bKeys, d.bSession, reply).Items {
			if protocol.ItemType(raw) == protocol.ItemPoll {
				polled = true
			}
			if protocol.ItemType(raw) != protocol.ItemDelegate {
				continue
			}
			it, _ := protocol.ReadDelegate(raw, false)
			if got, done, _ := j.Add(it, time.Now()); done && it.Step == protocol.DelegateFrame {
				whole = got
			}
		}
		if whole == nil && !polled {
			t.Fatal("pieces wait, and B is not told to come back soon")
		}
	}
	m := openCore(t, d.keys, a, whole)
	var ti protocol.TaskItem
	if len(m.Items) != 1 || protocol.UnmarshalCBOR(m.Items[0], &ti) != nil || !bytes.Equal(ti.Task, task[:]) {
		t.Fatalf("the task joined from pieces: %+v", m)
	}
}

// waitStep sends B's empty messages until core's reply carries a step of
// s, and returns it.
func (d *delegateRig) waitStep(t *testing.T, s int) protocol.DelegateItem {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, _ := d.fromB(t)
		for _, it := range items {
			if it.Step == s {
				return it
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s came", protocol.DelegateStepName(s))
	return protocol.DelegateItem{}
}

// A bind-persistent target that names a delegate opens through it: an
// open step to B, B's answer, then A's frames over it; Disconnect closes
// it through B.
func TestDelegatedOpen(t *testing.T) {
	tcp := endpoint(protocol.KindBindPersistent, "tcp")
	d := newDelegateRig(t, tcp, 0)
	ctx := context.Background()
	targets := &fakeTargets{rows: map[uuid.UUID]models.MinionTarget{}}
	d.svc.WithTargets(targets, nil)
	tg := &models.MinionTarget{OperationID: d.build.OperationID, BuildID: d.build.BuildID, Conn: 1, ModuleName: "tcp",
		Kind: protocol.KindBindPersistent, Policy: models.PolicyManual, Values: models.JSONMap{"host": "10.0.0.5"}, Name: "10.0.0.5",
		DelegateID: &d.bSession, State: models.TargetNew}
	_ = targets.Insert(ctx, tg)
	done := make(chan *models.MinionTarget, 1)
	go func() {
		got, err := d.svc.ConnectTarget(ctx, tg.TargetID)
		if err != nil {
			t.Error(err)
		}
		done <- got
	}()
	open := d.waitStep(t, protocol.DelegateOpen)
	if open.Serve != 1 || open.Values["host"] != "10.0.0.5" || open.Deadline == 0 || !bytes.Equal(open.Task, d.bind[:]) {
		t.Fatalf("the open %+v", open)
	}
	answer := item(d.bind, protocol.DelegateAnswer, open.N)
	answer.Peer = []byte{10, 0, 0, 5}
	d.fromB(t, answer)
	items, _ := d.fromB(t, framed(item(d.bind, protocol.DelegateFrame, open.N), d.build.BuildID,
		sealedMinionMessage(t, d.keys, d.build.BuildID, nil, hello("a"))))
	a := sessionID(t, openCore(t, d.keys, d.build.BuildID, step(t, items, protocol.DelegateFrame).Frame))
	if got := <-done; got.State != models.TargetConnected || !d.svc.Connected(a) {
		t.Fatalf("the target %+v", got)
	}
	waitFor(t, "the target linked to A's session", func() bool {
		got, _ := targets.FindByTargetID(ctx, tg.TargetID)
		return got.SessionID != nil && *got.SessionID == a
	})
	go func() { _, _ = d.svc.DisconnectTarget(ctx, tg.TargetID) }()
	closing := d.waitStep(t, protocol.DelegateClose)
	if closing.N != open.N || closing.Reason != protocol.DelegateClosed {
		t.Fatalf("core's close %+v", closing)
	}
	closed := item(d.bind, protocol.DelegateClose, open.N)
	closed.Reason = protocol.DelegateClosed
	d.fromB(t, closed)
	waitFor(t, "A disconnected", func() bool { return !d.svc.Connected(a) })
	// The close answered, core no longer sends it.
	items, _ = d.fromB(t)
	for _, it := range items {
		if it.Step == protocol.DelegateClose {
			t.Fatal("a close sent again once answered")
		}
	}
}

// A target whose delegate runs no task serving it waits, connecting, and
// core queues one; a delegate whose minion serves nothing of the kind is
// refused.
func TestDelegateTaskQueued(t *testing.T) {
	d := newDelegateRig(t, endpoint(protocol.KindBindPersistent, "tcp"), 0)
	ctx := context.Background()
	d.tasks.set(d.bind, func(t *models.MinionTask) { t.State = models.MinionTaskDone })
	targets := &fakeTargets{rows: map[uuid.UUID]models.MinionTarget{}}
	d.svc.WithTargets(targets, nil)
	tg := &models.MinionTarget{OperationID: d.build.OperationID, BuildID: d.build.BuildID, Conn: 1, ModuleName: "tcp",
		Kind: protocol.KindBindPersistent, Policy: models.PolicyManual, Values: models.JSONMap{"host": "10.0.0.5"},
		DelegateID: &d.bSession, State: models.TargetNew}
	_ = targets.Insert(ctx, tg)
	got, err := d.svc.ConnectTarget(ctx, tg.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != models.TargetConnecting || !strings.Contains(got.StateDetail, "to take its delegate task") || got.NextCallAt == nil {
		t.Fatalf("the target %+v", got)
	}
	open, _ := d.tasks.OpenWithAction(ctx, d.bSession, protocol.ActionDelegate)
	if len(open) != 1 || open[0].Command != "relay" || open[0].Args[protocol.ParamStopIdle] != "1h" || open[0].IssuedBy != uuid.Nil {
		t.Fatalf("the task core queued %+v", open)
	}
	// Asked again, it does not queue another.
	_, _ = d.svc.ConnectTarget(ctx, tg.TargetID)
	if open, _ = d.tasks.OpenWithAction(ctx, d.bSession, protocol.ActionDelegate); len(open) != 1 {
		t.Fatalf("%d tasks queued", len(open))
	}
	// B acks it: the target waiting for it is called again, at once.
	d.tasks.set(open[0].TaskID, func(t *models.MinionTask) { now := time.Now(); t.AckedAt, t.State = &now, models.MinionTaskRunning })
	d.svc.checkDelegateTask(ctx, open[0].TaskID)
	open2 := d.waitStep(t, protocol.DelegateOpen)
	if !bytes.Equal(open2.Task, open[0].TaskID[:]) {
		t.Fatalf("the open went to %x, not the task core queued", open2.Task)
	}
	if why, err := d.svc.CheckDelegate(ctx, d.bSession, d.build.OperationID, "tcp", protocol.KindBindPersistent, 1); err != nil || why != "" {
		t.Fatalf("B can carry it: %q %v", why, err)
	}
	if why, _ := d.svc.CheckDelegate(ctx, d.bSession, d.build.OperationID, "ssh", protocol.KindBindPersistent, 1); !strings.Contains(why, "serves no ssh") {
		t.Fatalf("B serves no ssh: %q", why)
	}
	if why, _ := d.svc.CheckDelegate(ctx, d.bSession, uuid.New(), "tcp", protocol.KindBindPersistent, 1); !strings.Contains(why, "another operation") {
		t.Fatalf("another operation's: %q", why)
	}
}

// A bind-call target that names a delegate is called through it: core's
// message in a call step, the page's answer in B's answer.
func TestDelegatedCall(t *testing.T) {
	page := endpoint(protocol.KindBindCall, "dumbo")
	d := newDelegateRig(t, page, 0)
	ctx := context.Background()
	targets := &fakeTargets{rows: map[uuid.UUID]models.MinionTarget{}}
	d.svc.WithTargets(targets, nil)
	tg := &models.MinionTarget{OperationID: d.build.OperationID, BuildID: d.build.BuildID, Conn: 1, ModuleName: "dumbo",
		Kind: protocol.KindBindCall, Values: models.JSONMap{"url": "http://10.0.0.5/a.php"}, DelegateID: &d.bSession, State: models.TargetNew}
	_ = targets.Insert(ctx, tg)
	done := make(chan *models.MinionTarget, 1)
	go func() {
		got, err := d.svc.CheckTarget(ctx, tg.TargetID)
		if err != nil {
			t.Error(err)
		}
		done <- got
	}()
	call := d.waitStep(t, protocol.DelegateCall)
	if call.Serve != 2 || !bytes.Equal(call.ID, d.build.BuildID[:]) || call.Values["url"] != "http://10.0.0.5/a.php" {
		t.Fatalf("the call %+v", call)
	}
	rehello := openCore(t, d.keys, d.build.BuildID, call.Frame)
	answer := framed(item(d.bind, protocol.DelegateAnswer, call.N), d.build.BuildID,
		sealedMinionMessage(t, d.keys, d.build.BuildID, rehello.ID, hello("page")))
	answer.Status = 200
	d.fromB(t, answer)
	got := <-done
	if got.State != models.TargetConnected || got.SessionID == nil {
		t.Fatalf("the target %+v", got)
	}
	// The answer came in B's message: A's path is B's delegate task.
	if via := d.sessionRow(t, *got.SessionID).Via; len(via) != 1 || via[0].SessionID != d.bSession || via[0].TaskID != d.bind ||
		via[0].Number != d.sessionRow(t, d.bSession).Number {
		t.Fatalf("A's path %+v", via)
	}
}

// RunDelegates' look: a delegate gone lost takes its connections with it;
// a task ended does; one idle past its stop_idle is stopped.
func TestDelegatesLook(t *testing.T) {
	d := newDelegateRig(t, endpoint(protocol.KindReversePersistent, "http"), 0)
	ctx := context.Background()
	first := framed(item(d.rev, protocol.DelegateFrame, 1), d.build.BuildID, sealedMinionMessage(t, d.keys, d.build.BuildID, nil, hello("a")))
	first.Serve = 1
	items, _ := d.fromB(t, first)
	a := sessionID(t, openCore(t, d.keys, d.build.BuildID, step(t, items, protocol.DelegateFrame).Frame))
	past := time.Now().Add(-time.Hour)
	d.sessions.mu.Lock()
	d.sessionRow(t, d.bSession).LateAt, d.sessionRow(t, d.bSession).LostAt = &past, &past
	d.sessions.mu.Unlock()
	d.svc.lookAtDelegates(ctx)
	if d.svc.Connected(a) {
		t.Fatal("A connected through a delegate gone lost")
	}
	// Back: a new connection, then its task ends.
	d.sessions.mu.Lock()
	d.sessionRow(t, d.bSession).LateAt, d.sessionRow(t, d.bSession).LostAt = nil, nil
	d.sessions.mu.Unlock()
	first.N = 2
	first.Frame = sealedMinionMessage(t, d.keys, a, nil)
	first.ID = a[:]
	d.fromB(t, first)
	if !d.svc.Connected(a) {
		t.Fatal("A not connected again")
	}
	d.tasks.set(d.rev, func(t *models.MinionTask) { t.State = models.MinionTaskDone })
	d.svc.checkDelegateTask(ctx, d.rev)
	if d.svc.Connected(a) {
		t.Fatal("A connected through an ended task")
	}
	if _, ok := d.svc.Delegate(d.rev); ok {
		t.Fatal("an ended task still held")
	}
	// Idle: the delegate task's stop_idle past, nothing through it.
	d.tasks.set(d.bind, func(t *models.MinionTask) { t.Args = map[string]any{protocol.ParamStopIdle: "1ms"} })
	b, _ := d.sessions.FindBySessionID(ctx, d.bSession)
	if dt, err := d.svc.delegateTaskOf(ctx, b, d.bind); err != nil || dt == nil || dt.stopIdle != time.Millisecond {
		t.Fatalf("the delegate task %+v %v", dt, err)
	}
	time.Sleep(5 * time.Millisecond)
	d.svc.lookAtDelegates(ctx)
	if len(d.tasks.cancelled) != 1 || d.tasks.cancelled[0] != d.bind {
		t.Fatalf("cancelled %v", d.tasks.cancelled)
	}
}

// A delegate's delegate: what the inner one carries came through both,
// nearest first.
func TestDelegatedPathNests(t *testing.T) {
	c := &models.MinionSession{SessionID: uuid.New(), Number: 2}
	b := &models.MinionSession{SessionID: uuid.New(), Number: 3}
	cTask, bTask := &delegateTask{id: uuid.New()}, &delegateTask{id: uuid.New()}
	ctx := throughDelegate(throughDelegate(context.Background(), c, cTask), b, bTask)
	d, ok := delegatedOf(ctx)
	want := []models.MinionSessionVia{{SessionID: b.SessionID, Number: 3, TaskID: bTask.id}, {SessionID: c.SessionID, Number: 2, TaskID: cTask.id}}
	if !ok || !slices.Equal(d.via, want) {
		t.Fatalf("the path %+v", d.via)
	}
}
