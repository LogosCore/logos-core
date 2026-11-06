package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// MinionIdentity is who a session's tasks run as, or a task ran as, when its
// minion said another than its process's own (docs:
// contracts/minion-messages/identity; protocol.IdentityItem).
// How "" is the process's own: a task that ran as it whatever the session's
// identity (an agentless channel's file commands, say).
type MinionIdentity struct {
	User      string `bson:"user,omitempty" json:"user,omitempty"`
	Domain    string `bson:"domain,omitempty" json:"domain,omitempty"`
	Integrity string `bson:"integrity,omitempty" json:"integrity,omitempty"`
	// How is protocol.IdentityHowName's: impersonation, network, become.
	How string `bson:"how,omitempty" json:"how,omitempty"`
	// CredentialID is the credential it took, when one of a task's did.
	CredentialID *uuid.UUID `bson:"credential_id,omitempty" json:"credential_id,omitempty"`
	// TaskID is the task that made it (a session's: a become).
	TaskID *uuid.UUID `bson:"task_id,omitempty" json:"task_id,omitempty"`
	Since  time.Time  `bson:"since" json:"since"`
}

// Own: the process's own identity.
func (i MinionIdentity) Own() bool { return i.How == "" }

// SameAs: the same identity, whenever it came.
func (i MinionIdentity) SameAs(o MinionIdentity) bool {
	return i.User == o.User && i.Domain == o.Domain && i.Integrity == o.Integrity && i.How == o.How &&
		sameID(i.CredentialID, o.CredentialID) && sameID(i.TaskID, o.TaskID)
}

func sameID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// IdentityFrom is an identity item as core keeps it; credential is the
// credential it names, if any.
func IdentityFrom(it protocol.IdentityItem, credential *uuid.UUID, now time.Time) MinionIdentity {
	id := MinionIdentity{User: it.User, Domain: it.Domain, Integrity: protocol.IntegrityName(it.Integrity),
		How: protocol.IdentityHowName(it.How), CredentialID: credential, Since: now}
	if len(it.Task) == 16 {
		t, _ := uuid.FromBytes(it.Task)
		id.TaskID = &t
	}
	return id
}

// Identity history scopes (MinionIdentityEvent.Scope).
const (
	// IdentityScopeProcess: the process's own changed (a hello, a rehello).
	IdentityScopeProcess = "process"
	// IdentityScopeSession: who the session's tasks run as changed.
	IdentityScopeSession = "session"
	// IdentityScopeTask: one task ran as another than the session's.
	IdentityScopeTask = "task"
)

// MinionIdentityEvent is one change of a session's identity, kept in its
// history (collection minion_session_identities) until its operation goes.
// A session back to its process's own carries the process's user, so it
// reads as who.
type MinionIdentityEvent struct {
	EventID     uuid.UUID `bson:"event_id" json:"event_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	At          time.Time `bson:"at" json:"at"`
	Scope       string    `bson:"scope" json:"scope"`
	User        string    `bson:"user,omitempty" json:"user,omitempty"`
	Domain      string    `bson:"domain,omitempty" json:"domain,omitempty"`
	Integrity   string    `bson:"integrity,omitempty" json:"integrity,omitempty"`
	// How "" is the process's own.
	How          string     `bson:"how,omitempty" json:"how,omitempty"`
	CredentialID *uuid.UUID `bson:"credential_id,omitempty" json:"credential_id,omitempty"`
	// TaskID: the task (scope task), or the task that made it (session).
	TaskID *uuid.UUID `bson:"task_id,omitempty" json:"task_id,omitempty"`
}
