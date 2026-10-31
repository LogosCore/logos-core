package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// MinionFile is a file a minion sends core for a download_file task
// (docs: contracts/minion-messages.md, File transfers): received in chunks,
// in order, each kept as an object of its own under staging/; once all
// came, joined into one object and checked against the size and SHA-256
// the minion said. Kept in the minion files bucket until its operation is
// deleted.
type MinionFile struct {
	field.DefaultField `bson:",inline"`

	FileID uuid.UUID `bson:"file_id" json:"file_id"`
	// Kind is MinionFileKindTask for a task file an operator uploaded for
	// core to send minions (UploadedBy); empty for one a minion sent.
	Kind        string    `bson:"kind,omitempty" json:"kind,omitempty"`
	UploadedBy  uuid.UUID `bson:"uploaded_by,omitempty" json:"uploaded_by,omitempty"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	// Xfer is the transfer's id, the minion's: unique in its session.
	Xfer []byte `bson:"xfer" json:"-"`
	// Name is the file's as the minion has it: a path on its host.
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
}

// MinionFileKindTask is a task file: one an operator uploaded for a task's
// file param, which core sends the minion (docs:
// contracts/task-results-and-actions.md, Task files). Kept COMPLETE from its
// upload while a task that names it has not ended.
const MinionFileKindTask = "task"

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
