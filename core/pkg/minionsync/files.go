package minionsync

import (
	"context"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Files a minion sends (docs: contracts/minion-messages.md, File
// transfers): its chunks go to the files service, and each is answered with
// an ack saying how far core has the file, or why it does not take it.

// Files takes the chunks of the files minions send.
type Files interface {
	TakeChunk(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem) (minionfiles.Reply, error)
	// AbortTask ends a task's file still coming, for a task that ended.
	AbortTask(ctx context.Context, taskID uuid.UUID, reason string) error
}

// WithFiles gives the service the files minions send. Without it, a chunk
// is answered as an item not taken.
func (s *Service) WithFiles(files Files) *Service {
	s.files = files
	return s
}

// takeChunk takes one chunk of a file the minion sends.
func (s *Service) takeChunk(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	var c protocol.ChunkItem
	if protocol.UnmarshalCBOR(raw, &c) != nil {
		return errItem(protocol.ErrItemBadMessage, "the chunk does not read", i)
	}
	if err := c.Validate(); err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), i)
	}
	reply, err := s.files.TakeChunk(ctx, session, c)
	if err != nil {
		return nil, fmt.Errorf("take a chunk: %w", err)
	}
	if reply.Code != "" {
		return errItem(reply.Code, reply.Msg, i)
	}
	next := uint64(reply.Next)
	data, err := protocol.MarshalCBOR(protocol.AckItem{T: protocol.ItemAck, Xfer: c.Xfer, Next: &next})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{data}, nil
}

// endFile ends the file of a task that ended before all of it came.
func (s *Service) endFile(ctx context.Context, task *models.MinionTask) {
	if s.files == nil || task.Result != protocol.ResultFileDownload {
		return
	}
	reason := "the task ended before all of its file came"
	if task.State != models.MinionTaskDone {
		reason = "the task " + map[string]string{models.MinionTaskCancelled: "was stopped", models.MinionTaskFailed: "failed"}[task.State]
		if task.Err != "" {
			reason += ": " + task.Err
		}
	}
	_ = s.files.AbortTask(ctx, task.TaskID, reason)
}
