package factorycommands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

// The service's fakes. fakeTasks keeps tasks as the repository does
// (repository/minion_task_factory.go): each method's filter and change
// mirror its query's, which the Mongo integration test checks the same way.

type fakeTasks struct {
	mu    sync.Mutex
	rows  map[uuid.UUID]*models.MinionTask
	parts map[uuid.UUID]map[uint64]models.MinionTaskPart
}

func newFakeTasks() *fakeTasks {
	return &fakeTasks{rows: map[uuid.UUID]*models.MinionTask{}, parts: map[uuid.UUID]map[uint64]models.MinionTaskPart{}}
}

var errNone = qmgo.ErrNoSuchDocuments

func clone(t *models.MinionTask) *models.MinionTask {
	c := *t
	if t.Factory != nil {
		f := *t.Factory
		f.Files = slices.Clone(t.Factory.Files)
		c.Factory = &f
	}
	return &c
}

func (f *fakeTasks) put(t *models.MinionTask) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[t.TaskID] = clone(t)
}

func (f *fakeTasks) row(id uuid.UUID) *models.MinionTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	return clone(f.rows[id])
}

func (f *fakeTasks) partsOf(id uuid.UUID) []models.MinionTaskPart {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.MinionTaskPart
	for _, p := range f.parts[id] {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func (f *fakeTasks) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok {
		return nil, errNone
	}
	return clone(t), nil
}

func open(t *models.MinionTask) bool { return t.State == models.MinionTaskFactory }

func minTime(p **time.Time, at time.Time) {
	if *p == nil || at.Before(**p) {
		*p = &at
	}
}

func maxTime(p **time.Time, at time.Time) {
	if *p == nil || at.After(**p) {
		*p = &at
	}
}

func clearWait(fr *models.MinionFactoryRun) {
	fr.Waiting, fr.WaitingSince, fr.OfferAt, fr.LeaseBy, fr.LeaseUntil = "", nil, nil, "", nil
}

func (f *fakeTasks) ClaimFactoryOffer(_ context.Context, by string, until, now time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var due []*models.MinionTask
	for _, t := range f.rows {
		fr := t.Factory
		if open(t) && fr.Instance == "" && fr.OfferAt != nil && !fr.OfferAt.After(now) && (fr.LeaseUntil == nil || fr.LeaseUntil.Before(now)) {
			due = append(due, t)
		}
	}
	if len(due) == 0 {
		return nil, nil
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Factory.OfferAt.Before(*due[j].Factory.OfferAt) })
	t := due[0]
	t.Factory.LeaseBy, t.Factory.LeaseUntil = by, &until
	return clone(t), nil
}

func (f *fakeTasks) OfferFactory(_ context.Context, id uuid.UUID, by, instance string, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) || t.Factory.Instance != "" || t.Factory.LeaseBy != by {
		return false, nil
	}
	t.Factory.Instance, t.Factory.OfferedAt = instance, &at
	t.Rev++
	return true, nil
}

func (f *fakeTasks) UnofferFactory(_ context.Context, id uuid.UUID, instance string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) || t.Factory.Instance != instance || t.Factory.AcceptedAt != nil || t.Factory.ReportAt != nil {
		return false, nil
	}
	t.Factory.Instance, t.Factory.OfferedAt = "", nil
	t.Rev++
	return true, nil
}

func (f *fakeTasks) AcceptFactory(_ context.Context, id uuid.UUID, instance string, at time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) || t.Factory.Instance != instance {
		return nil, errNone
	}
	minTime(&t.Factory.AcceptedAt, at)
	minTime(&t.StartedAt, at)
	maxTime(&t.Factory.ReportAt, at)
	clearWait(t.Factory)
	t.Rev++
	return clone(t), nil
}

func (f *fakeTasks) WaitFactory(_ context.Context, id uuid.UUID, by string, next time.Time, why string, note bool, at time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) || t.Factory.Instance != "" || t.Factory.LeaseBy != by {
		return nil, errNone
	}
	t.Factory.OfferAt, t.Factory.LeaseBy, t.Factory.LeaseUntil = &next, "", nil
	if note {
		t.Factory.Waiting = why
		minTime(&t.Factory.WaitingSince, at)
		t.Rev++
	}
	return clone(t), nil
}

func (f *fakeTasks) FactoryOutput(_ context.Context, id uuid.UUID, instance string, seq int64, out []byte, at time.Time) (*models.MinionTask, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) || t.Factory.Instance != instance {
		return nil, false, errNone
	}
	maxTime(&t.Factory.ReportAt, at)
	if seq <= t.Factory.Seq {
		return clone(t), false, nil
	}
	room := max(int64(models.MaxMinionTaskOutput)-t.OutSize-t.DataSize, 0)
	if int64(len(out)) > room {
		out, t.OutTruncated = out[:room], true
	}
	if len(out) > 0 {
		if f.parts[id] == nil {
			f.parts[id] = map[uint64]models.MinionTaskPart{}
		}
		f.parts[id][uint64(seq-1)] = models.MinionTaskPart{TaskID: id, Seq: uint64(seq - 1), Out: out, At: at} //nolint:gosec // 1 or more
		t.Parts++
		t.OutSize += int64(len(out))
		t.Rev++
	}
	t.Factory.Seq = seq
	minTime(&t.Factory.AcceptedAt, at)
	minTime(&t.StartedAt, at)
	return clone(t), true, nil
}

func (f *fakeTasks) FinishFactory(_ context.Context, id uuid.UUID, instance string, res repository.FactoryResult) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) || t.Factory.Instance != instance {
		return nil, errNone
	}
	ok2 := res.OK
	t.State, t.DoneAt, t.OK, t.Factory.ResultAt = res.State, &res.At, &ok2, &res.At
	if res.Err != "" {
		t.Err = res.Err
	}
	t.Exit = res.Exit
	t.DataProblem = res.Problem
	if res.DataCut {
		t.DataTruncated = true
	}
	next := uint64(t.Factory.Seq) //nolint:gosec // not negative
	switch {
	case len(res.Data) > 0:
		if f.parts[id] == nil {
			f.parts[id] = map[uint64]models.MinionTaskPart{}
		}
		f.parts[id][next] = models.MinionTaskPart{TaskID: id, Seq: next, DataCBOR: res.Data, At: res.At}
		t.Parts++
		t.DataSize += int64(len(res.Data))
		t.DataParts++
		t.DoneSeq = &next
	case next > 0:
		d := next - 1
		t.DoneSeq = &d
	}
	minTime(&t.StartedAt, res.At)
	clearWait(t.Factory)
	t.Rev++
	return clone(t), nil
}

func (f *fakeTasks) EndFactory(_ context.Context, id uuid.UUID, state, err string, at time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || !open(t) {
		return nil, errNone
	}
	t.State, t.DoneAt = state, &at
	if state == models.MinionTaskFailed {
		no := false
		t.OK = &no
	}
	if err != "" {
		t.Err = err
	}
	clearWait(t.Factory)
	t.Rev++
	return clone(t), nil
}

func (f *fakeTasks) FactoryUnderWay(_ context.Context, limit int64) ([]models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.MinionTask
	for _, t := range f.rows {
		if open(t) && t.Factory.Instance != "" && int64(len(out)) < limit {
			out = append(out, *clone(t))
		}
	}
	return out, nil
}

type fakeSessions struct {
	s map[uuid.UUID]*models.MinionSession
}

func (f fakeSessions) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionSession, error) {
	s, ok := f.s[id]
	if !ok {
		return nil, errNone
	}
	c := *s
	return &c, nil
}

type fakeBuilds struct{ b map[uuid.UUID]*models.Build }

func (f fakeBuilds) FindByBuildID(_ context.Context, id uuid.UUID) (*models.Build, error) {
	b, ok := f.b[id]
	if !ok {
		return nil, errNone
	}
	c := *b
	return &c, nil
}

// fakeBuildFiles are a build bucket: build files and artifacts by id.
type fakeBuildFiles struct {
	files     map[uuid.UUID]models.BuildFile
	bytes     map[uuid.UUID][]byte
	artifacts map[uuid.UUID][]byte
	builds    fakeBuilds
}

func (f fakeBuildFiles) File(_ context.Context, id string) (*models.BuildFile, error) {
	fid, err := uuid.Parse(id)
	bf, ok := f.files[fid]
	if err != nil || !ok {
		return nil, builds.ErrFileNotFound
	}
	return &bf, nil
}

func (f fakeBuildFiles) OpenFile(_ context.Context, b *models.Build, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error) {
	bf, ok := f.files[file]
	if !ok || !slices.Contains(b.FileIDs, file) {
		return nil, nil, builds.ErrTransferNotFound
	}
	return io.NopCloser(bytes.NewReader(f.bytes[file])), &bf, nil
}

func (f fakeBuildFiles) OpenArtifact(ctx context.Context, id string) (io.ReadCloser, *models.Build, error) {
	b, err := f.builds.FindByBuildID(ctx, uuid.MustParse(id))
	if err != nil {
		return nil, nil, err
	}
	a, ok := f.artifacts[b.BuildID]
	if !ok || b.Artifact == nil || b.Artifact.Deleted {
		return nil, b, errors.New("the build has no artifact to download")
	}
	return io.NopCloser(bytes.NewReader(a)), b, nil
}

type fakeModules struct {
	mu   sync.Mutex
	mods []models.Module
}

func (f *fakeModules) ListActive(context.Context) ([]models.Module, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.mods), nil
}

func (f *fakeModules) set(mods ...models.Module) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mods = mods
}

type fakeCreds struct {
	c map[uuid.UUID]protocol.Credential
}

func (f fakeCreds) Resolve(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error) {
	out, revs := map[string]protocol.Credential{}, map[string]int64{}
	for _, id := range ids {
		c, ok := f.c[id]
		if !ok {
			return nil, nil, errUnusable(id)
		}
		out[id.String()], revs[id.String()] = c, 1
	}
	return out, revs, nil
}

// fakeFiles are the minion files bucket's task files.
type fakeFiles struct {
	mu       sync.Mutex
	files    map[uuid.UUID]*models.MinionFile
	bytes    map[uuid.UUID][]byte
	dropped  []uuid.UUID
	released []uuid.UUID
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{files: map[uuid.UUID]*models.MinionFile{}, bytes: map[uuid.UUID][]byte{}}
}

func (f *fakeFiles) add(file *models.MinionFile, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[file.FileID], f.bytes[file.FileID] = file, body
}

func (f *fakeFiles) FileByID(_ context.Context, id uuid.UUID) (*models.MinionFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.files[id]
	if !ok {
		return nil, nil
	}
	c := *m
	return &c, nil
}

func (f *fakeFiles) Open(_ context.Context, id uuid.UUID) (io.ReadCloser, *models.MinionFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.files[id]
	if !ok {
		return nil, nil, errors.New("no such file")
	}
	c := *m
	return io.NopCloser(bytes.NewReader(f.bytes[id])), &c, nil
}

func (f *fakeFiles) PutFactoryFile(_ context.Context, op, forTask, by uuid.UUID, name string, body io.Reader) (*models.MinionFile, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	m := &models.MinionFile{FileID: uuid.New(), Kind: models.MinionFileKindTask, OperationID: op, ForTask: forTask, UploadedBy: by,
		Name: name, Total: int64(len(b)), Received: int64(len(b)), SHA256: []byte{1, 2, 3}, State: models.MinionFileComplete}
	f.add(m, b)
	return m, nil
}

func (f *fakeFiles) DropTaskFile(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropped = append(f.dropped, id)
	delete(f.files, id)
	return nil
}

func (f *fakeFiles) ReleaseTaskFile(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	return nil
}

func (f *fakeFiles) AdoptFactoryFiles(_ context.Context, t *models.MinionTask, ids []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		m, ok := f.files[id]
		if !ok || m.Kind != models.MinionFileKindTask || m.ForTask != t.TaskID || m.State != models.MinionFileComplete {
			return minionfiles.ErrNotPutForTask
		}
	}
	for _, id := range ids {
		m := f.files[id]
		m.Kind, m.ForTask, m.TaskID, m.SessionID = "", uuid.Nil, t.TaskID, t.SessionID
	}
	return nil
}

// fakeRPC answers calls by answer, and keeps them.
type fakeRPC struct {
	mu     sync.Mutex
	calls  []rpcCall
	answer func(queue, opType string, payload any) (protocol.ReplyEnvelope, error)
}

type rpcCall struct {
	queue, opType string
	payload       any
}

func (f *fakeRPC) Call(_ context.Context, queue, opType string, payload any, _ time.Duration) (protocol.ReplyEnvelope, error) {
	f.mu.Lock()
	f.calls = append(f.calls, rpcCall{queue: queue, opType: opType, payload: payload})
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return protocol.ReplyEnvelope{}, messaging.ErrRPCTimeout
	}
	return answer(queue, opType, payload)
}

func (f *fakeRPC) callsOf(opType string) []rpcCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []rpcCall
	for _, c := range f.calls {
		if c.opType == opType {
			out = append(out, c)
		}
	}
	return out
}

func okReply(payload any) protocol.ReplyEnvelope {
	raw, _ := json.Marshal(payload)
	return protocol.ReplyEnvelope{Status: protocol.StatusOK, Payload: raw}
}

func errReply(code, msg string) protocol.ReplyEnvelope {
	return protocol.ReplyEnvelope{Status: protocol.StatusError, Error: &protocol.EnvelopeError{Code: code, Message: msg}}
}

// fakeBus counts what is published, by task.
type fakeBus struct {
	eventbus.IEventBus
	mu  sync.Mutex
	got map[string]int
}

func (b *fakeBus) Publish(e eventbus.Event) {
	p, ok := e.Payload.(eventbus.MinionTaskEventPayload)
	if !ok {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.got == nil {
		b.got = map[string]int{}
	}
	b.got[p.TaskID]++
}

func (b *fakeBus) count(id uuid.UUID) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.got[id.String()]
}
