package builds

import (
	"bytes"
	"context"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/minio/minio-go/v7"
	"github.com/qiniu/qmgo"
)

// In-memory stand-ins with the repositories' compare-and-set semantics.

type fakeFiles struct {
	mu   sync.Mutex
	rows map[uuid.UUID]models.BuildFile
}

func (f *fakeFiles) Insert(_ context.Context, file *models.BuildFile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[file.FileID] = *file
	return nil
}

func (f *fakeFiles) FindByFileID(_ context.Context, id uuid.UUID) (*models.BuildFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.rows[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	return &file, nil
}

func (f *fakeFiles) List(_ context.Context, op uuid.UUID) ([]models.BuildFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.BuildFile
	for _, file := range f.rows {
		if file.OperationID == op {
			out = append(out, file)
		}
	}
	return out, nil
}

func (f *fakeFiles) ListCreatedBefore(_ context.Context, cutoff time.Time, _ int64) ([]models.BuildFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.BuildFile
	for _, file := range f.rows {
		if file.CreatedAt.Before(cutoff) {
			out = append(out, file)
		}
	}
	return out, nil
}

func (f *fakeFiles) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.rows[id]
	delete(f.rows, id)
	return ok, nil
}

func (f *fakeFiles) DeleteByOperationID(_ context.Context, op uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, file := range f.rows {
		if file.OperationID == op {
			delete(f.rows, id)
		}
	}
	return nil
}

type fakeBuilds struct {
	mu   sync.Mutex
	rows map[string]*models.Build
}

func (f *fakeBuilds) Insert(_ context.Context, b *models.Build) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *b
	f.rows[b.BuildID] = &cp
	return nil
}

func (f *fakeBuilds) FindByBuildID(_ context.Context, id string) (*models.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	cp := *b
	return &cp, nil
}

func (f *fakeBuilds) sorted(keep func(*models.Build) bool) []models.Build {
	var out []models.Build
	for _, b := range f.rows {
		if keep(b) {
			out = append(out, *b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (f *fakeBuilds) List(_ context.Context, op uuid.UUID, _ int64) ([]models.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sorted(func(b *models.Build) bool { return b.OperationID == op }), nil
}

func (f *fakeBuilds) ListUnfinished(context.Context) ([]models.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sorted(func(b *models.Build) bool { return !b.Finished() }), nil
}

func (f *fakeBuilds) ClaimQueued(_ context.Context, now, until time.Time) (*models.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	queued := f.sorted(func(b *models.Build) bool {
		return b.State == models.BuildStateQueued && (b.LeaseUntil == nil || b.LeaseUntil.Before(now))
	})
	if len(queued) == 0 {
		return nil, nil
	}
	b := f.rows[queued[0].BuildID]
	b.LeaseUntil = &until
	cp := *b
	return &cp, nil
}

func (f *fakeBuilds) ReleaseClaim(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.rows[id]; ok && b.State == models.BuildStateQueued {
		b.LeaseUntil = nil
	}
	return nil
}

func (f *fakeBuilds) MarkBuilding(_ context.Context, id, instance, version string, deadline, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok || b.State != models.BuildStateQueued {
		return false, nil
	}
	b.State, b.Instance, b.FactoryVersion, b.Deadline, b.StartedAt, b.LeaseUntil = models.BuildStateBuilding, instance, version, deadline, &at, nil
	return true, nil
}

func (f *fakeBuilds) Unoffer(_ context.Context, id, instance string, until time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok || b.State != models.BuildStateBuilding || b.Instance != instance || b.ResultReceived {
		return false, nil
	}
	b.State, b.LeaseUntil, b.Instance, b.FactoryVersion, b.StartedAt = models.BuildStateQueued, &until, "", "", nil
	b.Plan, b.StepReports = nil, nil
	return true, nil
}

func (f *fakeBuilds) SetPlan(_ context.Context, id, instance string, plan []models.BuildPlanStep) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok || b.State != models.BuildStateBuilding || b.Instance != instance {
		return false, nil
	}
	b.Plan = plan
	return true, nil
}

// ReportStep keeps a step's first time and its latest message, as the
// repository's $min and $set do.
func (f *fakeBuilds) ReportStep(_ context.Context, id, instance, step, message string, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok || b.State != models.BuildStateBuilding || b.Instance != instance {
		return false, nil
	}
	if b.StepReports == nil {
		b.StepReports = map[string]models.BuildStepReport{}
	}
	r, seen := b.StepReports[step]
	if !seen || at.Before(r.StartedAt) {
		r.StartedAt = at
	}
	r.Message = message
	b.StepReports[step] = r
	return true, nil
}

func (f *fakeBuilds) RecordResult(_ context.Context, id, instance string, art models.BuildArtifact, caps, warnings []string, log string, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok || b.State != models.BuildStateBuilding || b.Instance != instance || b.ResultReceived {
		return false, nil
	}
	b.ResultReceived, b.ResultAt, b.Artifact, b.Capabilities, b.Warnings, b.Log = true, &at, &art, caps, warnings, log
	return true, nil
}

func (f *fakeBuilds) Finish(_ context.Context, id string, from []string, out repository.BuildOutcome) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return false, nil
	}
	match := false
	for _, s := range from {
		match = match || b.State == s
	}
	if !match {
		return false, nil
	}
	b.State, b.FinishedAt, b.LeaseUntil = out.State, &out.At, nil
	if out.Error != nil {
		b.Error = out.Error
	}
	if out.Artifact != nil {
		b.Artifact = out.Artifact
	}
	if out.Capabilities != nil {
		b.Capabilities = out.Capabilities
	}
	if out.Warnings != nil {
		b.Warnings = out.Warnings
	}
	if out.Log != "" {
		b.Log = out.Log
	}
	if out.CancelledBy != nil {
		b.CancelledBy = out.CancelledBy
	}
	return true, nil
}

func (f *fakeBuilds) MarkArtifactDeleted(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.rows[id]; ok && b.Artifact != nil {
		b.Artifact.Deleted = true
	}
	return nil
}

func (f *fakeBuilds) CountFileReferences(_ context.Context, id string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, b := range f.rows {
		for _, fid := range b.FileIDs {
			if fid == id {
				n++
			}
		}
	}
	return n, nil
}

func (f *fakeBuilds) DeleteByOperationID(_ context.Context, op uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, b := range f.rows {
		if b.OperationID == op {
			delete(f.rows, id)
		}
	}
	return nil
}

type fakeMinions struct {
	mu   sync.Mutex
	rows map[string]models.Minion
}

func (f *fakeMinions) Insert(_ context.Context, m *models.Minion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[m.MinionID]; !ok {
		f.rows[m.MinionID] = *m
	}
	return nil
}

func (f *fakeMinions) List(_ context.Context, op uuid.UUID) ([]models.Minion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.Minion
	for _, m := range f.rows {
		if m.OperationID == op {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMinions) DeleteByOperationID(_ context.Context, op uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, m := range f.rows {
		if m.OperationID == op {
			delete(f.rows, id)
		}
	}
	return nil
}

// --- collaborators ---

type fakeRegistry struct {
	mu      sync.Mutex
	schemas map[string]*models.ModuleBuildSchema
	active  []models.Module
	icons   map[string]string // by module name, factories only
}

func (f *fakeRegistry) LatestBuildSchema(_ context.Context, name string) (*models.ModuleBuildSchema, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.schemas[name], nil
}

func (f *fakeRegistry) LatestBuildSchemas(context.Context) (map[string]*models.ModuleBuildSchema, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.schemas, nil
}

func (f *fakeRegistry) LatestIcons(_ context.Context, moduleType string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if moduleType != protocol.ServiceFactory {
		return nil, nil
	}
	return f.icons, nil
}

func (f *fakeRegistry) ListActive(context.Context) ([]models.Module, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]models.Module(nil), f.active...), nil
}

func (f *fakeRegistry) FindByInstance(_ context.Context, instance string) (models.Module, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.active {
		if m.Instance == instance {
			return m, nil
		}
	}
	return models.Module{}, qmgo.ErrNoSuchDocuments
}

func (f *fakeRegistry) setActive(mods ...models.Module) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = mods
}

// fakeChannels holds channel kinds' connection schemas by name.
type fakeChannels struct {
	schemas map[string]*protocol.ConnectionSchema
}

func (f *fakeChannels) Schema(_ context.Context, moduleName string) (*protocol.ConnectionSchema, error) {
	return f.schemas[moduleName], nil
}

// call is one call to a factory's rpc_queue.
type call struct {
	queue, op string
	payload   any
}

// fakeCaller answers factory calls with answer, recording them.
type fakeCaller struct {
	mu     sync.Mutex
	calls  []call
	answer func(c call) (protocol.ReplyEnvelope, error)
}

func (f *fakeCaller) Call(_ context.Context, queue, op string, payload any, _ time.Duration) (protocol.ReplyEnvelope, error) {
	c := call{queue: queue, op: op, payload: payload}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return accept(c), nil
	}
	return answer(c)
}

func (f *fakeCaller) recorded() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// bobPlan is the plan bob-1 accepts builds with.
var bobPlan = []protocol.BuildStep{{ID: "badge", Label: "Check the badge"}, {ID: "render", Label: "Describe the minion"}, {ID: "upload", Label: "Upload"}}

// accept is a factory's yes to a build or a cancel.
func accept(c call) protocol.ReplyEnvelope {
	req := protocol.Envelope{Type: c.op, Version: protocol.EnvelopeVersionV1}
	var payload any = struct{}{}
	switch p := c.payload.(type) {
	case protocol.BuildRequest:
		payload = protocol.BuildReply{BuildID: p.BuildID, Accepted: true, Steps: bobPlan}
	case protocol.BuildCancelRequest:
		payload = protocol.BuildCancelReply{BuildID: p.BuildID, Cancelled: true}
	}
	rep, _ := protocol.NewReply(req, protocol.Source{Service: "factory"}, payload)
	return rep
}

func refuse(c call, code, msg string) protocol.ReplyEnvelope {
	return protocol.NewErrorReply(protocol.Envelope{Type: c.op, Version: protocol.EnvelopeVersionV1}, protocol.Source{Service: "factory"}, code, msg)
}

type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeStore) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = b
	return nil
}

func (f *fakeStore) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, blob.ObjectInfo{}, minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}
	}
	return io.NopCloser(bytes.NewReader(b)), blob.ObjectInfo{ContentLength: int64(len(b))}, nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func (f *fakeStore) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

type fakeSigner struct{}

func (fakeSigner) GetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "http://store.test/" + key + "?sig=get", nil
}

func (fakeSigner) PutURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "http://store.test/" + key + "?sig=put", nil
}
