// Package minionstreams relays the files minions stream (docs:
// contracts/minion-messages.md, Streaming a file): a stream_file task's
// minion announces its file, then sends chunks only while core's acks let
// it, from where they say; core puts each in a small buffer the downloader
// (GET /api/v1/minion-streams/{task}) reads from, on whichever core
// instance it is, and keeps none of the file. The downloader sets the
// pace: core acks a chunk when the buffer has room, holds the minion when
// it has not or when no one downloads, and tells it to go back when a
// download resumes from earlier. The newest downloader takes the stream.
// A stream an hour without a downloader is stopped.
package minionstreams

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Buffer keeps a stream's chunks between the minion's exchange and the
// downloader (Redis: core's cache), shared by core's instances.
type Buffer interface {
	IsEnabled() bool
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// Tasks is what the service reads and writes of tasks.
type Tasks interface {
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
	StreamAnnounce(ctx context.Context, sessionID, taskID uuid.UUID, s models.MinionTaskStream) (*models.MinionTask, error)
	FindByStreamXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionTask, error)
	StreamAttach(ctx context.Context, taskID uuid.UUID, reader string, from int64, at time.Time) (*models.MinionTask, error)
	StreamDelivered(ctx context.Context, taskID uuid.UUID, gen, delivered int64, at time.Time) (bool, error)
	StreamDetach(ctx context.Context, taskID uuid.UUID, gen int64, at time.Time) error
	StreamTook(ctx context.Context, taskID uuid.UUID, gen, off, n int64, sha256 []byte) (bool, error)
	ActiveStreams(ctx context.Context, sessionID uuid.UUID) ([]models.MinionTask, error)
	IdleStreams(ctx context.Context, before time.Time) ([]models.MinionTask, error)
	RequestCancel(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error)

	// Streams core sends a minion (upstream.go).
	UploadAttach(ctx context.Context, taskID uuid.UUID, uploader string, at time.Time) (*models.MinionTask, error)
	UploadSize(ctx context.Context, taskID uuid.UUID, size int64) (*models.MinionTask, error)
	UploadReceived(ctx context.Context, taskID uuid.UUID, gen, off, n int64, hashState, sha256 []byte, at time.Time) (bool, error)
	UploadDetach(ctx context.Context, taskID uuid.UUID, gen int64, at time.Time) error
}

type Config struct {
	// Window is how far the minion may be ahead of the downloader.
	Window int64
	// ReaderGone is how long a downloader may say nothing before the
	// stream counts it gone.
	ReaderGone time.Duration
	// IdleAfter stops a stream with no downloader for that long.
	IdleAfter time.Duration
	// ChunkTTL is how long a chunk waits in the buffer.
	ChunkTTL time.Duration
	// Poll is how often a downloader looks for its next chunk.
	Poll time.Duration
	// LinkTTL is the longest a signed link lasts.
	LinkTTL time.Duration
	// PullWait is how long a minion's ask for a stream's chunk that has
	// not come yet waits for it while an uploader is there, and PullPoll
	// how often it looks: the minion's asks then follow the upload, rather
	// than being held a check-in's while each time they catch up with it.
	PullWait time.Duration
	PullPoll time.Duration
}

// DefaultConfig is the docs' numbers: a window of eight chunks (4 MiB), an
// hour without a downloader.
func DefaultConfig() Config {
	return Config{Window: 8 * protocol.MaxChunkData, ReaderGone: 30 * time.Second, IdleAfter: time.Hour,
		ChunkTTL: 15 * time.Minute, Poll: 50 * time.Millisecond, LinkTTL: 24 * time.Hour,
		PullWait: 2 * time.Second, PullPoll: 20 * time.Millisecond}
}

type Service struct {
	tasks Tasks
	buf   Buffer
	bus   eventbus.IEventBus
	// waker pushes a session's stream acks to its minion at once; nil:
	// they go with its next message.
	waker  Waker
	key    []byte
	cfg    Config
	logger *zap.Logger
	now    func() time.Time
	stop   chan struct{}
	done   chan struct{}
}

// NewService: key signs the links streams download from.
// Waker tells a minion over a persistent connection serving its session,
// at once, what its streams may do now (minionsync): a stream it holds goes
// when a downloader comes, and one it receives when bytes it has not got
// arrive from the uploader, without waiting for the minion's next message.
type Waker interface {
	WakeStreams(sessionID uuid.UUID)
}

// WithWaker gives the service a Waker, told when a downloader attaches and
// when an uploader's bytes reach a minion that has all the others.
func (s *Service) WithWaker(w Waker) *Service {
	s.waker = w
	return s
}

// wake tells the waker, if any, that t's stream may go.
func (s *Service) wake(t *models.MinionTask) {
	if s.waker != nil {
		s.waker.WakeStreams(t.SessionID)
	}
}

func NewService(tasks Tasks, buf Buffer, bus eventbus.IEventBus, key []byte, cfg Config, logger *zap.Logger) *Service {
	return &Service{tasks: tasks, buf: buf, bus: bus, key: key, cfg: cfg, logger: logger.Named("minionstreams"),
		now: func() time.Time { return time.Now().UTC() }, stop: make(chan struct{}), done: make(chan struct{})}
}

func chunkKey(taskID uuid.UUID, gen, off int64) string {
	return fmt.Sprintf("mstream:%s:%d:%d", taskID, gen, off)
}

// readerAlive: the stream has a downloader that said lately it is there.
func (s *Service) readerAlive(st *models.MinionTaskStream) bool {
	return st.Reader != "" && st.ReaderAt != nil && st.ReaderAt.After(s.now().Add(-s.cfg.ReaderGone))
}

// ack is what the minion is told of a stream: send from want, unless
// there is no downloader, the window is full, or all of it is sent and the
// downloader does not have it all yet. Once it has, the whole without hold
// ends the task.
func (s *Service) ack(st *models.MinionTaskStream) protocol.AckItem {
	next := uint64(st.Want)
	hold := !s.readerAlive(st) || st.Want-st.Delivered >= s.cfg.Window || st.Want >= st.Size
	if st.Delivered >= st.Size && st.Ran {
		next, hold = uint64(st.Size), false
	}
	return protocol.AckItem{T: protocol.ItemAck, Xfer: st.Xfer, Next: &next, Hold: hold}
}

// Reply is the service's answer to a chunk: an ack, or an err item's code
// and message.
type Reply struct {
	Ack  *protocol.AckItem
	Code int
	Msg  string
}

// TakeChunk takes a chunk if it is a stream's; handled false for one of
// another transfer (a download's).
func (s *Service) TakeChunk(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem) (Reply, bool, error) {
	if c.Off == 0 && c.Total != nil && c.Task != nil && len(c.Data) == 0 {
		taskID, _ := uuid.FromBytes(c.Task)
		t, err := s.tasks.FindByTaskID(ctx, taskID)
		if repository.IsNotFound(err) || (err == nil && t.Result != protocol.ResultFileStream) {
			return Reply{}, false, nil
		}
		if err != nil {
			return Reply{}, true, err
		}
		after, err := s.tasks.StreamAnnounce(ctx, session.SessionID, taskID, models.MinionTaskStream{Xfer: c.Xfer, Name: c.Name,
			Size: int64(*c.Total), AnnouncedAt: s.now(), IdleSince: ptr(s.now())})
		if repository.IsNotFound(err) {
			return Reply{Code: protocol.ErrItemUnknownTask, Msg: "no such task to stream under this session"}, true, nil
		}
		if err != nil {
			return Reply{}, true, fmt.Errorf("announce the stream: %w", err)
		}
		s.logger.Info("minion stream announced", zap.String("task_id", taskID.String()), zap.String("name", c.Name),
			zap.Uint64("size", *c.Total))
		s.publish(after)
		a := s.ack(after.Stream)
		return Reply{Ack: &a}, true, nil
	}
	t, err := s.tasks.FindByStreamXfer(ctx, session.SessionID, c.Xfer)
	if repository.IsNotFound(err) {
		return Reply{}, false, nil
	}
	if err != nil {
		return Reply{}, true, err
	}
	if t.Finished() {
		return Reply{Code: protocol.ErrItemUnknownTask, Msg: "the stream's task ended"}, true, nil
	}
	st := t.Stream
	off, n := int64(c.Off), int64(len(c.Data))
	if off+n > st.Size {
		return Reply{Code: protocol.ErrItemBadMessage, Msg: fmt.Sprintf("past the stream's %d bytes", st.Size)}, true, nil
	}
	// Kept only from where the downloader wants it, while it is there.
	if off == st.Want && n > 0 && s.readerAlive(st) {
		if err := s.buf.Set(ctx, chunkKey(t.TaskID, st.Gen, off), string(c.Data), s.cfg.ChunkTTL); err != nil {
			return Reply{}, true, fmt.Errorf("buffer a chunk: %w", err)
		}
		took, err := s.tasks.StreamTook(ctx, t.TaskID, st.Gen, off, n, c.SHA256)
		if err != nil {
			return Reply{}, true, err
		}
		if took {
			st.Want += n
		}
	}
	a := s.ack(st)
	return Reply{Ack: &a}, true, nil
}

// Acks are a session's streams' acks for a reply, but for the transfers in
// answered (those its chunks were answered for already): what each
// minion's stream may do now, said with every exchange, as a downloader
// comes, catches up or goes.
func (s *Service) Acks(ctx context.Context, session *models.MinionSession, answered map[string]bool) ([]protocol.AckItem, error) {
	tasks, err := s.tasks.ActiveStreams(ctx, session.SessionID)
	if err != nil {
		return nil, err
	}
	var out []protocol.AckItem
	for _, t := range tasks {
		switch {
		case t.Stream != nil && !answered[string(t.Stream.Xfer)]:
			out = append(out, s.ack(t.Stream))
		case t.Upload != nil && t.Upload.Stream && !answered[string(t.Upload.Xfer)]:
			out = append(out, s.upAck(t.Upload, t.Upload.Acked))
		}
	}
	return out, nil
}

// ErrGone is a stream that cannot be downloaded: its task ended, or it is
// not a stream.
var ErrGone = errors.New("the stream is over")

// ErrNotReady: the minion has not announced the file yet.
var ErrNotReady = errors.New("the minion has not started the stream yet")

// Stream is what a downloader needs to answer before the bytes come.
func (s *Service) Stream(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error) {
	t, err := s.tasks.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	switch {
	case t.Result != protocol.ResultFileStream || t.Finished():
		return t, ErrGone
	case t.Stream == nil:
		return t, ErrNotReady
	}
	return t, nil
}

// Read takes the stream for a downloader from offset from to end (excluded)
// and writes its bytes to w as the minion sends them, until all are
// written, the task ends, another downloader takes the stream, or ctx is
// done. It returns how far it wrote.
func (s *Service) Read(ctx context.Context, taskID uuid.UUID, from, end int64, write func([]byte) error) (int64, error) {
	if !s.buf.IsEnabled() {
		return from, errors.New("streams need core's Redis cache")
	}
	reader := uuid.NewString()
	t, err := s.tasks.StreamAttach(ctx, taskID, reader, from, s.now())
	if err != nil {
		return from, err
	}
	gen := t.Stream.Gen
	// What the stream records does not hang on the downloader's
	// connection: a client with every byte closes it at once, and the last
	// chunk must still count as delivered.
	wctx := context.WithoutCancel(ctx)
	s.logger.Info("minion stream downloader attached", zap.String("task_id", taskID.String()), zap.Int64("from", from), zap.Int64("gen", gen))
	s.publish(t)
	s.wake(t)
	pos := from
	defer func() {
		// Its own context may be over: let go regardless.
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.tasks.StreamDetach(dctx, taskID, gen, s.now())
		if after, err := s.tasks.FindByTaskID(dctx, taskID); err == nil {
			s.publish(after)
		}
		s.logger.Info("minion stream downloader gone", zap.String("task_id", taskID.String()), zap.Int64("at", pos), zap.Int64("gen", gen))
	}()
	beat := s.now()
	lastPublish := s.now()
	for pos < end {
		key := chunkKey(taskID, gen, pos)
		data, err := s.buf.Get(ctx, key)
		if err == nil && data != "" {
			b := []byte(data)
			if int64(len(b)) > end-pos {
				b = b[:end-pos]
			}
			if err := write(b); err != nil {
				return pos, err
			}
			_ = s.buf.Del(ctx, key)
			pos += int64(len(data))
			ours, err := s.tasks.StreamDelivered(wctx, taskID, gen, min(pos, t.Stream.Size), s.now())
			if err != nil {
				return pos, err
			}
			if !ours {
				return pos, nil
			}
			beat = s.now()
			if s.now().Sub(lastPublish) >= time.Second || pos >= end {
				if after, err := s.tasks.FindByTaskID(wctx, taskID); err == nil {
					s.publish(after)
				}
				lastPublish = s.now()
			}
			continue
		}
		if s.now().Sub(beat) >= time.Second {
			// Still there, still ours, and the task still going.
			ours, err := s.tasks.StreamDelivered(wctx, taskID, gen, pos, s.now())
			if err != nil || !ours {
				return pos, err
			}
			after, err := s.tasks.FindByTaskID(wctx, taskID)
			if err != nil || after.Finished() {
				return pos, err
			}
			beat = s.now()
		}
		select {
		case <-ctx.Done():
			return pos, ctx.Err()
		case <-time.After(s.cfg.Poll):
		}
	}
	return pos, nil
}

// Link is a signed path for downloading a stream without a session: valid
// while the stream lasts, and LinkTTL at most.
func (s *Service) Link(taskID uuid.UUID) (string, time.Time) {
	exp := s.now().Add(s.cfg.LinkTTL).Truncate(time.Second)
	e := strconv.FormatInt(exp.Unix(), 10)
	return fmt.Sprintf("/minion-streams/%s?exp=%s&sig=%s", taskID, e, s.sign(taskID, e)), exp
}

// ReaderGone is how long a downloader may say nothing before it counts
// gone.
func (s *Service) ReaderGone() time.Duration { return s.cfg.ReaderGone }

// Verify checks a signed link's exp and sig for a task.
func (s *Service) Verify(taskID uuid.UUID, exp, sig string) bool {
	e, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Unix(e, 0).Before(s.now()) {
		return false
	}
	want := s.sign(taskID, exp)
	return hmac.Equal([]byte(want), []byte(sig))
}

func (s *Service) sign(taskID uuid.UUID, exp string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("minion-stream:" + taskID.String() + ":" + exp))
	return hex.EncodeToString(m.Sum(nil))
}

// Start sweeps every minute: a stream an hour without a downloader is
// stopped (its task cancelled; the minion closes the file).
func (s *Service) Start() {
	go func() {
		defer close(s.done)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-tick.C:
				s.Sweep(context.Background())
			}
		}
	}()
}

func (s *Service) Stop() {
	select {
	case <-s.stop:
		return
	default:
		close(s.stop)
	}
	<-s.done
}

// Sweep stops the streams idle for IdleAfter.
func (s *Service) Sweep(ctx context.Context) {
	idle, err := s.tasks.IdleStreams(ctx, s.now().Add(-s.cfg.IdleAfter))
	if err != nil {
		s.logger.Error("minion streams: sweep", zap.Error(err))
		return
	}
	for _, t := range idle {
		after, err := s.tasks.RequestCancel(ctx, t.TaskID, s.now())
		if err != nil {
			s.logger.Error("minion streams: stop an idle stream", zap.String("task_id", t.TaskID.String()), zap.Error(err))
			continue
		}
		s.logger.Info("minion stream stopped: no downloader for "+s.cfg.IdleAfter.String(), zap.String("task_id", t.TaskID.String()))
		s.publish(after)
	}
}

func (s *Service) publish(t *models.MinionTask) {
	if s.bus == nil || t == nil {
		return
	}
	s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(),
	}))
}

func ptr[T any](v T) *T { return &v }
