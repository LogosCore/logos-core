package minionsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

func (f *fakeTasks) FindByUploadXfer(_ context.Context, sid uuid.UUID, xfer []byte) (*models.MinionTask, error) {
	for i := range f.rows {
		if t := &f.rows[i]; t.SessionID == sid && t.Upload != nil && bytes.Equal(t.Upload.Xfer, xfer) {
			c := *t
			return &c, nil
		}
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeTasks) UploadAcked(_ context.Context, id uuid.UUID, next int64, at time.Time) (*models.MinionTask, error) {
	t := f.find(id)
	t.Upload.Acked, t.Upload.LastAckAt = next, &at
	if t.Upload.FirstAckAt == nil {
		t.Upload.FirstAckAt = &at
	}
	return t, nil
}

func (f *fakeTasks) SetDataProblem(_ context.Context, id uuid.UUID, problem string) (*models.MinionTask, error) {
	t := f.find(id)
	if t.DataProblem == "" {
		t.DataProblem = problem
	}
	return t, nil
}

// fakeFiles holds task files in memory.
type fakeFiles struct {
	bodies   map[uuid.UUID][]byte
	released []uuid.UUID
}

func (f *fakeFiles) TakeChunk(context.Context, *models.MinionSession, protocol.ChunkItem) (minionfiles.Reply, error) {
	return minionfiles.Reply{}, nil
}

func (f *fakeFiles) AbortTask(context.Context, uuid.UUID, string) error { return nil }

func (f *fakeFiles) ReadChunk(_ context.Context, id uuid.UUID, off int64, n int) ([]byte, error) {
	b := f.bodies[id]
	return b[off:min(off+int64(n), int64(len(b)))], nil
}

func (f *fakeFiles) ReleaseTaskFile(_ context.Context, id uuid.UUID) error {
	f.released = append(f.released, id)
	return nil
}

// A minion pulls a task's file: each ack asks for the chunk from its next,
// the first saying the total and task, the last the SHA-256; core records
// how far it has it. Its data is checked against the file at the end, and
// the file let go.
func TestUploadPull(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	body := make([]byte, protocol.MaxChunkData+1000)
	_, _ = rand.Read(body)
	sum := sha256.Sum256(body)
	files := &fakeFiles{bodies: map[uuid.UUID][]byte{}}
	r.svc.WithFiles(files)
	fileID, taskID, xfer := uuid.New(), uuid.New(), bytes.Repeat([]byte{7}, 16)
	files.bodies[fileID] = body
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: taskID, SessionID: sid, OperationID: r.build.OperationID,
		Command: "upload", Result: protocol.ResultFileUpload, State: models.MinionTaskRunning,
		Upload: &models.MinionTaskUpload{FileID: fileID, Name: "a.bin", Size: int64(len(body)), SHA256: sum[:], Xfer: xfer}})
	send := func(items ...any) protocol.CoreMessage {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), items...)
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return protocol.CoreMessage{}
		}
		return r.openUnder(t, sid, r.keys, reply)
	}
	pull := func(next uint64, x []byte) protocol.CoreMessage {
		return send(protocol.AckItem{T: protocol.ItemAck, Xfer: x, Next: &next})
	}
	chunk := func(msg protocol.CoreMessage) protocol.ChunkItem {
		t.Helper()
		got := itemsOf(msg, protocol.ItemChunk)
		var c protocol.ChunkItem
		if len(got) != 1 || protocol.UnmarshalCBOR(got[0], &c) != nil {
			t.Fatalf("no chunk in %d items", len(msg.Items))
		}
		return c
	}

	first := chunk(pull(0, xfer))
	if first.Off != 0 || len(first.Data) != protocol.MaxChunkData || first.Total == nil || *first.Total != uint64(len(body)) ||
		!bytes.Equal(first.Task, taskID[:]) || first.SHA256 != nil {
		t.Fatalf("first: off %d, %d bytes", first.Off, len(first.Data))
	}
	if up := tasks.find(taskID).Upload; up.FirstAckAt == nil || up.Acked != 0 {
		t.Fatalf("after the first ask: %+v", up)
	}
	last := chunk(pull(uint64(len(first.Data)), xfer))
	if !bytes.Equal(append(first.Data, last.Data...), body) || !bytes.Equal(last.SHA256, sum[:]) || last.Total != nil {
		t.Fatalf("last: off %d, %d bytes", last.Off, len(last.Data))
	}
	errCode := func(msg protocol.CoreMessage) string {
		var e protocol.ErrItem
		got := itemsOf(msg, protocol.ItemErr)
		if len(got) != 1 || protocol.UnmarshalCBOR(got[0], &e) != nil {
			return ""
		}
		return e.Code
	}
	if c := errCode(pull(uint64(len(body))+1, xfer)); c != protocol.ErrItemBadMessage {
		t.Fatalf("past the end: %q", c)
	}
	if c := errCode(pull(0, bytes.Repeat([]byte{8}, 16))); c != protocol.ErrItemUnknownTask {
		t.Fatalf("another transfer: %q", c)
	}

	// Its end, with the last ack saying the minion has it all: taken, no
	// chunk, no err. Data of another SHA-256 is a problem; the file let go.
	data, _ := protocol.ResultData(protocol.FileUpload{Xfer: xfer, Path: "/tmp/a.bin", Size: uint64(len(body)), SHA256: make([]byte, 32)})
	ok := true
	all := uint64(len(body))
	msg := send(protocol.ResultItem{T: protocol.ItemResult, Task: taskID[:], Seq: 0, Done: true, OK: &ok, Data: data},
		protocol.AckItem{T: protocol.ItemAck, Xfer: xfer, Next: &all})
	if len(msg.Items) != 0 || tasks.find(taskID).Upload.Acked != int64(len(body)) {
		t.Fatalf("the end: %d items, acked %d", len(msg.Items), tasks.find(taskID).Upload.Acked)
	}
	if p := tasks.find(taskID).DataProblem; p != "the minion's SHA-256 is not the file's" {
		t.Fatalf("problem: %q", p)
	}
	if len(files.released) != 1 || files.released[0] != fileID {
		t.Fatalf("released %v", files.released)
	}
	// A task ended takes no more asks.
	if c := errCode(pull(0, xfer)); c != protocol.ErrItemUnknownTask {
		t.Fatalf("after its end: %q", c)
	}
}
