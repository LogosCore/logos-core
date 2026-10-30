package models

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo/field"
)

// MinionSession is one running process of a minion, opened by its hello
// (docs: contracts/minion-messages.md): under a temporary id with the keys
// its stage got, or straight under the build id with the build's own keys.
// Every message after the hello is sent under the session id and sealed with
// the session's keys. A session waits a day for its first check-in; after
// one it stays until core closes it. A closed session (one that expired
// waiting, or of a deleted operation) is a tombstone: its keys are kept for
// MinionSessionTombstone so its minion can be told it is closed, then
// MongoDB removes it (PurgeAt, a TTL index).
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
	// Description is what the session is for, the operators' note: its
	// build's description when it opens, then theirs to edit
	// (MaxSessionDescription). A claim keeps it.
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	// DescriptionSet marks a description given (at opening, by an
	// operator, cleared included): the backfill leaves those alone.
	DescriptionSet bool   `bson:"description_set,omitempty" json:"-"`
	ModuleName     string `bson:"module_name" json:"module_name"`
	Target         string `bson:"target" json:"target"`
	Platform       string `bson:"platform" json:"platform"`
	// Staged is true for a session opened under a temporary id, false for a
	// hello straight under the build id.
	Staged bool `bson:"staged" json:"staged"`
	// Crypto is the suite and the session's key material. Never shown.
	Crypto protocol.BuildCrypto `bson:"crypto" json:"-"`
	// Hello is what the latest hello said, as it said it.
	Hello MinionHello `bson:"hello" json:"hello"`
	// Address is the one of Hello.Addrs the session shows: an operator's
	// pick (AddressPicked), shared by the whole team, or else core's guess at
	// the host's real one (DefaultAddress). Empty when the hello sent none.
	Address       string `bson:"address,omitempty" json:"address,omitempty"`
	AddressPicked bool   `bson:"address_picked,omitempty" json:"address_picked,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	// PeerAddress is the IP address the latest message came from, as its
	// channel saw it (protocol.InboundMinionMessage.PeerAddress): the
	// session's current external address. Peers is every one it was seen
	// from, most recent first, at most MaxSessionPeers. Empty where the
	// channels had none.
	PeerAddress string              `bson:"peer_address,omitempty" json:"peer_address,omitempty"`
	Peers       []MinionSessionPeer `bson:"peers,omitempty" json:"peers,omitempty"`
	// Sleep is the check-in schedule the minion last said (its hello's, or
	// a sleep_update); nil when it never did. LateAt is when the session
	// turns late: its next check-in overdue by that schedule (LateAtFrom);
	// LostAt when it is lost: SessionLostMissed check-ins missed
	// (LostAtFrom). Both nil without a schedule.
	Sleep  *MinionSessionSleep `bson:"sleep,omitempty" json:"sleep,omitempty"`
	LateAt *time.Time          `bson:"late_at,omitempty" json:"late_at,omitempty"`
	LostAt *time.Time          `bson:"lost_at,omitempty" json:"lost_at,omitempty"`
	// LastSeenAt is the latest message under the session id; nil before the
	// first check-in.
	LastSeenAt *time.Time `bson:"last_seen_at,omitempty" json:"last_seen_at,omitempty"`
	// Archived hides the session from the operation's sessions (the
	// Archived view lists it); an operator archives and restores it, and a
	// check-in restores it: a minion that comes back is never hidden.
	Archived bool `bson:"archived,omitempty" json:"archived,omitempty"`
	// Persisted is set once a build names the session (Build.PersistedSessionID):
	// that build's minions claim it when they start.
	Persisted bool `bson:"persisted,omitempty" json:"persisted,omitempty"`
	// Restarts are the claims that brought a process into the session, most
	// recent first, at most MaxSessionRestarts; RestartCount counts them all.
	Restarts     []MinionSessionRestart `bson:"restarts,omitempty" json:"restarts,omitempty"`
	RestartCount int64                  `bson:"restart_count,omitempty" json:"restart_count,omitempty"`
	// ReplacedKeys are the keys earlier holders had, most recent first, at
	// most MaxSessionReplacedKeys and MinionSessionTombstone old: a message
	// that opens with one gets session_replaced. Never shown.
	ReplacedKeys []MinionSessionReplacedKeys `bson:"replaced_keys,omitempty" json:"-"`
	// ClosedAt is when core closed the session, and CloseReason why
	// (MinionSessionClosedOperationDeleted); nil while it is open. A session
	// that expired waiting for its first check-in is closed too, without
	// these set (Closed tells).
	ClosedAt    *time.Time `bson:"closed_at,omitempty" json:"closed_at,omitempty"`
	CloseReason string     `bson:"close_reason,omitempty" json:"close_reason,omitempty"`
	// PurgeAt is when MongoDB removes the session: a tombstone's end. Set
	// from the start (the day's wait and the tombstone's 30 days), unset by
	// the first check-in, set again when core closes it.
	PurgeAt *time.Time `bson:"purge_at,omitempty" json:"-"`
}

// How long a session waits for its first check-in, and how long core keeps
// a closed session's keys to tell its minion so.
const (
	MinionSessionCheckInWindow = 24 * time.Hour
	MinionSessionTombstone     = 30 * 24 * time.Hour
)

// Why core closed a session.
const MinionSessionClosedOperationDeleted = "operation_deleted"

// Closed reports whether the session is closed at now: closed by core, or
// expired waiting for its first check-in.
func (s *MinionSession) Closed(now time.Time) bool {
	return s.ClosedAt != nil || (s.LastSeenAt == nil && now.Sub(s.CreatedAt) >= MinionSessionCheckInWindow)
}

// MaxSessionDescription caps a session's description, in characters, as a
// build's is.
const MaxSessionDescription = 500

// MaxSessionRestarts and MaxSessionReplacedKeys cap a session's lists.
const (
	MaxSessionRestarts     = 32
	MaxSessionReplacedKeys = 4
)

// MinionSessionRestart is one claim of a persisted session: a process that
// took it over.
type MinionSessionRestart struct {
	At      time.Time `bson:"at" json:"at"`
	PID     uint64    `bson:"pid" json:"pid"`
	Host    string    `bson:"host,omitempty" json:"host,omitempty"`
	BuildID uuid.UUID `bson:"build_id" json:"build_id"`
}

// MinionSessionReplacedKeys are keys a session had before a claim.
type MinionSessionReplacedKeys struct {
	Crypto protocol.BuildCrypto `bson:"crypto"`
	At     time.Time            `bson:"at"`
}

// MinionSessionSleep is a minion's check-in schedule: Interval milliseconds
// between check-ins, spread by Jitter percent of it either way.
type MinionSessionSleep struct {
	Interval int64 `bson:"interval" json:"interval"`
	Jitter   int64 `bson:"jitter" json:"jitter"`
}

// SessionLateGrace is the slack a check-in gets past its schedule before
// the session is late: slow networks, a busy host.
const SessionLateGrace = 30 * time.Second

// SleepFrom keeps a schedule a minion said.
func SleepFrom(s protocol.Sleep) *MinionSessionSleep {
	return &MinionSessionSleep{Interval: int64(s.Interval), Jitter: int64(s.Jitter)} //nolint:gosec // validated small
}

// SessionLostMissed is how many check-ins a session misses before it is lost.
const SessionLostMissed = 3

// LateAtFrom is when a session that checked in at base turns late: the
// interval stretched by its jitter, and the grace.
func (s MinionSessionSleep) LateAtFrom(base time.Time) time.Time {
	return base.Add(s.stretched() + SessionLateGrace)
}

// LostAtFrom is when it is lost: SessionLostMissed stretched intervals,
// and the grace once.
func (s MinionSessionSleep) LostAtFrom(base time.Time) time.Time {
	return base.Add(SessionLostMissed*s.stretched() + SessionLateGrace)
}

// stretched is the interval with its jitter added.
func (s MinionSessionSleep) stretched() time.Duration {
	period := time.Duration(s.Interval) * time.Millisecond
	return period + period*time.Duration(s.Jitter)/100
}

// MaxSessionPeers caps MinionSession.Peers.
const MaxSessionPeers = 32

// MinionSessionPeer is one external address a session was seen from.
type MinionSessionPeer struct {
	Address   string    `bson:"address" json:"address"`
	FirstSeen time.Time `bson:"first_seen" json:"first_seen"`
	LastSeen  time.Time `bson:"last_seen" json:"last_seen"`
	// Count is the messages from it.
	Count int64 `bson:"count" json:"count"`
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

// DefaultAddress is core's guess at which of a host's addresses is its real
// one: an IPv4 address on an ordinary network (LAN or public) before a global
// IPv6 one, before a virtual bridge's (Docker's 172.17-31.x, libvirt's
// 192.168.122.x, IPv6 ULA), before link-local and loopback; the minion's
// order among equals. Empty for none.
func DefaultAddress(addrs []string) string {
	best, bestRank := "", 99
	for _, a := range addrs {
		if r := addressRank(a); r < bestRank {
			best, bestRank = a, r
		}
	}
	return best
}

var (
	dockerBridges  = netip.MustParsePrefix("172.16.0.0/12")
	dockerFirst    = netip.MustParsePrefix("172.16.0.0/16")
	libvirtDefault = netip.MustParsePrefix("192.168.122.0/24")
	ipv6ULA        = netip.MustParsePrefix("fc00::/7")
)

func addressRank(s string) int {
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return 5
	}
	switch {
	case ip.IsLoopback() || ip.IsUnspecified():
		return 4
	case ip.IsLinkLocalUnicast():
		return 3
	case ip.Is4() && (dockerBridges.Contains(ip) && !dockerFirst.Contains(ip) || libvirtDefault.Contains(ip)):
		return 2
	case ip.Is6() && ipv6ULA.Contains(ip):
		return 2
	case ip.Is4() || ip.Is4In6():
		return 0
	}
	return 1
}
