package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// streamTask is a stream_file task of the session, and the announce of
// its file of total bytes.
func streamTask(session *models.MinionSession, xfer []byte, total uint64) (*models.MinionTask, protocol.ChunkItem) {
	t := &models.MinionTask{TaskID: uuid.New(), SessionID: session.SessionID, OperationID: session.OperationID, Result: protocol.ResultFileStream}
	return t, protocol.ChunkItem{Xfer: xfer, Total: &total, Task: t.TaskID[:], Name: "/var/notes.txt", Data: []byte{}}
}

// A stream within the cap is kept as it passes: chunks sent again after a
// resume, whole or in part, add only what the copy lacks, and the last
// chunk's SHA-256 checks the whole.
func TestStreamCopy(t *testing.T) {
	s, store, files, session, _ := setup(t)
	ctx := context.Background()
	body := []byte("a file streamed in three chunks, resumed once")
	sum := sha256.Sum256(body)
	xfer := bytes.Repeat([]byte{9}, 16)
	task, first := streamTask(session, xfer, uint64(len(body)))

	s.KeepStream(ctx, session, task, first)
	f, err := files.FindByTask(ctx, task.TaskID)
	if err != nil || f.State != models.MinionFileReceiving || f.Total != int64(len(body)) {
		t.Fatalf("copy %+v %v", f, err)
	}
	s.KeepStream(ctx, session, task, first) // the same announce again
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 0, Data: body[:20]})
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 0, Data: body[:20]})    // again, whole
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 10, Data: body[10:30]}) // again, in part
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: xfer, Off: 30, Data: body[30:], SHA256: sum[:]})

	f = waitState(t, files, f.FileID, models.MinionFileComplete)
	rc, _, err := s.Open(ctx, f.FileID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, body) || store.keys("staging/") != 0 {
		t.Fatalf("kept %q, %d chunks left", got, store.keys("staging/"))
	}
}

// A stream resumed from the middle may end without the minion's SHA-256:
// the copy is kept, with its own.
func TestStreamCopyWithoutSum(t *testing.T) {
	s, _, files, session, _ := setup(t)
	ctx := context.Background()
	body := []byte("resumed from the middle")
	task, first := streamTask(session, bytes.Repeat([]byte{8}, 16), uint64(len(body)))
	s.KeepStream(ctx, session, task, first)
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: first.Xfer, Off: 0, Data: body[:8]})
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: first.Xfer, Off: 8, Data: body[8:]})
	f, _ := files.FindByTask(ctx, task.TaskID)
	f = waitState(t, files, f.FileID, models.MinionFileComplete)
	if sum := sha256.Sum256(body); !bytes.Equal(f.SHA256, sum[:]) {
		t.Fatalf("its SHA-256 %x, want the copy's %x", f.SHA256, sum)
	}
}

// Over the cap, nothing is kept; an empty file is kept at once.
func TestStreamCopyCapAndEmpty(t *testing.T) {
	s, _, files, session, _ := setup(t) // MaxSize 100
	ctx := context.Background()
	task, first := streamTask(session, bytes.Repeat([]byte{1}, 16), 101)
	s.KeepStream(ctx, session, task, first)
	if f, err := files.FindByTask(ctx, task.TaskID); err == nil {
		t.Fatalf("over the cap, kept: %+v", f)
	}
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: first.Xfer, Off: 0, Data: []byte("x")}) // nothing to add to

	task, first = streamTask(session, bytes.Repeat([]byte{2}, 16), 0)
	s.KeepStream(ctx, session, task, first)
	f, err := files.FindByTask(ctx, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if f = waitState(t, files, f.FileID, models.MinionFileComplete); f.Total != 0 {
		t.Fatalf("empty: %+v", f)
	}
}

// A stream that goes on past the copy's end, a last chunk that does not
// check out, or a new announce end the copy, never the stream.
func TestStreamCopyGivenUp(t *testing.T) {
	s, store, files, session, _ := setup(t)
	ctx := context.Background()
	body := []byte("0123456789abcdefghij")

	gap, first := streamTask(session, bytes.Repeat([]byte{3}, 16), uint64(len(body)))
	s.KeepStream(ctx, session, gap, first)
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: first.Xfer, Off: 0, Data: body[:5]})
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: first.Xfer, Off: 10, Data: body[10:]})
	f, _ := files.FindByTask(ctx, gap.TaskID)
	if f.State != models.MinionFileCancelled || store.keys("staging/") != 0 {
		t.Fatalf("a gap: %+v, %d chunks left", f, store.keys("staging/"))
	}

	bad, first := streamTask(session, bytes.Repeat([]byte{4}, 16), uint64(len(body)))
	s.KeepStream(ctx, session, bad, first)
	wrong := sha256.Sum256([]byte("another file"))
	s.StreamChunk(ctx, session, protocol.ChunkItem{Xfer: first.Xfer, Off: 0, Data: body, SHA256: wrong[:]})
	f, _ = files.FindByTask(ctx, bad.TaskID)
	waitState(t, files, f.FileID, models.MinionFileFailed)

	again, first := streamTask(session, bytes.Repeat([]byte{5}, 16), uint64(len(body)))
	s.KeepStream(ctx, session, again, first)
	old, _ := files.FindByTask(ctx, again.TaskID)
	first.Xfer = bytes.Repeat([]byte{6}, 16)
	s.KeepStream(ctx, session, again, first)
	if o, _ := files.FindByID(ctx, old.FileID); o.State != models.MinionFileCancelled {
		t.Fatalf("started over, the earlier copy: %+v", o)
	}
	if n, _ := files.FindByXfer(ctx, session.SessionID, first.Xfer); n == nil || n.State != models.MinionFileReceiving {
		t.Fatalf("started over, the new copy: %+v", n)
	}
}
