package minionsync

import (
	"bytes"
	"context"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Files core sends a minion (docs: contracts/minion-messages.md, Core
// sending a file): a task's file param names a task file, which the minion
// pulls. Its ack {xfer, next} says how far it has the file and asks for the
// chunk from there; the reply brings it, in what room the reply has left
// once its other items are in. An ack the reply has no room for is answered
// by none: the minion asks again.

// pull is an ack asking for a chunk: item i of the message. Its ctx goes
// back on what answers it (the chunk, or a stream's hold), so a minion that
// keeps nothing between messages finds there where the file goes.
type pull struct {
	xfer []byte
	next uint64
	ctx  []byte
	i    int
}

// chunkOverhead is room a chunk item takes besides its data: its keys, the
// ids, total and sha256 (and the message's own framing, generously).
const chunkOverhead = 512

// minChunk is the least worth sending: less room than that, and the ack
// is answered in the next reply.
const minChunk = 4 << 10

// serveUploads answers the pulls with chunks, in the room items leave.
func (s *Service) serveUploads(ctx context.Context, session *models.MinionSession, pulls []pull, items []cbor.RawMessage) ([]cbor.RawMessage, error) {
	used := 0
	for _, it := range items {
		used += len(it) + 8
	}
	var out []cbor.RawMessage
	for _, p := range pulls {
		// The chunk carries the pull's ctx back: its room too.
		item, err := s.serveUpload(ctx, session, p, protocol.MaxMinionMessageSize-used-chunkOverhead-len(p.ctx))
		if err != nil {
			return nil, err
		}
		for _, it := range item {
			used += len(it) + 8
		}
		out = append(out, item...)
	}
	return out, nil
}

// serveUpload answers one pull: a chunk of at most room bytes, an err, or
// nothing when there is no room.
func (s *Service) serveUpload(ctx context.Context, session *models.MinionSession, p pull, room int) ([]cbor.RawMessage, error) {
	t, err := s.tasks.FindByUploadXfer(ctx, session.SessionID, p.xfer)
	if repository.IsNotFound(err) {
		return errItem(protocol.ErrItemUnknownTask, "no task of this session sends a file in that transfer", p.i)
	}
	if err != nil {
		return nil, fmt.Errorf("load the transfer's task: %w", err)
	}
	up := t.Upload
	// The minion's last ack says it has all of the file; its task's end
	// may come in the same message, before it is read.
	if t.Finished() && (p.next != uint64(up.Size) || up.Acked == up.Size) {
		if p.next == uint64(up.Size) {
			return nil, nil
		}
		return errItem(protocol.ErrItemUnknownTask, "the transfer's task ended", p.i)
	}
	if (!up.Stream || up.Sized) && p.next > uint64(up.Size) {
		return errItem(protocol.ErrItemBadMessage, fmt.Sprintf("next is past the file's %d bytes", up.Size), p.i)
	}
	if up.Stream && p.next > uint64(up.Received) {
		return errItem(protocol.ErrItemBadMessage, fmt.Sprintf("next is past the %d bytes core sent", up.Received), p.i)
	}
	if (up.Stream && s.streams == nil) || (!up.Stream && s.files == nil) {
		return errItem(protocol.ErrItemUnknownItem, "no files are sent", p.i)
	}
	next := int64(p.next)
	if next != up.Acked || up.FirstAckAt == nil {
		after, err := s.tasks.UploadAcked(ctx, t.TaskID, next, s.now())
		if err != nil {
			return nil, fmt.Errorf("record the upload's progress: %w", err)
		}
		s.publishTask(after)
	}
	if up.Stream && p.ctx != nil && !bytes.Equal(p.ctx, up.Ctx) {
		// Kept for the acks core sends it unasked: its uploader's bytes came.
		if err := s.tasks.UploadCtx(ctx, t.TaskID, p.ctx); err != nil {
			return nil, fmt.Errorf("record the stream's ctx: %w", err)
		}
		up.Ctx = p.ctx
	}
	if up.Stream {
		// From the uploader's buffer: the chunk, or hold (minionstreams).
		c, a, err := s.streams.Pull(ctx, t, next, room)
		if err != nil {
			return nil, fmt.Errorf("a stream's chunk: %w", err)
		}
		var item []byte
		switch {
		case c != nil:
			c.Ctx = p.ctx
			item, err = protocol.MarshalCBOR(*c)
		case a != nil:
			a.Ctx = p.ctx
			item, err = protocol.MarshalCBOR(*a)
		default:
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []cbor.RawMessage{item}, nil
	}
	n := min(protocol.MaxChunkData, room, int(up.Size-next))
	if next == up.Size || n < min(minChunk, int(up.Size-next)) {
		// All there (the minion checks it and ends the task), or no room.
		return nil, nil
	}
	data, err := s.files.ReadChunk(ctx, up.FileID, next, n)
	if err != nil {
		s.logger.Warn("minion upload: the task file does not read", zap.String("task_id", t.TaskID.String()),
			zap.String("file_id", up.FileID.String()), zap.Error(err))
		return errItem(protocol.ErrItemBadMessage, "the file is gone from core", p.i)
	}
	c := protocol.ChunkItem{T: protocol.ItemChunk, Xfer: p.xfer, Off: p.next, Data: data, Ctx: p.ctx}
	if next == 0 {
		total := uint64(up.Size)
		c.Total, c.Task = &total, t.TaskID[:]
	}
	if next+int64(len(data)) == up.Size {
		c.SHA256 = up.SHA256
	}
	item, err := protocol.MarshalCBOR(c)
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

// endUpload checks a file_upload task's data against the file sent, and
// lets the task file go once no task needs it.
func (s *Service) endUpload(ctx context.Context, task *models.MinionTask, res protocol.ResultItem) {
	up := task.Upload
	if up == nil {
		return
	}
	if task.Result == protocol.ResultFileUpload && len(res.Data) > 0 {
		if problem := uploadProblem(*up, res); problem != "" {
			if after, err := s.tasks.SetDataProblem(ctx, task.TaskID, problem); err == nil {
				s.publishTask(after)
			}
		}
	}
	if s.files != nil {
		if err := s.files.ReleaseTaskFile(ctx, up.FileID); err != nil {
			s.logger.Error("minion upload: release the task file", zap.String("file_id", up.FileID.String()), zap.Error(err))
		}
	}
}

// uploadProblem is why a file_upload's data, as the minion sent it, is not
// the file core sent; "" when it is.
func uploadProblem(up models.MinionTaskUpload, res protocol.ResultItem) string {
	data, err := protocol.NamedResultData(protocol.ResultFileUpload, res.Data)
	if err != nil {
		return "" // taskdata's check says it
	}
	f, err := protocol.ReadFileUpload(res.Seq, data)
	switch {
	case err != nil:
		return "" // taskdata's check says it
	case !bytes.Equal(f.Xfer, up.Xfer):
		return "the data names another transfer"
	case f.Size != uint64(up.Size):
		return fmt.Sprintf("the minion wrote %d bytes, not the file's %d", f.Size, up.Size)
	case !bytes.Equal(f.SHA256, up.SHA256):
		return "the minion's SHA-256 is not the file's"
	}
	return ""
}
