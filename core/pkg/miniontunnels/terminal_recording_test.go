package miniontunnels

import (
	"context"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// fakeRecorder is a terminal session's recording, as it came.
type fakeRecorder struct {
	mu        sync.Mutex
	b         TerminalBeginning
	in, out   []byte
	sizes     []string
	end, msg  string
	ended     int
	afterEnds int
}

func (f *fakeRecorder) Output(b []byte) { f.add(&f.out, b) }
func (f *fakeRecorder) Input(b []byte)  { f.add(&f.in, b) }

func (f *fakeRecorder) add(to *[]byte, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended > 0 {
		f.afterEnds++
	}
	*to = append(*to, b...)
}

func (f *fakeRecorder) Resize(cols, rows int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, strings.Join([]string{itoa(cols), itoa(rows)}, "x"))
}

func (f *fakeRecorder) EndWith(end, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended++
	if f.ended == 1 {
		f.end, f.msg = end, msg
	}
}

func (f *fakeRecorder) Fail(msg string) { f.EndWith(models.RecordingEndNotOpened, msg) }

func (f *fakeRecorder) state() (end, msg, in, out string, sizes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.end, f.msg, string(f.in), string(f.out), append([]string(nil), f.sizes...)
}

// fakeRecordings records every session.
type fakeRecordings struct {
	mu   sync.Mutex
	recs []*fakeRecorder
}

func (f *fakeRecordings) BeginTerminal(_ context.Context, b TerminalBeginning) TerminalRecorder {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := &fakeRecorder{b: b}
	f.recs = append(f.recs, r)
	return r
}

func (f *fakeRecordings) get(t *testing.T, i int) *fakeRecorder {
	t.Helper()
	var r *fakeRecorder
	eventually(t, "a recording begun", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.recs) > i {
			r = f.recs[i]
		}
		return r != nil
	})
	return r
}

func itoa(n int) string { return strconv.Itoa(n) }

// Each session of a terminal is recorded: a raw port's client by its
// address, at the task's size, what it typed and what came back, ended as
// its process did, with its exit; a web terminal's by its operator, at its
// own size, its resizes, ended closed as the web app closed it.
func TestTerminalSessionsRecorded(t *testing.T) {
	r := newRig(t, 0, nil, aTerminal("bash"))
	recs := &fakeRecordings{}
	r.svc.WithTerminalRecordings(recs)
	r.tasks.ack(r.task)
	startMinion(t, r)

	c := dialPort(t, r.port)
	if _, err := c.Write([]byte("id\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "id\n")
	if _, err := c.Write([]byte("exit 3\n")); err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(c)
	_ = c.Close()
	raw := recs.get(t, 0)
	eventually(t, "the raw session's recording ends", func() bool { end, _, _, _, _ := raw.state(); return end != "" })
	end, msg, in, out, _ := raw.state()
	if raw.b.Task != r.task || raw.b.Stream != 1 || raw.b.Client != c.LocalAddr().String() || raw.b.User.ID != uuid.Nil ||
		raw.b.Cols != 100 || raw.b.Rows != 30 {
		t.Errorf("raw beginning %+v", raw.b)
	}
	if end != models.RecordingEndProcess || msg != "exit 3" || in != "id\nexit 3\n" || !strings.Contains(out, "id\n") {
		t.Errorf("raw: end %q %q, in %q, out %q", end, msg, in, out)
	}

	alice := TerminalUser{ID: uuid.New(), Name: "alice"}
	tm, err := r.svc.OpenTerminal(r.task, alice, 40, 120)
	if err != nil {
		t.Fatal(err)
	}
	web := recs.get(t, 1)
	if _, err := tm.Write([]byte("whoami\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, tm, "whoami\n")
	if err := tm.Resize(50, 200); err != nil {
		t.Fatal(err)
	}
	tm.Close()
	eventually(t, "the web session's recording ends", func() bool { end, _, _, _, _ := web.state(); return end != "" })
	end, _, in, out, sizes := web.state()
	if web.b.User != alice || web.b.Client != "web: alice" || web.b.Cols != 120 || web.b.Rows != 40 || web.b.Stream != 2 {
		t.Errorf("web beginning %+v", web.b)
	}
	if end != models.RecordingEndClosed || in != "whoami\n" || !strings.Contains(out, "whoami\n") || strings.Join(sizes, ",") != "200x50" {
		t.Errorf("web: end %q, in %q, out %q, sizes %v", end, in, out, sizes)
	}
	if raw.afterEnds != 0 || web.afterEnds != 0 || raw.ended != 1 || web.ended != 1 {
		t.Errorf("recorded after its end: %d %d; ended %d %d", raw.afterEnds, web.afterEnds, raw.ended, web.ended)
	}
}

// A process that does not start fails its recording, saying why as its
// client was told.
func TestTerminalRecordingNotStarted(t *testing.T) {
	r := newRig(t, 0, nil, aTerminal("missing"))
	recs := &fakeRecordings{}
	r.svc.WithTerminalRecordings(recs)
	r.tasks.ack(r.task)
	startMinion(t, r)
	c := dialPort(t, r.port)
	_, _ = io.ReadAll(c)
	rec := recs.get(t, 0)
	eventually(t, "its recording fails", func() bool { end, _, _, _, _ := rec.state(); return end != "" })
	if end, msg, _, _, _ := rec.state(); end != models.RecordingEndNotOpened || msg != "missing did not start: not_found: no such program" {
		t.Errorf("end %q %q", end, msg)
	}
}
