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

	// Rev counts the task's writes: 1 when queued, one more with each change.
	// A client keeps the copy with the higher one, whichever comes last (a
	// mutation's answer after the live updates it raced).
	Rev int64 `bson:"rev" json:"rev"`
	// TaskID is the id the task travels under: 16 bytes, a UUIDv4.
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	// Command is one of the session's build's commands, Args its params'
	// values as the operator gave them (JSON values: a duration "30s"); the
	// task carries them in their CBOR forms, defaults filled in.
	Command string  `bson:"command" json:"command"`
	Args    JSONMap `bson:"args,omitempty" json:"args,omitempty"`
	// Result is the command's result when it was queued
	// (protocol.BuildCommand.Result): the shape its data is checked
	// against and joined by. Empty: no declared shape.
	Result string `bson:"result,omitempty" json:"result,omitempty"`
	// Action is the command's action when it was queued
	// (protocol.BuildCommand.Action): what the file browser reads a task
	// without a result by (a delete_path's rm).
	Action string `bson:"action,omitempty" json:"action,omitempty"`
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
	// DataProblem is the first part whose data does not fit the result's
	// shape, and why: the data is then shown raw.
	DataProblem string `bson:"data_problem,omitempty" json:"data_problem,omitempty"`

	// Upload is the task file core sends the minion for the task's file
	// param (docs: contracts/minion-messages.md, Core sending a file); nil
	// for a task without one.
	Upload *MinionTaskUpload `bson:"upload,omitempty" json:"upload,omitempty"`
	// Stream is the file the task's minion streams to whoever downloads
	// it (docs: contracts/minion-messages.md, Streaming a file), from its
	// announcement; nil before, and for other tasks.
	Stream *MinionTaskStream `bson:"stream,omitempty" json:"stream,omitempty"`
}

// MinionTaskStream is a file a minion streams: what it announced, and
// where its downloader and the minion are. Core keeps a few chunks of it at
// a time (minionstreams), never the file.
type MinionTaskStream struct {
	Xfer []byte `bson:"xfer" json:"-"`
	// Name is the file's path on the host; Size what the minion announced.
	Name string `bson:"name" json:"name"`
	Size int64  `bson:"size" json:"size"`
	// Gen counts the downloaders: each takes the stream from the one before
	// (the newest wins), and the chunks kept for one are not another's.
	Gen int64 `bson:"gen" json:"gen"`
	// Reader is the downloader now (an id of its request), "" for none;
	// ReaderAt when it last said it is there.
	Reader   string     `bson:"reader,omitempty" json:"reader,omitempty"`
	ReaderAt *time.Time `bson:"reader_at,omitempty" json:"reader_at,omitempty"`
	// Want is where core wants the minion's next chunk from: the chunks
	// before it are kept for the downloader or written to it. Delivered is
	// how far the downloader has the file.
	Want      int64 `bson:"want" json:"want"`
	Delivered int64 `bson:"delivered" json:"delivered"`
	// Ran: a downloader took the stream once. From and AttachedAt are
	// where and when the downloader now took it: its rate is what it got
	// since.
	Ran        bool       `bson:"ran,omitempty" json:"ran,omitempty"`
	From       int64      `bson:"from" json:"from"`
	AttachedAt *time.Time `bson:"attached_at,omitempty" json:"attached_at,omitempty"`
	// IdleSince is when the stream last had no downloader: an hour of it
	// ends the task.
	IdleSince   *time.Time `bson:"idle_since,omitempty" json:"idle_since,omitempty"`
	AnnouncedAt time.Time  `bson:"announced_at" json:"announced_at"`
	// SHA256 is the whole's, when the minion read it in order once.
	SHA256 []byte `bson:"sha256,omitempty" json:"sha256,omitempty"`
}

// MinionTaskUpload is a task's file going to its minion: the file as it
// was when the task was queued (the file itself is dropped once its tasks
// end), the transfer's id, and how far the minion has it.
type MinionTaskUpload struct {
	FileID uuid.UUID `bson:"file_id" json:"file_id"`
	Name   string    `bson:"name" json:"name"`
	Size   int64     `bson:"size" json:"size"`
	SHA256 []byte    `bson:"sha256" json:"sha256"`
	Xfer   []byte    `bson:"xfer" json:"-"`
	// Acked is how far the minion has the file: its last ack's next.
	Acked int64 `bson:"acked" json:"acked"`
	// FirstAckAt is the minion's first ask for a chunk; LastAckAt its last.
	FirstAckAt *time.Time `bson:"first_ack_at,omitempty" json:"first_ack_at,omitempty"`
	LastAckAt  *time.Time `bson:"last_ack_at,omitempty" json:"last_ack_at,omitempty"`

	// A stream (docs: contracts/minion-messages.md, Streaming a file to a
	// minion): no task file; the file comes as its uploader sends it, the
	// size once it says it (Sized), the SHA-256 once all of it came.
	Stream bool `bson:"stream,omitempty" json:"stream,omitempty"`
	Sized  bool `bson:"sized,omitempty" json:"sized,omitempty"`
	// Received is how much core took from the uploader: the chunks from
	// Acked to it wait in the buffer for the minion. HashState is the
	// SHA-256 of those bytes so far (encoding.BinaryMarshaler's).
	Received  int64  `bson:"received" json:"received"`
	HashState []byte `bson:"hash_state,omitempty" json:"-"`
	// The uploader now, as a stream's downloader (MinionTaskStream).
	Gen        int64      `bson:"gen,omitempty" json:"gen,omitempty"`
	Uploader   string     `bson:"uploader,omitempty" json:"uploader,omitempty"`
	UploaderAt *time.Time `bson:"uploader_at,omitempty" json:"uploader_at,omitempty"`
	From       int64      `bson:"from,omitempty" json:"from,omitempty"`
	AttachedAt *time.Time `bson:"attached_at,omitempty" json:"attached_at,omitempty"`
	IdleSince  *time.Time `bson:"idle_since,omitempty" json:"idle_since,omitempty"`
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
// contracts/task-results-and-actions.md, How core keeps data). Unique by
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
