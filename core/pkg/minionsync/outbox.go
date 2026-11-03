package minionsync

import (
	"context"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// What core has for a session, and how it reaches the minion (docs:
// contracts/minion-messages/, Mechanics by connection kind).
//
// Everything core starts sending a minion comes from one place, outbox:
// cancels, the operator's limit of tasks, resumes, the streams' acks and
// tasks. The paths a message takes
// differ only in what one message may carry (a delivery) and in when it
// goes (Nudge):
//
//   - the reply to a message the minion sent, through the sync call or
//     over a connection socket (underSession);
//   - a push over a persistent connection (Notify);
//   - a call core makes to a bind-call page (sessionCalls).
//
// A mechanic that gives core something new to send a minion adds it to
// outbox, and nudges the session when it has it: it then reaches minions on
// every kind of connection. What answers a minion's own items (an ack for a
// chunk, the chunk a pull asks for, a resume for a part) goes with the
// reply to them, before the outbox's items.

// path is how a message core sends goes.
type path int

const (
	// pathSync: the reply to a minion's message through the sync call
	// (reverse-call).
	pathSync path = iota
	// pathSocket: the reply to a minion's message over a connection
	// socket, or a push there (reverse-persistent, bind-persistent).
	pathSocket
	// pathCall: a call core makes to a bind-call page.
	pathCall
)

// delivery is what one message may carry of the outbox.
type delivery struct {
	// tasks is how many tasks at most; 0 none.
	tasks int
	// resumes: the resumes of tasks under way that said a ctx. A call
	// carries them every time: the page keeps nothing, and a call that
	// failed lost the one before. A minion that starts its exchanges sends
	// a part again until it is answered, and gets its resume then.
	resumes bool
	// streams: the session's streams' acks, but those in answered (whose
	// chunks the message answers already). A push carries them only when
	// a stream may go on (Nudge's streams): the minion holds then, so the
	// ack cannot cross a chunk of its.
	streams  bool
	answered map[string]bool
	// room is how many bytes the outbox's items may take.
	room int
}

// outcome is what the outbox gave one message.
type outcome struct {
	items []cbor.RawMessage
	// fresh: something the minion has not had (a cancel, a task never
	// sent, a resume): a call goes for it.
	fresh bool
	// going: a stream's ack says go on: a call goes for it.
	going bool
	// soon: a stream is held while its downloader or uploader is there: a
	// minion that starts its exchanges is told to come back soon (a poll).
	soon bool
	// used is how many bytes the items took.
	used int
}

const (
	// messageOverhead is what a message takes besides its items: v, id or
	// re, at, the items' array (generously).
	messageOverhead = 64
	// itemOverhead is what each item adds to the items' array.
	itemOverhead = 8
	// pollSoon is how soon a minion with a stream about to go on is told
	// to come back.
	pollSoon = time.Second
)

// itemsSize is how much items take in a message.
func itemsSize(items []cbor.RawMessage) int {
	n := 0
	for _, it := range items {
		n += len(it) + itemOverhead
	}
	return n
}

// roomFor is the room a message of the session's connection leaves for
// items once answers are in.
func roomFor(limit int, answers []cbor.RawMessage) int {
	return max(limit-messageOverhead-itemsSize(answers), 0)
}

// outbox is what goes to the session's minion in one message, as d allows,
// most urgent first: cancels, the operator's limit of tasks (a max_tasks,
// in every message until the minion says it has it: whatever it runs, as a
// cancel goes), resumes, the streams' acks, then tasks (those sent and not
// acknowledged first). What does not fit waits for the next
// message; a task too large for any message of the connection ends failed,
// saying so.
func (s *Service) outbox(ctx context.Context, session *models.MinionSession, d delivery) (outcome, error) {
	var o outcome
	if s.tasks == nil {
		return o, nil
	}
	room := d.room
	add := func(item cbor.RawMessage) bool {
		n := len(item) + itemOverhead
		if n > room {
			return false
		}
		room -= n
		o.items = append(o.items, item)
		return true
	}
	cancels, err := s.cancels(ctx, session)
	if err != nil {
		return o, err
	}
	for _, c := range cancels {
		if !add(c) {
			break
		}
		o.fresh = true
	}
	// A limit sent with MaxTasksTries messages and never answered: the
	// minion does not take it, and the max_tasks commands for it end so.
	if session.MaxTasksNotTaken() != nil {
		s.endMaxTasksCommands(ctx, session)
	}
	if due := session.MaxTasksDue(); due != nil {
		item, err := protocol.MarshalCBOR(protocol.MaxTasksItem{T: protocol.ItemMaxTasks, Max: uint64(max(*due, 0))})
		if err != nil {
			return o, err
		}
		if add(item) {
			o.fresh = true
			// Counted: a minion that does not take it is not sent it for
			// ever (models.MaxTasksTries).
			if err := s.sessions.MaxTasksSent(ctx, session.SessionID); err != nil {
				return o, fmt.Errorf("count a max_tasks: %w", err)
			}
			session.MaxTasksSends++
		}
	}
	if d.resumes {
		resumes, err := s.resumes(ctx, session)
		if err != nil {
			return o, err
		}
		for _, r := range resumes {
			if !add(r) {
				break
			}
			o.fresh = true
		}
	}
	if s.streams != nil {
		acks, soon, err := s.streams.Acks(ctx, session, d.answered)
		if err != nil {
			return o, fmt.Errorf("streams' acks: %w", err)
		}
		o.soon = soon
		if d.streams {
			for _, a := range acks {
				item, err := protocol.MarshalCBOR(a)
				if err != nil {
					return o, err
				}
				if !add(item) {
					break
				}
				o.going = o.going || !a.Hold
			}
		}
	}
	if d.tasks > 0 {
		fresh, err := s.taskItems(ctx, session, d.tasks, add)
		if err != nil {
			return o, err
		}
		o.fresh = o.fresh || fresh
	}
	o.used = d.room - room
	return o, nil
}

// taskItems adds up to limit of the tasks waiting for the session through
// add, those sent and not acknowledged first, and marks those added sent;
// fresh when one was never sent. A task whose item no message of the
// session's connection could carry ends failed.
func (s *Service) taskItems(ctx context.Context, session *models.MinionSession, limit int, add func(cbor.RawMessage) bool) (fresh bool, err error) {
	waiting, err := s.tasks.ForDelivery(ctx, session.SessionID, limit)
	if err != nil {
		return false, fmt.Errorf("tasks to deliver: %w", err)
	}
	most := roomFor(session.MessageLimit(), nil)
	var sent []uuid.UUID
	var went []*models.MinionTask
	for i := range waiting {
		t := &waiting[i]
		item, err := taskItem(t)
		if err != nil {
			return false, err
		}
		if len(item)+itemOverhead > most {
			if err := s.tooLarge(ctx, session, t, len(item)); err != nil {
				return false, err
			}
			continue
		}
		if !add(item) {
			break
		}
		fresh = fresh || t.State == models.MinionTaskQueued
		sent, went = append(sent, t.TaskID), append(went, t)
	}
	if len(sent) == 0 {
		return fresh, nil
	}
	if err := s.tasks.MarkSent(ctx, sent, s.now()); err != nil {
		return false, fmt.Errorf("mark tasks sent: %w", err)
	}
	for _, t := range went {
		s.publishTask(t)
	}
	return fresh, nil
}

// taskItem is the task item of t: its command and args by number.
func taskItem(t *models.MinionTask) (cbor.RawMessage, error) {
	var args protocol.Args
	if len(t.WireArgs) > 0 {
		if err := protocol.UnmarshalCBOR(t.WireArgs, &args); err != nil {
			return nil, fmt.Errorf("the args of task %s: %w", t.TaskID, err)
		}
	}
	item, err := protocol.MarshalCBOR(protocol.TaskItem{T: protocol.ItemTask, Task: t.TaskID[:], Cmd: uint64(t.Op), Args: args})
	if err != nil {
		return nil, fmt.Errorf("a task item: %w", err)
	}
	return item, nil
}

// tooLarge ends a task no message of the session's connection can carry.
func (s *Service) tooLarge(ctx context.Context, session *models.MinionSession, t *models.MinionTask, size int) error {
	why := fmt.Sprintf("too large for the connection: the task is %d bytes, its messages carry %d", size, session.MessageLimit())
	after, err := s.tasks.FailUndelivered(ctx, session.SessionID, t.TaskID, why, s.now())
	if err != nil {
		if repository.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("fail a task too large to send: %w", err)
	}
	s.logger.Info("minion task too large for its session's connection", zap.String("task_id", t.TaskID.String()),
		zap.Int("size", size), zap.Int("limit", session.MessageLimit()))
	s.publishTask(after)
	return nil
}

// Nudge says core has something new for the session's minion: what changed
// (a task queued, cancelled, a part taken), or, with streams, that a stream
// it holds may go on. It goes the way the minion's connection allows: a
// push over a persistent connection serving the session, a call to its
// bind-call page, an open of its bind-persistent target as the target's
// policy says; nothing for a minion that starts its exchanges, which hears
// it in the reply to its next one. It returns at once, and coalesces what
// comes within a moment.
func (s *Service) Nudge(session uuid.UUID, streams bool) {
	if s.Connected(session) {
		s.pushSoon(session, streams)
		return
	}
	if s.targets == nil {
		return
	}
	t, err := s.targets.FindBySessionID(context.Background(), session)
	if err != nil {
		return
	}
	if streams {
		s.callSoon(t.TargetID, bindWake, bindWakeDelay)
	} else {
		s.callSoon(t.TargetID, bindNews, bindCoalesce)
	}
}

// WakeStreams: a stream the session's minion holds may go on (a downloader
// came, made room or has it all; an uploader's bytes came). The streams
// service's Waker.
func (s *Service) WakeStreams(session uuid.UUID) { s.Nudge(session, true) }

// unixNow is t as a core message's at: unix seconds.
func unixNow(t time.Time) uint64 { return uint64(max(t.Unix(), 0)) }

// pollItem is the poll a reply carries to a minion that starts its
// exchanges, when something of its may go on soon.
func pollItem() (cbor.RawMessage, error) {
	return protocol.MarshalCBOR(protocol.PollItem{T: protocol.ItemPoll, Within: uint64(pollSoon.Milliseconds())})
}
