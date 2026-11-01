package minionstreams

import (
	"context"
	"crypto/sha256"
	"encoding"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Streams core sends a minion (docs: contracts/minion-messages.md,
// Streaming a file to a minion): an upload_stream task's uploader PUTs the
// file (Write), core takes it in order into the same kind of buffer, at
// most a window ahead of the minion, and the minion's asks take it out
// (Pull). The uploader sets nothing of the pace but its own: core reads its
// body as the minion takes the chunks. Core hashes the bytes as they come,
// once each and in order, so the last chunk says the whole's SHA-256.

func upKey(taskID uuid.UUID, off int64) string {
	return fmt.Sprintf("mupload:%s:%d", taskID, off)
}

// OffsetError: the uploader sent a part from elsewhere than where the
// stream is (At).
type OffsetError struct{ At int64 }

func (e *OffsetError) Error() string { return fmt.Sprintf("the stream is at %d", e.At) }

// ErrSize: the uploader says a size other than the one it said, or sends
// past it.
var ErrSize = errors.New("not the stream's size")

// ErrNoSize: the uploader says no size, and none is known.
var ErrNoSize = errors.New("the stream's size is not known: say it (Content-Length, or Content-Range's total)")

// Upstream is a task's stream core sends, for an uploader; ErrGone when it
// is not one, or its task ended.
func (s *Service) Upstream(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error) {
	t, err := s.tasks.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Upload == nil || !t.Upload.Stream || t.Finished() {
		return t, ErrGone
	}
	return t, nil
}

// Write takes the uploader's bytes from start, the stream's size total (-1
// when this part does not say it), as the minion takes them: it returns
// how far the stream is when body ends, the task ends, or another uploader
// takes the stream.
func (s *Service) Write(ctx context.Context, taskID uuid.UUID, start, total int64, body io.Reader) (int64, error) {
	if !s.buf.IsEnabled() {
		return 0, errors.New("streams need core's Redis cache")
	}
	up := s.tasks
	t, err := s.Upstream(ctx, taskID)
	if err != nil {
		return 0, err
	}
	u := t.Upload
	switch {
	case start != u.Received:
		return u.Received, &OffsetError{At: u.Received}
	case u.Sized && total >= 0 && total != u.Size:
		return u.Received, ErrSize
	case !u.Sized && total < 0:
		return u.Received, ErrNoSize
	}
	if !u.Sized {
		if _, err = up.UploadSize(ctx, taskID, total); err != nil {
			return start, err
		}
	}
	uploader := uuid.NewString()
	t, err = up.UploadAttach(ctx, taskID, uploader, s.now())
	if err != nil {
		return start, err
	}
	u = t.Upload
	gen, size := u.Gen, u.Size
	s.logger.Info("minion stream uploader attached", zap.String("task_id", taskID.String()), zap.Int64("from", u.Received), zap.Int64("gen", gen))
	s.publish(t)
	// What the stream records does not hang on the uploader's connection.
	wctx := context.WithoutCancel(ctx)
	pos := u.Received
	// A part that ends as it should (its body all taken) keeps its
	// uploader: the next part comes in a moment, and an uploader that
	// sends none counts gone after ReaderGone. One cut short lets go now.
	clean := false
	defer func() {
		if clean {
			if after, err := up.FindByTaskID(wctx, taskID); err == nil {
				s.publish(after)
			}
			return
		}
		_ = up.UploadDetach(wctx, taskID, gen, s.now())
		if after, err := up.FindByTaskID(wctx, taskID); err == nil {
			s.publish(after)
		}
		s.logger.Info("minion stream uploader gone", zap.String("task_id", taskID.String()), zap.Int64("at", pos), zap.Int64("gen", gen))
	}()
	h := sha256.New()
	if len(u.HashState) > 0 {
		if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(u.HashState); err != nil {
			return pos, fmt.Errorf("the stream's hash: %w", err)
		}
	}

	buf := make([]byte, protocol.MaxChunkData)
	lastPublish := s.now()
	for pos < size {
		// Room in the window: the minion is not too far behind.
		cur, err := up.FindByTaskID(wctx, taskID)
		if err != nil {
			return pos, err
		}
		if cur.Finished() || cur.Upload.Gen != gen {
			return pos, nil
		}
		if pos-cur.Upload.Acked >= s.cfg.Window {
			select {
			case <-ctx.Done():
				return pos, ctx.Err()
			case <-time.After(s.cfg.Poll):
			}
			continue
		}
		// The minion has all core had: it holds, and these bytes are news.
		held := pos <= cur.Upload.Acked
		n, rerr := io.ReadFull(body, buf[:min(int64(len(buf)), size-pos)])
		if n > 0 {
			if err := s.buf.Set(wctx, upKey(taskID, pos), string(buf[:n]), s.cfg.ChunkTTL); err != nil {
				return pos, fmt.Errorf("buffer a chunk: %w", err)
			}
			h.Write(buf[:n])
			state, err := h.(encoding.BinaryMarshaler).MarshalBinary()
			if err != nil {
				return pos, err
			}
			var sum []byte
			if pos+int64(n) == size {
				sum = h.Sum(nil)
			}
			ours, err := up.UploadReceived(wctx, taskID, gen, pos, int64(n), state, sum, s.now())
			if err != nil || !ours {
				return pos, err
			}
			pos += int64(n)
			if held {
				s.wake(cur)
			}
			if s.now().Sub(lastPublish) >= time.Second || pos == size {
				if after, err := up.FindByTaskID(wctx, taskID); err == nil {
					s.publish(after)
				}
				lastPublish = s.now()
			}
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			// This part is over; the uploader may send the next.
			clean = true
			return pos, nil
		}
		if rerr != nil {
			return pos, rerr
		}
	}
	// All of it: anything more is past the size.
	if n, _ := body.Read(buf[:1]); n > 0 {
		return pos, ErrSize
	}
	clean = true
	return pos, nil
}

// Pull answers a minion asking for its stream's chunk from next: the chunk
// when the buffer has it (the one before let go), or hold. A chunk not come
// yet while an uploader is there is waited for, PullWait at most: the
// minion is faster than the upload, and a hold would idle it a check-in
// (a second) each time it catches up.
func (s *Service) Pull(ctx context.Context, t *models.MinionTask, next int64, room int) (*protocol.ChunkItem, *protocol.AckItem, error) {
	u := t.Upload
	if next > u.Acked {
		// The minion has the chunk before: no longer needed.
		_ = s.buf.Del(ctx, upKey(t.TaskID, u.Acked))
	}
	if u.Sized && next >= u.Size {
		return nil, nil, nil
	}
	if next >= u.Received && s.uploaderAlive(u) {
		if after, ok := s.awaitChunk(ctx, t.TaskID, next); ok {
			t, u = after, after.Upload
		}
	}
	if next < u.Received {
		data, err := s.buf.Get(ctx, upKey(t.TaskID, next))
		if err != nil {
			return nil, nil, err
		}
		if data != "" && len(data)+512 <= room {
			c := &protocol.ChunkItem{T: protocol.ItemChunk, Xfer: u.Xfer, Off: uint64(next), Data: []byte(data)}
			if next == 0 {
				total := uint64(u.Size)
				c.Total, c.Task, c.Name = &total, t.TaskID[:], u.Name
			}
			if next+int64(len(data)) == u.Size {
				c.SHA256 = u.SHA256
			}
			return c, nil, nil
		}
		if data != "" {
			// No room in this reply: the minion asks again.
			return nil, nil, nil
		}
	}
	a := s.upAck(u, next)
	return nil, &a, nil
}

// upAck is what the minion pulling a stream, which has it to at, is told:
// hold while core has nothing past that.
func (s *Service) upAck(u *models.MinionTaskUpload, at int64) protocol.AckItem {
	next := uint64(at)
	return protocol.AckItem{T: protocol.ItemAck, Xfer: u.Xfer, Next: &next, Hold: u.Received <= at && !(u.Sized && at >= u.Size)}
}

// uploaderAlive: the stream has an uploader that sent lately.
func (s *Service) uploaderAlive(u *models.MinionTaskUpload) bool {
	return u.Uploader != "" && u.UploaderAt != nil && u.UploaderAt.After(s.now().Add(-s.cfg.ReaderGone))
}

// awaitChunk waits, PullWait at most, for the chunk from next to come in
// the buffer: the task as it is then, ok when it came.
func (s *Service) awaitChunk(ctx context.Context, taskID uuid.UUID, next int64) (*models.MinionTask, bool) {
	deadline := time.Now().Add(s.cfg.PullWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(s.cfg.PullPoll):
		}
		if data, err := s.buf.Get(ctx, upKey(taskID, next)); err != nil || data == "" {
			continue
		}
		// Come: the task as it is now (its size, and the SHA-256 when the
		// chunk is the last), once the uploader recorded it, a moment after
		// it buffered it.
		t, err := s.tasks.FindByTaskID(ctx, taskID)
		if err != nil || t.Upload == nil {
			return nil, false
		}
		if t.Upload.Received > next {
			return t, true
		}
	}
	return nil, false
}
