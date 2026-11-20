package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// MinionFile is a file a minion sends core for a download_file or a
// screenshot task (docs: contracts/minion-messages.md, File transfers):
// received in chunks, in order, each kept as an object of its own under
// staging/; once all came, joined into one object and checked against the
// size and SHA-256 the minion said. Kept in the minion files bucket until
// its operation is deleted, with a screenshot's thumbnail (thumbs/).
type MinionFile struct {
	field.DefaultField `bson:",inline"`

	FileID uuid.UUID `bson:"file_id" json:"file_id"`
	// Kind is what the file is: MinionFileKindTask for a task file an
	// operator uploaded for core to send minions (UploadedBy),
	// MinionFileKindScreenshot for a screenshot task's picture; empty for
	// one a minion sent from its file system (a download, a stream's copy).
	Kind        string    `bson:"kind,omitempty" json:"kind,omitempty"`
	UploadedBy  uuid.UUID `bson:"uploaded_by,omitempty" json:"uploaded_by,omitempty"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	// ForTask is the composite task a task file was put for, by its
	// factory (docs: contracts/task-outsourcing, Files): kept while that
	// task, or a subtask that names it, has not ended.
	ForTask uuid.UUID `bson:"for_task,omitempty" json:"for_task,omitempty"`
	// AlsoTasks are tasks that took this file as theirs too: a composite
	// task whose finish took the result of the subtask that got it.
	AlsoTasks []uuid.UUID `bson:"also_tasks,omitempty" json:"also_tasks,omitempty"`
	// Xfer is the transfer's id, the minion's: unique in its session.
	Xfer []byte `bson:"xfer" json:"-"`
	// Name is the file's as the minion has it: a path on its host, or a
	// name alone (a screenshot's picture is on no file system).
	Name string `bson:"name" json:"name"`
	// Total is the size the first chunk said; Received the bytes taken, in
	// order: the next chunk is the one from Received.
	Total    int64 `bson:"total" json:"total"`
	Received int64 `bson:"received" json:"received"`
	// SHA256 is the whole's, as the last chunk said it.
	SHA256 []byte `bson:"sha256,omitempty" json:"sha256,omitempty"`
	// ContentType is what its first bytes say it is (mimetype), set when it
	// is kept; empty before.
	ContentType string `bson:"content_type,omitempty" json:"content_type,omitempty"`
	State       string `bson:"state" json:"state"`
	Err         string `bson:"err,omitempty" json:"err,omitempty"`
	// LastChunkAt is when a chunk last came: a transfer idle for long is
	// given up.
	LastChunkAt time.Time  `bson:"last_chunk_at" json:"last_chunk_at"`
	DoneAt      *time.Time `bson:"done_at,omitempty" json:"done_at,omitempty"`

	// A screenshot's picture: its size in pixels and its screen, as its
	// task's data said them (Display empty for every screen in one
	// picture), copied when the task's last part came; Thumb, a thumbnail
	// of it is kept (thumbs/<file>), made when the picture was kept.
	Width   int64  `bson:"width,omitempty" json:"width,omitempty"`
	Height  int64  `bson:"height,omitempty" json:"height,omitempty"`
	Display string `bson:"display,omitempty" json:"display,omitempty"`
	Thumb   bool   `bson:"thumb,omitempty" json:"thumb,omitempty"`
}

// MinionFileKindTask is a task file: one an operator uploaded for a task's
// file param, which core sends the minion (docs:
// contracts/task-results-and-actions.md, Task files). Kept COMPLETE from its
// upload while a task that names it has not ended.
const MinionFileKindTask = "task"

// MinionFileKindScreenshot is a screenshot task's picture (docs:
// contracts/task-results-and-actions.md, screenshot): a file a minion made
// and sent, on no file system; core finds an operation's by this kind.
const MinionFileKindScreenshot = "screenshot"

// Minion file states.
const (
	// MinionFileReceiving: chunks coming.
	MinionFileReceiving = "RECEIVING"
	// MinionFileJoining: all came; the chunks are being joined and checked.
	MinionFileJoining = "JOINING"
	// MinionFileComplete: kept, its size and SHA-256 as the minion said.
	MinionFileComplete = "COMPLETE"
	// MinionFileFailed: it did not check out, or was given up.
	MinionFileFailed = "FAILED"
	// MinionFileCancelled: its task was stopped or failed on the way.
	MinionFileCancelled = "CANCELLED"
)

// Ended reports whether no more chunks are taken for the file.
func (f MinionFile) Ended() bool {
	return f.State == MinionFileComplete || f.State == MinionFileFailed || f.State == MinionFileCancelled
}
