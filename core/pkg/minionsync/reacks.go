package minionsync

import (
	"context"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Tasks a minion lost (docs: contracts/minion-messages/tasks-and-results/,
// Tasks the minion lost). A minion back as a new process (a claim of its
// persisted session), or over a new bind-persistent connection (a channel
// acting as the minion runs its tasks inside the connection, which went),
// may not have the tasks core thinks it runs. Core asks it with a reack
// naming them, in its next message to it: the minion acks those it has,
// which go on (over a bind-persistent connection they are the new one's
// now: carry), and answers unknown_task for each it does not, which core
// ends (endUnknown). A minion that does not take a reack answers
// unknown_item, and nothing changes. A reack goes once: lost with its
// message, it leaves the tasks as they were.

const (
	// reackAsk is how many of a session's tasks core asks about at once.
	reackAsk = 4 * protocol.MaxReackTasks
	// reackKeep is how long a question waits for a message to carry it.
	reackKeep = 24 * time.Hour
	// reackIDSize is what each task id adds to a reack item (a 16-byte
	// byte string's head and bytes), reackHead what the item takes besides.
	reackIDSize = 17
	reackHead   = 8
)

// reacks are the tasks core asks the minions about, by session, until a
// message carries them.
type reacks struct {
	mu        sync.Mutex
	bySession map[uuid.UUID]reackWait
}

// reackWait is a session's question: the tasks, and when it was asked.
type reackWait struct {
	tasks []uuid.UUID
	at    time.Time
}

func newReacks() *reacks { return &reacks{bySession: map[uuid.UUID]reackWait{}} }

// askTasks has core ask the session's minion, in its next message to it,
// which of the tasks it runs (as far as core knows) it still has. why says
// what brought the minion back, for the log.
func (s *Service) askTasks(ctx context.Context, session uuid.UUID, why string) {
	if s.tasks == nil {
		return
	}
	ids, err := s.tasks.RunningIDs(ctx, session, reackAsk)
	if err != nil {
		s.logger.Warn("minion tasks: the tasks to ask about", zap.String("session_id", session.String()), zap.Error(err))
		return
	}
	if len(ids) == 0 {
		return
	}
	now := s.now()
	s.reacks.mu.Lock()
	for id, w := range s.reacks.bySession {
		if now.Sub(w.at) > reackKeep {
			delete(s.reacks.bySession, id)
		}
	}
	s.reacks.bySession[session] = reackWait{tasks: ids, at: now}
	s.reacks.mu.Unlock()
	s.logger.Info("minion tasks: asking the minion which it still has", zap.String("session_id", session.String()),
		zap.Int("tasks", len(ids)), zap.String("why", why))
	// A message of its own when the minion waits for one (a push): a reply
	// to the minion's message carries it anyway.
	s.Nudge(session, false)
}

// reackItem is the reack a message of the session carries in room, and the
// tasks it names; nil when core asks nothing, or none fit.
func (s *Service) reackItem(session uuid.UUID, room int) (cbor.RawMessage, []uuid.UUID, error) {
	s.reacks.mu.Lock()
	w := s.reacks.bySession[session]
	s.reacks.mu.Unlock()
	n := min(len(w.tasks), protocol.MaxReackTasks, (room-itemOverhead-reackHead)/reackIDSize)
	if n < 1 {
		return nil, nil, nil
	}
	ids := w.tasks[:n]
	tasks := make([][]byte, n)
	for i, id := range ids {
		tasks[i] = id[:]
	}
	item, err := protocol.MarshalCBOR(protocol.ReackItem{T: protocol.ItemReack, Tasks: tasks})
	return item, ids, err
}

// reacked records that a message carries the question about ids: those
// are asked.
func (s *Service) reacked(session uuid.UUID, ids []uuid.UUID) {
	s.reacks.mu.Lock()
	defer s.reacks.mu.Unlock()
	w, ok := s.reacks.bySession[session]
	if !ok {
		return
	}
	asked := map[uuid.UUID]bool{}
	for _, id := range ids {
		asked[id] = true
	}
	left := w.tasks[:0:0]
	for _, id := range w.tasks {
		if !asked[id] {
			left = append(left, id)
		}
	}
	if len(left) == 0 {
		delete(s.reacks.bySession, session)
		return
	}
	s.reacks.bySession[session] = reackWait{tasks: left, at: w.at}
}

// asksFor records that session's messages come over c: true the first
// time, when core asks the minion which tasks it still has.
func (c *Conn) asksFor(session uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked == session {
		return false
	}
	c.asked = session
	return true
}
