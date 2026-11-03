package minionsync

import (
	"context"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/logoscore/logos-core/core/pkg/minionstreams"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Files a minion streams (docs: contracts/minion-messages.md, Streaming a
// file): its chunks go to the streams service, answered by an ack that
// says where to send from, and whether to hold; every reply also carries
// the acks of the session's other streams (outbox), so a minion that holds
// hears when its downloader comes, catches up, or goes.

// Streams relays the files minions stream (minionstreams.Service).
type Streams interface {
	TakeChunk(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem) (minionstreams.Reply, bool, error)
	// Acks are the session's streams' acks, but those in answered; soon: a
	// stream is held while its downloader or uploader is there.
	Acks(ctx context.Context, session *models.MinionSession, answered map[string]bool) (acks []protocol.AckItem, soon bool, err error)
	// Pull answers a minion asking for a stream's chunk (uploads.go).
	Pull(ctx context.Context, t *models.MinionTask, next int64, room int) (*protocol.ChunkItem, *protocol.AckItem, error)
}

// FileBrowser keeps the snapshots of machines' file systems, from tasks'
// results (filebrowser.Service).
type FileBrowser interface {
	TaskEnded(ctx context.Context, t *models.MinionTask)
	Listings(ctx context.Context, t *models.MinionTask, session *models.MinionSession, pieces []protocol.ListingPiece, now time.Time)
}

// WithFileBrowser gives the service the file browser, told of each task's
// end; without it, nothing is.
func (s *Service) WithFileBrowser(fb FileBrowser) *Service {
	s.fileBrowser = fb
	return s
}

// WithStreams gives the service the streams. Without it, a stream's chunks
// go where a download's do, and fail there.
func (s *Service) WithStreams(streams Streams) *Service {
	s.streams = streams
	return s
}

// streamChunk takes a chunk of a stream; handled false for another's.
func (s *Service) streamChunk(ctx context.Context, session *models.MinionSession, c protocol.ChunkItem, i int, answered map[string]bool) ([]cbor.RawMessage, bool, error) {
	if s.streams == nil {
		return nil, false, nil
	}
	reply, handled, err := s.streams.TakeChunk(ctx, session, c)
	if err != nil {
		return nil, true, fmt.Errorf("take a stream's chunk: %w", err)
	}
	if !handled {
		return nil, false, nil
	}
	if reply.Code != 0 {
		out, err := errItem(reply.Code, reply.Msg, i)
		return out, true, err
	}
	answered[string(c.Xfer)] = true
	// The minion's ctx goes back on the ack that answers its chunk.
	ack := *reply.Ack
	ack.Ctx = c.Ctx
	item, err := protocol.MarshalCBOR(ack)
	if err != nil {
		return nil, true, err
	}
	return []cbor.RawMessage{item}, true, nil
}
