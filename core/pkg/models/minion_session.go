package models

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
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

	// Rev counts the session's writes: 1 when it opens, one more with each
	// change (a check-in, a description). A client keeps the copy with the
	// higher one, whichever comes last (a mutation's answer after the live
	// updates it raced).
	Rev int64 `bson:"rev" json:"rev"`

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
	// Conn is the connection the minion uses: its place in the build's
	// connections, from 1, as its hello or its last conn_update said; 0
	// when it never said. ConnKind and ConnName are that connection's kind
	// and name, copied from the build when it is said (empty when the build
	// does not have it), so a list shows them without the build; ConnCount
	// is how many connections the build has. ConnChanges are the moves it
	// reported, most recent first, at most MaxSessionConnChanges.
	Conn      int    `bson:"conn,omitempty" json:"conn,omitempty"`
	ConnKind  string `bson:"conn_kind,omitempty" json:"conn_kind,omitempty"`
	ConnName  string `bson:"conn_name,omitempty" json:"conn_name,omitempty"`
	ConnCount int    `bson:"conn_count,omitempty" json:"conn_count,omitempty"`
	// MaxTasks is how many tasks the minion runs at once, as it last said
	// (its hello's, or a max_tasks of its); nil when it never said.
	// MaxTasksSettable: its build's factory declares max_tasks (the build
	// has a limit), so its minion takes core's max_tasks; a minion whose
	// factory does not runs as many as its factory made it, which it may
	// say all the same. MaxTasksSet is the operator's: core sends it (a
	// max_tasks item) while the minion says another, a process that claims
	// the session with the build's own included; nil until one is set.
	// MaxTasksSends counts the messages that carried it unanswered: at
	// MaxTasksTries core stops, the minion not taking it.
	// FeedbackErrors and FeedbackWarnings count the feedback its minion
	// sent since an operator last marked it read (FeedbackReadAt), for
	// everyone of the operation alike; FeedbackUnread is both, what the
	// Sessions table sorts and filters by; FeedbackAt is when the latest
	// came (MinionFeedback).
	FeedbackErrors   int        `bson:"feedback_errors,omitempty" json:"feedback_errors,omitempty"`
	FeedbackWarnings int        `bson:"feedback_warnings,omitempty" json:"feedback_warnings,omitempty"`
	FeedbackUnread   int        `bson:"feedback_unread,omitempty" json:"feedback_unread,omitempty"`
	FeedbackAt       *time.Time `bson:"feedback_at,omitempty" json:"feedback_at,omitempty"`
	FeedbackReadAt   *time.Time `bson:"feedback_read_at,omitempty" json:"feedback_read_at,omitempty"`
	MaxTasks         *int       `bson:"max_tasks,omitempty" json:"max_tasks,omitempty"`
	MaxTasksSettable bool       `bson:"max_tasks_settable,omitempty" json:"max_tasks_settable,omitempty"`
	MaxTasksSet      *int       `bson:"max_tasks_set,omitempty" json:"max_tasks_set,omitempty"`
	MaxTasksSends    int        `bson:"max_tasks_sends,omitempty" json:"max_tasks_sends,omitempty"`
	// Connected: a persistent connection serving the session is open. Core
	// holds the connections (minionsync); it writes this when the session's
	// first opens and its last closes, and clears it as it starts, so the
	// Sessions list can filter on it.
	Connected bool `bson:"connected,omitempty" json:"-"`
	// ConnMaxMessage is the largest message the connection in use carries
	// (protocol.BuildConnection.MaxMessage): what core sends the minion
	// keeps within it. 0: the protocol's limit.
	ConnMaxMessage int                       `bson:"conn_max_message,omitempty" json:"-"`
	ConnChanges    []MinionSessionConnChange `bson:"conn_changes,omitempty" json:"conn_changes,omitempty"`
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

// MaxSessionRestarts, MaxSessionReplacedKeys and MaxSessionConnChanges cap
// a session's lists.
const (
	MaxSessionRestarts     = 32
	MaxSessionReplacedKeys = 4
	MaxSessionConnChanges  = 32
)

// MinionSessionConn is the connection a minion uses: its place in the
// build's connections, its kind and its name.
type MinionSessionConn struct {
	Place int
	Kind  string
	Name  string
	// MaxMessage is the largest message the connection carries
	// (protocol.BuildConnection.MaxMessage); 0 for the protocol's limit.
	MaxMessage int
}

// MessageLimit is the largest message core sends the session's minion: its
// connection's, or the protocol's.
func (s *MinionSession) MessageLimit() int {
	return protocol.BuildConnection{MaxMessage: s.ConnMaxMessage}.MessageLimit()
}

// LateBySchedule reports whether a session whose minion is on a connection
// of kind turns late and lost by its schedule (docs: Sessions, Late and
// lost): every kind but bind-persistent, whose connection says whether the
// minion is there (connected, not connected on purpose, unreachable), so a
// closed one is not a missed check-in. A kind not known yet (no hello said
// one) counts by its schedule.
func LateBySchedule(kind string) bool { return kind != protocol.KindBindPersistent }

// MinionSessionConnChange is a move to another connection a minion reported
// (a conn_update): from the connection it used (0: unknown) to To, both
// places in the build's connections, and why, when it said.
type MinionSessionConnChange struct {
	At     time.Time `bson:"at" json:"at"`
	From   int       `bson:"from,omitempty" json:"from,omitempty"`
	To     int       `bson:"to" json:"to"`
	Reason string    `bson:"reason,omitempty" json:"reason,omitempty"`
}

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

// DefaultSessionSleep is the schedule a minion that says none is taken to
// keep where its check-ins say whether it is there (a reverse kind's): once
// a day, so it shows late after a day of silence, lost after three.
var DefaultSessionSleep = MinionSessionSleep{Interval: (24 * time.Hour).Milliseconds()}

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
	MachineID string         `bson:"machine_id,omitempty" json:"machine_id,omitempty"` // "<source>:<value>" (protocol.MachineID), by the source's name; empty when the minion said none
	X         map[string]any `bson:"x,omitempty" json:"x,omitempty"`
}

// MaxTasksTries is how many messages carry the operator's limit unanswered
// before core stops: a minion that takes it says so in its very next
// message, and one that does not (unknown_item) would be sent it forever.
// An err names an item only by its place in a message core no longer
// has, so the count is what tells.
const MaxTasksTries = 5

// maxTasksWanted: the operator's limit differs from the minion's, for a
// minion that takes one.
func (s MinionSession) maxTasksWanted() bool {
	return s.MaxTasksSettable && s.MaxTasks != nil && s.MaxTasksSet != nil && *s.MaxTasks != *s.MaxTasksSet
}

// MaxTasksDue is the limit core has to send the session's minion: the
// operator's while the minion says another; nil when there is none to
// send (none set, the minion has it, it takes none, or it did not take it
// after MaxTasksTries messages).
func (s MinionSession) MaxTasksDue() *int {
	if !s.maxTasksWanted() || s.MaxTasksSends >= MaxTasksTries {
		return nil
	}
	return s.MaxTasksSet
}

// MaxTasksNotTaken is the operator's limit the minion did not take: sent
// MaxTasksTries times, never answered. nil otherwise.
func (s MinionSession) MaxTasksNotTaken() *int {
	if !s.maxTasksWanted() || s.MaxTasksSends < MaxTasksTries {
		return nil
	}
	return s.MaxTasksSet
}

// Why an operator's limit of tasks cannot be set (CanSetMaxTasks).
var (
	ErrMaxTasksRange       = fmt.Errorf("max tasks is 0 to %d", protocol.MaxTasksLimit)
	ErrMaxTasksFixed       = errors.New("its factory sets how many tasks the minion runs at once: it cannot be changed")
	ErrMaxTasksUnsaid      = errors.New("the minion takes no max tasks: its hello said none")
	ErrMaxTasksSessionGone = errors.New("the session is closed")
)

// CanSetMaxTasks says why an operator may not set max as the session's
// limit at now; nil when they may.
func (s MinionSession) CanSetMaxTasks(max int, now time.Time) error {
	switch {
	case max < 0 || max > protocol.MaxTasksLimit:
		return ErrMaxTasksRange
	case !s.MaxTasksSettable:
		return ErrMaxTasksFixed
	case s.MaxTasks == nil:
		return ErrMaxTasksUnsaid
	case s.Closed(now):
		return ErrMaxTasksSessionGone
	}
	return nil
}

// IsMaxTasksRefusal: err is one of CanSetMaxTasks's, an operator's to fix.
func IsMaxTasksRefusal(err error) bool {
	return errors.Is(err, ErrMaxTasksRange) || errors.Is(err, ErrMaxTasksFixed) || errors.Is(err, ErrMaxTasksUnsaid) ||
		errors.Is(err, ErrMaxTasksSessionGone)
}

// MaxTasksOf is the limit a hello says, as a session keeps it; nil when it
// says none.
func MaxTasksOf(h protocol.HelloItem) *int {
	if h.MaxTasks == nil {
		return nil
	}
	n := int(min(*h.MaxTasks, protocol.MaxTasksLimit))
	return &n
}

// MinionHelloFrom keeps a hello's fields; proc as a process name.
func MinionHelloFrom(h protocol.HelloItem) MinionHello {
	return MinionHello{
		PID: h.PID, OS: h.OS, Arch: h.Arch, Host: h.Host, User: h.User, Domain: h.Domain, Proc: ProcessName(h.Proc),
		Integrity: protocol.IntegrityName(h.Integrity), Addrs: h.AddrStrings(), Version: h.Version, MachineID: h.MachineID(), X: h.X,
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

// ProcessName is a hello's proc as the contract has it, the process's name
// (its image's file name): the last part of a path, in either OS's form,
// for agents that send the whole path. A path told operators only where
// the Downloads folder was, cut off in the column.
func ProcessName(proc string) string {
	if i := strings.LastIndexAny(proc, `/\`); i >= 0 && i < len(proc)-1 {
		return proc[i+1:]
	}
	return proc
}
