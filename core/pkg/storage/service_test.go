package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

const mib = int64(1 << 20)

// fakeMaster is a SeaweedFS master and its one volume server: over HTTP,
// volumes by collection, each bucket's writable ones, the slots, the disk,
// the volumes as the volume server has them; grow adds a volume while a slot
// is free. Over gRPC (master_pb.Seaweed, its requests as core encodes them),
// VacuumVolume compacts one volume deleted files hold the threshold of
// (its revision moves), and DisableVacuum and EnableVacuum pause and resume
// SeaweedFS's own vacuum.
type fakeMaster struct {
	t        *testing.T
	srv      *httptest.Server
	grpcAddr string

	mu        sync.Mutex
	vols      []volume
	writables map[string][]uint32
	max, free int
	disk      [3]int64 // all, used, free
	nodeDown  bool
	// busy is how many VacuumVolume calls find another vacuum holding the
	// master's lock (they do nothing); stuck the volumes the volume server
	// never compacts; gate, when set, holds each VacuumVolume until it
	// takes a value or is closed, started saying each one that came.
	busy    int
	stuck   map[uint32]bool
	gate    chan struct{}
	started chan uint32
	// calls are the VacuumVolume calls, as decoded; paused is SeaweedFS's
	// own vacuum; pauses and resumes count DisableVacuum and EnableVacuum;
	// enableFails fails that many EnableVacuum calls.
	calls          []vacuumCall
	paused         bool
	pauses         int
	resumes        int
	enableFails    int
	unknownMethods []string
}

// vacuumCall is a VacuumVolume request as the fake decoded it.
type vacuumCall struct {
	threshold  float32
	id         uint32
	collection string
}

func newFakeMaster(t *testing.T) *fakeMaster {
	m := &fakeMaster{t: t, writables: map[string][]uint32{}, max: 20, free: 10, disk: [3]int64{1000 * mib, 600 * mib, 400 * mib},
		stuck: map[uint32]bool{}, started: make(chan uint32, 64)}
	mux := http.NewServeMux()
	mux.HandleFunc("/dir/status", m.dirStatus)
	mux.HandleFunc("/vol/status", m.volStatus)
	mux.HandleFunc("/cluster/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"IsLeader": true, "Leader": "seaweedfs-master:9333.19333"})
	})
	mux.HandleFunc("/status", m.nodeStatus)
	mux.HandleFunc("/vol/grow", m.grow)
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer(grpc.ForceServerCodecV2(rawCodec{}), grpc.UnknownServiceHandler(m.rpc))
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)
	m.grpcAddr = lis.Addr().String()
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

// vol is a volume as the fake has it now.
func (m *fakeMaster) vol(id uint32) volume {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.vols {
		if v.ID == id {
			return v
		}
	}
	return volume{}
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
	writeJSON(w, http.StatusOK, map[string]any{"DiskStatuses": []any{map[string]any{"dir": "/data", "all": m.disk[0], "used": m.disk[1],
		"free": m.disk[2]}}, "Volumes": m.vols})
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

// rpc is the master's gRPC side.
func (m *fakeMaster) rpc(_ any, stream grpc.ServerStream) error {
	method, _ := grpc.MethodFromServerStream(stream)
	var req []byte
	if err := stream.RecvMsg(&req); err != nil {
		return err
	}
	switch method {
	case "/master_pb.Seaweed/DisableVacuum":
		m.mu.Lock()
		m.paused, m.pauses = true, m.pauses+1
		m.mu.Unlock()
	case "/master_pb.Seaweed/EnableVacuum":
		m.mu.Lock()
		if m.enableFails > 0 {
			m.enableFails--
			m.mu.Unlock()
			return status.Error(codes.Unavailable, "raft: leader changing")
		}
		m.paused, m.resumes = false, m.resumes+1
		m.mu.Unlock()
	case "/master_pb.Seaweed/VacuumVolume":
		call, err := decodeVacuumVolume(req)
		if err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		if err := m.vacuumVolume(stream.Context(), call); err != nil {
			return err
		}
	default:
		m.mu.Lock()
		m.unknownMethods = append(m.unknownMethods, method)
		m.mu.Unlock()
		return status.Errorf(codes.Unimplemented, "unknown method %s", method)
	}
	return stream.SendMsg([]byte{})
}

// vacuumVolume compacts the volume called for, as SeaweedFS's master would:
// nothing while another vacuum holds its lock (busy), nor for a read-only
// volume, one the volume server never compacts (stuck), or one under the
// threshold.
func (m *fakeMaster) vacuumVolume(ctx context.Context, call vacuumCall) error {
	m.mu.Lock()
	m.calls = append(m.calls, call)
	gate := m.gate
	m.mu.Unlock()
	m.started <- call.id
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy > 0 {
		m.busy--
		return nil
	}
	for i := range m.vols {
		v := &m.vols[i]
		if v.ID != call.id || v.ReadOnly || m.stuck[v.ID] {
			continue
		}
		if v.Size > 0 && float32(v.DeletedByteCount) >= call.threshold*float32(v.Size) {
			v.Size, v.DeletedByteCount, v.CompactRevision = v.Size-v.DeletedByteCount, 0, v.CompactRevision+1
		}
	}
	return nil
}

// decodeVacuumVolume reads a VacuumVolumeRequest (master.proto: 1 the
// threshold, a float; 2 the volume id; 3 its collection).
func decodeVacuumVolume(b []byte) (vacuumCall, error) {
	var c vacuumCall
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return c, protowire.ParseError(n)
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.Fixed32Type:
			v, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				return c, protowire.ParseError(n)
			}
			c.threshold, b = math.Float32frombits(v), b[n:]
		case num == 2 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return c, protowire.ParseError(n)
			}
			c.id, b = uint32(v), b[n:]
		case num == 3 && typ == protowire.BytesType:
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return c, protowire.ParseError(n)
			}
			c.collection, b = v, b[n:]
		default:
			return c, fmt.Errorf("field %d of type %d is not VacuumVolumeRequest's", num, typ)
		}
	}
	return c, nil
}

// vacuumCalls are the VacuumVolume calls so far.
func (m *fakeMaster) vacuumCalls() []vacuumCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
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

func (f *fakeActions) Get(_ context.Context, id uuid.UUID) (*models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.list {
		if a.ActionID == id {
			out := a
			out.ReclaimVolumes = slices.Clone(a.ReclaimVolumes)
			return &out, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeActions) RequestCancel(_ context.Context, id uuid.UUID, by models.ModuleActor, at time.Time) (*models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		a := &f.list[i]
		if a.ActionID != id {
			continue
		}
		switch {
		case a.Action != models.StorageActionReclaim && a.Action != models.StorageActionCleanup:
			return nil, repository.ErrStorageActionNotCancellable
		case a.Status != models.StorageActionRunning:
			return nil, repository.ErrStorageActionNotRunning
		case a.CancelRequestedAt == nil:
			a.CancelRequestedAt, a.CancelledBy = &at, &by
		}
		out := *a
		return &out, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeActions) SetVacuumPaused(_ context.Context, id uuid.UUID, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ActionID == id {
			f.list[i].VacuumPaused = paused
			return nil
		}
	}
	return repository.ErrNotFound
}

func (f *fakeActions) VacuumPaused(context.Context) ([]models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.StorageAction
	for _, a := range f.list {
		if a.VacuumPaused {
			out = append(out, a)
		}
	}
	return out, nil
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
			a.Status, a.Err, a.Freed, a.Volumes, a.FinishedAt, a.Step, a.StepAt = status, why, freed, volumes, &at, "", nil
			out := *a
			return &out, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeActions) Progress(_ context.Context, id uuid.UUID, p models.StorageActionProgress) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if a := &f.list[i]; a.ActionID == id && a.Status == models.StorageActionRunning {
			a.Step, a.StepAt, a.Freed, a.Recordings, a.Bytes = p.Step, p.StepAt, p.Freed, p.Recordings, p.Bytes
			if p.ReclaimVolumes != nil {
				a.ReclaimVolumes = slices.Clone(p.ReclaimVolumes)
			}
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

func (f *fakeActions) EndRunning(_ context.Context, why string, at time.Time) ([]models.StorageAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endedBy = why
	var ended []models.StorageAction
	for i := range f.list {
		if f.list[i].Status == models.StorageActionRunning {
			ended = append(ended, f.list[i])
			f.list[i].Status, f.list[i].Err, f.list[i].FinishedAt = models.StorageActionFailed, why, &at
		}
	}
	return ended, nil
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
			a.ReclaimVolumes = slices.Clone(a.ReclaimVolumes)
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

// newService is the service of a fake master (nil: none), its gRPC side
// with it.
func newService(t *testing.T, m *fakeMaster, usage fakeUsage, ops fakeOps) (*Service, *fakeActions) {
	t.Helper()
	if m == nil {
		return newServiceAt(t, "", "", usage, ops)
	}
	return newServiceAt(t, m.srv.URL, m.grpcAddr, usage, ops)
}

// newServiceAt is the service of the master at a URL, and a gRPC address.
func newServiceAt(t *testing.T, master, grpcAddr string, usage fakeUsage, ops fakeOps) (*Service, *fakeActions) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.MasterURL, cfg.MasterGRPC, cfg.Buckets = master, grpcAddr, coreBuckets
	cfg.ReclaimRetryWait, cfg.ReclaimFirstTries = 10*time.Millisecond, 5
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

func warningCodes(ws []Warning) []string {
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
	s, _ := newService(t, m, usage, fakeOps{{OperationID: opA, Name: "Red"}, {OperationID: opB, Name: "Blue"}})
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

	got := warningCodes(snap.Warnings)
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
	s, _ := newService(t, m, fakeUsage{}, nil)
	got := warningCodes(s.Refresh(context.Background()).Warnings)
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
	s, _ := newServiceAt(t, down.URL, "", usage, nil)
	snap := s.Refresh(context.Background())
	if snap.SeaweedFS.Reachable || snap.SeaweedFS.Err == "" || len(snap.Warnings) != 1 || snap.Warnings[0].Code != "seaweedfs_unreachable" ||
		snap.Warnings[0].Level != LevelError {
		t.Fatalf("a master out of reach: %+v %+v", snap.SeaweedFS, snap.Warnings)
	}
	if b := bucketOf(t, snap, "logos-builds"); b.Objects != 1 || b.ObjectBytes != 5 || b.Written() {
		t.Fatalf("logos-builds by core's records %+v", b)
	}

	s, _ = newService(t, nil, usage, nil)
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
	s, _ = newServiceAt(t, down.URL, "", fakeUsage{err: errors.New("mongo down")}, nil)
	if got := warningCodes(s.Refresh(context.Background()).Warnings); !slices.Equal(got, []string{"error:seaweedfs_unreachable:", "warning:records_unreadable:"}) {
		t.Fatalf("the worst first: %v", got)
	}

	s, _ = newService(t, nil, fakeUsage{err: errors.New("mongo down")}, nil)
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
	s, _ := newService(t, m, fakeUsage{}, nil)
	snap := s.Refresh(context.Background())
	if len(snap.Disks) != 1 || !strings.Contains(snap.Disks[0].Err, "volume server starting") || !slices.Contains(warningCodes(snap.Warnings), "warning:disks_unreadable:") {
		t.Fatalf("disks %+v, warnings %v", snap.Disks, warningCodes(snap.Warnings))
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
	s, _ := newService(t, nil, fakeUsage{rows: rows}, names)
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

// Adding a volume: to one of core's buckets or one SeaweedFS has, never
// SeaweedFS's own; with no free slot it fails, saying SeaweedFS's reason.
func TestGrow(t *testing.T) {
	m := newFakeMaster(t)
	m.add("desktop-recordings", 10*mib, 0, false, true)
	m.add("itest-blob", 1, 0, true, false)
	// SeaweedFS's own files: a bucket SeaweedFS has, never grown.
	m.add("", 1, 0, true, false)
	m.free = 1
	s, _ := newService(t, m, fakeUsage{}, nil)
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

// counts are SeaweedFS's own vacuum as the fake has it: paused or not, and
// how many times it was paused and resumed.
func (m *fakeMaster) counts() (bool, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused, m.pauses, m.resumes
}

// waitStarted waits for a VacuumVolume call to come: its volume.
func waitStarted(t *testing.T, m *fakeMaster) uint32 {
	t.Helper()
	select {
	case id := <-m.started:
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("no VacuumVolume call came")
		return 0
	}
}

// ended waits for an action to end, as it is then.
func endedAction(t *testing.T, actions *fakeActions, id uuid.UUID) models.StorageAction {
	t.Helper()
	if !waitFor(t, func() bool { return actions.get(id).Status != models.StorageActionRunning }) {
		t.Fatalf("the action %s never ended: %+v", id, actions.get(id))
	}
	return actions.get(id)
}

func volumeIDs(vols []models.StorageReclaimVolume) []uint32 {
	out := []uint32{}
	for _, v := range vols {
		out = append(out, v.ID)
	}
	return out
}

func calledIDs(calls []vacuumCall) []uint32 {
	out := []uint32{}
	for _, c := range calls {
		out = append(out, c.id)
	}
	return out
}

// A reclaim compacts, one at a time and the most deleted first, every
// volume deleted files hold a tenth of or more, none read-only, SeaweedFS's
// own vacuum paused meanwhile: each kept as it went, compacted with the
// bytes it freed, the step saying where it is. It ends succeeded with all it
// freed, SeaweedFS's own vacuum resumed. One runs at a time; one with
// nothing to compact ends at once, pausing nothing.
func TestReclaim(t *testing.T) {
	m := newFakeMaster(t)
	m.add("minion-files", 100*mib, 40*mib, true, false)
	m.add("logos-builds", 100*mib, 5*mib, true, false)
	m.add("desktop-recordings", 50*mib, 30*mib, true, false)
	m.add("wiki-files", 10*mib, 9*mib, false, true)
	m.add("", 20*mib, 2*mib, true, false)
	m.gate = make(chan struct{})
	s, actions := newService(t, m, fakeUsage{}, nil)
	ctx := context.Background()
	alice := models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "alice"}

	a, err := s.Reclaim(ctx, alice)
	if err != nil || a.Status != models.StorageActionRunning || a.Actor.Name != "alice" || a.Action != models.StorageActionReclaim ||
		a.Step != "reading the volumes" {
		t.Fatalf("started %+v %v", a, err)
	}
	if _, err := s.Reclaim(ctx, alice); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second while one runs: %v", err)
	}
	if id := waitStarted(t, m); id != 1 {
		t.Fatalf("compacted %d first, not the most deleted", id)
	}
	got := actions.get(a.ActionID)
	if paused, _, _ := m.counts(); !paused || !got.VacuumPaused || got.Step != "compacting volume 1 of minion-files (1 of 3)" ||
		got.StepAt == nil || !slices.Equal(volumeIDs(got.ReclaimVolumes), []uint32{1, 3, 5}) ||
		got.ReclaimVolumes[0].State != models.ReclaimVolumeCompacting || got.ReclaimVolumes[1].State != models.ReclaimVolumeWaiting {
		t.Fatalf("as volume 1 compacts %+v", got)
	}
	if v := got.ReclaimVolumes[0]; v.Bucket != "minion-files" || v.Server != m.node() || v.Size != 100*mib || v.Deleted != 40*mib ||
		v.StartedAt == nil {
		t.Fatalf("volume 1 as found %+v", v)
	}
	close(m.gate)
	got = endedAction(t, actions, a.ActionID)
	if got.Status != models.StorageActionSucceeded || got.Err != "" || got.Freed != 72*mib || got.FinishedAt == nil || got.Step != "" ||
		got.VacuumPaused {
		t.Fatalf("ended %+v", got)
	}
	for _, v := range got.ReclaimVolumes {
		if v.State != models.ReclaimVolumeCompacted || v.Freed != v.Deleted || v.FinishedAt == nil || v.Msg != "" {
			t.Fatalf("volume %+v", v)
		}
	}
	calls := m.vacuumCalls()
	if len(calls) != 3 || calls[0] != (vacuumCall{threshold: 0.1, id: 1, collection: "minion-files"}) ||
		calls[1].collection != "desktop-recordings" || calls[2] != (vacuumCall{threshold: 0.1, id: 5}) {
		t.Fatalf("asked %+v", calls)
	}
	if paused, pauses, resumes := m.counts(); paused || pauses != 1 || resumes != 1 {
		t.Fatalf("SeaweedFS's own vacuum: paused %v, %d pauses, %d resumes", paused, pauses, resumes)
	}
	if v := m.vol(1); v.DeletedByteCount != 0 || v.CompactRevision != 1 || v.Size != 60*mib {
		t.Fatalf("volume 1 after %+v", v)
	}
	if b := bucketOf(t, s.Status(ctx), "minion-files"); b.Deleted != 0 || b.Size != 60*mib {
		t.Fatalf("the snapshot after it %+v", b)
	}

	m.mu.Lock()
	m.gate = nil
	m.mu.Unlock()
	a, _ = s.Reclaim(ctx, alice)
	got = endedAction(t, actions, a.ActionID)
	if _, pauses, _ := m.counts(); got.Status != models.StorageActionSucceeded || len(got.ReclaimVolumes) != 0 || pauses != 1 {
		t.Fatalf("nothing to compact: %+v, %d pauses", got, pauses)
	}
}

// The master busy with another vacuum: the volume is asked again a while
// later, and compacted. One the master never compacts fails, asked
// ReclaimTries times, saying why; the others go on, and the action fails,
// saying how many were not, SeaweedFS's own vacuum resumed all the same. A
// volume server that does not answer fails its volumes, saying so; a master
// whose gRPC side does not answer fails the reclaim before any volume.
func TestReclaimRetriesAndFailures(t *testing.T) {
	m := newFakeMaster(t)
	m.add("minion-files", 100*mib, 40*mib, true, false)
	m.add("logos-builds", 100*mib, 30*mib, true, false)
	m.busy, m.stuck[2] = 1, true
	s, actions := newService(t, m, fakeUsage{}, nil)
	ctx := context.Background()
	alice := models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "alice"}

	a, _ := s.Reclaim(ctx, alice)
	got := endedAction(t, actions, a.ActionID)
	if got.Status != models.StorageActionFailed || got.Err != "1 of 2 volumes were not compacted: each says why" || got.Freed != 40*mib {
		t.Fatalf("ended %+v", got)
	}
	if v := got.ReclaimVolumes[0]; v.ID != 1 || v.State != models.ReclaimVolumeCompacted || v.Freed != 40*mib {
		t.Fatalf("the volume asked again %+v", v)
	}
	if v := got.ReclaimVolumes[1]; v.ID != 2 || v.State != models.ReclaimVolumeFailed || !strings.Contains(v.Msg, "asked 3 times") {
		t.Fatalf("the volume never compacted %+v", v)
	}
	if ids := calledIDs(m.vacuumCalls()); !slices.Equal(ids, []uint32{1, 1, 2, 2, 2}) {
		t.Fatalf("asked %v", ids)
	}
	if paused, _, resumes := m.counts(); paused || resumes != 1 {
		t.Fatalf("SeaweedFS's own vacuum: paused %v, %d resumes", paused, resumes)
	}

	m.mu.Lock()
	m.stuck, m.nodeDown = map[uint32]bool{}, true
	m.mu.Unlock()
	a, _ = s.Reclaim(ctx, alice)
	got = endedAction(t, actions, a.ActionID)
	if v := got.ReclaimVolumes[0]; got.Status != models.StorageActionFailed || v.ID != 2 || v.State != models.ReclaimVolumeFailed ||
		!strings.Contains(v.Msg, "its volume server did not answer") {
		t.Fatalf("its volume server down %+v", got)
	}

	m.mu.Lock()
	m.nodeDown = false
	m.mu.Unlock()
	noGRPC, noGRPCActions := newServiceAt(t, m.srv.URL, "127.0.0.1:1", fakeUsage{}, nil)
	a, _ = noGRPC.Reclaim(ctx, alice)
	got = endedAction(t, noGRPCActions, a.ActionID)
	if got.Status != models.StorageActionFailed || !strings.Contains(got.Err, "does not answer over gRPC at 127.0.0.1:1") ||
		got.ReclaimVolumes[0].State != models.ReclaimVolumeStopped || got.VacuumPaused {
		t.Fatalf("no gRPC %+v", got)
	}
}

// Cancel stops a reclaim before its next volume: the one under way ends as
// SeaweedFS ends it, the rest are stopped, and it ends cancelled, by the
// first who asked, SeaweedFS's own vacuum resumed. A cancel through another
// core (its record alone) stops it too. Cancelling one that ended, a grow,
// or none is refused.
func TestReclaimCancel(t *testing.T) {
	m := newFakeMaster(t)
	m.add("minion-files", 100*mib, 40*mib, true, false)
	m.add("logos-builds", 100*mib, 30*mib, true, false)
	m.add("desktop-recordings", 100*mib, 20*mib, true, false)
	m.gate = make(chan struct{})
	s, actions := newService(t, m, fakeUsage{}, nil)
	ctx := context.Background()
	alice := models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "alice"}
	bob := models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "bob"}

	a, _ := s.Reclaim(ctx, alice)
	waitStarted(t, m)
	asked, err := s.Cancel(ctx, bob, a.ActionID)
	if err != nil || asked.Status != models.StorageActionRunning || asked.CancelRequestedAt == nil || asked.CancelledBy.Name != "bob" {
		t.Fatalf("asked %+v %v", asked, err)
	}
	if again, err := s.Cancel(ctx, alice, a.ActionID); err != nil || again.CancelledBy.Name != "bob" {
		t.Fatalf("asked again %+v %v", again, err)
	}
	m.gate <- struct{}{}
	got := endedAction(t, actions, a.ActionID)
	if got.Status != models.StorageActionCancelled || got.Err != "" || got.Freed != 40*mib || got.CancelledBy.Name != "bob" {
		t.Fatalf("ended %+v", got)
	}
	if v := got.ReclaimVolumes; v[0].State != models.ReclaimVolumeCompacted || v[1].State != models.ReclaimVolumeStopped ||
		v[2].State != models.ReclaimVolumeStopped || v[1].Msg != "the reclaim stopped before it" {
		t.Fatalf("its volumes %+v", v)
	}
	if ids := calledIDs(m.vacuumCalls()); !slices.Equal(ids, []uint32{1}) {
		t.Fatalf("asked %v", ids)
	}
	if paused, _, resumes := m.counts(); paused || resumes != 1 {
		t.Fatalf("SeaweedFS's own vacuum: paused %v, %d resumes", paused, resumes)
	}

	a, _ = s.Reclaim(ctx, alice)
	waitStarted(t, m)
	if _, err := actions.RequestCancel(ctx, a.ActionID, bob, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.gate <- struct{}{}
	if got := endedAction(t, actions, a.ActionID); got.Status != models.StorageActionCancelled || got.ReclaimVolumes[1].State != models.ReclaimVolumeStopped {
		t.Fatalf("a cancel through another core %+v", got)
	}

	if _, err := s.Cancel(ctx, bob, a.ActionID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("one that ended: %v", err)
	}
	grown, _ := s.Grow(ctx, alice, "minion-files")
	if _, err := s.Cancel(ctx, bob, grown.ActionID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("a grow: %v", err)
	}
	if _, err := s.Cancel(ctx, bob, uuid.New()); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("none: %v", err)
	}
	if one, err := s.Action(ctx, a.ActionID); err != nil || one.Status != models.StorageActionCancelled {
		t.Fatalf("read %+v %v", one, err)
	}
	if _, err := s.Action(ctx, uuid.New()); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("read none: %v", err)
	}
}

// Start fails the actions a core that stopped left running, resumes
// SeaweedFS's own vacuum a reclaim left paused, and reads the store. Stop
// ends a reclaim under way: the volume SeaweedFS compacts is left to it,
// the rest stopped, the action failed, SeaweedFS's own vacuum resumed.
func TestStartAndStop(t *testing.T) {
	m := newFakeMaster(t)
	m.add("minion-files", 10*mib, 5*mib, true, false)
	m.add("logos-builds", 10*mib, 4*mib, true, false)
	s, actions := newService(t, m, fakeUsage{}, nil)
	left := models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionReclaim, Status: models.StorageActionRunning,
		VacuumPaused: true}
	_ = actions.Insert(context.Background(), &left)
	m.mu.Lock()
	m.paused = true
	m.mu.Unlock()
	s.Start()
	if actions.get(left.ActionID).Status != models.StorageActionFailed || actions.endedBy != coreStopped {
		t.Fatalf("left running %+v", actions.get(left.ActionID))
	}
	if !waitFor(t, func() bool { paused, _, resumes := m.counts(); return !paused && resumes == 1 }) ||
		!waitFor(t, func() bool { return !actions.get(left.ActionID).VacuumPaused }) {
		t.Fatal("SeaweedFS's own vacuum stays paused")
	}
	if !waitFor(t, func() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.snap != nil }) {
		t.Fatal("no read at start")
	}
	m.mu.Lock()
	m.gate = make(chan struct{})
	m.mu.Unlock()
	a, err := s.Reclaim(context.Background(), models.ModuleActor{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, m)
	s.Stop()
	got := actions.get(a.ActionID)
	if got.Status != models.StorageActionFailed || got.Err != coreStopped || got.VacuumPaused {
		t.Fatalf("a reclaim at Stop %+v", got)
	}
	if v := got.ReclaimVolumes; v[0].State != models.ReclaimVolumeStopped || !strings.Contains(v[0].Msg, "SeaweedFS ends it on its own") ||
		v[1].State != models.ReclaimVolumeStopped {
		t.Fatalf("its volumes %+v", v)
	}
	if paused, _, resumes := m.counts(); paused || resumes != 2 {
		t.Fatalf("SeaweedFS's own vacuum at Stop: paused %v, %d resumes", paused, resumes)
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

// The master's gRPC address: the one set, else the master URL's host on its
// port plus 10000 (9333 when it names none); a URL with no host is refused.
func TestMasterGRPCAddr(t *testing.T) {
	for _, c := range []struct{ url, set, want string }{
		{"http://seaweedfs-master:9333", "", "seaweedfs-master:19333"},
		{"http://seaweedfs-master.logos.svc.cluster.local:9334", "", "seaweedfs-master.logos.svc.cluster.local:19334"},
		{"http://10.0.0.5", "", "10.0.0.5:19333"},
		{"http://[fd00::5]:9333", "", "[fd00::5]:19333"},
		{"http://seaweedfs-master:9333", "grpc.example:7777", "grpc.example:7777"},
	} {
		if got, err := masterGRPCAddr(c.url, c.set); err != nil || got != c.want {
			t.Errorf("%s (%q): %q %v, want %q", c.url, c.set, got, err, c.want)
		}
	}
	for _, bad := range []string{"seaweedfs-master:9333", "http://:9333", "http://host:port"} {
		if got, err := masterGRPCAddr(bad, ""); err == nil {
			t.Errorf("%s: %q, want an error", bad, got)
		}
	}
}
