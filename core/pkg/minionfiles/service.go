// Package minionfiles keeps the files minions send core (docs:
// contracts/minion-messages.md, File transfers): a download_file task's file
// comes in chunks, in order, each acked with how far core has it. Each chunk
// is kept as an object of its own (staging/<file>/<offset>), so any core pod
// takes the next one and a resent chunk only writes its object again; when
// the last comes, the chunks are joined into one object (files/<file>) in
// the background, checked against the size and SHA-256 the minion said, and
// dropped. A transfer with no chunk for a day is given up. The files stay in
// the minion files bucket until their operation is deleted.
package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// Store is the bucket the files and their chunks are kept in.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	PutStream(ctx context.Context, key string, body io.Reader, contentType string) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error)
	Head(ctx context.Context, key string) (blob.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

// Tasks reads the task a transfer says it is for.
type Tasks interface {
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
}

type Config struct {
	// MaxSize is the largest file taken.
	MaxSize int64
	// IdleAfter gives up a transfer with no chunk for that long.
	IdleAfter time.Duration
	// SweepEvery is how often idle transfers, and joins a stopped core left,
	// are looked for.
	SweepEvery time.Duration
}

type Service struct {
	files  repository.IMinionFileRepository
	tasks  Tasks
	store  Store
	bus    eventbus.IEventBus
	cfg    Config
	logger *zap.Logger
	now    func() time.Time

	// joining are the files this core is joining now.
	joining sync.Map
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewService(files repository.IMinionFileRepository, tasks Tasks, store Store, bus eventbus.IEventBus, cfg Config, logger *zap.Logger) *Service {
	return &Service{files: files, tasks: tasks, store: store, bus: bus, cfg: cfg, logger: logger.Named("minionfiles"),
		now: func() time.Time { return time.Now().UTC() }, stop: make(chan struct{})}
}

func objectKey(fileID uuid.UUID) string { return "files/" + fileID.String() }

func stagingKey(fileID uuid.UUID, off int64) string {
	return fmt.Sprintf("staging/%s/%020d", fileID, off)
}

// Reply is core's answer to a chunk: how far it has the file, or a problem
// (an err item's code and message).
type Reply struct {
	Next int64
	Code string
	Msg  string
}

func refuse(code, msg string) Reply { return Reply{Code: code, Msg: msg} }

// TakeChunk takes one chunk of a transfer under session. A chunk past where
// the file is (one sent again, or out of order) is not kept: the answer says
// where to go on from.
func (s *Service) TakeChunk(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem) (Reply, error) {
	f, err := s.files.FindByXfer(ctx, session.SessionID, c.Xfer)
	if repository.IsNotFound(err) {
		if c.Off != 0 {
			return refuse(protocol.ErrItemBadMessage, "no such transfer: send it from 0"), nil
		}
		var reply *Reply
		if f, reply, err = s.begin(ctx, session, c); err != nil || reply != nil {
			if reply != nil {
				return *reply, err
			}
			return Reply{}, err
		}
	} else if err != nil {
		return Reply{}, fmt.Errorf("load the transfer: %w", err)
	}

	switch f.State {
	case models.MinionFileComplete, models.MinionFileJoining:
		return Reply{Next: f.Received}, nil
	case models.MinionFileFailed, models.MinionFileCancelled:
		return refuse(protocol.ErrItemBadMessage, "the transfer ended: "+f.Err), nil
	}
	off, n := int64(c.Off), int64(len(c.Data))
	if off != f.Received {
		return Reply{Next: f.Received}, nil
	}
	if off+n > f.Total {
		return refuse(protocol.ErrItemBadMessage, fmt.Sprintf("past the file's %d bytes", f.Total)), nil
	}
	last := off+n == f.Total
	if last && c.SHA256 == nil {
		return refuse(protocol.ErrItemBadMessage, "the last chunk says sha256"), nil
	}
	if n > 0 {
		if err := s.store.Put(ctx, stagingKey(f.FileID, off), bytes.NewReader(c.Data), n, "application/octet-stream"); err != nil {
			return Reply{}, fmt.Errorf("keep a chunk: %w", err)
		}
	}
	after, err := s.files.Advance(ctx, f.FileID, off, n, s.now())
	if repository.IsNotFound(err) {
		// Raced by the same chunk sent again: where it is now.
		now, err := s.files.FindByID(ctx, f.FileID)
		if err != nil {
			return Reply{}, err
		}
		return Reply{Next: now.Received}, nil
	}
	if err != nil {
		return Reply{}, fmt.Errorf("advance the transfer: %w", err)
	}
	if last {
		if after, err = s.files.Move(ctx, f.FileID, []string{models.MinionFileReceiving}, models.MinionFileJoining,
			bson.M{"sha256": c.SHA256}); err != nil {
			return Reply{}, fmt.Errorf("finish the transfer: %w", err)
		}
		s.goJoin(*after)
	}
	s.publish(after)
	return Reply{Next: after.Received}, nil
}

// begin starts a transfer from its first chunk: for a file_download task of
// the session, of a size taken.
func (s *Service) begin(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem) (*models.MinionFile, *Reply, error) {
	// 16 bytes: the chunk's Validate checked it.
	taskID, _ := uuid.FromBytes(c.Task)
	t, err := s.tasks.FindByTaskID(ctx, taskID)
	if repository.IsNotFound(err) {
		t, err = nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load the task: %w", err)
	}
	// A task the minion names that is not its session's: told, not an error.
	if t == nil || t.SessionID != session.SessionID {
		r := refuse(protocol.ErrItemUnknownTask, "no such task under this session")
		return nil, &r, nil
	}
	if t.Result != protocol.ResultFileDownload {
		r := refuse(protocol.ErrItemBadMessage, "the task's command does not send a file (result file_download)")
		return nil, &r, nil
	}
	if *c.Total > uint64(s.cfg.MaxSize) {
		r := refuse(protocol.ErrItemTooLarge, fmt.Sprintf("a file of %d bytes is over core's %d", *c.Total, s.cfg.MaxSize))
		return nil, &r, nil
	}
	f := &models.MinionFile{FileID: uuid.New(), OperationID: t.OperationID, SessionID: session.SessionID, TaskID: taskID,
		Xfer: c.Xfer, Name: c.Name, Total: int64(*c.Total), State: models.MinionFileReceiving, LastChunkAt: s.now()}
	if err := s.files.Insert(ctx, f); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			f, err = s.files.FindByXfer(ctx, session.SessionID, c.Xfer)
			return f, nil, err
		}
		return nil, nil, fmt.Errorf("start the transfer: %w", err)
	}
	s.logger.Info("minion file transfer started", zap.String("file_id", f.FileID.String()), zap.String("name", f.Name),
		zap.Int64("total", f.Total), zap.String("task_id", taskID.String()))
	return f, nil, nil
}

func (s *Service) goJoin(f models.MinionFile) {
	if _, busy := s.joining.LoadOrStore(f.FileID, true); busy {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.joining.Delete(f.FileID)
		s.join(context.Background(), f)
	}()
}

// join makes the file's object from its chunks, in order, checks it, and
// drops the chunks. What its first bytes say it is becomes its content type.
func (s *Service) join(ctx context.Context, f models.MinionFile) {
	pr, pw := io.Pipe()
	sum := sha256.New()
	head := &prefix{max: sniffLen}
	go func() {
		var off int64
		for off < f.Total {
			body, _, err := s.store.Get(ctx, stagingKey(f.FileID, off))
			if err != nil {
				pw.CloseWithError(fmt.Errorf("chunk at %d: %w", off, err))
				return
			}
			n, err := io.Copy(io.MultiWriter(pw, sum, head), body)
			_ = body.Close()
			if err == nil && n == 0 {
				err = errors.New("empty")
			}
			if err != nil {
				pw.CloseWithError(fmt.Errorf("chunk at %d: %w", off, err))
				return
			}
			off += n
		}
		_ = pw.Close()
	}()
	size, err := s.store.PutStream(ctx, objectKey(f.FileID), pr, "application/octet-stream")
	_ = pr.Close()
	var problem string
	switch {
	case err != nil:
		problem = "could not join its chunks: " + err.Error()
	case size != f.Total:
		problem = fmt.Sprintf("it is %d bytes, not the %d said", size, f.Total)
	case !bytes.Equal(sum.Sum(nil), f.SHA256):
		problem = "its SHA-256 is not the one the minion said"
	}
	now := s.now()
	var after *models.MinionFile
	if problem != "" {
		_ = s.store.Delete(ctx, objectKey(f.FileID))
		after, err = s.files.Move(ctx, f.FileID, []string{models.MinionFileJoining}, models.MinionFileFailed,
			bson.M{"err": problem, "done_at": now})
		s.logger.Warn("minion file did not check out", zap.String("file_id", f.FileID.String()), zap.String("problem", problem))
	} else {
		after, err = s.files.Move(ctx, f.FileID, []string{models.MinionFileJoining}, models.MinionFileComplete,
			bson.M{"done_at": now, "content_type": mimetype.Detect(head.b).String()})
		s.logger.Info("minion file kept", zap.String("file_id", f.FileID.String()), zap.Int64("size", size))
	}
	if err != nil {
		s.logger.Error("minion file: record the end", zap.String("file_id", f.FileID.String()), zap.Error(err))
		return
	}
	s.dropStaging(ctx, *after)
	s.publish(after)
}

// sniffLen is how much of a file its type is read from: mimetype's own
// default.
const sniffLen = 3072

// prefix keeps the first max bytes written to it.
type prefix struct {
	b   []byte
	max int
}

func (p *prefix) Write(b []byte) (int, error) {
	if room := p.max - len(p.b); room > 0 {
		p.b = append(p.b, b[:min(room, len(b))]...)
	}
	return len(b), nil
}

// dropStaging deletes a file's chunks: as far as it got, each the size it
// was kept at.
func (s *Service) dropStaging(ctx context.Context, f models.MinionFile) {
	var off int64
	for off < f.Received {
		info, err := s.store.Head(ctx, stagingKey(f.FileID, off))
		if err != nil {
			return
		}
		_ = s.store.Delete(ctx, stagingKey(f.FileID, off))
		if info.ContentLength <= 0 {
			return
		}
		off += info.ContentLength
	}
}

// AbortTask ends the file of a task that ended without it (stopped, or
// failed on the way): cancelled, its chunks dropped. Nothing for a file
// that ended already, or a task without one.
func (s *Service) AbortTask(ctx context.Context, taskID uuid.UUID, reason string) error {
	f, err := s.files.FindByTask(ctx, taskID)
	if repository.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.end(ctx, *f, models.MinionFileCancelled, reason)
}

func (s *Service) end(ctx context.Context, f models.MinionFile, state, reason string) error {
	after, err := s.files.Move(ctx, f.FileID, []string{models.MinionFileReceiving}, state, bson.M{"err": reason, "done_at": s.now()})
	if repository.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	s.dropStaging(ctx, *after)
	s.publish(after)
	return nil
}

// Open is a kept file's bytes.
func (s *Service) Open(ctx context.Context, fileID uuid.UUID) (io.ReadCloser, *models.MinionFile, error) {
	f, err := s.files.FindByID(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	if f.State != models.MinionFileComplete {
		return nil, f, fmt.Errorf("the file is %s", f.State)
	}
	body, _, err := s.store.Get(ctx, objectKey(f.FileID))
	return body, f, err
}

// File is a task's file, nil for none.
func (s *Service) File(ctx context.Context, taskID uuid.UUID) (*models.MinionFile, error) {
	f, err := s.files.FindByTask(ctx, taskID)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	return f, err
}

// FileByID is a file by its id, nil for none.
func (s *Service) FileByID(ctx context.Context, fileID uuid.UUID) (*models.MinionFile, error) {
	f, err := s.files.FindByID(ctx, fileID)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	return f, err
}

// DeleteByOperation drops an operation's files, bytes and all.
func (s *Service) DeleteByOperation(ctx context.Context, operationID uuid.UUID) error {
	files, err := s.files.ListByOperation(ctx, operationID)
	if err != nil {
		return err
	}
	for _, f := range files {
		_ = s.store.Delete(ctx, objectKey(f.FileID))
		s.dropStaging(ctx, f)
	}
	return s.files.DeleteByOperationID(ctx, operationID)
}

// Start sweeps now and every SweepEvery: idle transfers are given up,
// joins a stopped core left are taken up again.
func (s *Service) Start() {
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

func (s *Service) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
}

// Sweep gives up transfers idle for IdleAfter, and joins anew the files
// left joining for a while (a core stopped on the way).
func (s *Service) Sweep(ctx context.Context) {
	files, err := s.files.Idle(ctx, s.now().Add(-s.cfg.IdleAfter))
	if err != nil {
		s.logger.Error("minion files: sweep", zap.Error(err))
		return
	}
	for _, f := range files {
		switch f.State {
		case models.MinionFileReceiving:
			if err := s.end(ctx, f, models.MinionFileFailed, "no chunk came for "+s.cfg.IdleAfter.String()); err != nil {
				s.logger.Error("minion files: give up", zap.String("file_id", f.FileID.String()), zap.Error(err))
			}
		case models.MinionFileJoining:
			s.goJoin(f)
		}
	}
}

func (s *Service) publish(f *models.MinionFile) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: f.TaskID.String(), SessionID: f.SessionID.String(), OperationID: f.OperationID.String(),
	}))
}
