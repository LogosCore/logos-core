package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
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
	Command string `bson:"command" json:"command"`
	// Op is the command's number when it was queued
	// (protocol.BuildCommand.Op): what the task names it by on the wire.
	Op   int     `bson:"op,omitempty" json:"-"`
	Args JSONMap `bson:"args,omitempty" json:"args,omitempty"`
	// CredentialIDs are the credentials Args name (credential params): the
	// task carries them in their place.
	CredentialIDs []uuid.UUID `bson:"credential_ids,omitempty" json:"credential_ids,omitempty"`
	// As is who the task ran as when not its process's own: what its
	// minion said of it, else the session's acting identity when the task
	// was acked. How "" (the process's own) when the minion said so whatever
	// the session's; nil when nothing was said.
	As *MinionIdentity `bson:"as,omitempty" json:"as,omitempty"`
	// Result is the command's result when it was queued
	// (protocol.BuildCommand.Result): the shape its data is checked
	// against and joined by. Empty: no declared shape.
	Result string `bson:"result,omitempty" json:"result,omitempty"`
	// Action is the command's action when it was queued
	// (protocol.BuildCommand.Action): what the file browser reads a task
	// without a result by (a delete_path's rm).
	Action string `bson:"action,omitempty" json:"action,omitempty"`
	// WireArgs are the args as the task carries them (protocol.Args, by
	// their params' numbers), CBOR-encoded: checked against the command's params when queued, so
	// delivery needs no schema; kept as bytes, so a duration's milliseconds
	// stay an unsigned integer (a document's numbers come back as floats).
	WireArgs []byte `bson:"wire_args,omitempty" json:"-"`
	// IssuedBy is the operator who gave it.
	IssuedBy uuid.UUID `bson:"issued_by" json:"issued_by"`
	// ImportantAt and ImportantBy: an operator marked it important, when
	// and who (a star in every console: the team's); nil when it is not.
	ImportantAt *time.Time `bson:"important_at,omitempty" json:"important_at,omitempty"`
	ImportantBy *uuid.UUID `bson:"important_by,omitempty" json:"important_by,omitempty"`
	// LineRest is the param its line shows bare, after the command: its
	// command's first param when that one takes the rest of a typed line (a
	// shell's command line, a path). Empty: every value goes as an option.
	// Set when it is queued, from its command's params.
	LineRest string `bson:"line_rest,omitempty" json:"-"`
	// LineJSON are its structured params (protocol.FieldStructured): its line
	// shows their values as JSON, which their stored args cannot tell from a
	// string_map's or a list's. Set when it is queued.
	LineJSON []string `bson:"line_json,omitempty" json:"-"`
	// ConnectionText says the connection its connection param names (an
	// int of protocol.FormatConnection: switch_connection's conn) as it was
	// when it was queued, for people: "2 · http · reverse-persistent ·
	// url-based · wss://edge.example.com". Its line says only the place,
	// which the session's connections may give another later. Empty without
	// such a param, or when the connection was not known.
	ConnectionText string `bson:"connection_text,omitempty" json:"connection_text,omitempty"`
	// SearchText is its line as the console shows it: what the session's
	// task list search reads (MinionTaskSearchText).
	SearchText string `bson:"search_text,omitempty" json:"-"`

	State    string    `bson:"state" json:"state"`
	QueuedAt time.Time `bson:"queued_at" json:"queued_at"`
	// ScheduledAt is when a task queued with a time goes on (docs:
	// contracts/minion-messages/tasks-and-results, Scheduled tasks): it
	// waits SCHEDULED until then. Kept once it went on; Run now drops it.
	ScheduledAt *time.Time `bson:"scheduled_at,omitempty" json:"scheduled_at,omitempty"`
	SentAt      *time.Time `bson:"sent_at,omitempty" json:"sent_at,omitempty"`
	SendCount   int        `bson:"send_count,omitempty" json:"send_count,omitempty"`
	// AckedAt: the minion acked it, or a result for it came.
	AckedAt   *time.Time `bson:"acked_at,omitempty" json:"acked_at,omitempty"`
	StartedAt *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	DoneAt    *time.Time `bson:"done_at,omitempty" json:"done_at,omitempty"`
	// CancelRequestedAt: an operator asked to stop it after it was sent; a
	// cancel goes with every reply until its last part comes. CancelSends
	// counts them.
	CancelRequestedAt *time.Time `bson:"cancel_requested_at,omitempty" json:"cancel_requested_at,omitempty"`
	// CancelReason is why core asked to stop it, when core did (a tunnel
	// idle past its stop_idle, its port not opening again); empty when an
	// operator did.
	CancelReason string `bson:"cancel_reason,omitempty" json:"cancel_reason,omitempty"`
	// ResumeCtx is the ctx of the task's newest part that said one (its
	// ResumeSeq): where a minion that keeps nothing stands with it (a
	// bind-call page's walk). Core gives it back with a resume, and with a
	// cancel.
	ResumeCtx   []byte  `bson:"resume_ctx,omitempty" json:"-"`
	ResumeSeq   *uint64 `bson:"resume_seq,omitempty" json:"-"`
	CancelSends int     `bson:"cancel_sends,omitempty" json:"cancel_sends,omitempty"`

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
	// FeedbackErrors and FeedbackWarnings count the feedback its minion
	// sent about it (models.MinionFeedback): a task list marks it by them.
	// FeedbackInfos counts its information, which marks nothing: it shows
	// the task has feedback to read.
	FeedbackErrors   int `bson:"feedback_errors,omitempty" json:"feedback_errors,omitempty"`
	FeedbackWarnings int `bson:"feedback_warnings,omitempty" json:"feedback_warnings,omitempty"`
	FeedbackInfos    int `bson:"feedback_infos,omitempty" json:"feedback_infos,omitempty"`
	// Recordings counts a desktop task's recordings kept
	// (models.Recording); RecordingsMade how many it had, deleted
	// ones too: the next one's number is one more.
	Recordings     int   `bson:"recordings,omitempty" json:"recordings,omitempty"`
	RecordingsMade int64 `bson:"recordings_made,omitempty" json:"recordings_made,omitempty"`
	// DataParts counts the parts with data: a client showing the data
	// reads it again when it grows (Parts grows with listing pieces too).
	DataParts int `bson:"data_parts,omitempty" json:"data_parts,omitempty"`
	// LatestData is the data of a result whose every part says it whole (a
	// dir_walk's progress: taskdata.Latest): the newest part's, by
	// LatestDataSeq, kept here instead of on its part, so reading it is one
	// row however many parts came, and it is not cut by the 4 MiB budget.
	LatestData    []byte  `bson:"latest_data,omitempty" json:"-"`
	LatestDataSeq *uint64 `bson:"latest_data_seq,omitempty" json:"-"`

	// Upload is the task file core sends the minion for the task's file
	// param (docs: contracts/minion-messages.md, Core sending a file); nil
	// for a task without one.
	Upload *MinionTaskUpload `bson:"upload,omitempty" json:"upload,omitempty"`
	// Stream is the file the task's minion streams to whoever downloads
	// it (docs: contracts/minion-messages.md, Streaming a file), from its
	// announcement; nil before, and for other tasks.
	Stream *MinionTaskStream `bson:"stream,omitempty" json:"stream,omitempty"`
	// Tunnel is what a tunnel task's tunnel came to, kept when it closed:
	// its streams are not (core holds them in memory while it runs). Nil
	// before, and for other tasks.
	Tunnel *MinionTaskTunnel `bson:"tunnel,omitempty" json:"tunnel,omitempty"`

	// Composite is a composite command's task's (docs: contracts/
	// task-outsourcing): what its factory's code decided so far, and what
	// core keeps for it between the calls. Nil for any other task.
	Composite *MinionComposite `bson:"composite,omitempty" json:"composite,omitempty"`
	// ParentID is the composite task a subtask runs for: it issued it, and
	// hears of its end. Nil for any other task.
	ParentID *uuid.UUID `bson:"parent_id,omitempty" json:"parent_id,omitempty"`
	// SubtaskKey is the factory's own name for a subtask (never shown); SubtaskAs
	// the line naming it in its view; Cleanup: a subtask a stopped composite
	// runs to clean up, whose end calls the factory no more.
	SubtaskKey string `bson:"subtask_key,omitempty" json:"-"`
	SubtaskAs  string `bson:"subtask_as,omitempty" json:"subtask_as,omitempty"`
	Cleanup    bool   `bson:"cleanup,omitempty" json:"cleanup,omitempty"`
	// Heard: its composite task took its end (a subtask's).
	Heard bool `bson:"heard,omitempty" json:"-"`
	// ResultOf is the subtask whose result a composite command with no op of
	// its own took as its own (its data, output and files).
	ResultOf *uuid.UUID `bson:"result_of,omitempty" json:"result_of,omitempty"`

	// Factory is a factory command's task's (docs: contracts/factory-commands):
	// its factory runs it, never the minion. Where it stands: waiting for an
	// instance to take it, or running on one. Nil for any other task.
	Factory *MinionFactoryRun `bson:"factory,omitempty" json:"factory,omitempty"`
}

// MinionFactoryRun is where a factory command's task stands (docs:
// contracts/factory-commands): offered to its factory's instances until one
// takes it, then running there until its result, a stop, or core gives up on
// it.
type MinionFactoryRun struct {
	// Module is the factory kind that runs it: its session's build's.
	Module string `bson:"module" json:"module"`
	// Args are its args as its call carries them: the operator's values over
	// the command's defaults, as JSON values.
	Args JSONMap `bson:"args,omitempty" json:"-"`
	// Files are the task files its args name (file params): linked in its
	// call, and kept while it runs, as a minion task's upload is.
	Files []uuid.UUID `bson:"files,omitempty" json:"-"`
	// Instance is the instance it was offered to, or runs on; "" while it
	// waits. OfferedAt is when core offered it there; AcceptedAt when the
	// instance took it (its reply, or a report that came first).
	Instance   string     `bson:"instance,omitempty" json:"instance,omitempty"`
	OfferedAt  *time.Time `bson:"offered_at,omitempty" json:"offered_at,omitempty"`
	AcceptedAt *time.Time `bson:"accepted_at,omitempty" json:"accepted_at,omitempty"`
	// ReportAt is the latest report of the instance that runs it (its
	// acceptance, its output): protocol.FactoryCommandNoReport past it fails
	// the task. Seq is the latest output report's seq core kept.
	ReportAt *time.Time `bson:"report_at,omitempty" json:"report_at,omitempty"`
	Seq      int64      `bson:"seq,omitempty" json:"-"`
	// ResultAt is when its factory's result ended it: a result sent again
	// is answered as the first was.
	ResultAt *time.Time `bson:"result_at,omitempty" json:"result_at,omitempty"`
	// Waiting is why no instance took it at its latest offer, WaitingSince
	// since when it waits for one, OfferAt when core offers it next: cleared
	// once one takes it.
	Waiting      string     `bson:"waiting,omitempty" json:"waiting,omitempty"`
	WaitingSince *time.Time `bson:"waiting_since,omitempty" json:"waiting_since,omitempty"`
	OfferAt      *time.Time `bson:"offer_at,omitempty" json:"-"`
	// LeaseBy is the core process that offers it, until LeaseUntil.
	LeaseBy    string     `bson:"lease_by,omitempty" json:"-"`
	LeaseUntil *time.Time `bson:"lease_until,omitempty" json:"-"`
}

// MinionComposite is where a composite command's task stands (docs:
// contracts/task-outsourcing): the factory's ctx and status, its own op,
// the ends its factory has not heard of, the subtasks waiting in it, its
// timer and the call out about it. Core keeps it all, so any factory
// instance may answer, and a restart of core loses nothing.
type MinionComposite struct {
	// OwnOp is where its own op stands: protocol.OwnOpNotRun, OwnOpOut,
	// OwnOpEnded; "" for a command with no op of its own.
	OwnOp string `bson:"own_op,omitempty" json:"own_op,omitempty"`
	// OwnArgs are the args its own op ran with, when a decision gave its own
	// (the task's Args are the operator's); nil for the task's.
	OwnArgs JSONMap `bson:"own_args,omitempty" json:"own_args,omitempty"`
	// OwnEnd is how its own op ended: the task's own state, ok and err stay
	// for the task's end.
	OwnEnd *MinionComposeOwnEnd `bson:"own_end,omitempty" json:"own_end,omitempty"`
	// Status is the line its factory's code shows on it while it runs.
	Status string `bson:"status,omitempty" json:"status,omitempty"`
	// Ctx is the factory's own bytes, as its decisions left them.
	Ctx []byte `bson:"ctx,omitempty" json:"-"`
	// Seq is the number of the latest call about it (from 1); Answered the
	// latest whose answer core took.
	Seq      int `bson:"seq" json:"seq"`
	Answered int `bson:"answered" json:"answered"`
	// Pending are the ends its factory has not heard of, in order: each goes
	// in a call of its own, then the stopped call takes all that are left.
	Pending []MinionComposeEnd `bson:"pending,omitempty" json:"-"`
	// Waiting are subtasks its decisions asked for past those under way at
	// once: queued, in order, as others end.
	Waiting []MinionSubtask `bson:"waiting,omitempty" json:"-"`
	// Subtasks counts those its decisions asked for, in all (protocol.MaxSubtasks).
	Subtasks int `bson:"subtasks" json:"subtasks"`
	// TimerAt is when the timer its latest decision set runs out; nil for none.
	TimerAt *time.Time `bson:"timer_at,omitempty" json:"timer_at,omitempty"`
	// Phase is what the task waits for: "" its factory's code; one of the
	// Compose* phases after a finish or a stop.
	Phase string `bson:"phase,omitempty" json:"phase,omitempty"`
	// Finish is the finish its factory decided, applied once what was under
	// way has ended.
	Finish *MinionComposeFinish `bson:"finish,omitempty" json:"-"`
	// StoppedAt: an operator stopped it, when; StoppedCallAt: the stopped
	// call went, when (its answer waits a minute at most).
	StoppedAt     *time.Time `bson:"stopped_at,omitempty" json:"stopped_at,omitempty"`
	StoppedCallAt *time.Time `bson:"stopped_call_at,omitempty" json:"-"`
	// CallAt is when the call out about it went (nil: none is out), and
	// CallDeadline when core stops waiting for its answer. CallEvent is the
	// latest call's event (Seq's): one sent again is the same.
	CallAt       *time.Time `bson:"call_at,omitempty" json:"-"`
	CallDeadline *time.Time `bson:"call_deadline,omitempty" json:"-"`
	CallEvent    string     `bson:"call_event,omitempty" json:"-"`
	// Cancel and SendOwn are what the latest decision asked of the task
	// that core has not done yet: what it stops, and its own op to send (its
	// subtasks are in Waiting). Kept with the decision, so a restart of core
	// does them still, and once.
	Cancel  *MinionComposeCancel `bson:"cancel,omitempty" json:"-"`
	SendOwn *MinionOwnOpRun      `bson:"send_own,omitempty" json:"-"`
	// SilentSince is since when its factory has not answered the call out
	// (no instance took it, or none answered in time): ComposeNoAnswer of it
	// fails the task.
	SilentSince *time.Time `bson:"silent_since,omitempty" json:"silent_since,omitempty"`
	// LeaseBy is the core process that makes its calls, until LeaseUntil.
	LeaseBy    string     `bson:"lease_by,omitempty" json:"-"`
	LeaseUntil *time.Time `bson:"lease_until,omitempty" json:"-"`
	// WakeAt is when core looks at it next: its timer, the call's deadline,
	// an end to take. Nil once it ended.
	WakeAt *time.Time `bson:"wake_at,omitempty" json:"-"`
}

// Phases of a composite task past its factory's code (MinionComposite.Phase).
const (
	// ComposeFinishing: its factory finished it; what was under way ends
	// first.
	ComposeFinishing = "finishing"
	// ComposeStopping: an operator stopped it; once what was under way has
	// ended, its factory hears of it (the stopped call).
	ComposeStopping = "stopping"
	// ComposeCleanup: its factory answered the stopped call; it ends
	// cancelled once the cleanup subtasks have.
	ComposeCleanup = "cleanup"
	// ComposeClosed: its session closed: core calls its factory no more.
	ComposeClosed = "closed"
)

// MinionComposeOwnEnd is how a composite task's own op ended.
type MinionComposeOwnEnd struct {
	// State is a protocol.EndState.
	State string     `bson:"state" json:"state"`
	OK    bool       `bson:"ok" json:"ok"`
	Exit  *int64     `bson:"exit,omitempty" json:"exit,omitempty"`
	Err   string     `bson:"err,omitempty" json:"err,omitempty"`
	At    time.Time  `bson:"at" json:"at"`
	Seq   *uint64    `bson:"seq,omitempty" json:"-"`
	Done  *time.Time `bson:"done,omitempty" json:"-"`
}

// MinionComposeEnd is an end its factory has not heard of: a subtask's
// (TaskID), its own op's (OwnOp), or, with no TaskID, a subtask that never
// became a task (refused, or dropped while it waited), told whole here.
type MinionComposeEnd struct {
	TaskID  *uuid.UUID `bson:"task_id,omitempty"`
	OwnOp   bool       `bson:"own_op,omitempty"`
	Command string     `bson:"command,omitempty"`
	Args    JSONMap    `bson:"args,omitempty"`
	Key     string     `bson:"key,omitempty"`
	As      string     `bson:"as,omitempty"`
	State   string     `bson:"state,omitempty"`
	Err     string     `bson:"err,omitempty"`
	At      time.Time  `bson:"at"`
}

// MinionSubtask is a subtask a decision asked for, waiting in its task. Its
// task id is drawn when the decision is taken: queued twice (core stopped
// between queueing it and taking it from here), it is one task still.
type MinionSubtask struct {
	TaskID  uuid.UUID `bson:"task_id"`
	Command string    `bson:"command"`
	Args    JSONMap   `bson:"args,omitempty"`
	Key     string    `bson:"key,omitempty"`
	As      string    `bson:"as,omitempty"`
	// Cleanup: one the answer to the stopped call asked for.
	Cleanup bool `bson:"cleanup,omitempty"`
}

// MinionComposeCancel is what a decision stops: its subtasks with one of
// Keys, its own op (OwnOp).
type MinionComposeCancel struct {
	Keys  []string `bson:"keys,omitempty"`
	OwnOp bool     `bson:"own_op,omitempty"`
}

// MinionOwnOpRun is a decision's own op to send: with Args, or the task's
// own when nil.
type MinionOwnOpRun struct {
	Args JSONMap `bson:"args,omitempty"`
}

// MinionComposeFinish is a finish its factory decided, kept until what was
// under way has ended.
type MinionComposeFinish struct {
	OK       bool      `bson:"ok"`
	Message  string    `bson:"message,omitempty"`
	Err      string    `bson:"err,omitempty"`
	Out      []byte    `bson:"out,omitempty"`
	OutFile  string    `bson:"out_file,omitempty"`
	Data     []byte    `bson:"data,omitempty"`
	DataFile string    `bson:"data_file,omitempty"`
	ResultOf string    `bson:"result_of,omitempty"`
	At       time.Time `bson:"at"`
}

// MinionTaskTunnel is a tunnel task's totals: how many streams it had (the
// last one's number), and their bytes each way (Up: what its tools sent,
// Down: what came back).
type MinionTaskTunnel struct {
	Streams uint64 `bson:"streams" json:"streams"`
	Up      int64  `bson:"up" json:"up"`
	Down    int64  `bson:"down" json:"down"`
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
	// Ctx is the transfer's ctx, as the minion's announcement set it: every
	// ack of the stream carries it, those core sends unasked too (a
	// downloader came, or made room), so a minion that keeps nothing (a
	// bind-call page) finds there what it streams.
	Ctx []byte `bson:"ctx,omitempty" json:"-"`
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
	// Ctx is a stream's ctx, as the minion's latest ask set it: the acks
	// core sends unasked (its uploader's bytes came) carry it.
	Ctx []byte `bson:"ctx,omitempty" json:"-"`
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
	// MinionTaskScheduled is a task queued with a time (ScheduledAt) that
	// has not come: it waits in core, nothing of it gone anywhere (its
	// minion, its factory's code, its factory), and goes on at its time in
	// the state it would have been queued in (StartState).
	MinionTaskScheduled = "SCHEDULED"
	MinionTaskQueued    = "QUEUED"
	MinionTaskSent      = "SENT"
	MinionTaskRunning   = "RUNNING"
	// MinionTaskComposing is a composite command's task while it waits in
	// core for its factory's code: it never goes to the minion, and takes
	// none of its room. Its own op, when a decision sends it, makes it
	// QUEUED, then SENT and RUNNING as any task; its done part makes it
	// COMPOSING again: the task ends at its factory's finish.
	MinionTaskComposing = "COMPOSING"
	// MinionTaskFactory is a factory command's task (docs:
	// contracts/factory-commands) until it ends: its factory runs it, never
	// the minion, so it takes none of the minion's room and no reply carries
	// it. It waits for an instance to take it (Factory.Instance empty), then
	// runs there; it ends DONE, FAILED or CANCELLED as any task.
	MinionTaskFactory   = "FACTORY"
	MinionTaskDone      = "DONE"
	MinionTaskFailed    = "FAILED"
	MinionTaskCancelled = "CANCELLED"
)

// MinionTaskOpenStates are the states of a task that has not ended.
var MinionTaskOpenStates = []string{MinionTaskScheduled, MinionTaskQueued, MinionTaskSent, MinionTaskRunning, MinionTaskComposing,
	MinionTaskFactory}

// StartState is the state a SCHEDULED task goes on in at its time, the one
// it would have been queued in: COMPOSING for a composite command's (its
// factory's code then called), FACTORY for a factory command's (then
// offered to its factory), else QUEUED (for its minion).
func (t MinionTask) StartState() string {
	switch {
	case t.Composite != nil:
		return MinionTaskComposing
	case t.Factory != nil:
		return MinionTaskFactory
	}
	return MinionTaskQueued
}

// InFactory reports whether t is a factory command's task that has not
// ended: waiting for an instance of its factory, or running on one.
func (t MinionTask) InFactory() bool {
	return t.Factory != nil && !t.Finished()
}

// OwnOpOut reports whether t is a composite task whose own op is out (sent,
// or queued to go): its minion's parts and ends are the own op's.
func (t MinionTask) OwnOpOut() bool {
	return t.Composite != nil && t.Composite.OwnOp == string(protocol.OwnOpOut)
}

// OwnOpEnded reports whether t is a composite task whose own op ended: a
// part its minion sends now is late.
func (t MinionTask) OwnOpEnded() bool {
	return t.Composite != nil && t.Composite.OwnOp == string(protocol.OwnOpEnded)
}

// Composing reports whether t is a composite command's task that has not
// ended: its factory's code runs it.
func (t MinionTask) Composing() bool {
	return t.Composite != nil && !t.Finished()
}

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
