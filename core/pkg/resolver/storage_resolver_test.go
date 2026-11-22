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
