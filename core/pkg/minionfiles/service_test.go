package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// memStore is a bucket in memory.
type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memStore) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = b
	return err
}

func (m *memStore) PutStream(ctx context.Context, key string, body io.Reader, ct string) (int64, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	return int64(len(b)), m.Put(ctx, key, bytes.NewReader(b), int64(len(b)), ct)
}

func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, blob.ObjectInfo{}, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(b)), blob.ObjectInfo{ContentLength: int64(len(b))}, nil
}

func (m *memStore) Head(ctx context.Context, key string) (blob.ObjectInfo, error) {
	_, info, err := m.Get(ctx, key)
	return info, err
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memStore) keys(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n
}

// memFiles is the file repository in memory.
type memFiles struct {
	repository.IMinionFileRepository
	mu    sync.Mutex
	files map[uuid.UUID]*models.MinionFile
}

func (m *memFiles) Insert(_ context.Context, f *models.MinionFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f.CreateAt.IsZero() {
		f.CreateAt = time.Now().UTC()
	}
	c := *f
	m.files[f.FileID] = &c
	return nil
}

func (m *memFiles) find(match func(*models.MinionFile) bool) (*models.MinionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		if match(f) {
			c := *f
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *memFiles) FindByID(_ context.Context, id uuid.UUID) (*models.MinionFile, error) {
	return m.find(func(f *models.MinionFile) bool { return f.FileID == id })
}

func (m *memFiles) FindByXfer(_ context.Context, s uuid.UUID, x []byte) (*models.MinionFile, error) {
	return m.find(func(f *models.MinionFile) bool { return f.SessionID == s && bytes.Equal(f.Xfer, x) })
}

func (m *memFiles) FindByTask(_ context.Context, t uuid.UUID) (*models.MinionFile, error) {
	return m.find(func(f *models.MinionFile) bool { return f.TaskID == t })
}

func (m *memFiles) Advance(_ context.Context, id uuid.UUID, off, n int64, at time.Time) (*models.MinionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[id]
	if f == nil || f.State != models.MinionFileReceiving || f.Received != off {
		return nil, repository.ErrNotFound
	}
	f.Received += n
	f.LastChunkAt = at
	c := *f
	return &c, nil
}

func (m *memFiles) Move(_ context.Context, id uuid.UUID, from []string, state string, set bson.M) (*models.MinionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[id]
	ok := false
	for _, s := range from {
		ok = ok || (f != nil && f.State == s)
	}
	if !ok {
		return nil, repository.ErrNotFound
	}
	f.State = state
	if v, ok := set["sha256"].([]byte); ok {
		f.SHA256 = v
	}
	if v, ok := set["err"].(string); ok {
		f.Err = v
	}
	if v, ok := set["content_type"].(string); ok {
		f.ContentType = v
	}
	c := *f
	return &c, nil
}

func (m *memFiles) Idle(_ context.Context, before time.Time) ([]models.MinionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.MinionFile
	for _, f := range m.files {
		if f.State == models.MinionFileReceiving && f.LastChunkAt.Before(before) {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (m *memFiles) TaskFiles(_ context.Context, before time.Time) ([]models.MinionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.MinionFile
	for _, f := range m.files {
		if f.Kind == models.MinionFileKindTask && f.CreateAt.Before(before) {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (m *memFiles) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, id)
	return nil
}

type tasks map[uuid.UUID]*models.MinionTask

func (t tasks) UploadFileUse(_ context.Context, fileID uuid.UUID) (bool, bool, error) {
	open, named := false, false
	for _, x := range t {
		if x.Upload != nil && x.Upload.FileID == fileID {
			named = true
			open = open || !x.Finished()
		}
	}
	return open, named, nil
}

func (t tasks) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	if x, ok := t[id]; ok {
		return x, nil
	}
	return nil, repository.ErrNotFound
}

func setup(t *testing.T) (*Service, *memStore, *memFiles, *models.MinionSession, uuid.UUID) {
	t.Helper()
	store := &memStore{objects: map[string][]byte{}}
	files := &memFiles{files: map[uuid.UUID]*models.MinionFile{}}
	session := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	taskID := uuid.New()
	ts := tasks{taskID: {TaskID: taskID, SessionID: session.SessionID, OperationID: session.OperationID, Result: protocol.ResultFileDownload}}
	s := NewService(files, ts, store, nil, Config{MaxSize: 100, IdleAfter: time.Hour, SweepEvery: time.Hour}, zap.NewNop())
	return s, store, files, session, taskID
}

func waitState(t *testing.T, files *memFiles, id uuid.UUID, state string) *models.MinionFile {
	t.Helper()
	for i := 0; i < 200; i++ {
		f, _ := files.FindByID(context.Background(), id)
		if f != nil && f.State == state {
			return f
		}
		time.Sleep(5 * time.Millisecond)
	}
	f, _ := files.FindByID(context.Background(), id)
	t.Fatalf("file is %+v, want %s", f, state)
	return nil
}

func TestTransfer(t *testing.T) {
	s, store, files, session, taskID := setup(t)
	ctx := context.Background()
	body := []byte("hello, world: a file in three chunks")
	sum := sha256.Sum256(body)
	xfer := bytes.Repeat([]byte{7}, 16)
	total := uint64(len(body))

	first := protocol.ChunkItem{Xfer: xfer, Off: 0, Data: body[:10], Total: &total, Task: taskID[:], Name: "/etc/hello"}
	if r, err := s.TakeChunk(ctx, session, first); err != nil || r.Code != 0 || r.Next != 10 {
		t.Fatalf("first: %+v %v", r, err)
	}
	// Sent again: not kept twice; where to go on from.
	if r, _ := s.TakeChunk(ctx, session, first); r.Next != 10 {
		t.Fatalf("again: %+v", r)
	}
	// Out of order: not kept.
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 20, Data: body[20:]}); r.Next != 10 {
		t.Fatalf("ahead: %+v", r)
	}
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 10, Data: body[10:20]}); r.Next != 20 {
		t.Fatalf("second: %+v", r)
	}
	// The last says the SHA-256.
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 20, Data: body[20:]}); r.Code != protocol.ErrItemBadMessage {
		t.Fatalf("last without sha256: %+v", r)
	}
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 20, Data: body[20:], SHA256: sum[:]}); r.Next != int64(total) {
		t.Fatalf("last: %+v", r)
	}
	f, _ := files.FindByXfer(ctx, session.SessionID, xfer)
	f = waitState(t, files, f.FileID, models.MinionFileComplete)
	rc, _, err := s.Open(ctx, f.FileID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, body) || store.keys("staging/") != 0 {
		t.Fatalf("kept %q, %d chunks left", got, store.keys("staging/"))
	}
	if f.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("its type: %q", f.ContentType)
	}
}

func TestTransferRefused(t *testing.T) {
	s, store, files, session, taskID := setup(t)
	ctx := context.Background()
	big, small := uint64(101), uint64(4)
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: make([]byte, 16), Total: &big, Task: taskID[:]}); r.Code != protocol.ErrItemTooLarge {
		t.Fatalf("over the max: %+v", r)
	}
	other := uuid.New()
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: make([]byte, 16), Total: &small, Task: other[:]}); r.Code != protocol.ErrItemUnknownTask {
		t.Fatalf("another task: %+v", r)
	}
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: make([]byte, 16), Off: 4, Data: []byte("x")}); r.Code != protocol.ErrItemBadMessage {
		t.Fatalf("no transfer: %+v", r)
	}

	// A file that does not check out is failed, its chunks dropped.
	xfer := bytes.Repeat([]byte{9}, 16)
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Data: []byte("abcd"), Total: &small, Task: taskID[:],
		SHA256: make([]byte, 32)}); r.Next != 4 {
		t.Fatalf("one chunk: %+v", r)
	}
	f, _ := files.FindByXfer(ctx, session.SessionID, xfer)
	f = waitState(t, files, f.FileID, models.MinionFileFailed)
	if !strings.Contains(f.Err, "SHA-256") || store.keys("") != 0 {
		t.Fatalf("failed: %q, %d objects left", f.Err, store.keys(""))
	}
	// A transfer that ended takes no more.
	if r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 4}); r.Code != protocol.ErrItemBadMessage {
		t.Fatalf("after its end: %+v", r)
	}
}

func TestAbortAndSweep(t *testing.T) {
	s, store, files, session, taskID := setup(t)
	ctx := context.Background()
	total := uint64(8)
	xfer := bytes.Repeat([]byte{3}, 16)
	_, _ = s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Data: []byte("abcd"), Total: &total, Task: taskID[:]})
	if err := s.AbortTask(ctx, taskID, "stopped"); err != nil {
		t.Fatal(err)
	}
	f, _ := files.FindByXfer(ctx, session.SessionID, xfer)
	if f.State != models.MinionFileCancelled || store.keys("staging/") != 0 {
		t.Fatalf("aborted: %+v, %d chunks", f, store.keys("staging/"))
	}

	// Idle for longer than IdleAfter: given up.
	xfer2 := bytes.Repeat([]byte{4}, 16)
	_, _ = s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer2, Data: []byte("abcd"), Total: &total, Task: taskID[:]})
	s.now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	s.Sweep(ctx)
	f2, _ := files.FindByXfer(ctx, session.SessionID, xfer2)
	if f2.State != models.MinionFileFailed || !strings.Contains(f2.Err, "no chunk came") {
		t.Fatalf("swept: %+v", f2)
	}
}
