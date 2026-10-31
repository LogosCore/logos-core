package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

func TestTaskFile(t *testing.T) {
	s, store, files, session, _ := setup(t)
	ctx := context.Background()
	body := []byte("#!/bin/sh\necho hi\n")
	f, err := s.PutTaskFile(ctx, session.OperationID, uuid.New(), "hi.sh", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if f.Total != int64(len(body)) || !bytes.Equal(f.SHA256, sum[:]) || f.Kind != models.MinionFileKindTask ||
		!strings.HasPrefix(f.ContentType, "text/") || len(f.Xfer) != 16 {
		t.Fatalf("kept %+v", f)
	}
	// The operation's, by its id; another's is none.
	if got, err := s.TaskFile(ctx, session.OperationID, f.FileID.String()); err != nil || got.FileID != f.FileID {
		t.Fatalf("TaskFile: %+v %v", got, err)
	}
	if _, err := s.TaskFile(ctx, uuid.New(), f.FileID.String()); !errors.Is(err, ErrNoTaskFile) {
		t.Fatalf("another operation's: %v", err)
	}
	if got, err := s.ReadChunk(ctx, f.FileID, 10, 100); err != nil || string(got) != "echo hi\n" {
		t.Fatalf("ReadChunk: %q %v", got, err)
	}

	// Over the max: refused, nothing kept.
	if _, err := s.PutTaskFile(ctx, session.OperationID, uuid.New(), "big", bytes.NewReader(make([]byte, 101))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	if store.keys("files/") != 1 || len(files.files) != 1 {
		t.Fatalf("%d objects, %d files", store.keys("files/"), len(files.files))
	}
}

// A task file stays while a task that names it has not ended, goes when
// the last one ends; one no task named goes a day after its upload.
func TestTaskFileRelease(t *testing.T) {
	s, store, files, session, _ := setup(t)
	ctx := context.Background()
	f, _ := s.PutTaskFile(ctx, session.OperationID, uuid.New(), "a", bytes.NewReader([]byte("abc")))
	ts := s.tasks.(tasks)
	task := &models.MinionTask{TaskID: uuid.New(), State: models.MinionTaskRunning, Upload: &models.MinionTaskUpload{FileID: f.FileID}}
	ts[task.TaskID] = task

	s.Sweep(ctx)
	if err := s.ReleaseTaskFile(ctx, f.FileID); err != nil || len(files.files) != 1 {
		t.Fatalf("in use: %v, %d files", err, len(files.files))
	}
	task.State = models.MinionTaskDone
	if err := s.ReleaseTaskFile(ctx, f.FileID); err != nil || len(files.files) != 0 || store.keys("files/") != 0 {
		t.Fatalf("its task ended: %v, %d files", err, len(files.files))
	}

	// Named by none: kept a day.
	g, _ := s.PutTaskFile(ctx, session.OperationID, uuid.New(), "b", bytes.NewReader([]byte("abc")))
	s.Sweep(ctx)
	if len(files.files) != 1 {
		t.Fatal("a fresh upload dropped")
	}
	s.now = func() time.Time { return time.Now().UTC().Add(25 * time.Hour) }
	s.Sweep(ctx)
	if _, err := files.FindByID(ctx, g.FileID); err == nil {
		t.Fatal("a day old, named by none: kept")
	}
}
