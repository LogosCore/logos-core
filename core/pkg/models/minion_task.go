package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// MinionTask is a command an operator gave a session's minion
// (docs: contracts/minion-messages.md): queued until the minion asks for
// tasks, sent (again, until it acks it or a result comes), running once
// output comes, and done, failed or cancelled with its last part. The
// output is in MinionTaskParts, one per result part. The team's: every
// member of the operation sees a session's tasks, and who gave each.
type MinionTask struct {
	field.DefaultField `bson:",inline"`

	// TaskID is the id the task travels under: 16 bytes, a UUIDv4.
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	// Command is one of the session's build's commands, Args its params'
	// values as the operator gave them (JSON values: a duration "30s"); the
	// task carries them in their CBOR forms, defaults filled in.
	Command string  `bson:"command" json:"command"`
	Args    JSONMap `bson:"args,omitempty" json:"args,omitempty"`
	// Handler is the command's result handler when it was queued
	// (protocol.BuildCommand.Handler): the shape its data is checked
	// against and joined by. Empty: no declared shape.
	Handler string `bson:"handler,omitempty" json:"handler,omitempty"`
	// WireArgs are the args as the task carries them (protocol.TaskArgs),
	// CBOR-encoded: checked against the command's params when queued, so
	// delivery needs no schema; kept as bytes, so a duration's milliseconds
	// stay an unsigned integer (a document's numbers come back as floats).
	WireArgs []byte `bson:"wire_args,omitempty" json:"-"`
	// IssuedBy is the operator who gave it.
	IssuedBy uuid.UUID `bson:"issued_by" json:"issued_by"`

	State     string     `bson:"state" json:"state"`
	QueuedAt  time.Time  `bson:"queued_at" json:"queued_at"`
	SentAt    *time.Time `bson:"sent_at,omitempty" json:"sent_at,omitempty"`
	SendCount int        `bson:"send_count,omitempty" json:"send_count,omitempty"`
	// AckedAt: the minion acked it, or a result for it came.
	AckedAt   *time.Time `bson:"acked_at,omitempty" json:"acked_at,omitempty"`
	StartedAt *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	DoneAt    *time.Time `bson:"done_at,omitempty" json:"done_at,omitempty"`
	// CancelRequestedAt: an operator asked to stop it after it was sent; a
	// cancel goes with every reply until its last part comes. CancelSends
	// counts them.
	CancelRequestedAt *time.Time `bson:"cancel_requested_at,omitempty" json:"cancel_requested_at,omitempty"`
	CancelSends       int        `bson:"cancel_sends,omitempty" json:"cancel_sends,omitempty"`

	// What the last part said: OK, the process's Exit code, Err.
	OK   *bool  `bson:"ok,omitempty" json:"ok,omitempty"`
	Exit *int64 `bson:"exit,omitempty" json:"exit,omitempty"`
	Err  string `bson:"err,omitempty" json:"err,omitempty"`
	// DoneSeq is the done part's seq: the task has DoneSeq + 1 parts when
	// none is missing (MissingParts).
	DoneSeq *uint64 `bson:"done_seq,omitempty" json:"done_seq,omitempty"`
	// Parts counts the parts kept. OutSize and DataSize are the output and
	// data bytes kept, together at most MaxMinionTaskOutput; OutTruncated
	// and DataTruncated: more came and was dropped.
	Parts         int   `bson:"parts,omitempty" json:"parts,omitempty"`
	OutSize       int64 `bson:"out_size,omitempty" json:"out_size,omitempty"`
	OutTruncated  bool  `bson:"out_truncated,omitempty" json:"out_truncated,omitempty"`
	DataSize      int64 `bson:"data_size,omitempty" json:"data_size,omitempty"`
	DataTruncated bool  `bson:"data_truncated,omitempty" json:"data_truncated,omitempty"`
	// DataProblem is the first part whose data does not fit the handler's
	// shape, and why: the data is then shown raw.
	DataProblem string `bson:"data_problem,omitempty" json:"data_problem,omitempty"`
}

// MissingParts is how many parts before the done part never came: parts
// lost for good, since a minion resends a part until it is acked.
func (t MinionTask) MissingParts() int {
	if t.DoneSeq == nil {
		return 0
	}
	return max(int(*t.DoneSeq)+1-t.Parts, 0)
}

// Minion task states.
const (
	MinionTaskQueued    = "QUEUED"
	MinionTaskSent      = "SENT"
	MinionTaskRunning   = "RUNNING"
	MinionTaskDone      = "DONE"
	MinionTaskFailed    = "FAILED"
	MinionTaskCancelled = "CANCELLED"
)

// MaxMinionTaskOutput is the output and data core keeps of one task,
// together: the rest is dropped (OutTruncated, DataTruncated).
const MaxMinionTaskOutput = 4 << 20

// MaxMinionTasksPerReply caps the tasks one reply carries, whatever the
// minion's get_tasks allows: the rest go in later replies.
const MaxMinionTasksPerReply = 32

// MinionCancelGiveUp is how many cancels core sends for a task the minion
// never acked before it counts it cancelled: the minion most likely never
// got it.
const MinionCancelGiveUp = 3

// Finished reports whether the task is over: done, failed or cancelled.
func (t MinionTask) Finished() bool {
	return t.State == MinionTaskDone || t.State == MinionTaskFailed || t.State == MinionTaskCancelled
}

// MinionTaskPart is one result part of a task, kept as the minion sent it:
// Out raw bytes (in the host's own encoding), DataCBOR the command's
// structured output, the CBOR map as it came (docs:
// contracts/task-result-handlers.md, How core keeps data). Unique by
// (task, seq): a part sent again changes nothing.
type MinionTaskPart struct {
	field.DefaultField `bson:",inline"`

	TaskID   uuid.UUID `bson:"task_id" json:"task_id"`
	Seq      uint64    `bson:"seq" json:"seq"`
	Out      []byte    `bson:"out,omitempty" json:"out,omitempty"`
	DataCBOR []byte    `bson:"data_cbor,omitempty" json:"-"`
	// Data is a part's data as parts were kept before DataCBOR, through
	// JSON: read, never written.
	Data JSONMap   `bson:"data,omitempty" json:"data,omitempty"`
	At   time.Time `bson:"at" json:"at"`
}
