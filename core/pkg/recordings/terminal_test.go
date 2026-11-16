package recordings

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func (g *rig) beginTerminal(t *testing.T, cols, rows int) *Recorder {
	t.Helper()
	r := g.svc.Begin(context.Background(), Beginning{TaskID: g.task.TaskID, Front: protocol.FrontTerminal, UserID: uuid.New(),
		Username: "ann", Client: "web: ann", Stream: 7, Width: cols, Height: rows, Keys: true})
	if r == nil {
		t.Fatal("not recorded")
	}
	return r
}

// castLines reads an asciicast: its header, and its events.
func castLines(t *testing.T, file string) (asciicastHeader, [][]any) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(file, "\n"), "\n")
	var h asciicastHeader
	if err := json.Unmarshal([]byte(lines[0]), &h); err != nil {
		t.Fatalf("header %q: %v", lines[0], err)
	}
	var events [][]any
	for _, l := range lines[1:] {
		var e []any
		if err := json.Unmarshal([]byte(l), &e); err != nil || len(e) != 3 {
			t.Fatalf("event %q: %v", l, err)
		}
		events = append(events, e)
	}
	return h, events
}

// A terminal's recording is an asciicast: a header of its size, when it
// began and who; then what the process wrote, what was typed and its new
// sizes, each timed from its start, a UTF-8 character cut between two
// reads kept whole, bytes that are not UTF-8 as U+FFFD. Its record says
// its format, who, its stream, how long it plays and how it ended.
func TestTerminalRecordingIsAnAsciicast(t *testing.T) {
	g := newRig(testConfig())
	r := g.beginTerminal(t, 120, 40)
	r.Output([]byte("$ "))
	g.clock.add(1500 * time.Millisecond)
	r.Input([]byte("echo <é>\r"))
	g.clock.add(250 * time.Millisecond)
	// é cut between two reads, then a byte that is not UTF-8.
	r.Output([]byte("<\xc3"))
	r.Output([]byte("\xa9>\r\n\xff"))
	g.clock.add(time.Second)
	r.Resize(100, 30)
	r.Resize(0, 30) // not a size: none
	r.EndWith(models.RecordingEndProcess, "exit 0")

	rec, err := g.repo.FindByID(context.Background(), r.ID())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Format != models.RecordingFormatAsciicast || rec.Client != "web: ann" || rec.Stream != 7 || !rec.Keys ||
		rec.State != models.RecordingStateSaved || rec.End != models.RecordingEndProcess || rec.Msg != "exit 0" || rec.DurationMs != 2750 {
		t.Errorf("record %+v", rec)
	}
	h, events := castLines(t, g.file(t, r.ID()))
	if h.Version != 2 || h.Width != 120 || h.Height != 40 || h.Timestamp != rec.StartedAt.Unix() || h.Title != "terminal · web: ann · stream 7" {
		t.Errorf("header %+v", h)
	}
	want := [][]any{{0.0, "o", "$ "}, {1.5, "i", "echo <é>\r"}, {1.75, "o", "<"}, {1.75, "o", "é>\r\n�"}, {2.75, "r", "100x30"}}
	if len(events) != len(want) {
		t.Fatalf("events %v", events)
	}
	for i, w := range want {
		if events[i][0] != w[0] || events[i][1] != w[1] || events[i][2] != w[2] {
			t.Errorf("event %d: %v, want %v", i, events[i], w)
		}
	}
}

// A raw port's terminal says no size: it is the raw port's default; no one
// signed in opened it, and its process did not start: nothing to play.
func TestTerminalRecordingNotStarted(t *testing.T) {
	g := newRig(testConfig())
	r := g.svc.Begin(context.Background(), Beginning{TaskID: g.task.TaskID, Front: protocol.FrontTerminal, Client: "10.0.0.5:41022",
		Stream: 2, Keys: true})
	r.Fail("/bin/zsh did not start: not_found")
	rec, _ := g.repo.FindByID(context.Background(), r.ID())
	if rec.State != models.RecordingStateFailed || rec.Msg != "/bin/zsh did not start: not_found" || rec.UserID != uuid.Nil {
		t.Errorf("record %+v", rec)
	}
	body, _, err := g.store.Get(context.Background(), partKey(r.ID(), 0))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	if h, _ := castLines(t, string(b)); h.Width != 80 || h.Height != 24 {
		t.Errorf("header %+v", h)
	}
}

func TestSplitUTF8(t *testing.T) {
	for _, c := range []struct{ in, whole, rest string }{
		{"abc", "abc", ""},
		{"a\xc3", "a", "\xc3"},
		{"a\xe2\x82", "a", "\xe2\x82"},
		{"a\xf0\x9f\x98", "a", "\xf0\x9f\x98"},
		{"a\xf0\x9f\x98\x80", "a\xf0\x9f\x98\x80", ""},
		{"\xff", "\xff", ""},
		{"a\x82\x82\x82", "a\x82\x82\x82", ""},
	} {
		whole, rest := splitUTF8([]byte(c.in))
		if string(whole) != c.whole || string(rest) != c.rest {
			t.Errorf("%q: %q %q", c.in, whole, rest)
		}
	}
}
