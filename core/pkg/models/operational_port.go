package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// OperationalPort is one of core's operational ports in use
// (contracts/minion-messages/tunnels.md, Operational ports): the task it
// serves, and for what. It goes back to the range when the task ends; a
// restart opens again the ports of tasks still running.
type OperationalPort struct {
	field.DefaultField `bson:",inline"`

	Port        int       `bson:"port" json:"port"`
	Purpose     string    `bson:"purpose" json:"purpose"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	OpenedBy    uuid.UUID `bson:"opened_by" json:"opened_by"`
	OpenedAt    time.Time `bson:"opened_at" json:"opened_at"`
	// SessionNumber and OpenedByName, as they were when it opened: what
	// the Ports list shows and searches (SearchText).
	SessionNumber int    `bson:"session_number" json:"session_number"`
	OpenedByName  string `bson:"opened_by_name,omitempty" json:"opened_by_name,omitempty"`
	SearchText    string `bson:"search_text" json:"-"`
	// Host and HostPort are a forward's destination (OperationalPurposeForward):
	// every connection to the port is a stream to it.
	Host     string `bson:"host,omitempty" json:"host,omitempty"`
	HostPort int    `bson:"host_port,omitempty" json:"host_port,omitempty"`
	// UDP: its SOCKS proxy takes UDP ASSOCIATE (its command opens udp).
	UDP bool `bson:"udp,omitempty" json:"udp,omitempty"`
	// Streams is the highest stream number core took for the task, a
	// thousand at a time (miniontunnels.StreamBlock): a stream's is never
	// used again in the task, a restart's streams' neither, as a restart
	// goes on past it. Opened is how many streams it has opened, as core
	// last kept it (with its bytes).
	Streams uint64 `bson:"streams" json:"streams"`
	Opened  uint64 `bson:"opened,omitempty" json:"opened,omitempty"`
	// Cap is its bandwidth cap, bytes a second each way; 0 for none.
	Cap int64 `bson:"cap,omitempty" json:"cap,omitempty"`
	// Up and Down are its tools' bytes so far, each way, as core last kept
	// them (every sweep, and when it stops): where a restart's count goes
	// on from.
	Up   int64 `bson:"up,omitempty" json:"up,omitempty"`
	Down int64 `bson:"down,omitempty" json:"down,omitempty"`
	// MaxStreams and Timeout are the task's max_streams and timeout args:
	// how many streams may be open at once, and how long the minion tries
	// a connect.
	MaxStreams int           `bson:"max_streams,omitempty" json:"max_streams,omitempty"`
	Timeout    time.Duration `bson:"timeout,omitempty" json:"timeout,omitempty"`
	// StopIdle is the task's stop_idle: core stops it once no stream has
	// been open that long; 0, never.
	StopIdle time.Duration `bson:"stop_idle,omitempty" json:"stop_idle,omitempty"`
}

// What an operational port serves: its task's front (protocol.ParamFront).
const (
	// OperationalPurposeSOCKS: a SOCKS proxy (protocol.FrontSOCKS).
	OperationalPurposeSOCKS = "socks"
	// OperationalPurposeHTTP: an HTTP proxy (protocol.FrontHTTP).
	OperationalPurposeHTTP = "http"
	// OperationalPurposeForward: a port forward, to its destination
	// (protocol.FrontForward).
	OperationalPurposeForward = "forward"
	// OperationalPurposeReverse: a reverse tunnel, which listens on the
	// minion's host (protocol.ActionReverseTunnel): no operational port, a
	// MinionListener, whose Front says what core does with its streams.
	OperationalPurposeReverse = "reverse"
)

// MinionListener is a reverse port forward's listener
// (contracts/minion-messages/tunnels.md, Reverse port forward): the port
// its task's minion listens on, the destination core connects each of its
// connections to, and how many streams the minion has opened. It goes when
// the task ends; a restart takes up the tasks still running.
type MinionListener struct {
	field.DefaultField `bson:",inline"`

	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	OpenedBy    uuid.UUID `bson:"opened_by" json:"opened_by"`
	OpenedAt    time.Time `bson:"opened_at" json:"opened_at"`
	// SessionNumber and OpenedByName, as they were when it opened: what
	// the Ports list shows and searches (SearchText).
	SessionNumber int    `bson:"session_number" json:"session_number"`
	OpenedByName  string `bson:"opened_by_name,omitempty" json:"opened_by_name,omitempty"`
	SearchText    string `bson:"search_text" json:"-"`
	// Bind and Listen are where the minion is to listen, as the task asks
	// (Listen 0: any free port); Bound where it does, as its tunnel_listen
	// said ("" until it says).
	Bind   string `bson:"bind" json:"bind"`
	Listen int    `bson:"listen" json:"listen"`
	Bound  string `bson:"bound,omitempty" json:"bound,omitempty"`
	// UDP: the minion listens for datagrams, a stream for each address
	// they come from.
	UDP bool `bson:"udp,omitempty" json:"udp,omitempty"`
	// Front is what core does with each stream (protocol.FrontForward,
	// protocol.FrontSOCKS): forward connects it to Host:HostPort, from its
	// own network (the minion never learns it); socks reads a SOCKS5
	// request from it, behind User and Password, and connects where it
	// asks.
	Front    string `bson:"front" json:"front"`
	Host     string `bson:"host,omitempty" json:"host,omitempty"`
	HostPort int    `bson:"host_port,omitempty" json:"host_port,omitempty"`
	User     string `bson:"user,omitempty" json:"user,omitempty"`
	Password string `bson:"password,omitempty" json:"password,omitempty"`
	// Streams is the highest stream number the minion has opened: a tunnel
	// item for one at or below it that core no longer has is an old one's
	// (sent again, or a restart's), never connected again. Opened is how
	// many it has opened, as core last kept it.
	Streams uint64 `bson:"streams" json:"streams"`
	Opened  uint64 `bson:"opened,omitempty" json:"opened,omitempty"`
	// Cap is its bandwidth cap, bytes a second each way; 0 for none.
	Cap int64 `bson:"cap,omitempty" json:"cap,omitempty"`
	// Up and Down are its bytes so far, as an operational port's.
	Up   int64 `bson:"up,omitempty" json:"up,omitempty"`
	Down int64 `bson:"down,omitempty" json:"down,omitempty"`
	// MaxStreams and Timeout are the task's max_streams and timeout args:
	// how many streams may be open at once, and how long core tries a
	// connect.
	MaxStreams int           `bson:"max_streams,omitempty" json:"max_streams,omitempty"`
	Timeout    time.Duration `bson:"timeout,omitempty" json:"timeout,omitempty"`
	// StopIdle is the task's stop_idle, as an operational port's.
	StopIdle time.Duration `bson:"stop_idle,omitempty" json:"stop_idle,omitempty"`
}

// MinionListenerSearchText is what the Ports list's search matches: the
// port, the session's number, who opened it, where it listens and what to.
func MinionListenerSearchText(l *MinionListener) string {
	text := fmt.Sprintf("%d #%d %s %s %s %s", l.Listen, l.SessionNumber, l.OpenedByName, l.Bind, l.Bound, l.Front)
	if l.Host != "" {
		text += fmt.Sprintf(" %s:%d", l.Host, l.HostPort)
	}
	if l.UDP {
		text += " udp"
	}
	return text
}

// OperationalPortSearchText is what the Ports list's search matches: the
// port, the session's number, who opened it and for what.
func OperationalPortSearchText(p *OperationalPort) string {
	text := fmt.Sprintf("%d #%d %s %s", p.Port, p.SessionNumber, p.OpenedByName, p.Purpose)
	if p.Host != "" {
		text += fmt.Sprintf(" %s:%d", p.Host, p.HostPort)
	}
	return text
}

// MinionTunnelStream is one stream of a tunnel task: who reached what
// through the minion, when, and how much went each way. Core holds it in
// memory while the task runs, every open one and the latest ended ones
// (miniontunnels.KeptStreams), and stores none: once the task ends, what
// is left is its totals (MinionTaskTunnel). The bytes are never kept.
type MinionTunnelStream struct {
	TaskID uuid.UUID
	Stream uint64
	// Client is the address of the tool that asked for it (the SOCKS
	// client's).
	Client string
	// Host and Port: the destination, as the tool named it; none for a UDP
	// association (UDP), whose datagrams each name their own.
	Host     string
	Port     int
	UDP      bool
	OpenedAt time.Time
	// Result is the minion's reply (protocol.TunnelOpen, ...): nil while
	// it has not come. Bound is the minion's socket's address when open;
	// Msg why not.
	Result *int
	Bound  string
	Msg    string
	// EndedAt and End: when and how it ended (TunnelEnd...).
	EndedAt *time.Time
	End     string
	// Up: bytes the tool sent the destination; Down: back.
	Up   int64
	Down int64
	// SearchText is what a task's streams' search matches: the tool's
	// address and the destination.
	SearchText string
}

// TunnelStreamSearchText is a stream's SearchText.
func TunnelStreamSearchText(s *MinionTunnelStream) string {
	if s.UDP {
		return s.Client + " udp"
	}
	return fmt.Sprintf("%s %s:%d", s.Client, s.Host, s.Port)
}

// How a tunnel stream ended (MinionTunnelStream.End).
const (
	// TunnelEndDone: both ends closed, everything delivered.
	TunnelEndDone = "done"
	// TunnelEndNotOpened: the minion could not connect (Result says why).
	TunnelEndNotOpened = "not_opened"
	// TunnelEndNoReply: the minion did not answer in time.
	TunnelEndNoReply = "no_reply"
	// TunnelEndClientReset: the tool aborted its connection.
	TunnelEndClientReset = "client_reset"
	// TunnelEndReset: the destination, or the minion, aborted.
	TunnelEndReset = "reset"
	// TunnelEndLost: the minion no longer had the stream.
	TunnelEndLost = "lost"
	// TunnelEndTaskEnded: the task ended (stopped, or ended on the
	// minion's side).
	TunnelEndTaskEnded = "task_ended"
	// TunnelEndCoreStopped: core stopped, its clients' connections with it.
	TunnelEndCoreStopped = "core_stopped"
)
