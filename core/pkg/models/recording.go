package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// Recording is the recording of one desktop a desktop task's tunnel drew
// in the web app, or of one terminal session of a terminal task (docs:
// contracts/minion-messages/tunnels.md, Recordings): who opened it and
// when, what it opened with, how far it came and how it ended. What it
// recorded is in the recordings bucket (DESKTOP_RECORDING_BUCKET, named
// when desktops alone were recorded, as this collection is), in parts as
// core kept it (pkg/recordings), in its Format. Kept until its operation
// is deleted, or an operator deletes it.
type Recording struct {
	field.DefaultField `bson:",inline"`

	RecordingID uuid.UUID `bson:"recording_id" json:"recording_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	// Number is its number in the task, from 1: the task's recordings are
	// listed by it, the latest first.
	Number int64 `bson:"number" json:"number"`
	// Front is the task's: rdp, vnc, ssh or telnet (a desktop), or
	// terminal.
	Front string `bson:"front" json:"front"`
	// Format is its file's (RecordingFormat...): a desktop's guac, a
	// terminal's asciicast.
	Format string `bson:"format" json:"format"`
	// UserID and Username are who opened it: nil and empty for a
	// terminal's raw port, which no one signs in to.
	UserID   uuid.UUID `bson:"user_id" json:"user_id"`
	Username string    `bson:"username" json:"username"`
	// Client is who, as the task's streams list says it: web: <user>, or
	// a raw client's address. Stream is a terminal's stream (0 for a
	// desktop).
	Client string `bson:"client,omitempty" json:"client,omitempty"`
	Stream uint64 `bson:"stream,omitempty" json:"stream,omitempty"`
	// Settings are what it opened with: the settings the operator picked
	// (desktops.Fields), as the web app sent them, each left out at its
	// default.
	Settings JSONMap `bson:"settings,omitempty" json:"settings,omitempty"`
	// Width and Height are the size it opened at: a desktop's display, in
	// pixels; a terminal's columns and rows.
	Width  int `bson:"width" json:"width"`
	Height int `bson:"height" json:"height"`
	// Keys: it holds the operator's mouse and keys, or what was typed in
	// the terminal. A view-only desktop's does not: its server took none.
	Keys  bool   `bson:"keys" json:"keys"`
	State string `bson:"state" json:"state"`
	// StartedAt is when core asked guacd for the desktop; EndedAt when it
	// ended.
	StartedAt time.Time  `bson:"started_at" json:"started_at"`
	EndedAt   *time.Time `bson:"ended_at,omitempty" json:"ended_at,omitempty"`
	// Parts is how many parts it is kept in (parts/<recording>/<n>, from 0,
	// each after the one before), Size their bytes.
	Parts int   `bson:"parts" json:"parts"`
	Size  int64 `bson:"size" json:"size"`
	// DurationMs is how long it plays: a desktop's from guacd's first frame
	// to its last, as guacd timed them; a terminal's to its last event.
	DurationMs int64 `bson:"duration_ms" json:"duration_ms"`
	// Lost is the bytes core could not keep (its store failed): the
	// recording plays past where they were.
	Lost int64 `bson:"lost,omitempty" json:"lost,omitempty"`
	// End is how it ended (RecordingEnd...), empty while it records; Msg
	// what was said: guacd's error, the process's exit, or why it did not
	// open.
	End string `bson:"end,omitempty" json:"end,omitempty"`
	Msg string `bson:"msg,omitempty" json:"msg,omitempty"`
	// SeenAt is when the core recording it last kept its numbers: one
	// recording with none for a while lost its core, and is ended.
	SeenAt time.Time `bson:"seen_at" json:"seen_at"`

	// Video is a desktop's recording made a video (RecordingVideo...),
	// once it ended, where core has an encoder (docs: Tunnels,
	// Recordings): empty until then. Converted, its parts are gone, its
	// video (VideoSize bytes) and keys kept in their place; failed
	// (VideoError says why), its parts stay and play.
	Video      string `bson:"video,omitempty" json:"video,omitempty"`
	VideoSize  int64  `bson:"video_size,omitempty" json:"video_size,omitempty"`
	VideoError string `bson:"video_error,omitempty" json:"video_error,omitempty"`
	// VideoTries counts the encoder's failures on it; VideoNextAt is when
	// it is tried again (after a failure, or an encoder that did not
	// answer); VideoLease how long the core converting it holds it.
	VideoTries  int        `bson:"video_tries,omitempty" json:"video_tries,omitempty"`
	VideoNextAt *time.Time `bson:"video_next_at,omitempty" json:"video_next_at,omitempty"`
	VideoLease  *time.Time `bson:"video_lease,omitempty" json:"video_lease,omitempty"`
}

// A desktop recording's video (Recording.Video).
const (
	// RecordingVideoConverting: the encoder has it.
	RecordingVideoConverting = "CONVERTING"
	// RecordingVideoConverted: it is a video now; its parts are gone.
	RecordingVideoConverted = "CONVERTED"
	// RecordingVideoFailed: the encoder failed on it too often; its parts
	// stay, and play.
	RecordingVideoFailed = "FAILED"
)

// FileFormat is its Format; a desktop's recorded before recordings had
// one, guac.
func (r *Recording) FileFormat() string {
	if r.Format == "" {
		return RecordingFormatGuac
	}
	return r.Format
}

// A recording's file formats.
const (
	// RecordingFormatGuac: Guacamole's session recording, as guacd writes
	// its own (a desktop's).
	RecordingFormatGuac = "guac"
	// RecordingFormatAsciicast: an asciicast v2 (a terminal's): a JSON
	// header line, then an event a line, [seconds, "o"|"i"|"r", data].
	RecordingFormatAsciicast = "asciicast"
)

// Recording states.
const (
	// RecordingStateRecording: its desktop or terminal is open, and
	// recorded.
	RecordingStateRecording = "RECORDING"
	// RecordingStateSaved: it ended, and plays.
	RecordingStateSaved = "SAVED"
	// RecordingStateFailed: it did not open (Msg says why): nothing was
	// recorded.
	RecordingStateFailed = "FAILED"
)

// How a recording ended.
const (
	// RecordingEndClosed: the web app closed the desktop or the terminal
	// (the operator did, or the page went), or a raw terminal's client
	// left.
	RecordingEndClosed = "closed"
	// RecordingEndDesktop: guacd ended the session: its server's end, or
	// an error (Msg).
	RecordingEndDesktop = "desktop"
	// RecordingEndProcess: the terminal's process ended (Msg: its exit,
	// once the minion said it).
	RecordingEndProcess = "process"
	// RecordingEndTaskEnded: the terminal's task ended (it was stopped).
	RecordingEndTaskEnded = "task_ended"
	// RecordingEndReset: the minion's end of the terminal was lost (it
	// reset the stream, or no longer had it).
	RecordingEndReset = "reset"
	// RecordingEndCoreStopped: core stopped while it recorded it: what it
	// had not kept yet, its last seconds, is lost.
	RecordingEndCoreStopped = "core_stopped"
	// RecordingEndNotOpened: it did not open (Msg).
	RecordingEndNotOpened = "not_opened"
)
