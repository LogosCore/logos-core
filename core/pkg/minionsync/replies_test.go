package minionsync

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A check-in sent again (its reply lost on the way) gets the reply it got:
// the task it carried is not sent a second time. One core had nothing to
// say to gets nothing again (the sync call's 204), and one whose reply was
// too large to keep is a replay.
func TestReplyKeptForAMessageSentAgain(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	task := uuid.New()
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: task, SessionID: sid, OperationID: r.build.OperationID,
		Command: "shell", Op: 11, State: models.MinionTaskQueued})
	one := uint64(1)
	id := protocol.NewMessageID()
	first, err := r.sendUnder(t, sid, r.keys, id, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &one})
	if err != nil || len(taskIDsIn(t, r.openUnder(t, sid, r.keys, first))) != 1 {
		t.Fatalf("the check-in: %v", err)
	}
	again, err := r.sendUnder(t, sid, r.keys, id, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &one})
	if err != nil || !bytes.Equal(again, first) {
		t.Fatalf("sent again: %v, the same reply %v", err, bytes.Equal(again, first))
	}
	if n := tasks.find(task).SendCount; n != 1 {
		t.Fatalf("the task sent %d times", n)
	}

	// Nothing to say: nothing again.
	quiet := protocol.NewMessageID()
	if reply, err := r.sendUnder(t, sid, r.keys, quiet); err != nil || reply != nil {
		t.Fatalf("a check-in with nothing back: %x %v", reply, err)
	}
	if reply, err := r.sendUnder(t, sid, r.keys, quiet); err != nil || reply != nil {
		t.Fatalf("sent again: %x %v", reply, err)
	}

	// A reply too large to keep: the message again is a replay.
	r.svc.keepReply(context.Background(), []byte("too-large-message"), make([]byte, replyKeepMax+1))
	if _, kept := r.svc.keptReply(context.Background(), []byte("too-large-message")); kept {
		t.Fatal("a reply over replyKeepMax kept")
	}
}

// Over a connection socket too, a request sent again gets its reply.
func TestReplyKeptOverASocket(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, hello("ws01"))
	// One message, sent twice.
	frame := sealedMinionMessage(t, r.keys, sid, nil, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	a, errA := r.svc.Exchange(ctx, c, sid.String(), "10.0.0.9", frame)
	b, errB := r.svc.Exchange(ctx, c, sid.String(), "10.0.0.9", frame)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		t.Fatalf("sent again over the socket: %v %v, the same reply %v", errA, errB, bytes.Equal(a, b))
	}
	r.forgetReplies()
	if _, err := r.svc.Exchange(ctx, c, sid.String(), "10.0.0.9", frame); !errors.Is(err, ErrReplayed) {
		t.Fatalf("a replay: %v", err)
	}
}

// A minion that says no schedule is taken to keep a day's where its
// check-ins say whether it is there; a page that says none is never late,
// and a bind-persistent connection says it itself.
func TestCheckInWithoutASchedule(t *testing.T) {
	now := time.Now()
	for _, kind := range []string{protocol.KindReverseCall, protocol.KindReversePersistent, ""} {
		c := checkIn(now, "", nil, nil, kind)
		if c.LateAt == nil || c.LostAt == nil || !c.LateAt.Equal(now.Add(24*time.Hour+models.SessionLateGrace)) ||
			!c.LostAt.Equal(now.Add(3*24*time.Hour+models.SessionLateGrace)) {
			t.Errorf("%q: late %v, lost %v", kind, c.LateAt, c.LostAt)
		}
	}
	if c := checkIn(now, "", nil, nil, protocol.KindBindCall); c.LateAt != nil || c.Untimed {
		t.Errorf("a page without a schedule: %+v", c)
	}
	if c := checkIn(now, "", nil, nil, protocol.KindBindPersistent); !c.Untimed {
		t.Errorf("bind-persistent: %+v", c)
	}
}
