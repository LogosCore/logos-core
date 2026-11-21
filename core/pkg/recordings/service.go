// Package recordings keeps the desktops' and the terminals' recordings
// (docs: contracts/minion-messages/tunnels.md, Recordings). Each desktop
// core relays for the web app (controller: minion_desktop_controller.go)
// is recorded as it goes, in the format guacd's own recordings have, which
// Guacamole's player plays (desktop.go); each terminal session of a
// terminal task (miniontunnels: a web terminal, or a client of its raw
// port) as an asciicast, which asciinema plays (terminal.go). Core keeps
// it in parts as it goes
// (parts/<recording>/<n>, a part every few seconds, and when one grows
// large), so a core that stops loses only what it had not kept; a part the
// store does not take is tried again with the next, and past a bound
// dropped (the recording's lost bytes: it plays past them). A recording is
// read as its parts one after another. One whose core stopped is ended by
// the next sweep. Recordings stay in their bucket until their operation is
// deleted, or an operator deletes one.
package recordings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// Store is the bucket the recordings' parts are kept in.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error)
	Head(ctx context.Context, key string) (blob.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

// Tasks counts a desktop or terminal task's recordings on the task
// (repository.IMinionTaskRepository).
type Tasks interface {
	AddRecording(ctx context.Context, taskID uuid.UUID, kept, made int) (*models.MinionTask, error)
}

type Config struct {
	// PartEvery is how often what came is kept as a part; PartMax the size
	// a part is kept at sooner.
	PartEvery time.Duration
	PartMax   int
	// MaxHeld bounds what a recording holds that is not kept yet: past it,
	// what comes is dropped (lost) until the store takes a part again.
	MaxHeld int
	// StaleAfter ends a recording whose core kept nothing of it for that
	// long (it stopped); SweepEvery is how often they are looked for.
	StaleAfter time.Duration
	SweepEvery time.Duration
	// PutWait bounds a part's write; RetryWait is the wait before a last
	// part's next try (twice that before the third).
	PutWait   time.Duration
	RetryWait time.Duration
	// ConvertEvery is how often recordings waiting for their video are
	// looked for, and the first pause after an encoder that did not answer
	// (each after it twice the last, 30 minutes at most).
	ConvertEvery time.Duration
}

// DefaultConfig: a part each 5 seconds or 4 MiB, at most 32 MiB held, a
// recording with no word for 2 minutes ended.
func DefaultConfig() Config {
	return Config{PartEvery: 5 * time.Second, PartMax: 4 << 20, MaxHeld: 32 << 20, StaleAfter: 2 * time.Minute,
		SweepEvery: time.Minute, PutWait: 30 * time.Second, RetryWait: time.Second, ConvertEvery: 30 * time.Second}
}

type Service struct {
	repo   repository.IRecordingRepository
	tasks  Tasks
	store  Store
	bus    eventbus.IEventBus
	cfg    Config
	logger *zap.Logger
	// now is the wall clock (UTC); clock the one durations are read on
	// (with its monotonic reading).
	now   func() time.Time
	clock func() time.Time

	// active are the recordings this core records now; gone the
	// operations whose recordings are being deleted (the zero time), or
	// were lately (until when: goneFor), whose desktops and terminals are
	// not recorded.
	mu     sync.Mutex
	active map[uuid.UUID]*Recorder
	gone   map[uuid.UUID]time.Time
	stop   chan struct{}
	wg     sync.WaitGroup

	// encoder makes desktops' recordings videos (WithEncoder); nil: none
	// is converted.
	encoder Encoder
}

// WithEncoder has the service make a video of each desktop's recording
// once it ended (convert.go), and of those that wait; before Start.
func (s *Service) WithEncoder(e Encoder) *Service {
	s.encoder = e
	return s
}

// goneFor is how long after an operation's recordings were deleted a
// desktop or terminal of its that begins is still not recorded: one that
// counted its task as the operation went is not left behind it.
const goneFor = 10 * time.Minute

func NewService(repo repository.IRecordingRepository, tasks Tasks, store Store, bus eventbus.IEventBus, cfg Config, logger *zap.Logger) *Service {
	return &Service{repo: repo, tasks: tasks, store: store, bus: bus, cfg: cfg, logger: logger.Named("recordings"),
		now: func() time.Time { return time.Now().UTC() }, clock: time.Now,
		active: map[uuid.UUID]*Recorder{}, gone: map[uuid.UUID]time.Time{}, stop: make(chan struct{})}
}

func partKey(id uuid.UUID, n int) string { return fmt.Sprintf("parts/%s/%06d", id, n) }

// Beginning is a desktop or a terminal session about to open: whose, and
// what with.
type Beginning struct {
	TaskID uuid.UUID
	// Front is the task's: rdp, vnc, ssh or telnet (a desktop, recorded
	// as guac), or terminal (an asciicast).
	Front string
	// UserID and Username: the operator who opens it; none for a
	// terminal's raw port. Client: who, as the task's streams say it.
	UserID   uuid.UUID
	Username string
	Client   string
	// Stream is a terminal's, its number in the task.
	Stream uint64
	// Settings are what a desktop opens with, as the web app sent them.
	Settings map[string]any
	// Width and Height: a desktop's display in pixels, a terminal's
	// columns and rows.
	Width  int
	Height int
	// Keys: the operator's mouse and keys reach the desktop's server (it
	// is not view only), or what is typed reaches the terminal's process,
	// so the recording holds them.
	Keys bool
}

// Begin starts a desktop's or a terminal's recording, as it opens: nil
// when it cannot be kept (said in the log), and it opens unrecorded. Each
// of the Recorder's methods takes nil.
func (s *Service) Begin(ctx context.Context, b Beginning) *Recorder {
	what := "A desktop"
	format := models.RecordingFormatGuac
	if b.Front == protocol.FrontTerminal {
		what, format = "A terminal", models.RecordingFormatAsciicast
	}
	t, err := s.tasks.AddRecording(ctx, b.TaskID, 1, 1)
	if err != nil {
		s.logger.Error(what+" opens unrecorded: count its recording", zap.String("task_id", b.TaskID.String()), zap.Error(err))
		return nil
	}
	now := s.now()
	rec := &models.Recording{RecordingID: uuid.New(), OperationID: t.OperationID, SessionID: t.SessionID, TaskID: t.TaskID,
		Number: t.RecordingsMade, Front: b.Front, Format: format, UserID: b.UserID, Username: b.Username, Client: b.Client,
		Stream: b.Stream, Settings: b.Settings, Width: b.Width, Height: b.Height, Keys: b.Keys, State: models.RecordingStateRecording,
		StartedAt: now, SeenAt: now}
	if err := s.repo.Insert(ctx, rec); err != nil {
		s.logger.Error(what+" opens unrecorded: keep its recording", zap.String("task_id", b.TaskID.String()), zap.Error(err))
		if t, err := s.tasks.AddRecording(context.WithoutCancel(ctx), b.TaskID, -1, 0); err == nil {
			s.publish(t)
		}
		return nil
	}
	s.mu.Lock()
	if s.goneLocked(t.OperationID) {
		s.mu.Unlock()
		// Its operation is being deleted, or was as it began: its record
		// goes rather than outlive it.
		s.logger.Info(what+" opens unrecorded: its operation is deleted", zap.String("task_id", b.TaskID.String()))
		_ = s.repo.Delete(context.WithoutCancel(ctx), rec.RecordingID)
		_, _ = s.tasks.AddRecording(context.WithoutCancel(ctx), b.TaskID, -1, 0)
		return nil
	}
	r := newRecorder(s, rec)
	s.active[rec.RecordingID] = r
	s.mu.Unlock()
	s.publish(t)
	go r.loop()
	return r
}

// goneLocked reports whether an operation's recordings are being deleted,
// or were within goneFor. Called with mu held.
func (s *Service) goneLocked(operationID uuid.UUID) bool {
	until, ok := s.gone[operationID]
	return ok && (until.IsZero() || s.now().Before(until))
}

// Recording is one by its id: IsNotFound for none.
func (s *Service) Recording(ctx context.Context, id uuid.UUID) (*models.Recording, error) {
	return s.repo.FindByID(ctx, id)
}

// List is a page of a task's recordings, the latest first: those numbered
// below before (0: from the latest), at most limit.
func (s *Service) List(ctx context.Context, taskID uuid.UUID, before, limit int64) ([]models.Recording, error) {
	return s.repo.ListByTask(ctx, taskID, before, limit)
}

// ErrNothing: the recording has nothing to read (its desktop or terminal
// did not open).
var ErrNothing = errors.New("it did not open: nothing was recorded")

// Open reads a recording: its parts one after another, as far as they are
// kept now (one recording goes on). Its size is the record's.
func (s *Service) Open(ctx context.Context, id uuid.UUID) (io.ReadCloser, *models.Recording, error) {
	rec, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if rec.State == models.RecordingStateFailed {
		return nil, rec, ErrNothing
	}
	return &partsReader{ctx: ctx, store: s.store, id: rec.RecordingID, n: rec.Parts}, rec, nil
}

// ErrRecording: a recording still recording is not deleted.
var ErrRecording = errors.New("it is still recording: close its desktop or terminal first")

// Delete drops a recording that ended, its parts with it: IsNotFound for
// none.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (*models.Recording, error) {
	rec, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.State == models.RecordingStateRecording {
		return rec, ErrRecording
	}
	s.dropAll(ctx, *rec)
	if err := s.repo.Delete(ctx, id); err != nil {
		return rec, err
	}
	if t, err := s.tasks.AddRecording(ctx, rec.TaskID, -1, 0); err == nil {
		s.publish(t)
	} else if !repository.IsNotFound(err) {
		s.logger.Warn("A recording's task: count it gone", zap.String("recording_id", id.String()), zap.Error(err))
	}
	s.logger.Info("A recording was deleted", zap.String("recording_id", id.String()), zap.String("task_id", rec.TaskID.String()))
	return rec, nil
}

// DeleteByOperation drops an operation's recordings, parts and all: those
// recording now stop being recorded first, and a desktop or terminal of
// its that begins meanwhile, or within goneFor, is not recorded.
func (s *Service) DeleteByOperation(ctx context.Context, operationID uuid.UUID) error {
	s.mu.Lock()
	s.gone[operationID] = time.Time{}
	var going []*Recorder
	for _, r := range s.active {
		if r.rec.OperationID == operationID {
			going = append(going, r)
		}
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := s.now()
		for op, until := range s.gone {
			if !until.IsZero() && !now.Before(until) {
				delete(s.gone, op)
			}
		}
		s.gone[operationID] = now.Add(goneFor)
	}()
	endAll(going, models.RecordingEndClosed)
	recs, err := s.repo.ListByOperation(ctx, operationID)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		s.dropAll(ctx, rec)
	}
	return s.repo.DeleteByOperationID(ctx, operationID)
}

// dropAll deletes what a recording keeps: its parts, and its video and keys.
func (s *Service) dropAll(ctx context.Context, rec models.Recording) {
	s.dropParts(ctx, rec)
	_ = s.store.Delete(ctx, videoKey(rec.RecordingID))
	_ = s.store.Delete(ctx, keysKey(rec.RecordingID))
}

// dropParts deletes a recording's parts: those it says, and any past them
// (kept as its core stopped, before it said so).
func (s *Service) dropParts(ctx context.Context, rec models.Recording) {
	for n := 0; n < rec.Parts; n++ {
		_ = s.store.Delete(ctx, partKey(rec.RecordingID, n))
	}
	for n := rec.Parts; ; n++ {
		if _, err := s.store.Head(ctx, partKey(rec.RecordingID, n)); err != nil {
			return
		}
		_ = s.store.Delete(ctx, partKey(rec.RecordingID, n))
	}
}

// Start sweeps now and every SweepEvery: recordings whose core stopped
// are ended.
func (s *Service) Start() {
	if s.encoder != nil {
		s.wg.Add(1)
		go s.convertLoop()
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(s.cfg.SweepEvery)
		defer tick.Stop()
		for {
			s.Sweep(context.Background())
			select {
			case <-s.stop:
				return
			case <-tick.C:
			}
		}
	}()
}

// Stop ends the sweeps, and the recordings this core records: each keeps
// what it has, ended core_stopped (their desktops and terminals go on
// unrecorded until core does stop).
func (s *Service) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
	s.mu.Lock()
	going := make([]*Recorder, 0, len(s.active))
	for _, r := range s.active {
		going = append(going, r)
	}
	s.mu.Unlock()
	endAll(going, models.RecordingEndCoreStopped)
}

// endAll ends recordings at once (each keeps its last part as it can),
// returning once all have.
func endAll(rs []*Recorder, end string) {
	var wg sync.WaitGroup
	for _, r := range rs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.End(end)
		}()
	}
	wg.Wait()
}

// Sweep ends the recordings recording with no word from their core for
// StaleAfter (it stopped): saved as far as their parts go, parts kept
// after the last word too.
func (s *Service) Sweep(ctx context.Context) {
	before := s.now().Add(-s.cfg.StaleAfter)
	stale, err := s.repo.Stale(ctx, before)
	if err != nil {
		s.logger.Error("Recordings: sweep", zap.Error(err))
		return
	}
	for _, rec := range stale {
		s.mu.Lock()
		_, mine := s.active[rec.RecordingID]
		s.mu.Unlock()
		if mine {
			// This core records it: its saves failed; it ends it itself.
			continue
		}
		parts, size := rec.Parts, rec.Size
		for {
			info, err := s.store.Head(ctx, partKey(rec.RecordingID, parts))
			if err != nil {
				break
			}
			parts, size = parts+1, size+info.ContentLength
		}
		ok, err := s.repo.EndStale(ctx, rec.RecordingID, before, bson.M{"state": models.RecordingStateSaved,
			"end": models.RecordingEndCoreStopped, "ended_at": rec.SeenAt, "parts": parts, "size": size})
		if err != nil {
			s.logger.Error("Recordings: end one whose core stopped", zap.String("recording_id", rec.RecordingID.String()), zap.Error(err))
			continue
		}
		if !ok {
			continue
		}
		s.logger.Info("A recording whose core stopped was ended", zap.String("recording_id", rec.RecordingID.String()),
			zap.Int("parts", parts), zap.Int64("size", size))
		if t, err := s.tasks.AddRecording(ctx, rec.TaskID, 0, 0); err == nil {
			s.publish(t)
		}
	}
}

func (s *Service) publish(t *models.MinionTask) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(),
	}))
}

// partsReader reads a recording's parts one after another.
type partsReader struct {
	ctx   context.Context
	store Store
	id    uuid.UUID
	next  int
	n     int
	cur   io.ReadCloser
}

func (p *partsReader) Read(b []byte) (int, error) {
	for {
		if p.cur == nil {
			if p.next >= p.n {
				return 0, io.EOF
			}
			body, _, err := p.store.Get(p.ctx, partKey(p.id, p.next))
			if err != nil {
				return 0, fmt.Errorf("the recording's part %d: %w", p.next, err)
			}
			p.cur, p.next = body, p.next+1
		}
		n, err := p.cur.Read(b)
		if errors.Is(err, io.EOF) {
			_ = p.cur.Close()
			p.cur = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (p *partsReader) Close() error {
	if p.cur != nil {
		err := p.cur.Close()
		p.cur = nil
		return err
	}
	return nil
}
