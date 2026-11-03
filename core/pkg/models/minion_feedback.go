package models

import (
	"time"

	"github.com/google/uuid"
)

// MinionFeedback is a warning or an error a session's minion sent its
// operators (docs: contracts/minion-messages/sessions, Feedback;
// protocol.FeedbackItem): what it would otherwise only log on the host.
// Kept per session, the newest MaxSessionFeedback; read for everyone of the
// operation alike (MinionSession.FeedbackReadAt).
type MinionFeedback struct {
	// FeedbackID is core's, for paging and links.
	FeedbackID  uuid.UUID `bson:"feedback_id" json:"feedback_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	// MinionID is the minion's own id for it: one entry per id in a
	// session, so a message sent again adds nothing.
	MinionID []byte `bson:"minion_id" json:"-"`
	// Level is FeedbackWarning or FeedbackError.
	Level string     `bson:"level" json:"level"`
	Msg   string     `bson:"msg" json:"msg"`
	Code  string     `bson:"code,omitempty" json:"code,omitempty"`
	Task  *uuid.UUID `bson:"task_id,omitempty" json:"task_id,omitempty"`
	// At is when it happened, as the minion says; nil when it did not.
	At *time.Time `bson:"at,omitempty" json:"at,omitempty"`
	// Count is how many times it happened (the minion folds repeats).
	Count      int       `bson:"count" json:"count"`
	ReceivedAt time.Time `bson:"received_at" json:"received_at"`
}

// Feedback levels, as core keeps them.
const (
	FeedbackWarning = "warning"
	FeedbackError   = "error"
)

// MaxSessionFeedback is how many feedback entries a session keeps: older
// ones go as new ones come.
const MaxSessionFeedback = 1000
