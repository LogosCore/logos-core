package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A file read while it comes: what has come, the rest as it comes (woken by
// each chunk, not the poll: livePoll is an hour here), from any place, and
// from the kept file once joined. Its type is known from the first chunk.
func TestReadFrom(t *testing.T) {
	s, _, files, session, taskID := setup(t)
	s.livePoll = time.Hour
	ctx := context.Background()
	body := []byte("hello, brave new world: one file in three chunks!")
	sum := sha256.Sum256(body)
	xfer := bytes.Repeat([]byte{4}, 16)
	total := uint64(len(body))
	if r, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Data: body[:7], Total: &total, Task: taskID[:], Name: "/tmp/hello.txt"}); err != nil || r.Next != 7 {
		t.Fatalf("first: %+v %v", r, err)
	}
	f, _ := files.FindByXfer(ctx, session.SessionID, xfer)
	if f.ContentType != "text/plain; charset=utf-8" {
		t.Errorf("its type from the first chunk: %q", f.ContentType)
	}
	read := func(off int64, follow bool) (string, error) {
		r := s.ReadFrom(ctx, f.FileID, off, follow)
		defer r.Close()
		b, err := io.ReadAll(r)
		return string(b), err
	}
	if got, err := read(0, false); err != nil || got != "hello, " {
		t.Fatalf("what has come: %q %v", got, err)
	}

	following := make(chan string, 1)
	go func() {
		got, err := read(2, true)
		if err != nil {
			got = "error: " + err.Error()
		}
		following <- got
	}()
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 7, Data: body[7:30]}); err != nil {
		t.Fatal(err)
	}
	// From the middle of the second chunk, as far as has come.
	if got, err := read(10, false); err != nil || got != string(body[10:30]) {
		t.Fatalf("from the middle: %q %v", got, err)
	}
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 30, Data: body[30:], SHA256: sum[:]}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-following:
		if got != string(body[2:]) {
			t.Fatalf("followed: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the follower was not woken")
	}
	waitState(t, files, f.FileID, "COMPLETE")
	if got, err := read(36, false); err != nil || got != string(body[36:]) {
		t.Fatalf("from the kept file: %q %v", got, err)
	}
}

// A follower of a transfer that ends without its file ends with why; one
// whose caller goes ends too.
func TestReadFromEnds(t *testing.T) {
	s, _, files, session, taskID := setup(t)
	s.livePoll = 10 * time.Millisecond
	ctx := context.Background()
	total := uint64(10)
	xfer := bytes.Repeat([]byte{5}, 16)
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Data: []byte("abcd"), Total: &total, Task: taskID[:]}); err != nil {
		t.Fatal(err)
	}
	f, _ := files.FindByXfer(ctx, session.SessionID, xfer)
	ended := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(s.ReadFrom(ctx, f.FileID, 0, true))
		ended <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := s.AbortTask(ctx, taskID, "the task was stopped"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		if err == nil || !strings.Contains(err.Error(), "the file is cancelled: the task was stopped") {
			t.Fatalf("ended: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the follower did not end")
	}

	gone, cancel := context.WithCancel(ctx)
	xfer2 := bytes.Repeat([]byte{6}, 16)
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer2, Data: []byte("ab"), Total: &total, Task: taskID[:]}); err != nil {
		t.Fatal(err)
	}
	g, _ := files.FindByXfer(ctx, session.SessionID, xfer2)
	go func() {
		_, err := io.ReadAll(s.ReadFrom(gone, g.FileID, 0, true))
		ended <- err
	}()
	cancel()
	select {
	case err := <-ended:
		if err == nil {
			t.Fatal("ended without an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the follower outlived its caller")
	}
}

// A file still coming: its strings as far as it has come; one cut by the
// end of what came waits for the next page, which reads it whole.
func TestStringsWhileComing(t *testing.T) {
	s, _, files, session, taskID := setup(t)
	ctx := context.Background()
	body := []byte("\x00\x00first string\x00\x00second string\x00\x00tail")
	total := uint64(len(body))
	xfer := bytes.Repeat([]byte{7}, 16)
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Data: body[:22], Total: &total, Task: taskID[:]}); err != nil {
		t.Fatal(err)
	}
	f, _ := files.FindByXfer(ctx, session.SessionID, xfer)
	q := StringsQuery{First: 10, MinLength: 5, Encoding: EncodingASCII}
	p, err := s.Strings(ctx, *f, q)
	if err != nil || p.Done || len(p.Strings) != 1 || p.Strings[0].Text != "first string" || p.Next != 16 || p.ReadTo != 22 {
		t.Fatalf("what has come: %+v %v", p, err)
	}
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 22, Data: body[22:28]}); err != nil {
		t.Fatal(err)
	}
	f, _ = files.FindByXfer(ctx, session.SessionID, xfer)
	q.After = p.Next
	if p, err = s.Strings(ctx, *f, q); err != nil || p.Done || len(p.Strings) != 0 || p.ReadTo != 28 {
		t.Fatalf("still cut: %+v %v", p, err)
	}
	sum := sha256.Sum256(body)
	if _, err := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 28, Data: body[28:], SHA256: sum[:]}); err != nil {
		t.Fatal(err)
	}
	f = waitState(t, files, f.FileID, "COMPLETE")
	if p, err = s.Strings(ctx, *f, q); err != nil || !p.Done || len(p.Strings) != 1 || p.Strings[0].Text != "second string" {
		t.Fatalf("whole: %+v %v", p, err)
	}
}
