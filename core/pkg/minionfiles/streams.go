package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// Copies of the files minions stream (minionstreams): a stream_file task's
// file within MaxSize is kept as core relays it, as a download's is, so the
// file browser and the task show it whatever became of the downloader's.
// Each chunk core takes for the downloader is staged here too, at the
// copy's end; the copy is joined once the last chunk comes, and checked
// against the file's SHA-256 when that chunk says it (a stream resumed from
// the middle may end without: the copy is kept with its own). A chunk sent
// again (a download resumed from earlier) adds only what the copy lacks;
// one past its end (resumed from further on) or a staging that fails ends
// the copy. Nothing here fails the stream: the downloader gets the file
// either way.

// KeepStream starts the copy of the file a stream task's minion announced
// (c: its first chunk, without data): none for a file over MaxSize. The
// same announce again leaves the copy as it is; a new one (another
// transfer) cancels the earlier copy and starts over.
func (s *Service) KeepStream(ctx context.Context, session *models.MinionSession, t *models.MinionTask, c protocol.ChunkItem) {
	if c.Total == nil {
		return
	}
	total := int64(*c.Total)
	log := s.logger.With(zap.String("task_id", t.TaskID.String()), zap.String("name", c.Name), zap.Int64("total", total))
	if total > s.cfg.MaxSize {
		log.Info("minion stream not kept: over the size cap", zap.Int64("max", s.cfg.MaxSize))
		return
	}
	if old, err := s.files.FindByTask(ctx, t.TaskID); err == nil {
		if bytes.Equal(old.Xfer, c.Xfer) {
			return
		}
		if err := s.end(ctx, *old, models.MinionFileCancelled, "the stream started over"); err != nil {
			log.Warn("minion stream copy: end the earlier one", zap.Error(err))
		}
	}
	f := &models.MinionFile{FileID: uuid.New(), OperationID: t.OperationID, SessionID: session.SessionID, TaskID: t.TaskID,
		Xfer: c.Xfer, Name: c.Name, Total: total, State: models.MinionFileReceiving, LastChunkAt: s.now()}
	if err := s.files.Insert(ctx, f); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			log.Warn("minion stream copy: start", zap.Error(err))
		}
		return
	}
	log.Info("minion stream kept as it passes", zap.String("file_id", f.FileID.String()))
	if total == 0 {
		// No chunk comes for an empty file: it is all there.
		empty := sha256.Sum256(nil)
		s.finishCopy(ctx, *f, empty[:])
		return
	}
	s.publish(f)
}

// StreamChunk adds a chunk core took for a stream's downloader to the
// stream's copy, if core keeps one.
func (s *Service) StreamChunk(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem) {
	f, err := s.files.FindByXfer(ctx, session.SessionID, c.Xfer)
	if err != nil || f.State != models.MinionFileReceiving {
		// Not kept (over the cap), or the copy ended.
		return
	}
	off, data := int64(c.Off), c.Data
	switch {
	case off+int64(len(data)) <= f.Received:
		// Had already: sent again after a resume.
		return
	case off > f.Received:
		s.giveUp(ctx, *f, models.MinionFileCancelled,
			fmt.Sprintf("the stream went on from byte %d, past the %d core had kept", off, f.Received))
		return
	case off < f.Received:
		// Partly had: what is new.
		data, off = data[f.Received-off:], f.Received
	}
	n := int64(len(data))
	if err := s.store.Put(ctx, stagingKey(f.FileID, off), bytes.NewReader(data), n, "application/octet-stream"); err != nil {
		s.giveUp(ctx, *f, models.MinionFileFailed, "could not keep a chunk: "+err.Error())
		return
	}
	after, err := s.files.Advance(ctx, f.FileID, off, n, s.now())
	if err != nil {
		// Raced by the same chunk: the copy is where the other left it.
		return
	}
	if after.Received == after.Total {
		s.finishCopy(ctx, *after, c.SHA256)
		return
	}
	s.publish(after)
}

// finishCopy joins a copy all of whose bytes came, checked against the
// file's SHA-256 when the stream said it (sum; nil: not said).
func (s *Service) finishCopy(ctx context.Context, f models.MinionFile, sum []byte) {
	set := bson.M{}
	if sum != nil {
		set["sha256"] = sum
	}
	after, err := s.files.Move(ctx, f.FileID, []string{models.MinionFileReceiving}, models.MinionFileJoining, set)
	if err != nil {
		s.logger.Warn("minion stream copy: finish", zap.String("file_id", f.FileID.String()), zap.Error(err))
		return
	}
	s.goJoin(*after)
	s.publish(after)
}

// giveUp ends a copy the stream goes on without.
func (s *Service) giveUp(ctx context.Context, f models.MinionFile, state, reason string) {
	s.logger.Info("minion stream copy given up", zap.String("file_id", f.FileID.String()), zap.String("reason", reason))
	if err := s.end(ctx, f, state, reason); err != nil {
		s.logger.Warn("minion stream copy: end", zap.String("file_id", f.FileID.String()), zap.Error(err))
	}
}
