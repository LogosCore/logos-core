package miniontunnels

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// What core keeps of a tunnel (docs: contracts/minion-messages/tunnels.md,
// What core keeps and shows). Its streams, in memory and only while its
// task runs: every open one and the latest KeptStreams ended ones, what its
// task's Streams tab lists. None is stored: a tunnel used for hours has
// streams by the hundred thousand (a scan through a proxy makes one a
// probe), and once its task ends what is left is its totals, kept with the
// task (models.MinionTaskTunnel). Its bytes so far are kept with its port
// or listener now and then, so a restart counts on from them.

// KeptStreams is how many of a running tunnel's ended streams core keeps to
// list, the latest; its open ones it always has.
const KeptStreams = 1000

// endedStreams is a tunnel's latest ended streams, the oldest giving way.
type endedStreams struct {
	recs []models.MinionTunnelStream
	// next is where the next one goes, once full.
	next int
}

func (e *endedStreams) add(rec models.MinionTunnelStream) {
	if len(e.recs) < KeptStreams {
		e.recs = append(e.recs, rec)
		return
	}
	e.recs[e.next] = rec
	e.next = (e.next + 1) % KeptStreams
}

// StreamFilter narrows a tunnel's streams: Search matches the tool's
// address and the destination, in the lists' query language
// (repository.SearchMatcher); State is StreamsOpen (not ended) or
// StreamsEnded, "" for both.
type StreamFilter struct {
	Search string
	State  string
}

// The streams' states a filter names.
const (
	StreamsOpen  = "open"
	StreamsEnded = "ended"
)

// Streams is a page of the task's streams, the latest first: limit at
// most, numbered below before (0: from the latest), as filter narrows
// them. A tunnel core does not hold (its task ended) has none.
func (s *Service) Streams(_ context.Context, task uuid.UUID, filter StreamFilter, before uint64, limit int64) ([]models.MinionTunnelStream, error) {
	match := func(string) bool { return true }
	if search := strings.TrimSpace(filter.Search); search != "" {
		match = repository.SearchMatcher(search)
	}
	out := []models.MinionTunnelStream{}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnels[task]
	if t == nil {
		return out, nil
	}
	take := func(rec models.MinionTunnelStream) {
		if (before == 0 || rec.Stream < before) && match(rec.SearchText) {
			out = append(out, rec)
		}
	}
	if filter.State != StreamsEnded {
		for _, st := range t.streams {
			rec := st.rec
			rec.Up, rec.Down = st.upBytes, st.downBytes
			take(rec)
		}
	}
	if filter.State != StreamsOpen {
		for _, rec := range t.ended.recs {
			take(rec)
		}
	}
	slices.SortFunc(out, func(a, b models.MinionTunnelStream) int { return compareUint(b.Stream, a.Stream) })
	if limit >= 0 && int64(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

// keepBytes keeps the bytes so far of each of the tunnels (nil: every one
// core holds) whose count moved since they were last kept, with its port
// or its listener: every sweep, and as core stops.
func (s *Service) keepBytes(ctx context.Context, tunnels []*tunnel) {
	type due struct {
		t        *tunnel
		up, down int64
		opened   uint64
	}
	s.mu.Lock()
	if tunnels == nil {
		for _, t := range s.tunnels {
			tunnels = append(tunnels, t)
		}
	}
	var dues []due
	for _, t := range tunnels {
		if t.up != t.keptUp || t.down != t.keptDown || t.opened != t.keptOpened {
			t.keptUp, t.keptDown, t.keptOpened = t.up, t.down, t.opened
			dues = append(dues, due{t: t, up: t.up, down: t.down, opened: t.opened})
		}
	}
	s.mu.Unlock()
	for _, d := range dues {
		var err error
		switch {
		case !d.t.reverse:
			err = s.ports.SetCounts(ctx, d.t.task, d.up, d.down, d.opened)
		case s.listeners != nil:
			err = s.listeners.SetCounts(ctx, d.t.task, d.up, d.down, d.opened)
		}
		if err != nil && !repository.IsNotFound(err) {
			s.logger.Warn("tunnels: keep a tunnel's bytes", zap.String("task_id", d.t.task.String()), zap.Error(err))
			// The next sweep tries again.
			s.mu.Lock()
			d.t.keptUp = -1
			s.mu.Unlock()
		}
	}
}
