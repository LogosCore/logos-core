package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// DesktopRecording is the recording of one desktop a desktop task's tunnel
// drew in the web app (docs: contracts/minion-messages/tunnels.md,
// Recordings): who opened it and when, what it opened with, how far it
// came and how it ended. What it recorded is in the desktop recordings
// bucket, in parts as core kept it (pkg/recordings). Kept until its
// operation is deleted, or an operator deletes it.
type DesktopRecording struct {
	field.DefaultField `bson:",inline"`

	RecordingID uuid.UUID `bson:"recording_id" json:"recording_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	// Number is its number in the task, from 1: the task's recordings are
	// listed by it, the latest first.
	Number int64 `bson:"number" json:"number"`
	// Front is the desktop's: rdp, vnc, ssh or telnet.
	Front string `bson:"front" json:"front"`
	// UserID and Username are who opened it.
	UserID   uuid.UUID `bson:"user_id" json:"user_id"`
	Username string    `bson:"username" json:"username"`
	// Settings are what it opened with: the settings the operator picked
	// (desktops.Fields), as the web app sent them, each left out at its
	// default.
	Settings JSONMap `bson:"settings,omitempty" json:"settings,omitempty"`
	// Width and Height are the display it opened at, in pixels.
	Width  int `bson:"width" json:"width"`
	Height int `bson:"height" json:"height"`
	// Keys: it holds the operator's mouse and keys. A view-only desktop's
	// does not: its server took none.
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
	// DurationMs is how long it plays: from guacd's first frame to its
	// last, as guacd timed them.
	DurationMs int64 `bson:"duration_ms" json:"duration_ms"`
	// Lost is the bytes core could not keep (its store failed): the
	// recording plays past where they were.
	Lost int64 `bson:"lost,omitempty" json:"lost,omitempty"`
	// End is how it ended (DesktopRecordingEnd...), empty while it
	// records; Msg what was said: guacd's error, or why the desktop did
	// not open.
	End string `bson:"end,omitempty" json:"end,omitempty"`
	Msg string `bson:"msg,omitempty" json:"msg,omitempty"`
	// SeenAt is when the core recording it last kept its numbers: one
	// recording with none for a while lost its core, and is ended.
	SeenAt time.Time `bson:"seen_at" json:"seen_at"`
}

// Desktop recording states.
const (
	// DesktopRecordingRecording: its desktop is open, and recorded.
	DesktopRecordingRecording = "RECORDING"
	// DesktopRecordingSaved: it ended, and plays.
	DesktopRecordingSaved = "SAVED"
	// DesktopRecordingFailed: the desktop did not open (Msg says why):
	// nothing was recorded.
	DesktopRecordingFailed = "FAILED"
)

// How a desktop recording ended.
const (
	// DesktopRecordingEndClosed: the web app closed the desktop (the
	// operator did, or the page went).
	DesktopRecordingEndClosed = "closed"
	// DesktopRecordingEndDesktop: guacd ended the session: its server's
	// end, or an error (Msg).
	DesktopRecordingEndDesktop = "desktop"
	// DesktopRecordingEndCoreStopped: core stopped while it recorded it:
	// what it had not kept yet, its last seconds, is lost.
	DesktopRecordingEndCoreStopped = "core_stopped"
	// DesktopRecordingEndNotOpened: the desktop did not open (Msg).
	DesktopRecordingEndNotOpened = "not_opened"
)
