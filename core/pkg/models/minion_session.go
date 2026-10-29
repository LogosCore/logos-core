package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo/field"
)

// MinionSession is one running process of a minion, opened by its hello
// (docs: contracts/minion-messages.md): under a temporary id with the keys
// its stage got, or straight under the build id with the build's own keys.
// Every message after the hello is sent under the session id and sealed with
// the session's keys. A session waits a day for its first check-in
// (ExpiresAt, a TTL index); after one it stays.
type MinionSession struct {
	field.DefaultField `bson:",inline"`

	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	// Number is the session's display id for operators: the operation's
	// count of sessions when it opened, 1 for the first. Never reused, so a
	// removed session leaves a gap.
	Number  int64     `bson:"number" json:"number"`
	BuildID uuid.UUID `bson:"build_id" json:"build_id"`
	// The build's, copied when the session opens: the build may be deleted
	// while its minions run.
	BuildDescription string `bson:"build_description,omitempty" json:"build_description,omitempty"`
	ModuleName       string `bson:"module_name" json:"module_name"`
	Target           string `bson:"target" json:"target"`
	Platform         string `bson:"platform" json:"platform"`
	// Staged is true for a session opened under a temporary id, false for a
	// hello straight under the build id.
	Staged bool `bson:"staged" json:"staged"`
	// Crypto is the suite and the session's key material. Never shown.
	Crypto protocol.BuildCrypto `bson:"crypto" json:"-"`
	// Hello is what the latest hello said, as it said it.
	Hello MinionHello `bson:"hello" json:"hello"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	// LastSeenAt is the latest message under the session id; nil before the
	// first check-in.
	LastSeenAt *time.Time `bson:"last_seen_at,omitempty" json:"last_seen_at,omitempty"`
	// ExpiresAt is when MongoDB removes a session nobody checked in to;
	// cleared by the first check-in.
	ExpiresAt *time.Time `bson:"expires_at,omitempty" json:"-"`
}

// MinionHello is a hello's host and process (protocol.HelloItem).
type MinionHello struct {
	PID       uint64         `bson:"pid" json:"pid"`
	OS        string         `bson:"os" json:"os"`
	Arch      string         `bson:"arch" json:"arch"`
	Host      string         `bson:"host,omitempty" json:"host,omitempty"`
	User      string         `bson:"user,omitempty" json:"user,omitempty"`
	Domain    string         `bson:"domain,omitempty" json:"domain,omitempty"`
	Proc      string         `bson:"proc,omitempty" json:"proc,omitempty"`
	Integrity string         `bson:"integrity,omitempty" json:"integrity,omitempty"`
	Addrs     []string       `bson:"addrs,omitempty" json:"addrs,omitempty"`
	Version   string         `bson:"version,omitempty" json:"version,omitempty"`
	X         map[string]any `bson:"x,omitempty" json:"x,omitempty"`
}

// MinionHelloFrom keeps a hello's fields.
func MinionHelloFrom(h protocol.HelloItem) MinionHello {
	return MinionHello{
		PID: h.PID, OS: h.OS, Arch: h.Arch, Host: h.Host, User: h.User, Domain: h.Domain, Proc: h.Proc,
		Integrity: h.Integrity, Addrs: h.Addrs, Version: h.Version, X: h.X,
	}
}
