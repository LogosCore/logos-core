package minionsync

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

// fakeTargets keeps targets in memory.
type fakeTargets struct {
	mu   sync.Mutex
	rows map[uuid.UUID]models.MinionTarget
}

func (f *fakeTargets) Insert(_ context.Context, t *models.MinionTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.TargetID == uuid.Nil {
		t.TargetID = uuid.New()
	}
	f.rows[t.TargetID] = *t
	return nil
}

func (f *fakeTargets) FindByTargetID(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	return &t, nil
}

func (f *fakeTargets) FindBySessionID(_ context.Context, sid uuid.UUID) (*models.MinionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.rows {
		if t.SessionID != nil && *t.SessionID == sid {
			return &t, nil
		}
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeTargets) FindByOperationIDWithCursor(context.Context, uuid.UUID, repository.MinionTargetFilter, *pagination.Cursor, int64, bool) ([]models.MinionTarget, error) {
	return nil, nil
}

func (f *fakeTargets) CountByOperationID(context.Context, uuid.UUID, repository.MinionTargetFilter) (int64, error) {
	return 0, nil
}

func (f *fakeTargets) SetValues(context.Context, uuid.UUID, models.JSONMap, string, bool, time.Time) error {
	return nil
}

func (f *fakeTargets) SetCallState(_ context.Context, t *models.MinionTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[t.TargetID] = *t
	return nil
}

func (f *fakeTargets) ClaimDue(_ context.Context, now time.Time) (*models.MinionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, t := range f.rows {
		if t.NextCallAt != nil && !t.NextCallAt.After(now) {
			t.NextCallAt = nil
			f.rows[id] = t
			return &t, nil
		}
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeTargets) Delete(context.Context, uuid.UUID) (bool, error)      { return false, nil }
func (f *fakeTargets) DeleteByOperationID(context.Context, uuid.UUID) error { return nil }

// fakePage is a channel and the page it calls: it opens what core sends
// with the build's keys, under whatever id it came, and answers under the
// same id. A rehello gets a hello and a get_tasks; each task an ack and its
// result.
type fakePage struct {
	t    *testing.T
	keys protocol.AES256CBCHMACSHA256Material
	// answer, when set, is what the channel answers instead.
	answer *protocol.LinkFrame
	// noTasks: the page asks for no tasks.
	noTasks bool
	// more, when set, are items the page adds to its answer to msg (a
	// file's next chunk, say).
	more func(msg protocol.CoreMessage) []any

	mu       sync.Mutex
	calls    []protocol.CoreMessage
	ids      []uuid.UUID
	values   []map[string]any
	modules  []string
	maxTasks uint64
}

func (p *fakePage) Call(_ context.Context, module string, c channellink.Call) (protocol.LinkFrame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modules = append(p.modules, module)
	p.values = append(p.values, c.Values)
	p.ids = append(p.ids, c.ID)
	if p.answer != nil {
		return *p.answer, nil
	}
	plain, err := protocol.OpenFrame(p.keys, c.ID, protocol.FrameCoreToMinion, c.Data)
	if err != nil {
		p.t.Errorf("the page cannot open core's message: %v", err)
		return protocol.LinkFrame{}, err
	}
	var msg protocol.CoreMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		p.t.Fatal(err)
	}
	p.calls = append(p.calls, msg)
	var out []any
	var acked [][]byte
	for _, raw := range msg.Items {
		switch protocol.ItemType(raw) {
		case protocol.ItemRehello:
			h := hello("www01")
			h.Conn = 1
			out = append(out, h)
		case protocol.ItemTask:
			var task protocol.TaskItem
			_ = protocol.UnmarshalCBOR(raw, &task)
			acked = append(acked, task.Task)
			ok := true
			out = append(out, protocol.ResultItem{T: protocol.ItemResult, Task: task.Task, Seq: 0, Done: true, OK: &ok, Out: []byte("hi")})
		}
	}
	if len(acked) > 0 {
		out = append([]any{protocol.AckItem{T: protocol.ItemAck, Tasks: acked}}, out...)
	}
	if p.more != nil {
		out = append(out, p.more(msg)...)
	}
	if !p.noTasks {
		max := p.maxTasks
		out = append(out, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max})
	}
	reply := protocol.MinionMessage{V: protocol.MinionMessageV3, Re: msg.ID}
	for _, it := range out {
		b, _ := protocol.MarshalCBOR(it)
		reply.Items = append(reply.Items, cbor.RawMessage(b))
	}
	b, _ := protocol.MarshalCBOR(reply)
	sealed, err := protocol.SealFrame(p.keys, c.ID, protocol.FrameMinionToCore, b)
	if err != nil {
		p.t.Fatal(err)
	}
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, ID: c.ID[:], Data: sealed, Status: 200}, nil
}

func (p *fakePage) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ids)
}

type bindRig struct {
	*rig
	page    *fakePage
	targets *fakeTargets
	tasks   *fakeTasks
	target  uuid.UUID
}

// newBindRig is a build with one bind-call connection through "dumbo", a
// target for it and its page.
func newBindRig(t *testing.T, retries bool) *bindRig {
	t.Helper()
	r := newRig(t)
	r.build.Connections.Items = []protocol.BuildConnection{{Endpoint: protocol.ChannelEndpointExport{
		Format: protocol.ChannelEndpointExportV2, Name: "page", Kind: protocol.KindBindCall,
		Channel: protocol.ExportChannel{ModuleName: "dumbo", SchemaVersion: 1}, Params: map[string]any{"method": "POST"}}}}
	targets := &fakeTargets{rows: map[uuid.UUID]models.MinionTarget{}}
	page := &fakePage{t: t, keys: r.keys, maxTasks: 2}
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks).WithTargets(targets, page)
	tg := &models.MinionTarget{OperationID: r.build.OperationID, BuildID: r.build.BuildID, Conn: 1, ModuleName: "dumbo",
		Kind: protocol.KindBindCall, Values: models.JSONMap{"url": "http://www/a.php"}, Name: "http://www/a.php",
		Retries: retries, State: models.TargetNew}
	_ = targets.Insert(context.Background(), tg)
	return &bindRig{rig: r, page: page, targets: targets, tasks: tasks, target: tg.TargetID}
}

func (b *bindRig) get(t *testing.T) *models.MinionTarget {
	t.Helper()
	tg, err := b.targets.FindByTargetID(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func TestBindFirstCallOpensSession(t *testing.T) {
	b := newBindRig(t, true)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetConnected || tg.SessionID == nil || tg.LastOKAt == nil || tg.NextCallAt != nil {
		t.Fatalf("target after its hello: %+v", tg)
	}
	if tg.Allowance == nil || *tg.Allowance != 2 {
		t.Fatalf("allowance from the page's get_tasks: %v", tg.Allowance)
	}
	// The first call: a rehello under the build id, with the build
	// connection's values and the target's, through its channel.
	first := b.page.calls[0]
	if b.page.ids[0] != b.build.BuildID || len(first.Items) != 1 || protocol.ItemType(first.Items[0]) != protocol.ItemRehello {
		t.Fatalf("first call: under %s, %+v", b.page.ids[0], first)
	}
	if v := b.page.values[0]; v["method"] != "POST" || v["url"] != "http://www/a.php" || b.page.modules[0] != "dumbo" {
		t.Fatalf("values %v through %v", v, b.page.modules)
	}
	s, err := b.sessions.FindBySessionID(context.Background(), *tg.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Staged || s.ConnKind != protocol.KindBindCall || s.Hello.Host != "www01" || s.LastSeenAt != nil && s.LastSeenAt.IsZero() {
		t.Fatalf("session %+v", s)
	}
	// Nothing waited: no call under the session yet.
	if n := b.page.callCount(); n != 1 {
		t.Fatalf("%d calls", n)
	}
}

func TestBindCallBringsTasks(t *testing.T) {
	b := newBindRig(t, true)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	sid := *tg.SessionID
	// News with nothing new: no call.
	b.svc.callTarget(context.Background(), b.target, bindNews)
	if n := b.page.callCount(); n != 1 {
		t.Fatalf("a call with nothing to send: %d calls", n)
	}
	for range 3 {
		b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, OperationID: b.build.OperationID,
			State: models.MinionTaskQueued, Op: 3})
	}
	b.svc.callTarget(context.Background(), b.target, bindNews)
	// Two tasks (the allowance) in the first call; the page's answer asks
	// for more, so the third goes in a call at once; then nothing is left.
	if n := b.page.callCount(); n != 3 {
		t.Fatalf("%d calls", n)
	}
	if got := len(itemsOfCore(b.page.calls[1], protocol.ItemTask)); got != 2 {
		t.Fatalf("first call under the session: %d tasks", got)
	}
	if got := len(itemsOfCore(b.page.calls[2], protocol.ItemTask)); got != 1 {
		t.Fatalf("second call: %d tasks", got)
	}
	for _, task := range b.tasks.rows {
		if task.State != models.MinionTaskDone {
			t.Fatalf("task %s %s", task.TaskID, task.State)
		}
	}
	for _, id := range b.page.ids[1:] {
		if id != sid {
			t.Fatalf("a call under %s, not the session", id)
		}
	}
	if tg := b.get(t); tg.State != models.TargetConnected || tg.Calls != 3 {
		t.Fatalf("target %+v", tg)
	}
}

func TestBindUnreachable(t *testing.T) {
	b := newBindRig(t, true)
	b.page.answer = &protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1,
		Failure: &protocol.LinkFailure{Kind: protocol.LinkFailUnreachable, Msg: "connection refused"}}
	start := time.Now().UTC()
	b.svc.now = func() time.Time { return start }
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetUnreachable || tg.Failures != 1 || tg.UnreachableSince == nil || tg.NextCallAt == nil ||
		!tg.NextCallAt.Equal(start.Add(time.Minute)) || tg.StateDetail != "unreachable: connection refused" {
		t.Fatalf("after one failure: %+v", tg)
	}
	// The second retry after 5 minutes, then 30, then hourly.
	b.svc.callTarget(context.Background(), b.target, bindScheduled)
	if tg := b.get(t); tg.Failures != 2 || !tg.NextCallAt.Equal(start.Add(5*time.Minute)) {
		t.Fatalf("after two: %+v", tg)
	}
	// A day of failures: it stops.
	b.svc.now = func() time.Time { return start.Add(models.TargetRetryFor) }
	b.svc.callTarget(context.Background(), b.target, bindScheduled)
	if tg := b.get(t); tg.State != models.TargetStopped || tg.NextCallAt != nil {
		t.Fatalf("after a day: %+v", tg)
	}
	// It comes back: connected, the failures forgotten.
	b.page.answer = nil
	if tg, _ := b.svc.CheckTarget(context.Background(), b.target); tg.State != models.TargetConnected || tg.Failures != 0 ||
		tg.UnreachableSince != nil {
		t.Fatalf("back: %+v", tg)
	}
}

func TestBindNoRetries(t *testing.T) {
	b := newBindRig(t, false)
	b.page.answer = &protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1,
		Failure: &protocol.LinkFailure{Kind: protocol.LinkFailTimeout, Msg: "no answer"}}
	if tg, _ := b.svc.CheckTarget(context.Background(), b.target); tg.State != models.TargetUnreachable || tg.NextCallAt != nil {
		t.Fatalf("%+v", tg)
	}
}

func TestBindNotTheMinion(t *testing.T) {
	b := newBindRig(t, true)
	b.page.answer = &protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Status: 404, Excerpt: []byte("<h1>Not Found</h1>")}
	tg, _ := b.svc.CheckTarget(context.Background(), b.target)
	if tg.State != models.TargetNotTheMinion || tg.NextCallAt != nil || tg.StateDetail != "HTTP 404: <h1>Not Found</h1>" {
		t.Fatalf("%+v", tg)
	}
	// An answer that does not open (another build's page).
	other := protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}
	sealed, _ := protocol.SealFrame(other, b.build.BuildID, protocol.FrameMinionToCore, []byte{0xa0})
	b.page.answer = &protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, ID: b.build.BuildID[:], Data: sealed, Status: 200}
	if tg, _ := b.svc.CheckTarget(context.Background(), b.target); tg.State != models.TargetNotTheMinion || tg.SessionID != nil {
		t.Fatalf("%+v", tg)
	}
	if len(b.sessions.rows) != 0 {
		t.Fatal("a session opened")
	}
}

func TestBindNoChannel(t *testing.T) {
	b := newBindRig(t, true)
	b.svc.WithTargets(b.targets, noLink{})
	if tg, _ := b.svc.CheckTarget(context.Background(), b.target); tg.State != models.TargetNoChannel || tg.NextCallAt == nil {
		t.Fatalf("%+v", tg)
	}
}

type noLink struct{}

func (noLink) Call(context.Context, string, channellink.Call) (protocol.LinkFrame, error) {
	return protocol.LinkFrame{}, channellink.ErrNoLink
}

func TestBindSchedule(t *testing.T) {
	b := newBindRig(t, true)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	// The page says a schedule: calls on it.
	tg := b.get(t)
	interval := int64(60_000)
	for i := range b.sessions.rows {
		b.sessions.rows[i].Sleep = &models.MinionSessionSleep{Interval: interval}
	}
	b.svc.callTarget(context.Background(), tg.TargetID, bindScheduled)
	tg = b.get(t)
	if tg.NextCallAt == nil || tg.NextCallAt.Sub(*tg.LastOKAt) != time.Minute {
		t.Fatalf("next call %v after %v", tg.NextCallAt, tg.LastOKAt)
	}
	// The scheduler takes it when due.
	due, err := b.targets.ClaimDue(context.Background(), tg.NextCallAt.Add(time.Second))
	if err != nil || due.TargetID != tg.TargetID {
		t.Fatalf("due: %v %v", due, err)
	}
}

func itemsOfCore(msg protocol.CoreMessage, kind int) []cbor.RawMessage {
	var out []cbor.RawMessage
	for _, it := range msg.Items {
		if protocol.ItemType(it) == kind {
			out = append(out, it)
		}
	}
	return out
}

// checker says a target's values no longer fit while problems is set.
type checker struct{ problems map[string]string }

func (c *checker) Problems(context.Context, *models.MinionTarget) (map[string]string, error) {
	return c.problems, nil
}

func TestBindNeedsEditing(t *testing.T) {
	b := newBindRig(t, true)
	c := &checker{problems: map[string]string{"token": "required"}}
	b.svc.WithTargetChecker(c, func(p map[string]string) string { return "token: " + p["token"] })
	// Values the channel no longer reads: not called, needs editing, no
	// retry scheduled.
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetNeedsEditing || tg.StateDetail != "token: required" || tg.NextCallAt != nil || b.page.callCount() != 0 {
		t.Fatalf("target %+v after %d calls", tg, b.page.callCount())
	}
	// Edited to fit: called again, as any target.
	c.problems = nil
	if tg, _ = b.svc.CheckTarget(context.Background(), b.target); tg.State != models.TargetConnected || b.page.callCount() != 1 {
		t.Fatalf("target %+v after %d calls", tg, b.page.callCount())
	}
}

// chunkFiles keeps a download's chunks in order: next moves on by each one
// that starts where it has the file; stuck, it never does.
type chunkFiles struct {
	fakeFiles
	mu    sync.Mutex
	next  uint64
	stuck bool
}

func (f *chunkFiles) TakeChunk(_ context.Context, _ *models.MinionSession, c protocol.ChunkItem) (minionfiles.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stuck && c.Off == f.next {
		f.next += uint64(len(c.Data))
	}
	return minionfiles.Reply{Next: int64(f.next)}, nil
}

// A page sends a file a chunk a call, keeping nothing: core's ack echoes
// the ctx its chunk set, and says where to go on from. The calls go on for
// as long as the file takes, past any count.
func TestBindDownloadTakesAsManyCallsAsItNeeds(t *testing.T) {
	b := newBindRig(t, true)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	files := &chunkFiles{}
	b.svc.WithFiles(files)
	const total = 40
	xfer, task, ctx := bytes.Repeat([]byte{7}, 16), bytes.Repeat([]byte{8}, 16), []byte("path=/var/www/big.bin")
	var badCtx []string
	b.page.more = func(msg protocol.CoreMessage) []any {
		next := uint64(0)
		started := false
		for _, raw := range itemsOfCore(msg, protocol.ItemAck) {
			var a protocol.AckItem
			_ = protocol.UnmarshalCBOR(raw, &a)
			if a.Xfer == nil {
				continue
			}
			started, next = true, *a.Next
			if !bytes.Equal(a.Ctx, ctx) {
				badCtx = append(badCtx, string(a.Ctx))
			}
		}
		if started && next >= total {
			return nil // all there: the page would end its task
		}
		c := protocol.ChunkItem{T: protocol.ItemChunk, Xfer: xfer, Off: next, Data: []byte{byte(next)}, Ctx: ctx}
		if next == 0 {
			sz := uint64(total)
			c.Total, c.Task, c.Name = &sz, task, "/var/www/big.bin"
		}
		return []any{c}
	}
	before := b.page.callCount()
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	if files.next != total {
		t.Fatalf("core has %d of %d bytes after %d calls", files.next, total, b.page.callCount()-before)
	}
	if n := b.page.callCount() - before; n != total+1 {
		t.Fatalf("%d calls for %d chunks", n, total)
	}
	if len(badCtx) > 0 {
		t.Fatalf("acks without the chunk's ctx: %q", badCtx)
	}
}

// A page that answers the same thing over and over (a chunk core never
// takes) ends the exchange: two calls that would carry the same items.
func TestBindSameAnswerEndsTheExchange(t *testing.T) {
	b := newBindRig(t, true)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	b.svc.WithFiles(&chunkFiles{stuck: true})
	xfer := bytes.Repeat([]byte{7}, 16)
	b.page.more = func(protocol.CoreMessage) []any {
		sz := uint64(10)
		return []any{protocol.ChunkItem{T: protocol.ItemChunk, Xfer: xfer, Off: 0, Data: []byte{1}, Total: &sz, Task: xfer}}
	}
	before := b.page.callCount()
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	// The check, and the call with core's ack; the next would carry the
	// same ack again, and is not made.
	if n := b.page.callCount() - before; n != 2 {
		t.Fatalf("%d calls", n)
	}
}

// A stream of a bind session may go on (a downloader came, or took a
// chunk): its page is called, with the stream's ack; while every stream
// holds, a wake calls nothing.
func TestBindWakeCallsWhenAStreamMayGo(t *testing.T) {
	b := newBindRig(t, true)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil || tg.SessionID == nil {
		t.Fatalf("%+v %v", tg, err)
	}
	next, xfer := uint64(0), bytes.Repeat([]byte{7}, 16)
	streams := &lockedStreams{ack: protocol.AckItem{T: protocol.ItemAck, Xfer: xfer, Next: &next, Hold: true, Ctx: []byte("c")}}
	b.svc.WithStreams(streams)
	before := b.page.callCount()
	b.svc.StreamRoom(*tg.SessionID)
	time.Sleep(4 * bindWakeDelay)
	if n := b.page.callCount() - before; n != 0 {
		t.Fatalf("%d calls while the stream holds", n)
	}
	streams.set(false)
	b.svc.WakeStreams(*tg.SessionID)
	deadline := time.Now().Add(2 * time.Second)
	for b.page.callCount() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if b.page.callCount() == before {
		t.Fatal("no call once the stream may go")
	}
	b.page.mu.Lock()
	last := b.page.calls[len(b.page.calls)-1]
	b.page.mu.Unlock()
	var a protocol.AckItem
	acks := itemsOfCore(last, protocol.ItemAck)
	if len(acks) != 1 || protocol.UnmarshalCBOR(acks[0], &a) != nil || a.Hold || string(a.Ctx) != "c" {
		t.Fatalf("the call's ack %+v", a)
	}
}

// lockedStreams has one stream, holding or not, read while calls run.
type lockedStreams struct {
	fakeStreams
	mu  sync.Mutex
	ack protocol.AckItem
}

func (l *lockedStreams) set(hold bool) { l.mu.Lock(); l.ack.Hold = hold; l.mu.Unlock() }

func (l *lockedStreams) Acks(context.Context, *models.MinionSession, map[string]bool) ([]protocol.AckItem, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return []protocol.AckItem{l.ack}, nil
}

// A long task (a walk) carried across calls: each part says where the
// page stands (ctx), core answers with a resume giving it back, and the
// calls go on until a part says done. A resume lost with a failed call is
// sent again by the next call core makes; a cancel carries the ctx too.
func TestBindResumeCarriesALongTaskAcrossCalls(t *testing.T) {
	b := newBindRig(t, true)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil || tg.SessionID == nil {
		t.Fatalf("%+v %v", tg, err)
	}
	task := uuid.New()
	b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: task, SessionID: *tg.SessionID, OperationID: b.build.OperationID,
		Command: "walk", State: models.MinionTaskRunning})
	const parts = 6
	var resumed []string
	started := false
	b.page.more = func(msg protocol.CoreMessage) []any {
		at := -1
		for _, raw := range itemsOfCore(msg, protocol.ItemResume) {
			var r protocol.ResumeItem
			if protocol.UnmarshalCBOR(raw, &r) != nil || r.Validate() != nil || !bytes.Equal(r.Task, task[:]) {
				t.Errorf("a resume that does not read: %x", raw)
				continue
			}
			resumed = append(resumed, string(r.Ctx))
			at = int(r.Ctx[0])
		}
		switch {
		case at < 0 && started:
			return nil
		case at < 0:
			started, at = true, 0
		}
		part := protocol.ResultItem{T: protocol.ItemResult, Task: task[:], Seq: uint64(at), Out: []byte{byte(at)}}
		if at == parts-1 {
			ok := true
			part.Done, part.OK = true, &ok
		} else {
			part.Ctx = []byte{byte(at + 1)}
		}
		return []any{part}
	}
	before := b.page.callCount()
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	if got := len(b.tasks.results[task]); got != parts {
		t.Fatalf("%d parts of %d after %d calls (resumed %q)", got, parts, b.page.callCount()-before, resumed)
	}
	if n := b.page.callCount() - before; n != parts {
		t.Fatalf("%d calls for %d parts", n, parts)
	}
	if st := b.tasks.find(task).State; st != models.MinionTaskDone {
		t.Fatalf("state %s", st)
	}

	// A walk whose resume was lost: the next call core makes brings it.
	other := uuid.New()
	seq := uint64(2)
	b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: other, SessionID: *tg.SessionID, OperationID: b.build.OperationID,
		Command: "walk", State: models.MinionTaskRunning, ResumeCtx: []byte{3}, ResumeSeq: &seq})
	items, fresh, _, err := b.svc.bindItems(context.Background(), b.get(t), b.sessionOf(t, *tg.SessionID))
	if err != nil || !fresh || len(items) != 1 || protocol.ItemType(items[0]) != protocol.ItemResume {
		t.Fatalf("a lost resume: %d items, fresh %v, %v", len(items), fresh, err)
	}
	// Asked to stop: no resume; the cancel says where it stood.
	now := time.Now()
	b.tasks.find(other).CancelRequestedAt = &now
	items, _, _, _ = b.svc.bindItems(context.Background(), b.get(t), b.sessionOf(t, *tg.SessionID))
	var c protocol.CancelItem
	if len(items) != 1 || protocol.UnmarshalCBOR(items[0], &c) != nil || !bytes.Equal(c.Task, other[:]) || !bytes.Equal(c.Ctx, []byte{3}) {
		t.Fatalf("the cancel %+v of %d items", c, len(items))
	}
}

func (b *bindRig) sessionOf(t *testing.T, id uuid.UUID) *models.MinionSession {
	t.Helper()
	s, err := b.sessions.FindBySessionID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
