package miniontunnels

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// cancelled is the task's cancel, as core asked it: why, or "" for none.
func (f *fakeTasks) cancelled(id uuid.UUID) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	return t.CancelRequestedAt != nil, t.CancelReason
}

func quickSweep(c *Config) { c.Sweep = 20 * time.Millisecond }

func stopIdle(d time.Duration) func(*Opening) { return func(o *Opening) { o.StopIdle = d } }

// A tunnel no stream used for its stop_idle is stopped by core, saying
// why: its clock runs once the minion has the task, not before, and the
// stop closes its port as Stop does.
func TestIdleStop(t *testing.T) {
	r := newRig(t, 0, quickSweep, stopIdle(200*time.Millisecond))
	time.Sleep(300 * time.Millisecond)
	if asked, _ := r.tasks.cancelled(r.task); asked {
		t.Fatal("stopped before the minion had the task")
	}
	if sum, _ := r.svc.Summary(r.task); !sum.StopsAt.IsZero() {
		t.Fatalf("a clock before the ack: %+v", sum)
	}
	r.tasks.ack(r.task)
	eventually(t, "the clock", func() bool {
		sum, _ := r.svc.Summary(r.task)
		return !sum.StopsAt.IsZero()
	})
	eventually(t, "the stop", func() bool {
		asked, _ := r.tasks.cancelled(r.task)
		return asked
	})
	if _, why := r.tasks.cancelled(r.task); !strings.Contains(why, "no stream open for 200ms") {
		t.Fatalf("why %q", why)
	}
	eventually(t, "the port given back", func() bool { return !r.ports.held(r.task) })
}

// An open stream holds the clock, however long it stays quiet; once it
// ends, the clock starts again from then.
func TestIdleStreamHolds(t *testing.T) {
	r := newRig(t, 0, quickSweep, stopIdle(300*time.Millisecond))
	r.tasks.ack(r.task)
	startMinion(t, r)
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	if code != 0 {
		t.Fatalf("socks reply %d", code)
	}
	time.Sleep(600 * time.Millisecond)
	if asked, _ := r.tasks.cancelled(r.task); asked {
		t.Fatal("stopped with a stream open")
	}
	if sum, _ := r.svc.Summary(r.task); sum.Open != 1 || !sum.StopsAt.IsZero() {
		t.Fatalf("summary with a stream open: %+v", sum)
	}
	_ = c.Close()
	eventually(t, "the stream's end", func() bool {
		sum, _ := r.svc.Summary(r.task)
		return sum.Open == 0 && !sum.StopsAt.IsZero()
	})
	eventually(t, "the stop", func() bool {
		asked, _ := r.tasks.cancelled(r.task)
		return asked
	})
}

// A reverse port forward is stopped the same way, its listener forgotten;
// one with no stop_idle never is.
func TestIdleStopReverse(t *testing.T) {
	listeners := &fakeListeners{recs: map[uuid.UUID]models.MinionListener{}}
	tasks := &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}
	svc := newReverseService(t, listeners, tasks, &recordingBus{}, quickSweep)
	host, port := splitAddr(t, echo(t))
	idle, never, session := uuid.New(), uuid.New(), uuid.New()
	for _, o := range []Opening{
		{Task: idle, StopIdle: 150 * time.Millisecond},
		{Task: never},
	} {
		tasks.tasks[o.Task] = models.MinionTask{TaskID: o.Task, SessionID: session, State: models.MinionTaskSent,
			Action: protocol.ActionReverseTunnel}
		o.Session, o.Bind, o.Listen, o.Host, o.HostPort = session, "0.0.0.0", 8080, host, port
		if err := svc.OpenReverse(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		tasks.ack(o.Task)
	}
	eventually(t, "the stop", func() bool {
		asked, _ := tasks.cancelled(idle)
		return asked
	})
	eventually(t, "the listener forgotten", func() bool {
		_, ok := listeners.get(idle)
		return !ok
	})
	time.Sleep(200 * time.Millisecond)
	if asked, _ := tasks.cancelled(never); asked {
		t.Fatal("a tunnel with no stop_idle stopped")
	}
	if sum, ok := svc.Summary(never); !ok || !sum.StopsAt.IsZero() {
		t.Fatalf("no stop_idle: %+v %v", sum, ok)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Hour: "1h", 90 * time.Minute: "1h30m", 30 * time.Minute: "30m", 45 * time.Second: "45s", 200 * time.Millisecond: "200ms",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
}
