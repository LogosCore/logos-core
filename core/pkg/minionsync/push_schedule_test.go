package minionsync

import (
	"context"
	"testing"
	"time"
)

// A tunnel's wake while a push is due later (a task's news, coalesced over
// notifyDelay) brings that push forward: the tunnel's bytes do not wait
// for the task's coalescing.
func TestWakeBringsADuePushForward(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := connected(t, r, sid, "c1")
	id := queueTask(r, sid, tasks)
	start := time.Now()
	r.svc.Nudge(sid, false) // due in notifyDelay
	r.svc.wake(sid)         // due in tunnelDelay
	for time.Since(start) < 3*notifyDelay {
		if n := carried(t, c.pushes(t), id); n > 0 {
			if took := time.Since(start); n != 1 || took >= notifyDelay {
				t.Fatalf("pushed %d times, after %s: the wake did not bring the push forward", n, took)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no push")
}

// While pushes fail, news does not bring the retry forward: a tunnel waking
// every 5 ms does not turn the retry's wait into a push every 5 ms.
func TestWakesKeepTheRetryWait(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := connected(t, r, sid, "c1")
	c.mu.Lock()
	c.fail = 1000
	c.mu.Unlock()
	queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid) // fails: the retry waits pushRetryWait
	for end := time.Now().Add(pushRetryWait * 7 / 10); time.Now().Before(end); {
		r.svc.wake(sid)
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	attempts := 1000 - c.fail
	c.mu.Unlock()
	if attempts > 2 {
		t.Fatalf("news defeated the retry's wait: %d pushes tried", attempts)
	}
}

// Pushes that failed on one connection hold nothing back on a connection
// new to the session: once the minion speaks over it, news goes at once.
func TestTheRetryWaitEndsOnANewConnection(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c1 := connected(t, r, sid, "c1")
	c1.mu.Lock()
	c1.fail = 1000
	c1.mu.Unlock()
	queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid) // fails: a retry waits
	c2 := connected(t, r, sid, "c2")        // its reply carries the first task
	c2.pushes(t)
	second := queueTask(r, sid, tasks)
	start := time.Now()
	r.svc.wake(sid)
	for time.Since(start) < pushRetryWait {
		if n := carried(t, c2.pushes(t), second); n > 0 {
			if took := time.Since(start); n != 1 || took >= pushRetryWait/2 {
				t.Fatalf("pushed %d times, after %s", n, took)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the push waited out the retry of another connection")
}

// A push due goes once for every ask: with the streams' acks when either
// asked for them, at the sooner time, never later.
func TestDuePushesMerge(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	due := func() (time.Duration, bool, bool) {
		r.svc.conns.mu.Lock()
		defer r.svc.conns.mu.Unlock()
		p := r.svc.conns.scheduled[sid]
		if p == nil {
			return 0, false, false
		}
		return time.Until(p.at), p.streams, true
	}
	r.svc.pushAfter(sid, true, time.Hour)
	r.svc.pushAfter(sid, false, 2*time.Hour)
	if in, streams, ok := due(); !ok || !streams || in > time.Hour {
		t.Fatalf("a later ask moved the push, or dropped its streams: in %s, streams %v", in, streams)
	}
	r.svc.pushAfter(sid, false, time.Minute)
	if in, streams, ok := due(); !ok || !streams || in > time.Minute {
		t.Fatalf("a sooner ask: in %s, streams %v", in, streams)
	}
	r.svc.conns.mu.Lock()
	r.svc.conns.scheduled[sid].timer.Stop()
	delete(r.svc.conns.scheduled, sid)
	r.svc.conns.mu.Unlock()
}
