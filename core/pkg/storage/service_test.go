package storage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

const mib = int64(1 << 20)

// fakeMaster is a SeaweedFS master and its one volume server, over HTTP:
// volumes by collection, each bucket's writable ones, the slots, the disk;
// vacuum drops the deleted bytes of the volumes past its threshold, grow adds
// a volume while a slot is free.
type fakeMaster struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	vols      []volume
	writables map[string][]uint32
	max, free int
	disk      [3]int64 // all, used, free
	// vacuumGate, when set, holds a vacuum until it is closed; vacuumErr
	// fails it; vacuums counts them, thresholds says each one's threshold.
	vacuumGate chan struct{}
	vacuumErr  string
	vacuums    int
	thresholds []string
	nodeDown   bool
}

func newFakeMaster(t *testing.T) *fakeMaster {
	m := &fakeMaster{t: t, writables: map[string][]uint32{}, max: 20, free: 10, disk: [3]int64{1000 * mib, 600 * mib, 400 * mib}}
	mux := http.NewServeMux()
	mux.HandleFunc("/dir/status", m.dirStatus)
	mux.HandleFunc("/vol/status", m.volStatus)
	mux.HandleFunc("/cluster/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"IsLeader": true, "Leader": "seaweedfs-master:9333.19333"})
	})
	mux.HandleFunc("/status", m.nodeStatus)
	mux.HandleFunc("/vol/vacuum", m.vacuum)
	mux.HandleFunc("/vol/grow", m.grow)
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

// node is the volume server's address as the master has it: this server's.
func (m *fakeMaster) node() string { return strings.TrimPrefix(m.srv.URL, "http://") }

// add is a volume of collection: size bytes, deleted of them deleted files';
// writable, a volume new files go to.
func (m *fakeMaster) add(collection string, size, deleted int64, writable, readOnly bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uint32(len(m.vols) + 1)
	m.vols = append(m.vols, volume{ID: id, Collection: collection, Size: size, DeletedByteCount: deleted, ReadOnly: readOnly})
	if writable {
		m.writables[collection] = append(m.writables[collection], id)
	}
}

func (m *fakeMaster) dirStatus(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var layouts []map[string]any
	for _, c := range slices.Sorted(func(yield func(string) bool) {
		for c := range m.writables {
			if !yield(c) {
				return
			}
		}
	}) {
		layouts = append(layouts, map[string]any{"collection": c, "writables": m.writables[c], "replication": "000"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"Version": "30GB 4.16 d4d2e511e", "Topology": map[string]any{
		"Max": m.max, "Free": m.free, "Layouts": layouts,
		"DataCenters": []any{map[string]any{"Id": "DefaultDataCenter", "Racks": []any{map[string]any{"Id": "DefaultRack",
			"DataNodes": []any{map[string]any{"Url": m.node(), "Volumes": len(m.vols), "Max": m.max}}}}}},
	}})
}

func (m *fakeMaster) volStatus(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"Version": "30GB 4.16 d4d2e511e", "Volumes": map[string]any{"Max": m.max, "Free": m.free,
		"DataCenters": map[string]any{"DefaultDataCenter": map[string]any{"DefaultRack": map[string]any{m.node(): m.vols}}}}})
}

func (m *fakeMaster) nodeStatus(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nodeDown {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "volume server starting"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"DiskStatuses": []any{map[string]any{"dir": "/data", "all": m.disk[0], "used": m.disk[1], "free": m.disk[2]}}})
}

func (m *fakeMaster) vacuum(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	gate := m.vacuumGate
	m.vacuums++
	m.thresholds = append(m.thresholds, r.FormValue("garbageThreshold"))
	m.mu.Unlock()
	if gate != nil {
		<-gate
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vacuumErr != "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": m.vacuumErr})
		return
	}
	for i, v := range m.vols {
		if v.Size > 0 && float64(v.DeletedByteCount) >= 0.1*float64(v.Size) {
			m.vols[i].Size -= v.DeletedByteCount
			m.vols[i].DeletedByteCount = 0
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"Topology": map[string]any{}})
}

func (m *fakeMaster) grow(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.free < 1 {
		writeJSON(w, http.StatusNotAcceptable, map[string]any{"error": "only 0 volumes left, not enough for 1"})
		return
	}
	c := r.FormValue("collection")
	id := uint32(len(m.vols) + 1)
	m.vols = append(m.vols, volume{ID: id, Collection: c})
	m.writables[c] = append(m.writables[c], id)
	m.free--
	writeJSON(w, http.StatusOK, map[string]any{"count": 1})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fakeUsage is core's records' sums.
type fakeUsage struct {
	rows []models.StorageUsage
	err  error
}

func (f fakeUsage) Usage(context.Context) ([]models.StorageUsage, error) { return f.rows, f.err }

// fakeActions keeps actions in memory: one running of an action for a
// bucket, as the repository's index keeps them.
type fakeActions struct {
	mu      sync.Mutex
	list    []models.StorageAction
	endedBy string
}

func (f *fakeActions) Insert(_ context.Context, a *models.StorageAction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.list {
		if x.Status == models.StorageActionRunning && x.Action == a.Action && x.Bucket == a.Bucket {
			return repository.ErrStorageActionRunning
		}
	}
	a.CreateAt = time.Now()
	f.list = append(f.list, *a)
	return nil
}

func (f *fakeActions) Finish(_ context.Context, id uuid.UUID, status, why string, freed int64, volumes int, at time.Time) (*models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ActionID == id && f.list[i].Status == models.StorageActionRunning {
			a := &f.list[i]
			a.Status, a.Err, a.Freed, a.Volumes, a.FinishedAt = status, why, freed, volumes, &at
			out := *a
			return &out, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeActions) Progress(_ context.Context, id uuid.UUID, recordings, bytes int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ActionID == id && f.list[i].Status == models.StorageActionRunning {
			f.list[i].Recordings, f.list[i].Bytes = recordings, bytes
			return nil
		}
	}
	return repository.ErrNotFound
}

func (f *fakeActions) Last(_ context.Context, action string, actor models.ModuleActor) (*models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var last *models.StorageAction
	for i := range f.list {
		a := &f.list[i]
		if a.Action == action && a.Actor.Type == actor.Type && a.Actor.ID == actor.ID && (last == nil || a.StartedAt.After(last.StartedAt)) {
			last = a
		}
	}
	if last == nil {
		return nil, nil
	}
	out := *last
	return &out, nil
}

func (f *fakeActions) EndRunning(_ context.Context, why string, at time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endedBy = why
	var n int64
	for i := range f.list {
		if f.list[i].Status == models.StorageActionRunning {
			f.list[i].Status, f.list[i].Err, f.list[i].FinishedAt = models.StorageActionFailed, why, &at
			n++
		}
	}
	return n, nil
}

func (f *fakeActions) FindWithCursor(context.Context, *pagination.Cursor, int64, bool) ([]models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.list), nil
}

func (f *fakeActions) Count(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.list)), nil
}

func (f *fakeActions) get(id uuid.UUID) models.StorageAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.list {
		if a.ActionID == id {
			return a
		}
	}
	return models.StorageAction{}
}

// fakeOps names operations.
type fakeOps []models.Operation

func (f fakeOps) FindAll(context.Context, string, int64, int64, *uuid.UUID) ([]models.Operation, error) {
	return f, nil
}

func (f fakeOps) FindByID(_ context.Context, id uuid.UUID) (models.Operation, error) {
	for _, op := range f {
		if op.OperationID == id {
			return op, nil
		}
	}
	return models.Operation{}, repository.ErrNotFound
}

// coreBuckets are core's buckets as the environment names them by default.
var coreBuckets = map[string]string{
	models.StoragePurposeMinionFiles: "minion-files", models.StoragePurposeRecordings: "desktop-recordings",
	models.StoragePurposeBuilds: "logos-builds", models.StoragePurposeWikiImages: "wiki-images",
	models.StoragePurposeWikiFiles: "wiki-files", models.StoragePurposeSkills: "skills",
}

func newService(t *testing.T, master string, usage fakeUsage, ops fakeOps) (*Service, *fakeActions) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.MasterURL, cfg.Buckets = master, coreBuckets
	actions := &fakeActions{}
	s := New(cfg, usage, actions, ops, zap.NewNop())
	t.Cleanup(s.Stop)
	return s, actions
}

func bucketOf(t *testing.T, snap Snapshot, name string) Bucket {
	t.Helper()
	for _, b := range snap.Buckets {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("no bucket %q in %+v", name, snap.Buckets)
	return Bucket{}
}

func codes(ws []Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Level+":"+w.Code+":"+w.Bucket)
	}
	return out
}

// The snapshot: SeaweedFS's numbers by bucket (a bucket is a collection), the
// disk and slots, core's records by bucket and by operation, in core's order
// (its purposes, the others, SeaweedFS's own last); and the warnings, the
// worst first.
func TestSnapshot(t *testing.T) {
	m := newFakeMaster(t)
	m.max, m.free = 20, 2
	m.disk = [3]int64{1000 * mib, 950 * mib, 50 * mib}
	m.add("minion-files", 100*mib, 50*mib, true, false)
	m.add("minion-files", 100*mib, 30*mib, false, true)
	m.add("desktop-recordings", 40*mib, 0, false, true)
	m.add("logos-builds", 10*mib, 0, true, false)
	m.add("itest-blob", 1, 0, true, false)
	m.add("", 4*mib, 0, true, false)
	opA, opB := uuid.New(), uuid.New()
	usage := fakeUsage{rows: []models.StorageUsage{
		{Purpose: models.StoragePurposeMinionFiles, Kind: "download", OperationID: opA, Objects: 3, Bytes: 90 * mib},
		{Purpose: models.StoragePurposeMinionFiles, Kind: "screenshot", OperationID: opB, Objects: 5, Bytes: 30 * mib},
		{Purpose: models.StoragePurposeRecordings, Kind: "desktop", OperationID: opA, Objects: 2, Bytes: 40 * mib},
		{Purpose: models.StoragePurposeWikiImages, Kind: "image", OperationID: models.PublicOperationID, Objects: 1, Bytes: 1000},
		{Purpose: models.StoragePurposeSkills, Kind: "version", Objects: 2, Bytes: 2000},
	}}
	s, _ := newService(t, m.srv.URL, usage, fakeOps{{OperationID: opA, Name: "Red"}, {OperationID: opB, Name: "Blue"}})
	snap := s.Refresh(context.Background())

	sw := snap.SeaweedFS
	if !sw.Configured || !sw.Reachable || sw.Version != "30GB 4.16 d4d2e511e" || sw.Leader != "seaweedfs-master:9333.19333" ||
		sw.Slots != 20 || sw.FreeSlots != 2 {
		t.Fatalf("SeaweedFS %+v", sw)
	}
	if len(snap.Disks) != 1 || snap.Disks[0].Dir != "/data" || snap.Disks[0].Free != 50*mib || snap.Disks[0].Volumes != 6 || snap.Disks[0].MaxVolumes != 20 {
		t.Fatalf("disks %+v", snap.Disks)
	}
	var order []string
	for _, b := range snap.Buckets {
		order = append(order, b.Name)
	}
	if got := strings.Join(order, ","); got != "minion-files,desktop-recordings,logos-builds,wiki-images,wiki-files,skills,itest-blob," {
		t.Fatalf("buckets in order %q", got)
	}
	mf := bucketOf(t, snap, "minion-files")
	if mf.Volumes != 2 || mf.Writable != 1 || mf.ReadOnly != 1 || mf.Size != 200*mib || mf.Deleted != 80*mib ||
		mf.Objects != 8 || mf.ObjectBytes != 120*mib || !slices.Equal(mf.Purposes, []string{models.StoragePurposeMinionFiles}) {
		t.Fatalf("minion-files %+v", mf)
	}
	if b := bucketOf(t, snap, "skills"); b.Written() || !b.Core() || b.Objects != 2 {
		t.Fatalf("skills, never written %+v", b)
	}
	if b := bucketOf(t, snap, "itest-blob"); b.Core() || !b.Written() {
		t.Fatalf("a bucket not core's %+v", b)
	}

	got := codes(snap.Warnings)
	want := []string{"warning:few_free_slots:", "warning:bucket_not_writable:desktop-recordings", "warning:disk_low:", "info:reclaimable:minion-files"}
	if !slices.Equal(got, want) {
		t.Fatalf("warnings %v, want %v", got, want)
	}

	page, err := snap.Consumers("minion-files", "", "", 10)
	if err != nil || page.Total != 2 || page.More || page.Consumers[0].OperationName != "Red" || page.Consumers[0].Kind != "download" ||
		page.Consumers[1].OperationName != "Blue" {
		t.Fatalf("minion-files' consumers %+v %v", page, err)
	}
	if page, _ := snap.Consumers("wiki-images", "", "", 10); len(page.Consumers) != 1 || page.Consumers[0].OperationName != models.PublicOperationName {
		t.Fatalf("the Public operation's %+v", page)
	}
	if page, _ := snap.Consumers("skills", "", "", 10); len(page.Consumers) != 1 || page.Consumers[0].OperationID != uuid.Nil {
		t.Fatalf("no operation's %+v", page)
	}
}

// Without a free slot, a bucket with no writable volume, or none yet, fails
// its writes: errors, the worst first.
func TestSnapshotNoFreeSlot(t *testing.T) {
	m := newFakeMaster(t)
	m.max, m.free = 3, 0
	m.add("minion-files", 10*mib, 0, true, false)
	m.add("desktop-recordings", 10*mib, 0, false, true)
	m.add("logos-builds", 10*mib, 0, true, false)
	s, _ := newService(t, m.srv.URL, fakeUsage{}, nil)
	got := codes(s.Refresh(context.Background()).Warnings)
	for _, w := range []string{"error:no_free_slots:", "error:bucket_not_writable:desktop-recordings", "error:bucket_not_writable:wiki-images",
		"error:bucket_not_writable:skills"} {
		if !slices.Contains(got, w) {
			t.Errorf("no %s in %v", w, got)
		}
	}
	if slices.Contains(got, "error:bucket_not_writable:minion-files") {
		t.Errorf("a bucket with a writable volume warned of: %v", got)
	}
}

// A master out of reach is an error, and the buckets are what core's records
// say; core not set to reach one shows those alone, with no warning of it.
func TestSnapshotWithoutSeaweedFS(t *testing.T) {
	usage := fakeUsage{rows: []models.StorageUsage{{Purpose: models.StoragePurposeBuilds, Kind: "artifact", OperationID: uuid.New(), Objects: 1, Bytes: 5}}}
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	s, _ := newService(t, down.URL, usage, nil)
	snap := s.Refresh(context.Background())
	if snap.SeaweedFS.Reachable || snap.SeaweedFS.Err == "" || len(snap.Warnings) != 1 || snap.Warnings[0].Code != "seaweedfs_unreachable" ||
		snap.Warnings[0].Level != LevelError {
		t.Fatalf("a master out of reach: %+v %+v", snap.SeaweedFS, snap.Warnings)
	}
	if b := bucketOf(t, snap, "logos-builds"); b.Objects != 1 || b.ObjectBytes != 5 || b.Written() {
		t.Fatalf("logos-builds by core's records %+v", b)
	}

	s, _ = newService(t, "", usage, nil)
	snap = s.Refresh(context.Background())
	if snap.SeaweedFS.Configured || len(snap.Warnings) != 0 || len(snap.Buckets) != len(coreBuckets) {
		t.Fatalf("no master set: %+v %+v %d buckets", snap.SeaweedFS, snap.Warnings, len(snap.Buckets))
	}
	if _, err := s.Reclaim(context.Background(), models.ModuleActor{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("a reclaim with no master: %v", err)
	}
	if _, err := s.Grow(context.Background(), models.ModuleActor{}, "logos-builds"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("a grow with no master: %v", err)
	}

	// Both: the error first, though core's records were read first.
	s, _ = newService(t, down.URL, fakeUsage{err: errors.New("mongo down")}, nil)
	if got := codes(s.Refresh(context.Background()).Warnings); !slices.Equal(got, []string{"error:seaweedfs_unreachable:", "warning:records_unreadable:"}) {
		t.Fatalf("the worst first: %v", got)
	}

	s, _ = newService(t, "", fakeUsage{err: errors.New("mongo down")}, nil)
	if snap := s.Refresh(context.Background()); snap.RecordsErr != "mongo down" || len(snap.Warnings) != 1 || snap.Warnings[0].Code != "records_unreadable" {
		t.Fatalf("records unreadable: %q %+v", snap.RecordsErr, snap.Warnings)
	}
}

// A volume server whose disks do not read is a warning, its disk a line
// saying why.
func TestSnapshotNodeUnreadable(t *testing.T) {
	m := newFakeMaster(t)
	m.nodeDown = true
	m.add("minion-files", mib, 0, true, false)
	s, _ := newService(t, m.srv.URL, fakeUsage{}, nil)
	snap := s.Refresh(context.Background())
	if len(snap.Disks) != 1 || !strings.Contains(snap.Disks[0].Err, "volume server starting") || !slices.Contains(codes(snap.Warnings), "warning:disks_unreadable:") {
		t.Fatalf("disks %+v, warnings %v", snap.Disks, codes(snap.Warnings))
	}
}

// A bucket's consumers come a page at a time, the largest first, each page
// after the one before; search keeps an operation's, or a kind.
func TestConsumersPages(t *testing.T) {
	var rows []models.StorageUsage
	names := fakeOps{}
	for i := range 5 {
		op := uuid.New()
		names = append(names, models.Operation{OperationID: op, Name: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"}[i]})
		rows = append(rows, models.StorageUsage{Purpose: models.StoragePurposeMinionFiles, Kind: []string{"download", "screenshot"}[i%2],
			OperationID: op, Objects: 1, Bytes: int64(100 - i*10)})
	}
	// Two of the same size: by name.
	rows[4].Bytes = rows[3].Bytes
	s, _ := newService(t, "", fakeUsage{rows: rows}, names)
	snap := s.Refresh(context.Background())

	var seen []string
	after := ""
	for range 3 {
		page, err := snap.Consumers("minion-files", "", after, 2)
		if err != nil || page.Total != 5 {
			t.Fatalf("page after %q: %+v %v", after, page, err)
		}
		for _, c := range page.Consumers {
			seen = append(seen, c.OperationName)
		}
		if !page.More {
			break
		}
		after = ConsumerCursor(page.Consumers[len(page.Consumers)-1])
	}
	if got := strings.Join(seen, ","); got != "Alpha,Bravo,Charlie,Delta,Echo" {
		t.Fatalf("pages %q", got)
	}
	if page, _ := snap.Consumers("minion-files", "brav", "", 10); page.Total != 1 || page.Consumers[0].OperationName != "Bravo" {
		t.Fatalf("by name %+v", page)
	}
	if page, _ := snap.Consumers("minion-files", "screenshot", "", 10); page.Total != 2 {
		t.Fatalf("by kind %+v", page)
	}
	if _, err := snap.Consumers("minion-files", "", "!!", 10); !errors.Is(err, ErrBadCursor) {
		t.Fatalf("a bad cursor: %v", err)
	}
	if page, _ := snap.Consumers("no-such", "", "", 10); page.Total != 0 || page.Consumers == nil {
		t.Fatalf("a bucket with none %+v", page)
	}
}

// A reclaim runs in the background, one at a time: it frees the deleted bytes
// of the volumes past its threshold, and says how much; a failure says
// SeaweedFS's reason.
func TestReclaim(t *testing.T) {
	m := newFakeMaster(t)
	m.add("minion-files", 100*mib, 40*mib, true, false)
	m.add("logos-builds", 100*mib, 5*mib, true, false)
	m.vacuumGate = make(chan struct{})
	s, actions := newService(t, m.srv.URL, fakeUsage{}, nil)
	ctx := context.Background()
	alice := models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "alice"}

	a, err := s.Reclaim(ctx, alice)
	if err != nil || a.Status != models.StorageActionRunning || a.Actor.Name != "alice" || a.Action != models.StorageActionReclaim {
		t.Fatalf("started %+v %v", a, err)
	}
	if _, err := s.Reclaim(ctx, alice); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second while one runs: %v", err)
	}
	close(m.vacuumGate)
	done := waitFor(t, func() bool { return actions.get(a.ActionID).Status != models.StorageActionRunning })
	if !done {
		t.Fatal("the reclaim never ended")
	}
	got := actions.get(a.ActionID)
	if got.Status != models.StorageActionSucceeded || got.Freed != 40*mib || got.FinishedAt == nil || m.thresholds[0] != "0.1" {
		t.Fatalf("ended %+v, thresholds %v", got, m.thresholds)
	}
	if b := bucketOf(t, s.Status(ctx), "minion-files"); b.Deleted != 0 || b.Size != 60*mib {
		t.Fatalf("the snapshot after it %+v", b)
	}

	m.mu.Lock()
	m.vacuumGate, m.vacuumErr = nil, "volume 3 is busy"
	m.mu.Unlock()
	a, _ = s.Reclaim(ctx, alice)
	waitFor(t, func() bool { return actions.get(a.ActionID).Status != models.StorageActionRunning })
	if got := actions.get(a.ActionID); got.Status != models.StorageActionFailed || got.Err != "SeaweedFS: volume 3 is busy" {
		t.Fatalf("failed %+v", got)
	}
}

// Adding a volume: to one of core's buckets or one SeaweedFS has, never
// SeaweedFS's own; with no free slot it fails, saying SeaweedFS's reason.
func TestGrow(t *testing.T) {
	m := newFakeMaster(t)
	m.add("desktop-recordings", 10*mib, 0, false, true)
	m.add("itest-blob", 1, 0, true, false)
	// SeaweedFS's own files: a bucket SeaweedFS has, never grown.
	m.add("", 1, 0, true, false)
	m.free = 1
	s, _ := newService(t, m.srv.URL, fakeUsage{}, nil)
	ctx := context.Background()
	alice := models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "alice"}

	a, err := s.Grow(ctx, alice, "desktop-recordings")
	if err != nil || a.Status != models.StorageActionSucceeded || a.Volumes != 1 || a.Bucket != "desktop-recordings" {
		t.Fatalf("grown %+v %v", a, err)
	}
	if b := bucketOf(t, s.Status(ctx), "desktop-recordings"); b.Writable != 1 || b.Volumes != 2 {
		t.Fatalf("the snapshot after it %+v", b)
	}
	a, err = s.Grow(ctx, alice, "skills")
	if err != nil || a.Status != models.StorageActionFailed || a.Err != "SeaweedFS: only 0 volumes left, not enough for 1" {
		t.Fatalf("no free slot: %+v %v", a, err)
	}
	for _, bucket := range []string{"", "no-such"} {
		if _, err := s.Grow(ctx, alice, bucket); !errors.Is(err, ErrUnknownBucket) {
			t.Fatalf("grow %q: %v", bucket, err)
		}
	}
}

// Start fails the actions a core that stopped left running, and reads the
// store; Stop waits for a reclaim, which then ends failed.
func TestStartAndStop(t *testing.T) {
	m := newFakeMaster(t)
	m.add("minion-files", 10*mib, 5*mib, true, false)
	s, actions := newService(t, m.srv.URL, fakeUsage{}, nil)
	left := models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionReclaim, Status: models.StorageActionRunning}
	_ = actions.Insert(context.Background(), &left)
	s.Start()
	if actions.get(left.ActionID).Status != models.StorageActionFailed || actions.endedBy != "core stopped before it ended" {
		t.Fatalf("left running %+v", actions.get(left.ActionID))
	}
	if !waitFor(t, func() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.snap != nil }) {
		t.Fatal("no read at start")
	}
	m.vacuumGate = make(chan struct{})
	a, err := s.Reclaim(context.Background(), models.ModuleActor{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.vacuums == 1 })
	go func() { time.Sleep(50 * time.Millisecond); close(m.vacuumGate) }()
	s.Stop()
	if got := actions.get(a.ActionID); got.Status != models.StorageActionFailed || got.Err != "core stopped before it ended" {
		t.Fatalf("a reclaim at Stop %+v", got)
	}
}

func waitFor(t *testing.T, ok func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
