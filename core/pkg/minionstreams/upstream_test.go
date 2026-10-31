package minionstreams

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func setupUp(t *testing.T, size int) (*Service, *memTasks, []byte) {
	t.Helper()
	s, tasks, _, _, xfer := setup(t, 0)
	tasks.t.Result = protocol.ResultFileUpload
	tasks.t.Upload = &models.MinionTaskUpload{Name: "big.iso", Xfer: xfer, Stream: true}
	body := make([]byte, size)
	_, _ = rand.Read(body)
	return s, tasks, body
}

// pullAll plays the minion: it asks for each chunk from what it has, as
// they come, and records how far it has the file, as minionsync does.
func pullAll(t *testing.T, s *Service, tasks *memTasks, want int) []byte {
	t.Helper()
	var got bytes.Buffer
	var sum []byte
	for tries := 0; got.Len() < want && tries < 5000; tries++ {
		cur := tasks.get()
		c, a, err := s.Pull(context.Background(), &cur, int64(got.Len()), protocol.MaxMinionMessageSize)
		if err != nil {
			t.Fatal(err)
		}
		tasks.mu.Lock()
		tasks.t.Upload.Acked = int64(got.Len())
		tasks.mu.Unlock()
		if c == nil {
			if a == nil || !a.Hold {
				t.Fatalf("neither a chunk nor hold at %d: %+v", got.Len(), a)
			}
			waitFor(t, "more bytes", func() bool { return tasks.get().Upload.Received > int64(got.Len()) })
			continue
		}
		if c.Off == 0 && (c.Total == nil || int(*c.Total) != want) {
			t.Fatalf("the first chunk's total: %v", c.Total)
		}
		got.Write(c.Data)
		if c.SHA256 != nil {
			sum = c.SHA256
		}
	}
	whole := sha256.Sum256(got.Bytes())
	if !bytes.Equal(sum, whole[:]) {
		t.Fatalf("the last chunk's sha256 %x, the bytes' %x", sum, whole)
	}
	return got.Bytes()
}

// An uploader's body goes to the minion through the buffer, a window
// ahead at most; the last chunk says the SHA-256 core hashed.
func TestUpstream(t *testing.T) {
	s, tasks, body := setupUp(t, 5*protocol.MaxChunkData+11)
	done := make(chan error)
	go func() {
		_, err := s.Write(context.Background(), tasks.t.TaskID, 0, int64(len(body)), bytes.NewReader(body))
		done <- err
	}()
	got := pullAll(t, s, tasks, len(body))
	if err := <-done; err != nil || !bytes.Equal(got, body) {
		t.Fatalf("write: %v, equal %v", err, bytes.Equal(got, body))
	}
}

// An upload resumed: a part, then the rest from where core is; a part from
// elsewhere is refused with where; the hash goes on across the parts.
func TestUpstreamResume(t *testing.T) {
	s, tasks, body := setupUp(t, 3*protocol.MaxChunkData)
	id := tasks.t.TaskID
	pulled := make(chan []byte)
	go func() { pulled <- pullAll(t, s, tasks, len(body)) }()

	cut := protocol.MaxChunkData + 7
	if at, err := s.Write(context.Background(), id, 0, int64(len(body)), io.LimitReader(bytes.NewReader(body), int64(cut))); err != nil || at != int64(cut) {
		t.Fatalf("the first part: %d %v", at, err)
	}
	var off *OffsetError
	if _, err := s.Write(context.Background(), id, 5, -1, bytes.NewReader(body[5:])); !errors.As(err, &off) || off.At != int64(cut) {
		t.Fatalf("from elsewhere: %v", err)
	}
	if _, err := s.Write(context.Background(), id, int64(cut), -1, bytes.NewReader(body[cut:])); err != nil {
		t.Fatalf("the rest: %v", err)
	}
	if got := <-pulled; !bytes.Equal(got, body) {
		t.Fatal("not the file")
	}
	// All of it: more is past the size.
	if _, err := s.Write(context.Background(), id, int64(len(body)), -1, bytes.NewReader([]byte("x"))); !errors.Is(err, ErrSize) {
		t.Fatalf("past the end: %v", err)
	}
}

// No size said, none known: refused.
func TestUpstreamNoSize(t *testing.T) {
	s, tasks, _ := setupUp(t, 10)
	if _, err := s.Write(context.Background(), tasks.t.TaskID, 0, -1, bytes.NewReader(nil)); !errors.Is(err, ErrNoSize) {
		t.Fatalf("%v", err)
	}
}

// A part that ends as it should keeps its uploader (the next comes in a
// moment); one cut short lets go of it.
func TestUpstreamPartKeepsUploader(t *testing.T) {
	s, tasks, body := setupUp(t, 2*protocol.MaxChunkData)
	go pullAll(t, s, tasks, len(body))
	if _, err := s.Write(context.Background(), tasks.t.TaskID, 0, int64(len(body)), io.LimitReader(bytes.NewReader(body), 100)); err != nil {
		t.Fatal(err)
	}
	if u := tasks.get().Upload; u.Uploader == "" || u.IdleSince != nil {
		t.Fatalf("after a part: uploader %q, idle since %v", u.Uploader, u.IdleSince)
	}
	// The connection drops: reading the body fails.
	cut := io.MultiReader(bytes.NewReader(body[100:200]), iotest.ErrReader(io.ErrClosedPipe))
	_, _ = s.Write(context.Background(), tasks.t.TaskID, 100, -1, cut)
	if u := tasks.get().Upload; u.Uploader != "" {
		t.Fatalf("cut short: uploader %q still", u.Uploader)
	}
}
