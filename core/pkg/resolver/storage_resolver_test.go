package resolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/storage"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// fakeStorage is pkg/storage's service, scripted.
type fakeStorage struct {
	snap    storage.Snapshot
	page    storage.ConsumerPage
	pageErr error
	actions []models.StorageAction
	err     error
	// by is who ran the last action, bucket its bucket.
	by     models.ModuleActor
	bucket string
	// ret is the retention policy, preview a cleanup's; days, op and
	// enabled what the last cleanup or policy was asked for.
	ret     storage.Retention
	preview storage.CleanupPreview
	days    int
	op      *uuid.UUID
	enabled bool
}

func (f *fakeStorage) Retention(context.Context) (storage.Retention, error) { return f.ret, f.err }

func (f *fakeStorage) SetRetention(_ context.Context, by models.ModuleActor, enabled bool, days int) (storage.Retention, error) {
	f.by, f.enabled, f.days = by, enabled, days
	if f.err != nil {
		return storage.Retention{}, f.err
	}
	return f.ret, nil
}

func (f *fakeStorage) CleanupPreview(_ context.Context, days int, _ string, _ int) (storage.CleanupPreview, error) {
	f.days = days
	return f.preview, f.pageErr
}

func (f *fakeStorage) Action(_ context.Context, id uuid.UUID) (*models.StorageAction, error) {
	for i := range f.actions {
		if f.actions[i].ActionID == id {
			return &f.actions[i], nil
		}
	}
	return nil, storage.ErrUnknownAction
}

func (f *fakeStorage) Cancel(_ context.Context, by models.ModuleActor, id uuid.UUID) (*models.StorageAction, error) {
	f.by = by
	if f.err != nil {
		return nil, f.err
	}
	a, err := f.Action(context.Background(), id)
	if err != nil {
		return nil, err
	}
	at := time.Now()
	out := *a
	out.CancelRequestedAt, out.CancelledBy = &at, &by
	return &out, nil
}

func (f *fakeStorage) CleanUp(_ context.Context, by models.ModuleActor, days int, op *uuid.UUID) (*models.StorageAction, error) {
	f.by, f.days, f.op = by, days, op
	if f.err != nil {
		return nil, f.err
	}
	before := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	a := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionCleanup, Bucket: "desktop-recordings", Actor: by,
		Status: models.StorageActionRunning, StartedAt: time.Now(), OlderThanDays: days, Before: &before}
	if op != nil {
		a.OperationID, a.OperationName = op, "Red"
	}
	return a, nil
}

func (f *fakeStorage) Status(context.Context) storage.Snapshot { return f.snap }

func (f *fakeStorage) Consumers(_ context.Context, bucket, _, _ string, _ int) (storage.ConsumerPage, error) {
	f.bucket = bucket
	return f.page, f.pageErr
}

func (f *fakeStorage) Actions(context.Context, *pagination.Cursor, int64, bool) ([]models.StorageAction, int64, error) {
	return f.actions, int64(len(f.actions)), nil
}

func (f *fakeStorage) Reclaim(_ context.Context, by models.ModuleActor) (*models.StorageAction, error) {
	f.by = by
	if f.err != nil {
		return nil, f.err
	}
	return &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionReclaim, Actor: by, Status: models.StorageActionRunning,
		StartedAt: time.Now()}, nil
}

func (f *fakeStorage) Grow(_ context.Context, by models.ModuleActor, bucket string) (*models.StorageAction, error) {
	f.by, f.bucket = by, bucket
	if f.err != nil {
		return nil, f.err
	}
	at := time.Now()
	return &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionGrow, Bucket: bucket, Actor: by,
		Status: models.StorageActionFailed, Err: "SeaweedFS: only 0 volumes left", StartedAt: at, FinishedAt: &at}, nil
}

func codeOf(err error) string {
	var e *gqlerror.Error
	if errors.As(err, &e) {
		code, _ := e.Extensions["code"].(string)
		return code
	}
	return ""
}

// The status as GraphQL has it: SeaweedFS's state, the disks and buckets,
// SeaweedFS's own files a bucket named "", the warnings.
func TestStorageStatus(t *testing.T) {
	svc := &fakeStorage{snap: storage.Snapshot{At: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC),
		SeaweedFS: storage.SeaweedFS{Configured: true, Reachable: true, Version: "4.16", Slots: 20, FreeSlots: 2},
		Disks:     []storage.Disk{{Node: "volume:8080", Dir: "/data", Total: 100, Used: 60, Free: 40, Volumes: 6, MaxVolumes: 20}},
		Buckets: []storage.Bucket{{Name: "minion-files", Purposes: []string{models.StoragePurposeMinionFiles}, Volumes: 2, Writable: 1,
			Size: 200, Deleted: 80, Objects: 8, ObjectBytes: 120}, {Name: ""}},
		Warnings: []storage.Warning{{Level: storage.LevelInfo, Code: "reclaimable", Bucket: "", Message: "m"},
			{Level: storage.LevelWarning, Code: "few_free_slots", Message: "n"}}}}
	r := NewStorageResolver(svc)
	got, err := r.StorageStatus(context.Background())
	if err != nil || got.At != "2026-11-22T10:00:00Z" || !got.Seaweedfs.Reachable || *got.Seaweedfs.Version != "4.16" || got.Seaweedfs.Error != nil ||
		got.Seaweedfs.FreeSlots != 2 || len(got.Disks) != 1 || *got.Disks[0].Dir != "/data" || got.Disks[0].Error != nil {
		t.Fatalf("status %+v %v", got, err)
	}
	b := got.Buckets[0]
	if b.Name != "minion-files" || b.WritableVolumes != 1 || b.DeletedBytes != 80 || b.ObjectBytes != 120 || len(b.Purposes) != 1 {
		t.Fatalf("bucket %+v", b)
	}
	if own := got.Buckets[1]; own.Purposes == nil || len(own.Purposes) != 0 {
		t.Fatalf("SeaweedFS's own: purposes %v, not null", own.Purposes)
	}
	if w := got.Warnings[0]; w.Bucket == nil || *w.Bucket != "" {
		t.Fatalf("a warning of SeaweedFS's own files: bucket %v", w.Bucket)
	}
	if w := got.Warnings[1]; w.Bucket != nil {
		t.Fatalf("a warning of the store: bucket %v", *w.Bucket)
	}
}

// A page of a bucket's consumers: an operation's, or none's (skills); an
// invalid cursor or count is the caller's error.
func TestStorageConsumers(t *testing.T) {
	op := uuid.New()
	svc := &fakeStorage{page: storage.ConsumerPage{Total: 3, More: true, Consumers: []storage.Consumer{
		{OperationID: op, OperationName: "Red", Purpose: models.StoragePurposeMinionFiles, Kind: "download", Objects: 2, Bytes: 90},
		{Purpose: models.StoragePurposeSkills, Kind: "version", Objects: 1, Bytes: 5},
	}}}
	r := NewStorageResolver(svc)
	conn, err := r.StorageConsumers(context.Background(), "minion-files", nil, nil, nil)
	if err != nil || svc.bucket != "minion-files" || conn.TotalCount != 3 || !conn.PageInfo.HasNextPage || conn.PageInfo.HasPreviousPage ||
		len(conn.Edges) != 2 || *conn.Edges[0].Node.OperationID != op.String() || *conn.Edges[0].Node.OperationName != "Red" ||
		conn.Edges[1].Node.OperationID != nil || conn.PageInfo.EndCursor == nil || *conn.PageInfo.EndCursor != conn.Edges[1].Cursor {
		t.Fatalf("page %+v %v", conn, err)
	}
	zero := 0
	if _, err := r.StorageConsumers(context.Background(), "b", nil, &zero, nil); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("first 0: %v", err)
	}
	svc.pageErr = storage.ErrBadCursor
	if _, err := r.StorageConsumers(context.Background(), "b", nil, nil, nil); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("a bad cursor: %v", err)
	}
}

// The actions run by the user who asks, never an agent key; what the store
// refuses comes back coded.
func TestStorageActionsByUsers(t *testing.T) {
	svc := &fakeStorage{}
	r := NewStorageResolver(svc)
	user := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Username: "alice", Roles: []string{"admin"}})

	a, err := r.ReclaimStorage(user)
	if err != nil || a.Action != models.StorageActionReclaim || a.Status != models.StorageActionRunning || svc.by.Name != "alice" ||
		a.Actor.Name == nil || *a.Actor.Name != "alice" || a.FinishedAt != nil {
		t.Fatalf("reclaim %+v %v", a, err)
	}
	a, err = r.GrowStorageBucket(user, "skills")
	if err != nil || svc.bucket != "skills" || *a.Bucket != "skills" || a.Status != models.StorageActionFailed ||
		*a.Error != "SeaweedFS: only 0 volumes left" || a.FinishedAt == nil {
		t.Fatalf("grow %+v %v", a, err)
	}

	agent := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Agent: &gqlctx.AgentInfo{}})
	if _, err := r.ReclaimStorage(agent); codeOf(err) != errCodeForbidden {
		t.Fatalf("an agent's reclaim: %v", err)
	}
	if _, err := r.GrowStorageBucket(agent, "skills"); codeOf(err) != errCodeForbidden {
		t.Fatalf("an agent's grow: %v", err)
	}
	for err, code := range map[error]string{
		storage.ErrNotConfigured: errCodeBadUserInput, storage.ErrUnknownBucket: errCodeBadUserInput, storage.ErrRunning: errCodeConflict,
	} {
		svc.err = err
		if _, got := r.ReclaimStorage(user); codeOf(got) != code {
			t.Errorf("%v: code %q, want %q", err, codeOf(got), code)
		}
	}
}

// The retention policy and the cleanups as GraphQL has them: set and run by
// the user who asks, never an agent key; the age and the operation checked;
// a preview's operations a page at a time, one gone unnamed.
func TestStorageRetentionAndCleanups(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	next := at.Add(24 * time.Hour)
	op := uuid.New()
	last := models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionCleanup, Actor: models.RetentionActor,
		Status: models.StorageActionSucceeded, StartedAt: at, FinishedAt: &at, OlderThanDays: 30, Before: &at, Recordings: 12, Bytes: 3400}
	svc := &fakeStorage{ret: storage.Retention{Policy: models.StorageRetention{Enabled: true, OlderThanDays: 30, UpdatedAt: &at,
		UpdatedBy: &models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}}, LastRun: &last, NextRun: &next}}
	r := NewStorageResolver(svc)
	got, err := r.StorageRetention(context.Background())
	if err != nil || !got.Enabled || got.OlderThanDays != 30 || *got.UpdatedAt != "2026-11-22T10:00:00Z" || *got.UpdatedBy.Name != "alice" ||
		*got.NextRunAt != "2026-11-23T10:00:00Z" || got.LastRun == nil || got.LastRun.Actor.ID != "retention_policy" ||
		*got.LastRun.OlderThanDays != 30 || got.LastRun.OperationID != nil || got.LastRun.Recordings != 12 || got.LastRun.Bytes != 3400 ||
		*got.LastRun.Before != "2026-11-22T10:00:00Z" {
		t.Fatalf("retention %+v %v", got, err)
	}
	svc.ret = storage.Retention{Policy: models.StorageRetention{OlderThanDays: 30}}
	if got, _ := r.StorageRetention(context.Background()); got.Enabled || got.UpdatedAt != nil || got.UpdatedBy != nil || got.LastRun != nil ||
		got.NextRunAt != nil {
		t.Fatalf("before anyone set it %+v", got)
	}

	user := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Username: "alice", Roles: []string{"admin"}})
	agent := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Agent: &gqlctx.AgentInfo{}})
	if _, err := r.SetStorageRetention(user, true, 45); err != nil || svc.by.Name != "alice" || !svc.enabled || svc.days != 45 {
		t.Fatalf("set by %+v: %v", svc, err)
	}
	if _, err := r.SetStorageRetention(agent, true, 45); codeOf(err) != errCodeForbidden {
		t.Fatalf("an agent's policy: %v", err)
	}

	id := op.String()
	a, err := r.CleanUpStorage(user, 30, &id)
	if err != nil || svc.op == nil || *svc.op != op || svc.by.Name != "alice" || a.Action != models.StorageActionCleanup ||
		*a.OlderThanDays != 30 || *a.OperationID != id || *a.OperationName != "Red" || *a.Before != "2026-10-23T10:00:00Z" || a.Recordings != 0 {
		t.Fatalf("a cleanup of one operation %+v %v", a, err)
	}
	if a, err := r.CleanUpStorage(user, 30, nil); err != nil || svc.op != nil || a.OperationID != nil || a.OperationName != nil {
		t.Fatalf("a cleanup of every operation %+v %v", a, err)
	}
	bad := "not an id"
	if _, err := r.CleanUpStorage(user, 30, &bad); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("a bad operation id: %v", err)
	}
	if _, err := r.CleanUpStorage(agent, 30, nil); codeOf(err) != errCodeForbidden {
		t.Fatalf("an agent's cleanup: %v", err)
	}
	for err, code := range map[error]string{
		storage.ErrBadAge: errCodeBadUserInput, storage.ErrUnknownOperation: errCodeBadUserInput, storage.ErrRunning: errCodeConflict,
	} {
		svc.err = err
		if _, got := r.CleanUpStorage(user, 30, nil); codeOf(got) != code {
			t.Errorf("a cleanup, %v: code %q, want %q", err, codeOf(got), code)
		}
	}
	svc.err = storage.ErrBadAge
	if _, err := r.SetStorageRetention(user, true, 0); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("a policy at 0 days: %v", err)
	}
	svc.err = nil

	svc.preview = storage.CleanupPreview{OlderThanDays: 30, Before: at, Recordings: 5, Bytes: 900, Total: 2, More: true,
		Operations: []storage.CleanupOperation{{OperationID: op, OperationName: "Red", Recordings: 4, Bytes: 800}}}
	p, err := r.StorageCleanupPreview(context.Background(), 30, nil, nil)
	if err != nil || svc.days != 30 || p.OlderThanDays != 30 || p.Recordings != 5 || p.Bytes != 900 || p.Before != "2026-11-22T10:00:00Z" ||
		p.Operations.TotalCount != 2 || !p.Operations.PageInfo.HasNextPage || len(p.Operations.Edges) != 1 ||
		p.Operations.Edges[0].Node.OperationID != id || *p.Operations.Edges[0].Node.OperationName != "Red" ||
		p.Operations.Edges[0].Node.Recordings != 4 || *p.Operations.PageInfo.EndCursor != p.Operations.Edges[0].Cursor {
		t.Fatalf("preview %+v %v", p, err)
	}
	svc.preview.Operations[0].OperationName = ""
	if p, _ := r.StorageCleanupPreview(context.Background(), 30, nil, nil); p.Operations.Edges[0].Node.OperationName != nil {
		t.Fatalf("an operation gone is named %q", *p.Operations.Edges[0].Node.OperationName)
	}
	zero := 0
	if _, err := r.StorageCleanupPreview(context.Background(), 30, &zero, nil); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("first 0: %v", err)
	}
	for _, err := range []error{storage.ErrBadCursor, storage.ErrBadAge} {
		svc.pageErr = err
		if _, got := r.StorageCleanupPreview(context.Background(), 30, nil, nil); codeOf(got) != errCodeBadUserInput {
			t.Errorf("a preview, %v: code %q", err, codeOf(got))
		}
	}
}

// One action as GraphQL has it, its progress with it: what it does now, the
// cancel asked, a reclaim's volumes, a cleanup's plan; none is null. Cancel
// is the asking user's, never an agent key's; what the service refuses comes
// back coded.
func TestStorageActionAndCancel(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	bob := models.ModuleActor{Type: "user", ID: "u2", Name: "bob"}
	reclaim := models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionReclaim, Status: models.StorageActionRunning,
		Actor: models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}, StartedAt: at, Freed: 40, Step: "compacting volume 39 of minion-files (2 of 5)",
		StepAt: &at, CancelRequestedAt: &at, CancelledBy: &bob, VacuumPaused: true,
		ReclaimVolumes: []models.StorageReclaimVolume{
			{ID: 37, Bucket: "minion-files", Server: "volume:8080", Size: 100, Deleted: 40, State: models.ReclaimVolumeCompacted, Freed: 40,
				StartedAt: &at, FinishedAt: &at},
			{ID: 39, Bucket: "", Server: "volume:8080", Size: 50, Deleted: 10, State: models.ReclaimVolumeFailed, Msg: "asked 3 times"},
		}}
	cleanup := models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionCleanup, Status: models.StorageActionRunning,
		StartedAt: at, PlannedRecordings: 12, PlannedBytes: 3400, Recordings: 2, Bytes: 100}
	svc := &fakeStorage{actions: []models.StorageAction{reclaim, cleanup}}
	r := NewStorageResolver(svc)

	got, err := r.StorageAction(context.Background(), reclaim.ActionID.String())
	if err != nil || got == nil || *got.Step != "compacting volume 39 of minion-files (2 of 5)" || *got.StepAt != "2026-11-22T10:00:00Z" ||
		*got.CancelRequestedAt != "2026-11-22T10:00:00Z" || *got.CancelledBy.Name != "bob" || !got.VacuumPaused || len(got.ReclaimVolumes) != 2 {
		t.Fatalf("a reclaim %+v %v", got, err)
	}
	if v := got.ReclaimVolumes[0]; v.ID != 37 || v.Bucket != "minion-files" || v.Server != "volume:8080" || v.Size != 100 || v.DeletedBytes != 40 ||
		v.State != models.ReclaimVolumeCompacted || v.Freed != 40 || v.Msg != nil || *v.StartedAt != "2026-11-22T10:00:00Z" || v.FinishedAt == nil {
		t.Fatalf("a volume %+v", v)
	}
	if v := got.ReclaimVolumes[1]; v.Bucket != "" || *v.Msg != "asked 3 times" || v.StartedAt != nil {
		t.Fatalf("a volume failed %+v", v)
	}
	if got, _ := r.StorageAction(context.Background(), cleanup.ActionID.String()); got.PlannedRecordings != 12 || got.PlannedBytes != 3400 ||
		got.ReclaimVolumes == nil || len(got.ReclaimVolumes) != 0 || got.Step != nil || got.CancelledBy != nil {
		t.Fatalf("a cleanup %+v", got)
	}
	if got, err := r.StorageAction(context.Background(), uuid.NewString()); got != nil || err != nil {
		t.Fatalf("none: %+v %v", got, err)
	}
	if _, err := r.StorageAction(context.Background(), "not an id"); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("a bad id: %v", err)
	}

	user := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Username: "carol", Roles: []string{"admin"}})
	agent := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Agent: &gqlctx.AgentInfo{}})
	if a, err := r.CancelStorageAction(user, cleanup.ActionID.String()); err != nil || svc.by.Name != "carol" || *a.CancelledBy.Name != "carol" {
		t.Fatalf("cancelled %+v %v", a, err)
	}
	if _, err := r.CancelStorageAction(agent, cleanup.ActionID.String()); codeOf(err) != errCodeForbidden {
		t.Fatalf("an agent's cancel: %v", err)
	}
	if _, err := r.CancelStorageAction(user, "not an id"); codeOf(err) != errCodeBadUserInput {
		t.Fatalf("a bad id: %v", err)
	}
	for err, code := range map[error]string{
		storage.ErrNotRunning: errCodeConflict, storage.ErrNotCancellable: errCodeBadUserInput, storage.ErrUnknownAction: errCodeNotFound,
	} {
		svc.err = err
		if _, got := r.CancelStorageAction(user, cleanup.ActionID.String()); codeOf(got) != code {
			t.Errorf("a cancel, %v: code %q, want %q", err, codeOf(got), code)
		}
	}
}
