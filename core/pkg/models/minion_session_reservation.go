package models

import (
	"time"

	"github.com/google/uuid"
)

// MinionSessionReservation is a session id core reserved with a build made
// with a new persisted session: the build's minion claims it in its first
// hello, and it opens there as a session (MinionSession) with that id. Until
// then it has no keys, host or number and is not among the operation's
// sessions. It does not expire; it goes with its build or operation.
type MinionSessionReservation struct {
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	BuildID     uuid.UUID `bson:"build_id" json:"build_id"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
}
