package composites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.uber.org/zap/zaptest"
)

// A rig: the service over in-memory tasks, a session of a build of a
// factory whose schema has composites, scripted factory instances, and a
// clock the test moves.

var notFound = qmgo.ErrNoSuchDocuments

type store struct {
	mu    sync.Mutex
	tasks map[uuid.UUID]*models.MinionTask
	parts map[uuid.UUID][]models.MinionTaskPart
	order []uuid.UUID
}

func newStore() *store {
	return &store{tasks: map[uuid.UUID]*models.MinionTask{}, parts: map[uuid.UUID][]models.MinionTaskPart{}}
}

// clone is a deep enough copy: the service never shares the store's.
func clone(t *models.MinionTask) *models.MinionTask {
	raw, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	var out models.MinionTask
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	// Fields JSON leaves out.
	out.SubtaskKey, out.WireArgs, out.Op = t.SubtaskKey, t.WireArgs, t.Op
	if c := t.Composite; c != nil {
		out.Composite.Ctx = slices.Clone(c.Ctx)
		out.Composite.Pending = slices.Clone(c.Pending)
		out.Composite.Waiting = slices.Clone(c.Waiting)
		out.Composite.Finish = c.Finish
		out.Composite.Cancel, out.Composite.SendOwn = c.Cancel, c.SendOwn
		out.Composite.CallAt, out.Composite.CallDeadline, out.Composite.CallEvent = c.CallAt, c.CallDeadline, c.CallEvent
		out.Composite.StoppedCallAt, out.Composite.LeaseBy, out.Composite.LeaseUntil, out.Composite.WakeAt = c.StoppedCallAt, c.LeaseBy, c.LeaseUntil, c.WakeAt
	}
	return &out
}

func (s *store) put(t *models.MinionTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.TaskID]; !ok {
		s.order = append(s.order, t.TaskID)
	}
	s.tasks[t.TaskID] = clone(t)
}

func (s *store) get(id uuid.UUID) *models.MinionTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil
	}
	return clone(t)
}

func (s *store) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	if t := s.get(id); t != nil {
		return t, nil
	}
	return nil, notFound
}

func (s *store) Parts(_ context.Context, id uuid.UUID) ([]models.MinionTaskPart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.parts[id]), nil
}

func (s *store) OutParts(ctx context.Context, id uuid.UUID) ([]models.MinionTaskPart, error) {
	return s.Parts(ctx, id)
}

func (s *store) RequestCancel(_ context.Context, id uuid.UUID, at time.Time) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, notFound
	}
	switch {
	case t.State == models.MinionTaskQueued:
		t.State, t.DoneAt, t.CancelRequestedAt = models.MinionTaskCancelled, &at, &at
	case (t.State == models.MinionTaskSent || t.State == models.MinionTaskRunning) && t.CancelRequestedAt == nil:
		t.CancelRequestedAt = &at
	}
	return clone(t), nil
}

func open(t *models.MinionTask) bool { return slices.Contains(models.MinionTaskOpenStates, t.State) }

// compositeFields are MinionComposite's fields by their bson names: an
// update naming another is a bug the repository's would not catch.
var compositeFields = func() map[string]int {
	out := map[string]int{}
	ty := reflect.TypeOf(models.MinionComposite{})
	for i := range ty.NumField() {
		name, _, _ := strings.Cut(ty.Field(i).Tag.Get("bson"), ",")
		out[name] = i
	}
	return out
}()

func setField(c *models.MinionComposite, name string, v any) {
	i, ok := compositeFields[name]
	if !ok {
		panic(fmt.Sprintf("the composite record has no field %q", name))
	}
	f := reflect.ValueOf(c).Elem().Field(i)
	if v == nil {
		f.Set(reflect.Zero(f.Type()))
		return
	}
	rv := reflect.ValueOf(v)
	switch {
	case rv.Type().AssignableTo(f.Type()):
		f.Set(rv)
	case f.Kind() == reflect.Pointer && rv.Type().AssignableTo(f.Type().Elem()):
		p := reflect.New(f.Type().Elem())
		p.Elem().Set(rv)
		f.Set(p)
	case rv.Type().ConvertibleTo(f.Type()):
		f.Set(rv.Convert(f.Type()))
	default:
		panic(fmt.Sprintf("field %q takes %s, not %s", name, f.Type(), rv.Type()))
	}
}

func (s *store) UpdateComposite(_ context.Context, id uuid.UUID, u repository.ComposeUpdate) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || t.Composite == nil || !open(t) {
		return nil, notFound
	}
	c := t.Composite
	if (u.IfSeq != nil && c.Seq != *u.IfSeq) || (u.IfPhase != nil && c.Phase != *u.IfPhase) {
		return nil, notFound
	}
	for k, v := range u.Set {
		setField(c, k, v)
	}
	for _, k := range u.Unset {
		setField(c, k, nil)
	}
	if u.DropEnds {
		c.Pending = nil
	}
	if u.PopEnd && len(c.Pending) > 0 {
		c.Pending = c.Pending[1:]
	}
	if u.Waiting != nil {
		c.Waiting = slices.Clone(*u.Waiting)
	}
	c.Subtasks += u.Subtasks
	c.Pending = append(c.Pending, u.Ends...)
	t.Rev++
	return clone(t), nil
}

func (s *store) ClaimComposite(_ context.Context, id uuid.UUID, by string, until, now time.Time) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || t.Composite == nil || !open(t) {
		return nil, notFound
	}
	c := t.Composite
	if c.LeaseUntil != nil && !c.LeaseUntil.Before(now) && c.LeaseBy != by {
		return nil, notFound
	}
	c.LeaseBy, c.LeaseUntil = by, &until
	return clone(t), nil
}

func (s *store) ReleaseComposite(_ context.Context, id uuid.UUID, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok && t.Composite != nil && t.Composite.LeaseBy == by {
		t.Composite.LeaseBy, t.Composite.LeaseUntil = "", nil
	}
	return nil
}

func (s *store) SendOwnOp(_ context.Context, id uuid.UUID, op int, wire []byte, upload *models.MinionTaskUpload, creds []uuid.UUID,
	own models.JSONMap, _ time.Time) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || t.State != models.MinionTaskComposing || t.Composite.OwnOp != string(protocol.OwnOpNotRun) {
		return nil, notFound
	}
	t.State, t.Op, t.WireArgs, t.Composite.OwnOp, t.Composite.OwnArgs = models.MinionTaskQueued, op, wire, string(protocol.OwnOpOut), own
	return clone(t), nil
}

// ownEnded is AddResult's (and the other writers') end of an own op.
func (s *store) ownEnded(t *models.MinionTask, end models.MinionComposeOwnEnd) {
	id := t.TaskID
	t.State, t.CancelRequestedAt = models.MinionTaskComposing, nil
	t.Composite.OwnOp, t.Composite.OwnEnd, t.Composite.WakeAt = string(protocol.OwnOpEnded), &end, &end.At
	t.Composite.Pending = append(t.Composite.Pending, models.MinionComposeEnd{TaskID: &id, OwnOp: true, At: end.At})
}

func (s *store) EndOwnOp(_ context.Context, id uuid.UUID, end models.MinionComposeOwnEnd) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || t.State != models.MinionTaskQueued || t.Composite == nil || t.Composite.OwnOp != string(protocol.OwnOpOut) {
		return nil, notFound
	}
	s.ownEnded(t, end)
	return clone(t), nil
}

func (s *store) CancelOwnOp(_ context.Context, id uuid.UUID, at time.Time) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || !t.OwnOpOut() || (t.State != models.MinionTaskSent && t.State != models.MinionTaskRunning) || t.CancelRequestedAt != nil {
		return nil, notFound
	}
	t.CancelRequestedAt = &at
	return clone(t), nil
}

func (s *store) EndComposite(_ context.Context, id uuid.UUID, state string, ok *bool, err string, at time.Time) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, found := s.tasks[id]
	if !found || t.Composite == nil || !open(t) {
		return nil, notFound
	}
	t.State, t.DoneAt = state, &at
	if ok != nil {
		t.OK = ok
	}
	if err != "" {
		t.Err = err
	}
	t.Composite.WakeAt, t.Composite.LeaseBy, t.Composite.LeaseUntil, t.Composite.TimerAt = nil, "", nil, nil
	return clone(t), nil
}

func (s *store) FinishComposite(_ context.Context, id uuid.UUID, f repository.CompositeResult) (*models.MinionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, found := s.tasks[id]
	if !found || t.Composite == nil || !open(t) {
		return nil, notFound
	}
	t.State, t.DoneAt, t.OK, t.Err = f.State, &f.At, &f.OK, f.Err
	switch {
	case f.From != nil:
		from := f.From.TaskID
		t.ResultOf = &from
		for _, p := range s.parts[from] {
			p.TaskID = id
			s.parts[id] = append(s.parts[id], p)
		}
		t.Parts, t.DataSize, t.OutSize = f.From.Parts, f.From.DataSize, f.From.OutSize
	case len(f.Out) > 0 || len(f.Data) > 0:
		s.parts[id] = append(s.parts[id], models.MinionTaskPart{TaskID: id, Out: f.Out, DataCBOR: f.Data, At: f.At})
		t.Parts, t.OutSize, t.DataSize, t.DataProblem, t.OutTruncated, t.DataTruncated = 1, int64(len(f.Out)), int64(len(f.Data)), f.Problem, f.OutCut, f.DataCut
	}
	t.Composite.WakeAt = nil
	return clone(t), nil
}

func (s *store) Subtask(_ context.Context, parent, id uuid.UUID) (*models.MinionTask, error) {
	t := s.get(id)
	if t == nil || t.ParentID == nil || *t.ParentID != parent {
		return nil, notFound
	}
	return t, nil
}

func (s *store) subtasks(parent uuid.UUID, keep func(*models.MinionTask) bool) []models.MinionTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.MinionTask
	for _, id := range s.order {
		t := s.tasks[id]
		if t.ParentID != nil && *t.ParentID == parent && keep(t) {
			out = append(out, *clone(t))
		}
	}
	return out
}

func (s *store) ListSubtasks(_ context.Context, parent uuid.UUID) ([]models.MinionTask, error) {
	return s.subtasks(parent, func(*models.MinionTask) bool { return true }), nil
}

func (s *store) OpenSubtasks(_ context.Context, parent uuid.UUID, keys []string) ([]models.MinionTask, error) {
	return s.subtasks(parent, func(t *models.MinionTask) bool {
		return open(t) && (keys == nil || slices.Contains(keys, t.SubtaskKey))
	}), nil
}

func (s *store) CountOpenSubtasks(ctx context.Context, parent uuid.UUID) (int, error) {
	subs, err := s.OpenSubtasks(ctx, parent, nil)
	return len(subs), err
}

func (s *store) UnheardSubtasks(_ context.Context, parent uuid.UUID, limit int64) ([]models.MinionTask, error) {
	subs := s.subtasks(parent, func(t *models.MinionTask) bool { return t.Finished() && !t.Heard })
	slices.SortStableFunc(subs, func(a, b models.MinionTask) int { return a.DoneAt.Compare(*b.DoneAt) })
	if int64(len(subs)) > limit {
		subs = subs[:limit]
	}
	return subs, nil
}

func (s *store) MarkHeard(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Heard = true
	}
	return nil
}

func (s *store) ComposeDue(_ context.Context, now time.Time, _ int64) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uuid.UUID
	for _, id := range s.order {
		if t := s.tasks[id]; t.Composite != nil && t.Composite.WakeAt != nil && !t.Composite.WakeAt.After(now) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *store) UnheardParents(context.Context, int64) ([]uuid.UUID, error) { return nil, nil }

// The session, its build and its factory.

type sessions struct{ s *models.MinionSession }

func (f sessions) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionSession, error) {
	if f.s.SessionID != id {
		return nil, notFound
	}
	s := *f.s
	return &s, nil
}

type buildRepo struct{ b *models.Build }

func (f buildRepo) FindByBuildID(_ context.Context, id uuid.UUID) (*models.Build, error) {
	if f.b.BuildID != id {
		return nil, notFound
	}
	b := *f.b
	return &b, nil
}

type registry struct {
	schema protocol.BuildSchema
	mods   []models.Module
}

func (r *registry) LatestBuildSchema(context.Context, string) (*models.ModuleBuildSchema, error) {
	return &models.ModuleBuildSchema{BuildSchema: r.schema}, nil
}

func (r *registry) ListActive(context.Context) ([]models.Module, error) { return r.mods, nil }

type users struct{}

func (users) FindByIDs(_ context.Context, ids []uuid.UUID) ([]models.User, error) {
	return []models.User{{UserID: ids[0], Username: "alice"}}, nil
}

// factory answers calls as its instances' code would: decide is the code,
// by instance (busy, an error, a timeout, or a decision).
type factory struct {
	mu     sync.Mutex
	calls  []call
	decide func(instance string, req protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error)
	block  chan struct{}
}

type call struct {
	instance string
	req      protocol.ComposeRequest
}

func (f *factory) Call(_ context.Context, queue, opType string, payload any, _ time.Duration) (protocol.ReplyEnvelope, error) {
	if opType != protocol.TypeFactoryCompose {
		return protocol.ReplyEnvelope{}, fmt.Errorf("%s: not a compose call", opType)
	}
	// What crosses the broker: the JSON of the request.
	raw, err := json.Marshal(payload)
	if err != nil {
		return protocol.ReplyEnvelope{}, err
	}
	var req protocol.ComposeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return protocol.ReplyEnvelope{}, err
	}
	if err := req.Validate(); err != nil {
		return protocol.ReplyEnvelope{}, fmt.Errorf("an invalid request went out: %w", err)
	}
	instance := strings.TrimPrefix(queue, "logos.factory.rpc.")
	f.mu.Lock()
	f.calls = append(f.calls, call{instance: instance, req: req})
	decide, block := f.decide, f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	d, e, err := decide(instance, req)
	if err != nil {
		return protocol.ReplyEnvelope{}, err
	}
	if e != nil {
		return protocol.ReplyEnvelope{Status: protocol.StatusError, Error: e}, nil
	}
	payloadRaw, _ := json.Marshal(d)
	return protocol.ReplyEnvelope{Status: protocol.StatusOK, Payload: payloadRaw}, nil
}

func (f *factory) requests() []protocol.ComposeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.ComposeRequest, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.req
	}
	return out
}

func (f *factory) last(t *testing.T) protocol.ComposeRequest {
	t.Helper()
	reqs := f.requests()
	if len(reqs) == 0 {
		t.Fatal("no call made")
	}
	return reqs[len(reqs)-1]
}

var timeout = fmt.Errorf("%w: no reply", messaging.ErrRPCTimeout)

// queuer queues subtasks and own ops as the task resolver does, a
// command named in refuse failing its checks.
type queuer struct {
	st     *store
	refuse map[string]string
}

func (q *queuer) QueueSubtask(_ context.Context, parent *models.MinionTask, sub models.MinionSubtask) (*models.MinionTask, error) {
	if t := q.st.get(sub.TaskID); t != nil {
		return t, nil
	}
	if why, ok := q.refuse[sub.Command]; ok {
		return nil, &Refusal{Reason: why}
	}
	t := &models.MinionTask{TaskID: sub.TaskID, OperationID: parent.OperationID, SessionID: parent.SessionID, Command: sub.Command,
		Args: sub.Args, IssuedBy: parent.IssuedBy, State: models.MinionTaskQueued, QueuedAt: time.Now(), ParentID: &parent.TaskID,
		SubtaskKey: sub.Key, SubtaskAs: sub.As, Cleanup: sub.Cleanup}
	q.st.put(t)
	return q.st.get(t.TaskID), nil
}

func (q *queuer) SendOwnOp(ctx context.Context, t *models.MinionTask, args map[string]any) (*models.MinionTask, error) {
	if why, ok := q.refuse[t.Command]; ok {
		return nil, &Refusal{Reason: why}
	}
	var own models.JSONMap
	if args != nil {
		own = args
	}
	after, err := q.st.SendOwnOp(ctx, t.TaskID, 40, nil, nil, nil, own, time.Now())
	if errors.Is(err, notFound) {
		// Sent before: as it is.
		return q.st.get(t.TaskID), nil
	}
	return after, err
}

type nudges struct {
	mu   sync.Mutex
	seen []uuid.UUID
}

func (n *nudges) Nudge(session uuid.UUID, _ bool) {
	n.mu.Lock()
	n.seen = append(n.seen, session)
	n.mu.Unlock()
}

// files keeps files tasks got and files the factory puts.
type files struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*models.MinionFile
	bytes map[uuid.UUID][]byte
}

func newFiles() *files {
	return &files{byID: map[uuid.UUID]*models.MinionFile{}, bytes: map[uuid.UUID][]byte{}}
}

func (f *files) add(m *models.MinionFile, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[m.FileID], f.bytes[m.FileID] = m, content
}

func (f *files) File(_ context.Context, task uuid.UUID) (*models.MinionFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.byID {
		if m.TaskID == task || slices.Contains(m.AlsoTasks, task) {
			c := *m
			return &c, nil
		}
	}
	return nil, nil
}

func (f *files) FileByID(_ context.Context, id uuid.UUID) (*models.MinionFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.byID[id]; ok {
		c := *m
		return &c, nil
	}
	return nil, nil
}

func (f *files) Open(_ context.Context, id uuid.UUID) (io.ReadCloser, *models.MinionFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[id]
	if !ok {
		return nil, nil, notFound
	}
	c := *m
	return io.NopCloser(bytes.NewReader(f.bytes[id])), &c, nil
}

func (f *files) PutFactoryFile(_ context.Context, op, forTask, by uuid.UUID, name string, body io.Reader) (*models.MinionFile, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	m := &models.MinionFile{FileID: uuid.New(), Kind: models.MinionFileKindTask, OperationID: op, ForTask: forTask, UploadedBy: by,
		Name: name, Total: int64(len(b)), SHA256: sha(b), State: models.MinionFileComplete}
	f.add(m, b)
	return m, nil
}

func (f *files) DropTaskFile(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
	delete(f.bytes, id)
	return nil
}

func (f *files) TakeFiles(_ context.Context, from, to uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.byID {
		if m.TaskID == from {
			m.AlsoTasks = append(m.AlsoTasks, to)
		}
	}
	return nil
}

// rig is one composite task's world.
type rig struct {
	t       *testing.T
	svc     *Service
	st      *store
	fac     *factory
	q       *queuer
	files   *files
	nudges  *nudges
	reg     *registry
	session *models.MinionSession
	build   *models.Build
	now     time.Time
}

// archiveSchema is the docs' example: archive (op 40) uses ls, download
// and rm; bundle has no op of its own.
func archiveSchema() protocol.BuildSchema {
	path := protocol.FieldSpec{Key: "path", Num: 1, Type: protocol.FieldString, Label: "Path"}
	return protocol.BuildSchema{SchemaVersion: protocol.BuildSchemaV3, Version: 1,
		Targets: []protocol.BuildTarget{{ID: "win", Format: "exe", Platforms: []string{"windows/amd64"}}},
		Commands: []protocol.BuildCommand{
			{ID: "ls", Op: 1, Params: []protocol.FieldSpec{path}},
			{ID: "download", Op: 2, Result: protocol.ResultFileDownload, Params: []protocol.FieldSpec{path}},
			{ID: "rm", Op: 3, Params: []protocol.FieldSpec{path}},
			{ID: "archive", Op: 40, Composite: true, Uses: []string{"ls", "download", "rm"}, Params: []protocol.FieldSpec{path}},
			{ID: "bundle", Composite: true, Uses: []string{"ls", "download"},
				Params: []protocol.FieldSpec{{Key: "path", Type: protocol.FieldString}}},
		}}
}

func newRig(t *testing.T, instances ...string) *rig {
	t.Helper()
	if len(instances) == 0 {
		instances = []string{"f1"}
	}
	r := &rig{t: t, st: newStore(), files: newFiles(), nudges: &nudges{}, now: time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)}
	seen := r.now
	r.session = &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), BuildID: uuid.New(),
		Hello: models.MinionHello{OS: "windows"}, CreatedAt: r.now, LastSeenAt: &seen}
	r.build = &models.Build{BuildID: r.session.BuildID, ModuleName: "acme", Target: "win", Platform: "windows/amd64",
		Commands: []string{"ls", "download", "rm", "archive"}}
	r.reg = &registry{schema: archiveSchema()}
	for _, name := range instances {
		r.reg.mods = append(r.reg.mods, models.Module{Type: protocol.ServiceFactory, Name: "acme", Instance: name,
			RPCQueue: "logos.factory.rpc." + name, Build: &models.ModuleBuildSchema{BuildSchema: r.reg.schema}})
	}
	r.fac = &factory{decide: func(string, protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error) {
		return protocol.Decision{}, nil, nil
	}}
	links, err := builds.NewLinks("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	r.q = &queuer{st: r.st, refuse: map[string]string{}}
	r.svc = New(Config{}, r.st, sessions{r.session}, buildRepo{r.build}, r.reg, r.reg, users{}, r.fac, links, r.files,
		nil, zaptest.NewLogger(t)).WithQueuer(r.q).WithNudger(r.nudges)
	r.svc.now = func() time.Time { return r.now }
	return r
}

// queue puts a composite task of command, as the resolver's queue does.
func (r *rig) queue(command string, args map[string]any) uuid.UUID {
	c, _ := r.reg.schema.Command(command)
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: r.session.OperationID, SessionID: r.session.SessionID, Command: command,
		Op: c.Op, Args: args, Result: c.Result, IssuedBy: uuid.New(), State: models.MinionTaskComposing, QueuedAt: r.now,
		Composite: &models.MinionComposite{}}
	if c.HasOp() {
		t.Composite.OwnOp = string(protocol.OwnOpNotRun)
	}
	r.st.put(t)
	return t.TaskID
}

// decide makes the factory's code d for every call from now.
func (r *rig) decide(d func(req protocol.ComposeRequest) protocol.Decision) {
	r.fac.mu.Lock()
	r.fac.decide = func(_ string, req protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error) {
		return d(req), nil, nil
	}
	r.fac.mu.Unlock()
}

func (r *rig) step(id uuid.UUID) {
	r.svc.step(context.Background(), id)
}

func (r *rig) task(id uuid.UUID) *models.MinionTask {
	r.t.Helper()
	t := r.st.get(id)
	if t == nil {
		r.t.Fatalf("no task %s", id)
	}
	return t
}

func (r *rig) subtasks(parent uuid.UUID) []models.MinionTask {
	subs, _ := r.st.ListSubtasks(context.Background(), parent)
	return subs
}

// end ends a task as its minion's last part does: done (ok or not) with its
// output and its data (named CBOR), or cancelled.
func (r *rig) end(id uuid.UUID, ok bool, out string, data any) {
	r.t.Helper()
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	t := r.st.tasks[id]
	at := r.now
	seq := uint64(0)
	var cbor []byte
	if data != nil {
		var err error
		if cbor, err = protocol.MarshalCBOR(data); err != nil {
			r.t.Fatal(err)
		}
	}
	r.st.parts[id] = append(r.st.parts[id], models.MinionTaskPart{TaskID: id, Seq: 0, Out: []byte(out), DataCBOR: cbor, At: at})
	t.Parts, t.DoneSeq = 1, &seq
	switch {
	case t.OwnOpOut():
		state := protocol.EndDone
		if !ok && t.CancelRequestedAt != nil {
			state = protocol.EndCancelled
		}
		r.st.ownEnded(t, models.MinionComposeOwnEnd{State: string(state), OK: ok, At: at, Seq: &seq})
	case ok:
		t.State, t.OK, t.DoneAt = models.MinionTaskDone, &ok, &at
	case t.CancelRequestedAt != nil:
		t.State, t.OK, t.DoneAt = models.MinionTaskCancelled, &ok, &at
	default:
		t.State, t.OK, t.DoneAt = models.MinionTaskFailed, &ok, &at
	}
}

// running makes the task sent and acked, as its minion's ack does.
func (r *rig) running(id uuid.UUID) {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	t := r.st.tasks[id]
	at := r.now
	t.State, t.SentAt, t.AckedAt = models.MinionTaskRunning, &at, &at
}

func sha(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
