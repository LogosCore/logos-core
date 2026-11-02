package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// A target is where a bind minion is reached (docs:
// contracts/channel-core-connections/, bind-call minions: targets and
// sessions): an operator puts a page a factory built on a server, and adds a
// target with what the channel needs to call it (its URL, a host header, a
// proxy). Core calls it over the channel's control link: the first call asks
// for a hello, which opens the session the target is linked to from then on.
// It is a record, not a session; it can be added before the page is in
// place.

// Target states.
const (
	// TargetNew: added, not called yet.
	TargetNew = "new"
	// TargetConnected: the last call reached the page.
	TargetConnected = "connected"
	// TargetUnreachable: the last call reached nothing (DNS, refused,
	// timeout, TLS); retried on its schedule.
	TargetUnreachable = "unreachable"
	// TargetNotTheMinion: something answered that is not the page (an HTTP
	// 404, a WAF's page, an answer that does not open); not retried.
	TargetNotTheMinion = "not_the_minion"
	// TargetRefused: the page answered with an err.
	TargetRefused = "refused"
	// TargetStopped: unreachable through every retry; only Check now calls
	// it again.
	TargetStopped = "stopped"
	// TargetNoChannel: no instance of the channel has its control link
	// open; retried as unreachable is.
	TargetNoChannel = "no_channel"
	// TargetNeedsEditing: its values no longer fit the channel's target
	// fields (one it now requires is missing, one is gone or changed); core
	// does not call it until an operator edits it.
	TargetNeedsEditing = "needs_editing"
)

// TargetRetryFor is how long an unreachable target is retried.
const TargetRetryFor = 24 * time.Hour

// TargetRetryAfter is the wait before the nth retry of an unreachable
// target (n from 1): 1 minute, 5, 30, then hourly.
func TargetRetryAfter(n int) time.Duration {
	switch {
	case n <= 1:
		return time.Minute
	case n == 2:
		return 5 * time.Minute
	case n == 3:
		return 30 * time.Minute
	}
	return time.Hour
}

// MinionTarget is one target of a bind connection of a build.
type MinionTarget struct {
	field.DefaultField `bson:",inline"`

	TargetID    uuid.UUID `bson:"target_id" json:"target_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	BuildID     uuid.UUID `bson:"build_id" json:"build_id"`
	// Conn is the build connection the target is of: its place, from 1.
	Conn int `bson:"conn" json:"conn"`
	// ModuleName and Kind are that connection's channel and kind.
	ModuleName string `bson:"module_name" json:"module_name"`
	Kind       string `bson:"kind" json:"kind"`
	// Values are the operator's values for the channel's target fields.
	Values JSONMap `bson:"values" json:"values"`
	// Name is what the target is shown as: its URL, or host.
	Name string `bson:"name" json:"name"`
	// Retries says whether an unreachable target is called again on its
	// schedule; off where every request in a server's log counts.
	Retries bool `bson:"retries" json:"retries"`

	State string `bson:"state" json:"state"`
	// StateDetail says more of the state: why a call failed, what answered.
	StateDetail string    `bson:"state_detail,omitempty" json:"state_detail,omitempty"`
	StateAt     time.Time `bson:"state_at" json:"state_at"`
	// SessionID is the session the target's hello opened.
	SessionID *uuid.UUID `bson:"session_id,omitempty" json:"session_id,omitempty"`
	// Allowance is how many tasks a call core starts may bring: the page's
	// last get_tasks max; nil for one a call, as when it never said.
	Allowance *uint64 `bson:"allowance,omitempty" json:"allowance,omitempty"`
	// Failures are the calls that failed in a row; UnreachableSince is
	// when the first of them was.
	Failures         int        `bson:"failures" json:"failures"`
	UnreachableSince *time.Time `bson:"unreachable_since,omitempty" json:"unreachable_since,omitempty"`
	// NextCallAt is when core calls next by itself: a retry, or the
	// session's schedule; nil for not until there is something to send.
	NextCallAt *time.Time `bson:"next_call_at,omitempty" json:"next_call_at,omitempty"`
	LastCallAt *time.Time `bson:"last_call_at,omitempty" json:"last_call_at,omitempty"`
	// LastOKAt is when a call last reached the page.
	LastOKAt *time.Time `bson:"last_ok_at,omitempty" json:"last_ok_at,omitempty"`
	Calls    int64      `bson:"calls" json:"calls"`

	CreatedBy ModuleActor `bson:"created_by" json:"created_by"`
	CreatedAt time.Time   `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time   `bson:"updated_at" json:"updated_at"`
}
