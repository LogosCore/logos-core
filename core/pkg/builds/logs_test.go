package builds

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// sendLog has instance send a chunk of b's live log, its lines "step: text".
func (r *rig) sendLog(t *testing.T, instance string, b *models.Build, seq, dropped int64, lines ...string) (any, error) {
	t.Helper()
	c := protocol.BuildLogChunk{BuildID: b.BuildID.String(), Seq: seq, Dropped: dropped}
	for _, l := range lines {
		step, text, _ := strings.Cut(l, ": ")
		c.Lines = append(c.Lines, protocol.BuildLogLine{Step: step, At: r.clock, Text: text})
	}
	return r.svc.HandleLog(context.Background(), envelope(t, instance, protocol.TypeFactoryBuildLog, c))
}

// pageText is a log page as "step: text" lines, a mark as "[missing n]".
func pageText(p LogPage) []string {
	var out []string
	for _, l := range p.Lines {
		if l.Missing != nil {
			out = append(out, fmt.Sprintf("[missing %d]", *l.Missing))
			continue
		}
		out = append(out, l.Step+": "+l.Text)
	}
	return out
}

// The live log is kept chunk by chunk, read a page at a time from a cursor;
// a chunk sent again is kept once, and the result's own log is not kept.
func TestLiveBuildLog(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	ctx := context.Background()
	for _, c := range []struct {
		seq   int64
		lines []string
	}{{1, []string{"badge: checking", "render: rendering"}}, {2, []string{"render: done"}}, {2, []string{"render: done"}}} {
		rep, err := r.sendLog(t, "bob-1", b, c.seq, 0, c.lines...)
		if err != nil || !rep.(protocol.BuildLogReply).Ack || rep.(protocol.BuildLogReply).Seq != c.seq {
			t.Fatalf("chunk %d: %+v %v", c.seq, rep, err)
		}
	}
	got := r.get(t, b.BuildID.String(), "")
	if !got.LogStreamed || got.LogBytes != int64(len("checkingrenderingdone")) {
		t.Fatalf("streamed %v, %d bytes", got.LogStreamed, got.LogBytes)
	}
	page, err := r.svc.Log(ctx, got, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "badge: checking|render: rendering|render: done"; strings.Join(pageText(page), "|") != want || !page.Live || page.Cursor != 2 || page.More {
		t.Fatalf("page %q, cursor %d, more %v", pageText(page), page.Cursor, page.More)
	}
	if page.Lines[0].At == nil || !page.Lines[0].At.Equal(r.clock) {
		t.Errorf("at %v", page.Lines[0].At)
	}
	// Polled again from its cursor: only what came since.
	if _, err := r.sendLog(t, "bob-1", b, 3, 0, "upload: uploading"); err != nil {
		t.Fatal(err)
	}
	if page, _ = r.svc.Log(ctx, got, page.Cursor); strings.Join(pageText(page), "|") != "upload: uploading" || page.Cursor != 3 {
		t.Fatalf("next page %q, cursor %d", pageText(page), page.Cursor)
	}

	// The result's log goes: core has the live one.
	if _, err := r.succeed(t, b, `{"minion":1}`, `{"minion":1}`); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.get(t, b.BuildID.String(), models.BuildStateSucceeded); got.Log != "" {
		t.Fatalf("the result's log kept: %q", got.Log)
	}
	// After the build, a chunk is refused: core no longer waits.
	if _, err := r.sendLog(t, "bob-1", b, 4, 0, "upload: late"); rpcCode(err) != protocol.CodeNotFound {
		t.Fatalf("a chunk after the build: %v", err)
	}

	if err := r.svc.DeleteBuild(ctx, b.BuildID.String(), alice); err != nil {
		t.Fatal(err)
	}
	if len(r.logs.of(b.BuildID)) != 0 {
		t.Fatal("the log outlived its build")
	}
}

// A factory that sends no live log: its result's log is the log, lines
// without steps or times, and cursor stays.
func TestResultBuildLog(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	res := protocol.BuildResult{BuildID: b.BuildID.String(), Status: protocol.BuildFailed,
		Error: &protocol.BuildError{Code: "compile_failed", Message: "no"}, Log: "line 1\r\nline 2\n"}
	if _, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res)); err != nil {
		t.Fatal(err)
	}
	got := r.get(t, b.BuildID.String(), models.BuildStateFailed)
	page, err := r.svc.Log(context.Background(), got, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pageText(page), "|") != ": line 1|: line 2" || page.Live || page.Cursor != 0 || page.Lines[0].At != nil {
		t.Fatalf("page %q, live %v, cursor %d", pageText(page), page.Live, page.Cursor)
	}
}

// Lines the factory dropped, and a chunk that never arrived, are marks in
// the page.
func TestBuildLogGaps(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	_, _ = r.sendLog(t, "bob-1", b, 1, 0, "render: a")
	_, _ = r.sendLog(t, "bob-1", b, 3, 5, "render: b")
	page, _ := r.svc.Log(context.Background(), r.get(t, b.BuildID.String(), ""), 0)
	if want := "render: a|[missing 0]|[missing 5]|render: b"; strings.Join(pageText(page), "|") != want {
		t.Fatalf("page %q", pageText(page))
	}
	// A chunk that is missing at the start of a page counts too.
	if page, _ = r.svc.Log(context.Background(), r.get(t, b.BuildID.String(), ""), 1); pageText(page)[0] != "[missing 0]" {
		t.Fatalf("page from 1: %q", pageText(page))
	}
}

// Only the instance building it sends a build's log; a chunk is checked.
func TestBuildLogRefusals(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	if _, err := r.sendLog(t, "bob-2", b, 1, 0, "render: x"); rpcCode(err) != protocol.CodeNotFound {
		t.Errorf("another instance: %v", err)
	}
	if _, err := r.sendLog(t, "bob-1", b, 0, 0, "render: x"); rpcCode(err) != protocol.CodeValidationFailed {
		t.Errorf("seq 0: %v", err)
	}
	if len(r.logs.of(b.BuildID)) != 0 {
		t.Error("a refused chunk was kept")
	}
}

// Past MaxBuildLogBytes the log's start goes, counted in Trimmed; a page
// from the start then begins without a mark.
func TestBuildLogTrimmed(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	line := strings.Repeat("x", protocol.MaxBuildLogLineBytes)
	perChunk := protocol.MaxBuildLogChunkBytes / protocol.MaxBuildLogLineBytes
	lines := make([]string, perChunk)
	for i := range lines {
		lines[i] = "render: " + line
	}
	chunks := MaxBuildLogBytes/protocol.MaxBuildLogChunkBytes + 2
	for seq := int64(1); seq <= int64(chunks); seq++ {
		r.clock = r.clock.Add(time.Millisecond)
		if _, err := r.sendLog(t, "bob-1", b, seq, 0, lines...); err != nil {
			t.Fatal(err)
		}
	}
	got := r.get(t, b.BuildID.String(), "")
	if got.LogBytes > MaxBuildLogBytes || got.LogTrimmed != int64(2*perChunk) {
		t.Fatalf("kept %d bytes, trimmed %d lines", got.LogBytes, got.LogTrimmed)
	}
	page, _ := r.svc.Log(context.Background(), got, 0)
	if page.Trimmed != got.LogTrimmed || page.Lines[0].Missing != nil || !page.More {
		t.Fatalf("trimmed %d, first %+v, more %v", page.Trimmed, page.Lines[0], page.More)
	}
}
