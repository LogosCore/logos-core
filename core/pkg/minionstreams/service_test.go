package minionstreams

import (
	"bytes"
	"context"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// memBuf is the buffer in memory.
type memBuf struct {
	mu sync.Mutex
	m  map[string]string
}

func (b *memBuf) IsEnabled() bool { return true }
func (b *memBuf) Get(_ context.Context, k string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m[k], nil
}
func (b *memBuf) Set(_ context.Context, k string, v interface{}, _ time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[k] = v.(string)
	return nil
}
func (b *memBuf) Del(_ context.Context, keys ...string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, k := range keys {
		delete(b.m, k)
	}
	return nil
}

// memTasks keeps one task, by the repository's rules as far as the
// service uses them (the real ones: TestIntegrationMinionStreams).
type memTasks struct {
	mu sync.Mutex
	t  models.MinionTask
}

func (m *memTasks) get() models.MinionTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.t
	if c.Stream != nil {
		st := *c.Stream
		c.Stream = &st
	}
	if c.Upload != nil {
		up := *c.Upload
		c.Upload = &up
	}
	return c
}

func (m *memTasks) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	if id != m.t.TaskID {
		return nil, repository.ErrNotFound
	}
	t := m.get()
	return &t, nil
}

func (m *memTasks) StreamAnnounce(_ context.Context, sid, id uuid.UUID, st models.MinionTaskStream) (*models.MinionTask, error) {
	m.mu.Lock()
	if m.t.Stream == nil && m.t.SessionID == sid && id == m.t.TaskID {
		m.t.Stream = &st
	}
	m.mu.Unlock()
	t := m.get()
	return &t, nil
}

func (m *memTasks) FindByStreamXfer(_ context.Context, sid uuid.UUID, xfer []byte) (*models.MinionTask, error) {
	t := m.get()
	if t.Stream == nil || t.SessionID != sid || !bytes.Equal(t.Stream.Xfer, xfer) {
		return nil, repository.ErrNotFound
	}
	return &t, nil
}

func (m *memTasks) StreamAttach(_ context.Context, _ uuid.UUID, reader string, from int64, at time.Time) (*models.MinionTask, error) {
	m.mu.Lock()
	st := m.t.Stream
	st.Gen++
	st.Reader, st.ReaderAt, st.Want, st.Delivered, st.Ran, st.IdleSince = reader, &at, from, from, true, nil
	m.mu.Unlock()
	t := m.get()
	return &t, nil
}

func (m *memTasks) StreamDelivered(ctx context.Context, _ uuid.UUID, gen, delivered int64, at time.Time) (bool, error) {
	// As Mongo: a cancelled context writes nothing.
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t.Stream.Gen != gen {
		return false, nil
	}
	m.t.Stream.Delivered, m.t.Stream.ReaderAt = delivered, &at
	return true, nil
}

func (m *memTasks) StreamDetach(_ context.Context, _ uuid.UUID, gen int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t.Stream.Gen == gen {
		m.t.Stream.Reader, m.t.Stream.IdleSince = "", &at
	}
	return nil
}

func (m *memTasks) StreamTook(_ context.Context, _ uuid.UUID, gen, off, n int64, sum []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t.Stream.Gen != gen || m.t.Stream.Want != off {
		return false, nil
	}
	m.t.Stream.Want += n
	if sum != nil {
		m.t.Stream.SHA256 = sum
	}
	return true, nil
}

func (m *memTasks) ActiveStreams(_ context.Context, sid uuid.UUID) ([]models.MinionTask, error) {
	t := m.get()
	if t.Stream == nil || t.Finished() || t.SessionID != sid {
		return nil, nil
	}
	return []models.MinionTask{t}, nil
}

func (m *memTasks) IdleStreams(_ context.Context, before time.Time) ([]models.MinionTask, error) {
	t := m.get()
	if t.Stream != nil && !t.Finished() && t.Stream.IdleSince != nil && t.Stream.IdleSince.Before(before) {
		return []models.MinionTask{t}, nil
	}
	return nil, nil
}

func (m *memTasks) RequestCancel(_ context.Context, _ uuid.UUID, at time.Time) (*models.MinionTask, error) {
	m.mu.Lock()
	m.t.CancelRequestedAt = &at
	m.mu.Unlock()
	t := m.get()
	return &t, nil
}

func setup(t *testing.T, size int) (*Service, *memTasks, *models.MinionSession, []byte, []byte) {
	t.Helper()
	session := &models.MinionSession{SessionID: uuid.New()}
	tasks := &memTasks{t: models.MinionTask{TaskID: uuid.New(), SessionID: session.SessionID, Result: protocol.ResultFileStream,
		State: models.MinionTaskRunning}}
	cfg := DefaultConfig()
	cfg.Window, cfg.Poll = 2*protocol.MaxChunkData, 2*time.Millisecond
	s := NewService(tasks, &memBuf{m: map[string]string{}}, nil, []byte("key"), cfg, zap.NewNop())
	body := make([]byte, size)
	_, _ = rand.Read(body)
	return s, tasks, session, body, bytes.Repeat([]byte{3}, 16)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s", what)
}

// minion plays a minion: it sends from where each ack says while it may,
// as many exchanges as it takes, and reports the last ack.
func minion(t *testing.T, s *Service, session *models.MinionSession, body, xfer []byte, a protocol.AckItem, exchanges int) protocol.AckItem {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < exchanges; i++ {
		if a.Hold || *a.Next >= uint64(len(body)) {
			acks, err := s.Acks(ctx, session, map[string]bool{})
			if err != nil || len(acks) != 1 {
				t.Fatalf("acks: %v %v", acks, err)
			}
			a = acks[0]
			time.Sleep(5 * time.Millisecond)
			continue
		}
		off := *a.Next
		end := min(off+protocol.MaxChunkData, uint64(len(body)))
		r, handled, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: off, Data: body[off:end]})
		if err != nil || !handled || r.Ack == nil {
			t.Fatalf("chunk at %d: %+v %v %v", off, r, handled, err)
		}
		a = *r.Ack
	}
	return a
}

func announce(t *testing.T, s *Service, tasks *memTasks, session *models.MinionSession, body, xfer []byte) protocol.AckItem {
	t.Helper()
	total := uint64(len(body))
	r, handled, err := s.TakeChunk(context.Background(), session, protocol.ChunkItem{Xfer: xfer, Total: &total,
		Task: tasks.t.TaskID[:], Name: "/var/big.iso", Data: []byte{}})
	if err != nil || !handled || r.Ack == nil || !r.Ack.Hold || *r.Ack.Next != 0 {
		t.Fatalf("announce: %+v %v %v", r, handled, err)
	}
	return *r.Ack
}

// Announced, a stream holds until a downloader comes; then the downloader
// gets the file, the minion paced by it, and the last ack, once it has
// every byte, is the whole without hold.
// wakes counts the sessions a Waker was told of.
type wakes struct {
	mu sync.Mutex
	of []uuid.UUID
}

func (w *wakes) WakeStreams(id uuid.UUID) { w.mu.Lock(); w.of = append(w.of, id); w.mu.Unlock() }

func (w *wakes) count() int { w.mu.Lock(); defer w.mu.Unlock(); return len(w.of) }

func TestStream(t *testing.T) {
	s, tasks, session, body, xfer := setup(t, 3*protocol.MaxChunkData+77)
	w := &wakes{}
	s.WithWaker(w)
	a := announce(t, s, tasks, session, body, xfer)
	// A chunk with no downloader: not kept, held.
	if r, _, _ := s.TakeChunk(context.Background(), session, protocol.ChunkItem{Xfer: xfer, Off: 0, Data: body[:10]}); !r.Ack.Hold {
		t.Fatal("no downloader: not held")
	}

	var got bytes.Buffer
	done := make(chan int64)
	go func() {
		n, _ := s.Read(context.Background(), tasks.t.TaskID, 0, int64(len(body)), func(b []byte) error {
			got.Write(b)
			return nil
		})
		done <- n
	}()
	waitFor(t, "downloader", func() bool { st := tasks.get().Stream; return st.Reader != "" })
	// The minion, holding the stream, is woken to send it.
	waitFor(t, "a wake", func() bool { return w.count() == 1 })
	if w.of[0] != session.SessionID {
		t.Fatalf("woke %v, not the stream's session", w.of)
	}
	a = minion(t, s, session, body, xfer, a, 40)
	if n := <-done; n != int64(len(body)) || !bytes.Equal(got.Bytes(), body) {
		t.Fatalf("read %d, equal %v", n, bytes.Equal(got.Bytes(), body))
	}
	acks, _ := s.Acks(context.Background(), session, map[string]bool{})
	if len(acks) != 1 || acks[0].Hold || *acks[0].Next != uint64(len(body)) {
		t.Fatalf("the end: %+v (last %+v)", acks, a)
	}
}

// A newer downloader takes the stream from its own offset (a resume): the
// one before is cut, and the minion is told to send from there.
func TestStreamResumeNewestWins(t *testing.T) {
	s, tasks, session, body, xfer := setup(t, 4*protocol.MaxChunkData)
	a := announce(t, s, tasks, session, body, xfer)

	first := make(chan int64)
	block := make(chan struct{})
	go func() {
		n, _ := s.Read(context.Background(), tasks.t.TaskID, 0, int64(len(body)), func([]byte) error { <-block; return nil })
		first <- n
	}()
	waitFor(t, "first downloader", func() bool { return tasks.get().Stream.Reader != "" })
	minion(t, s, session, body, xfer, a, 1)

	from := int64(protocol.MaxChunkData + 5)
	var got bytes.Buffer
	second := make(chan int64)
	go func() {
		n, _ := s.Read(context.Background(), tasks.t.TaskID, from, int64(len(body)), func(b []byte) error { got.Write(b); return nil })
		second <- n
	}()
	waitFor(t, "second downloader", func() bool { return tasks.get().Stream.Gen == 2 })
	close(block)
	<-first
	acks, _ := s.Acks(context.Background(), session, map[string]bool{})
	if len(acks) != 1 || *acks[0].Next != uint64(from) || acks[0].Hold {
		t.Fatalf("resume: %+v", acks)
	}
	minion(t, s, session, body, xfer, acks[0], 40)
	if n := <-second; n != int64(len(body)) || !bytes.Equal(got.Bytes(), body[from:]) {
		t.Fatalf("second read %d", n)
	}
}

// A slow downloader holds the minion once it is a window ahead.
func TestStreamWindow(t *testing.T) {
	s, tasks, session, body, xfer := setup(t, 8*protocol.MaxChunkData)
	a := announce(t, s, tasks, session, body, xfer)
	release := make(chan struct{})
	go func() {
		_, _ = s.Read(context.Background(), tasks.t.TaskID, 0, int64(len(body)), func([]byte) error { <-release; return nil })
	}()
	waitFor(t, "downloader", func() bool { return tasks.get().Stream.Reader != "" })
	a = minion(t, s, session, body, xfer, a, 6)
	if st := tasks.get().Stream; !a.Hold || st.Want-st.Delivered > s.cfg.Window {
		t.Fatalf("not held a window ahead: %+v, want %d delivered %d", a, st.Want, st.Delivered)
	}
	close(release)
}

func TestStreamLinkAndSweep(t *testing.T) {
	s, tasks, session, body, xfer := setup(t, 10)
	id := tasks.t.TaskID
	path, _ := s.Link(id)
	if path == "" {
		t.Fatal("no link")
	}
	q := path[len("/minion-streams/"+id.String()+"?"):]
	var exp, sig string
	for _, kv := range bytes.Split([]byte(q), []byte("&")) {
		k, v, _ := bytes.Cut(kv, []byte("="))
		if string(k) == "exp" {
			exp = string(v)
		} else {
			sig = string(v)
		}
	}
	if !s.Verify(id, exp, sig) || s.Verify(uuid.New(), exp, sig) || s.Verify(id, exp, sig[:10]) {
		t.Fatal("verify")
	}

	announce(t, s, tasks, session, body, xfer)
	s.Sweep(context.Background())
	if tasks.get().CancelRequestedAt != nil {
		t.Fatal("a fresh stream stopped")
	}
	s.now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	s.Sweep(context.Background())
	if tasks.get().CancelRequestedAt == nil {
		t.Fatal("an hour idle: not stopped")
	}
}

// A downloader that closes its connection the moment it has the last byte
// (curl) has still had every byte delivered: the stream ends, not pauses.
func TestStreamLastChunkCounts(t *testing.T) {
	s, tasks, session, body, xfer := setup(t, protocol.MaxChunkData+100)
	a := announce(t, s, tasks, session, body, xfer)
	ctx, cancel := context.WithCancel(context.Background())
	var got int64
	done := make(chan struct{})
	go func() {
		_, _ = s.Read(ctx, tasks.t.TaskID, 0, int64(len(body)), func(b []byte) error {
			if got += int64(len(b)); got == int64(len(body)) {
				cancel()
			}
			return nil
		})
		close(done)
	}()
	waitFor(t, "downloader", func() bool { return tasks.get().Stream.Reader != "" })
	minion(t, s, session, body, xfer, a, 10)
	<-done
	if st := tasks.get().Stream; st.Delivered != int64(len(body)) {
		t.Fatalf("delivered %d of %d", st.Delivered, len(body))
	}
	acks, _ := s.Acks(context.Background(), session, map[string]bool{})
	if len(acks) != 1 || acks[0].Hold || *acks[0].Next != uint64(len(body)) {
		t.Fatalf("the end: %+v", acks)
	}
}

func (m *memTasks) UploadAttach(_ context.Context, _ uuid.UUID, uploader string, at time.Time) (*models.MinionTask, error) {
	m.mu.Lock()
	u := m.t.Upload
	u.Gen++
	u.Uploader, u.UploaderAt, u.AttachedAt, u.From, u.IdleSince = uploader, &at, &at, u.Received, nil
	m.mu.Unlock()
	t := m.get()
	return &t, nil
}

func (m *memTasks) UploadSize(_ context.Context, _ uuid.UUID, size int64) (*models.MinionTask, error) {
	m.mu.Lock()
	if !m.t.Upload.Sized {
		m.t.Upload.Size, m.t.Upload.Sized = size, true
	}
	m.mu.Unlock()
	t := m.get()
	return &t, nil
}

func (m *memTasks) UploadReceived(ctx context.Context, _ uuid.UUID, gen, off, n int64, state, sum []byte, at time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.t.Upload
	if u.Gen != gen || u.Received != off {
		return false, nil
	}
	u.Received, u.HashState, u.UploaderAt = off+n, state, &at
	if sum != nil {
		u.SHA256 = sum
	}
	return true, nil
}

func (m *memTasks) UploadDetach(_ context.Context, _ uuid.UUID, gen int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t.Upload.Gen == gen {
		m.t.Upload.Uploader, m.t.Upload.IdleSince = "", &at
	}
	return nil
}
