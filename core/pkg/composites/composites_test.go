package composites

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func ptr[T any](v T) *T { return &v }

// The docs' archive: ls, its own op, download (with a timer), rm, finish;
// each call's end as the factory reads it.
func TestComposeArchive(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": `C:\docs`})
	zip := `C:\t\docs.zip`
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event == protocol.ComposeStart {
			return protocol.Decision{Run: []protocol.Subtask{{Command: "ls", Args: map[string]any{"path": req.Args["path"]}}}}
		}
		e := req.End
		switch {
		case e.Command == "ls":
			return protocol.Decision{OwnOp: &protocol.OwnOpRun{}, Status: ptr("archiving")}
		case e.OwnOp:
			ctx := []byte(`{"archive":"x"}`)
			return protocol.Decision{Run: []protocol.Subtask{{Command: "download", Args: map[string]any{"path": zip}, Key: "fetch",
				As: "fetch the archive"}}, After: "10m", Ctx: &ctx}
		case e.Key == "fetch":
			return protocol.Decision{Run: []protocol.Subtask{{Command: "rm", Args: map[string]any{"path": zip}}}}
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: true, Message: "archived"}}
	})

	r.step(id)
	start := r.fac.last(t)
	if start.Seq != 1 || start.Event != protocol.ComposeStart || start.Args["path"] != `C:\docs` || start.OwnOp != protocol.OwnOpNotRun ||
		start.Actor == nil || start.Actor.Name != "alice" || start.Platform != "windows/amd64" || start.OS != "windows" ||
		!strings.HasPrefix(start.UploadURL, UploadPath(id)+"?expires=") {
		t.Fatalf("start call: %+v", start)
	}
	subs := r.subtasks(id)
	if len(subs) != 1 || subs[0].Command != "ls" || subs[0].State != models.MinionTaskQueued || subs[0].IssuedBy != r.task(id).IssuedBy {
		t.Fatalf("after start: %+v", subs)
	}

	r.end(subs[0].TaskID, true, "3 entries", map[string]any{"entries": 3})
	r.step(id)
	ls := r.fac.last(t)
	if ls.Seq != 2 || ls.Event != protocol.ComposeEnded || ls.End.Command != "ls" || ls.End.State != protocol.EndDone || !ls.End.OK ||
		string(ls.End.Out) != "3 entries" || !jsonEqual(ls.End.Data, `{"entries":3}`) || ls.SubtasksEnded != 1 || ls.SubtasksUnderWay != 0 {
		t.Fatalf("ls's end: %+v", ls)
	}
	task := r.task(id)
	if task.State != models.MinionTaskQueued || task.Composite.OwnOp != string(protocol.OwnOpOut) || task.Composite.Status != "archiving" {
		t.Fatalf("own op not sent: %s %+v", task.State, task.Composite)
	}

	r.running(id)
	r.end(id, true, "archived 12 files", map[string]any{"archive": zip})
	r.step(id)
	own := r.fac.last(t)
	if own.Seq != 3 || !own.End.OwnOp || own.End.TaskID != id.String() || own.OwnOp != protocol.OwnOpEnded || own.End.State != protocol.EndDone ||
		string(own.End.Out) != "archived 12 files" {
		t.Fatalf("own op's end: %+v", own)
	}
	task = r.task(id)
	if task.State != models.MinionTaskComposing || task.Composite.TimerAt == nil || string(task.Composite.Ctx) != `{"archive":"x"}` {
		t.Fatalf("after its own op: %s %+v", task.State, task.Composite)
	}
	var fetch models.MinionTask
	for _, s := range r.subtasks(id) {
		if s.Command == "download" {
			fetch = s
		}
	}
	if fetch.SubtaskKey != "fetch" || fetch.SubtaskAs != "fetch the archive" {
		t.Fatalf("download: %+v", fetch)
	}

	file := &models.MinionFile{FileID: uuid.New(), TaskID: fetch.TaskID, Name: zip, Total: 5, SHA256: sha([]byte("hello")),
		State: models.MinionFileComplete, ContentType: "application/zip"}
	r.files.add(file, []byte("hello"))
	r.end(fetch.TaskID, true, "", map[string]any{"size": 5})
	r.step(id)
	got := r.fac.last(t)
	if got.End.Key != "fetch" || len(got.End.Files) != 1 || got.End.Files[0].Name != "docs.zip" || got.End.Files[0].Size != 5 ||
		string(got.Ctx) != `{"archive":"x"}` {
		t.Fatalf("download's end: %+v", got)
	}
	link, err := url.Parse(got.End.Files[0].URL)
	if err != nil || link.Path != FilePath(fetch.TaskID, file.FileID) {
		t.Fatalf("file link %q", got.End.Files[0].URL)
	}
	if err := r.svc.CheckLink(link.Path, link.Query().Get("expires"), link.Query().Get("sig")); err != nil {
		t.Fatalf("the file's link does not check: %v", err)
	}
	if r.task(id).Composite.TimerAt != nil {
		t.Fatal("the timer goes on past the next call")
	}
	body, _, err := r.svc.File(context.Background(), fetch.TaskID, file.FileID)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	_ = body.Close()

	var rm models.MinionTask
	for _, s := range r.subtasks(id) {
		if s.Command == "rm" {
			rm = s
		}
	}
	r.end(rm.TaskID, true, "", nil)
	r.step(id)
	task = r.task(id)
	if task.State != models.MinionTaskDone || task.OK == nil || !*task.OK || task.Composite.Status != "archived" {
		t.Fatalf("finished: %s %+v", task.State, task.Composite)
	}
	if n := len(r.fac.requests()); n != 5 {
		t.Fatalf("%d calls, want 5", n)
	}
}

// Subtasks past those under way at once wait in the task, and go as
// others end; one that fails core's checks ends refused, and the factory
// hears it as an end.
func TestComposeLimitsAndRefusals(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.q.refuse["rm"] = "path: required"
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event != protocol.ComposeStart {
			return protocol.Decision{After: "1h"}
		}
		run := []protocol.Subtask{{Command: "rm", Key: "bad"}}
		for range 40 {
			run = append(run, protocol.Subtask{Command: "ls", Args: map[string]any{"path": "/"}})
		}
		return protocol.Decision{Run: run}
	})
	r.step(id)
	open, _ := r.st.CountOpenSubtasks(context.Background(), id)
	task := r.task(id)
	if open != protocol.MaxSubtasksUnderWay || len(task.Composite.Waiting) != 41-1-protocol.MaxSubtasksUnderWay || task.Composite.Subtasks != 41 {
		t.Fatalf("open %d, waiting %d, asked %d", open, len(task.Composite.Waiting), task.Composite.Subtasks)
	}
	refused := r.fac.requests()[1]
	if refused.End == nil || refused.End.State != protocol.EndRefused || refused.End.TaskID != "" || refused.End.Err != "path: required" ||
		refused.End.Key != "bad" {
		t.Fatalf("refused end: %+v", refused.End)
	}
	subs := r.subtasks(id)
	r.end(subs[0].TaskID, true, "", nil)
	r.step(id)
	open, _ = r.st.CountOpenSubtasks(context.Background(), id)
	if task := r.task(id); open != protocol.MaxSubtasksUnderWay || len(task.Composite.Waiting) != 7 {
		t.Fatalf("after one ended: open %d, waiting %d", open, len(task.Composite.Waiting))
	}
}

// A cancel stops the subtasks with its keys, those waiting at once.
func TestComposeCancelByKey(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		switch {
		case req.Event == protocol.ComposeStart:
			return protocol.Decision{Run: []protocol.Subtask{{Command: "ls", Key: "a"}, {Command: "ls", Key: "b"}}}
		case req.End.Key == "b":
			return protocol.Decision{Cancel: &protocol.ComposeCancel{Keys: []string{"a"}}}
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: false, Err: req.End.Key + " " + string(req.End.State)}}
	})
	r.step(id)
	subs := r.subtasks(id)
	r.running(subs[0].TaskID)
	r.end(subs[1].TaskID, true, "", nil)
	r.step(id)
	a := r.task(subs[0].TaskID)
	if a.CancelRequestedAt == nil {
		t.Fatal("a not asked to stop")
	}
	r.end(a.TaskID, false, "", nil)
	r.step(id)
	if task := r.task(id); task.State != models.MinionTaskFailed || task.Err != "a cancelled" {
		t.Fatalf("task: %s %q", task.State, task.Err)
	}
}

// A timer calls the factory once it runs out, unless something ends first.
func TestComposeTimer(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event == protocol.ComposeStart {
			return protocol.Decision{After: "5s"}
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: true, Message: string(req.Event)}}
	})
	r.step(id)
	r.step(id)
	if n := len(r.fac.requests()); n != 1 {
		t.Fatalf("%d calls before the timer ran out", n)
	}
	if wake := r.task(id).Composite.WakeAt; wake == nil || !wake.Equal(r.now.Add(5*time.Second)) {
		t.Fatalf("wakes at %v", wake)
	}
	r.now = r.now.Add(6 * time.Second)
	// The timer's call goes unanswered: the timer stopped at it all the
	// same, and the call goes again as it was.
	r.fac.decide = func(string, protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error) {
		return protocol.Decision{}, nil, timeout
	}
	r.step(id)
	if c := r.task(id).Composite; c.TimerAt != nil || c.Seq != 2 || c.Answered != 1 || c.CallEvent != string(protocol.ComposeTimer) {
		t.Fatalf("its call unanswered: %+v", c)
	}
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		return protocol.Decision{Finish: &protocol.Finish{OK: true, Message: string(req.Event)}}
	})
	r.step(id)
	if task := r.task(id); task.State != models.MinionTaskDone || task.Composite.Status != "timer" || r.fac.last(t).Seq != 2 {
		t.Fatalf("task: %s %+v", task.State, task.Composite)
	}
}

// What fails a task: a wait for nothing, an invalid decision, an error its
// code answered.
func TestComposeFailures(t *testing.T) {
	cases := []struct {
		name    string
		command string
		decide  func(req protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError)
		err     string
	}{
		{"waits for nothing", "archive", func(protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError) {
			return protocol.Decision{}, nil
		}, "waits for nothing"},
		{"own op of a command with none", "bundle", func(protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError) {
			return protocol.Decision{OwnOp: &protocol.OwnOpRun{}}, nil
		}, "invalid: own_op: the command has no op of its own"},
		{"result_of names no subtask", "bundle", func(protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError) {
			return protocol.Decision{Finish: &protocol.Finish{OK: true, ResultOf: uuid.NewString()}}, nil
		}, "result_of names no subtask"},
		{"an error answered", "archive", func(protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError) {
			return protocol.Decision{}, &protocol.EnvelopeError{Code: "compose_failed", Message: "boom"}
		}, "could not decide: boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			id := r.queue(c.command, map[string]any{"path": "/x"})
			r.fac.decide = func(_ string, req protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error) {
				d, e := c.decide(req)
				return d, e, nil
			}
			r.step(id)
			task := r.task(id)
			if task.State != models.MinionTaskFailed || !strings.Contains(task.Err, c.err) {
				t.Fatalf("task: %s %q, want failed with %q", task.State, task.Err, c.err)
			}
		})
	}
}

// An instance busy, or one that cannot take the call, passes it on to the
// next; one that does not answer leaves it for later, with its seq, and
// ten minutes of no answer fail the task.
func TestComposeInstances(t *testing.T) {
	r := newRig(t, "f1", "f2", "f3")
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.fac.decide = func(instance string, req protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error) {
		switch instance {
		case "f1":
			return protocol.Decision{}, &protocol.EnvelopeError{Code: protocol.CodeBusy}, nil
		case "f2":
			return protocol.Decision{}, &protocol.EnvelopeError{Code: protocol.CodeUnsupportedVersion}, nil
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: true}}, nil, nil
	}
	r.step(id)
	var order []string
	for _, c := range r.fac.calls {
		order = append(order, c.instance)
	}
	if strings.Join(order, ",") != "f1,f2,f3" || r.task(id).State != models.MinionTaskDone {
		t.Fatalf("calls went to %v; task %s", order, r.task(id).State)
	}

	r = newRig(t)
	id = r.queue("archive", map[string]any{"path": "/x"})
	r.fac.decide = func(string, protocol.ComposeRequest) (protocol.Decision, *protocol.EnvelopeError, error) {
		return protocol.Decision{}, nil, timeout
	}
	r.step(id)
	task := r.task(id)
	if task.Composite.SilentSince == nil || task.Composite.Seq != 1 || task.Composite.Answered != 0 || task.Composite.CallAt != nil {
		t.Fatalf("after a timeout: %+v", task.Composite)
	}
	r.now = r.now.Add(time.Minute)
	r.step(id)
	reqs := r.fac.requests()
	if len(reqs) != 2 || reqs[1].Seq != 1 || reqs[1].Event != protocol.ComposeStart {
		t.Fatalf("sent again: %+v", reqs)
	}
	r.now = r.now.Add(protocol.ComposeNoAnswer)
	r.step(id)
	if task := r.task(id); task.State != models.MinionTaskFailed || !strings.Contains(task.Err, "did not answer") {
		t.Fatalf("after ten minutes: %s %q", task.State, task.Err)
	}
}

// A stop stops what is under way; once it ended, the factory hears of it
// with every end it had not, and its answer's subtasks clean up; the task
// ends cancelled once they have. A second stop ends it at once.
func TestComposeStop(t *testing.T) {
	for _, second := range []bool{false, true} {
		r := newRig(t)
		id := r.queue("archive", map[string]any{"path": "/x"})
		r.decide(func(req protocol.ComposeRequest) protocol.Decision {
			switch req.Event {
			case protocol.ComposeStart:
				return protocol.Decision{Run: []protocol.Subtask{{Command: "ls", Key: "a"}, {Command: "ls", Key: "b"}}}
			case protocol.ComposeStopped:
				return protocol.Decision{Run: []protocol.Subtask{{Command: "rm", Args: map[string]any{"path": "/tmp/x"}}}}
			}
			return protocol.Decision{}
		})
		r.step(id)
		subs := r.subtasks(id)
		r.running(subs[0].TaskID)
		if _, err := r.svc.StopTask(context.Background(), r.task(id)); err != nil {
			t.Fatal(err)
		}
		r.step(id)
		if a := r.task(subs[0].TaskID); a.CancelRequestedAt == nil {
			t.Fatal("the running subtask not asked to stop")
		}
		if b := r.task(subs[1].TaskID); b.State != models.MinionTaskCancelled {
			t.Fatalf("the queued subtask: %s", b.State)
		}
		if last := r.fac.last(t); last.Event == protocol.ComposeStopped {
			t.Fatal("the stopped call went before what was under way ended")
		}
		r.end(subs[0].TaskID, false, "", nil)
		r.step(id)
		stopped := r.fac.last(t)
		if stopped.Event != protocol.ComposeStopped || len(stopped.Ends) != 2 {
			t.Fatalf("stopped call: %+v", stopped)
		}
		task := r.task(id)
		var cleanup models.MinionTask
		for _, s := range r.subtasks(id) {
			if s.Cleanup {
				cleanup = s
			}
		}
		if task.Composite.Phase != models.ComposeCleanup || cleanup.Command != "rm" {
			t.Fatalf("cleanup: %s %+v", task.Composite.Phase, cleanup)
		}
		if second {
			r.running(cleanup.TaskID)
			after, err := r.svc.StopTask(context.Background(), r.task(id))
			if err != nil || after.State != models.MinionTaskCancelled {
				t.Fatalf("second stop: %v %+v", err, after)
			}
			if r.task(cleanup.TaskID).CancelRequestedAt == nil {
				t.Fatal("the cleanup not stopped")
			}
			continue
		}
		r.end(cleanup.TaskID, true, "", nil)
		r.step(id)
		if task := r.task(id); task.State != models.MinionTaskCancelled {
			t.Fatalf("after cleanup: %s", task.State)
		}
		if n := len(r.fac.requests()); n != 2 {
			t.Fatalf("%d calls: the cleanup's end called the factory", n)
		}
	}
}

// An answer that comes after a stop counts for its ctx alone.
func TestComposeAnswerAfterStop(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	block := make(chan struct{})
	r.fac.block = block
	ctx := []byte("kept")
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event == protocol.ComposeStopped {
			return protocol.Decision{}
		}
		return protocol.Decision{Run: []protocol.Subtask{{Command: "ls", Key: "late"}}, Ctx: &ctx, Status: ptr("late")}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.step(id)
	}()
	for len(r.fac.requests()) == 0 {
		time.Sleep(time.Millisecond)
	}
	if !r.svc.Deciding(r.session.SessionID) {
		t.Fatal("not deciding while the call is out")
	}
	if _, err := r.svc.StopTask(context.Background(), r.task(id)); err != nil {
		t.Fatal(err)
	}
	r.fac.mu.Lock()
	r.fac.block = nil
	r.fac.mu.Unlock()
	close(block)
	<-done
	task := r.task(id)
	if len(r.subtasks(id)) != 0 || string(task.Composite.Ctx) != "kept" || task.Composite.Status == "late" {
		t.Fatalf("after the stop: subtasks %d, ctx %q, status %q", len(r.subtasks(id)), task.Composite.Ctx, task.Composite.Status)
	}
	if stopped := r.fac.last(t); stopped.Event != protocol.ComposeStopped || len(stopped.Ends) != 0 {
		t.Fatalf("the stopped call: %+v", stopped)
	}
	if task.State != models.MinionTaskCancelled {
		t.Fatalf("task: %s", task.State)
	}
}

// A command with no op of its own finishes with its own output and data,
// a file the factory put, or a subtask's result.
func TestComposeFinishResult(t *testing.T) {
	r := newRig(t)
	id := r.queue("bundle", map[string]any{"path": "/x"})
	r.decide(func(protocol.ComposeRequest) protocol.Decision {
		return protocol.Decision{Finish: &protocol.Finish{OK: true, Out: []byte("two"), Data: json.RawMessage(`{"n":2,"raw":{"$bytes":"AQI="}}`)}}
	})
	r.step(id)
	task := r.task(id)
	parts, _ := r.st.Parts(context.Background(), id)
	if task.State != models.MinionTaskDone || len(parts) != 1 || string(parts[0].Out) != "two" {
		t.Fatalf("own result: %s %+v", task.State, parts)
	}
	var data map[string]any
	if err := protocol.UnmarshalCBOR(parts[0].DataCBOR, &data); err != nil || !bytes.Equal(data["raw"].([]byte), []byte{1, 2}) {
		t.Fatalf("data: %v %v", data, err)
	}

	// A file the factory put: core takes it, and drops it.
	r = newRig(t)
	id = r.queue("bundle", map[string]any{"path": "/x"})
	put, err := r.svc.PutFile(context.Background(), id, "out.txt", strings.NewReader("from a file"), 11)
	if err != nil || put.Size != 11 || put.ID == "" {
		t.Fatalf("put: %+v %v", put, err)
	}
	r.decide(func(protocol.ComposeRequest) protocol.Decision {
		return protocol.Decision{Finish: &protocol.Finish{OK: true, OutFile: put.ID}}
	})
	r.step(id)
	parts, _ = r.st.Parts(context.Background(), id)
	if task := r.task(id); task.State != models.MinionTaskDone || len(parts) != 1 || string(parts[0].Out) != "from a file" {
		t.Fatalf("out_file: %s %+v", task.State, parts)
	}
	if f, _ := r.files.FileByID(context.Background(), uuid.MustParse(put.ID)); f != nil {
		t.Fatal("the file the factory put was kept")
	}

	// A subtask's result: its parts, and its file.
	r = newRig(t)
	id = r.queue("bundle", map[string]any{"path": "/x"})
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event == protocol.ComposeStart {
			return protocol.Decision{Run: []protocol.Subtask{{Command: "ls"}}}
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: true, ResultOf: req.End.TaskID}}
	})
	r.step(id)
	ls := r.subtasks(id)[0]
	file := &models.MinionFile{FileID: uuid.New(), TaskID: ls.TaskID, Name: "a", SHA256: sha(nil), State: models.MinionFileComplete}
	r.files.add(file, nil)
	r.end(ls.TaskID, true, "listed", nil)
	r.step(id)
	task = r.task(id)
	parts, _ = r.st.Parts(context.Background(), id)
	if task.State != models.MinionTaskDone || task.ResultOf == nil || *task.ResultOf != ls.TaskID || len(parts) != 1 || string(parts[0].Out) != "listed" {
		t.Fatalf("result_of: %s %+v %+v", task.State, task.ResultOf, parts)
	}
	if f, _ := r.files.File(context.Background(), id); f == nil || f.FileID != file.FileID {
		t.Fatal("the subtask's file is not the task's")
	}
}

// Data and output past what a call carries go by link, served as JSON and
// as bytes.
func TestComposeLargeResults(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event == protocol.ComposeStart {
			return protocol.Decision{Run: []protocol.Subtask{{Command: "ls"}}}
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: true}}
	})
	r.step(id)
	ls := r.subtasks(id)[0]
	big := strings.Repeat("x", protocol.MaxComposeInline+1)
	r.end(ls.TaskID, true, big, map[string]any{"text": big})
	r.step(id)
	end := r.fac.last(t).End
	if end.Data != nil || end.Out != nil || end.DataURL == "" || end.OutURL == "" {
		t.Fatalf("large end: data %d out %d %q %q", len(end.Data), len(end.Out), end.DataURL, end.OutURL)
	}
	for _, l := range []struct {
		link string
		path string
	}{{end.DataURL, DataPath(ls.TaskID)}, {end.OutURL, OutPath(ls.TaskID)}} {
		u, _ := url.Parse(l.link)
		if u.Path != l.path || r.svc.CheckLink(u.Path, u.Query().Get("expires"), u.Query().Get("sig")) != nil {
			t.Fatalf("link %q", l.link)
		}
	}
	data, err := r.svc.Data(context.Background(), ls.TaskID)
	if err != nil || !jsonEqual(data, `{"text":"`+big+`"}`) {
		t.Fatalf("data by link: %v", err)
	}
	out, err := r.svc.Out(context.Background(), ls.TaskID)
	if err != nil || string(out) != big {
		t.Fatalf("out by link: %v", err)
	}
}

// What a factory puts: a name alone, a length said, within the limit, for
// a task that runs.
func TestComposePutFile(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	ctx := context.Background()
	var tooLarge *FileTooLargeError
	for _, c := range []struct {
		name string
		size int64
		want func(error) bool
	}{
		{"a/b", 1, func(err error) bool { return errors.Is(err, ErrNoFileName) }},
		{"", 1, func(err error) bool { return errors.Is(err, ErrNoFileName) }},
		{"a", -1, func(err error) bool { return errors.Is(err, ErrLengthRequired) }},
		{"a", 3 << 30, func(err error) bool { return errors.As(err, &tooLarge) }},
	} {
		if _, err := r.svc.PutFile(ctx, id, c.name, strings.NewReader("x"), c.size); !c.want(err) {
			t.Errorf("put %q %d: %v", c.name, c.size, err)
		}
	}
	f, err := r.svc.PutFile(ctx, id, "a.txt", strings.NewReader("abc"), 3)
	if err != nil || f.Name != "a.txt" || f.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("put: %+v %v", f, err)
	}
	if _, err := r.st.EndComposite(ctx, id, models.MinionTaskCancelled, nil, "", r.now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.PutFile(ctx, id, "b", strings.NewReader("x"), 1); !errors.Is(err, ErrTaskEnded) {
		t.Fatalf("put for an ended task: %v", err)
	}
}

// The own op, refused by core's checks, ends refused: the factory hears it.
func TestComposeOwnOpRefused(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.q.refuse["archive"] = "path: too long"
	r.decide(func(req protocol.ComposeRequest) protocol.Decision {
		if req.Event == protocol.ComposeStart {
			return protocol.Decision{OwnOp: &protocol.OwnOpRun{Args: map[string]any{"path": "/y"}}}
		}
		return protocol.Decision{Finish: &protocol.Finish{OK: false, Err: string(req.End.State) + ": " + req.End.Err}}
	})
	r.step(id)
	if task := r.task(id); task.State != models.MinionTaskFailed || task.Err != "refused: path: too long" {
		t.Fatalf("task: %s %q", task.State, task.Err)
	}
}

// A reverse-call reply waits for the decision while it comes in time.
func TestComposeHold(t *testing.T) {
	r := newRig(t)
	id := r.queue("archive", map[string]any{"path": "/x"})
	r.decide(func(protocol.ComposeRequest) protocol.Decision {
		return protocol.Decision{Run: []protocol.Subtask{{Command: "ls"}}}
	})
	if !r.svc.Hold(context.Background(), []uuid.UUID{id}, time.Second) || len(r.subtasks(id)) != 1 {
		t.Fatal("the decision did not come within the hold")
	}

	r = newRig(t)
	id = r.queue("archive", map[string]any{"path": "/x"})
	block := make(chan struct{})
	r.fac.block = block
	held := r.svc.Hold(context.Background(), []uuid.UUID{id}, 20*time.Millisecond)
	// The look goes on past the hold: its decision (none, so the task
	// fails) still comes.
	close(block)
	for deadline := time.Now().Add(5 * time.Second); !r.task(id).Finished() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if held {
		t.Fatal("a held reply did not give up")
	}
	if !r.task(id).Finished() {
		t.Fatal("the look did not go on past the hold")
	}
}

func jsonEqual(raw []byte, want string) bool {
	var a, b any
	return json.Unmarshal(raw, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && jsonText(a) == jsonText(b)
}

func jsonText(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
