package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// OperationalPurposeVPN: a tunnel's VPN (protocol.FrontVPN): no operational
// port; logos-vpn clients come over core's own address (docs:
// contracts/vpn-front), and the tunnel is kept as a VPNTunnel.
const OperationalPurposeVPN = "vpn"

// VPNTunnel is a VPN tunnel task's, kept while it runs (docs:
// contracts/vpn-front): the routes its clients take, what its command
// opens, and its numbers, as an operational port keeps them. A restart
// takes it up again; its clients connect again.
type VPNTunnel struct {
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
	// Routes are its networks, CIDRs (protocol.ParamRoutes): changed live.
	Routes []string `bson:"routes" json:"routes"`
	// UDP and ICMP: its command opens udp, icmp; Source the address of the
	// minion's host its TCP streams go from.
	UDP    bool   `bson:"udp,omitempty" json:"udp,omitempty"`
	ICMP   bool   `bson:"icmp,omitempty" json:"icmp,omitempty"`
	Source string `bson:"source,omitempty" json:"source,omitempty"`
	// Streams, Opened, Cap, Up, Down, MaxStreams, Timeout and StopIdle are
	// as an operational port's.
	Streams    uint64        `bson:"streams" json:"streams"`
	Opened     uint64        `bson:"opened,omitempty" json:"opened,omitempty"`
	Cap        int64         `bson:"cap,omitempty" json:"cap,omitempty"`
	Up         int64         `bson:"up,omitempty" json:"up,omitempty"`
	Down       int64         `bson:"down,omitempty" json:"down,omitempty"`
	MaxStreams int           `bson:"max_streams,omitempty" json:"max_streams,omitempty"`
	Timeout    time.Duration `bson:"timeout,omitempty" json:"timeout,omitempty"`
	StopIdle   time.Duration `bson:"stop_idle,omitempty" json:"stop_idle,omitempty"`
}

// VPNTunnelSearchText is what the Ports list's search matches: the
// session's number, who opened it, and its routes.
func VPNTunnelSearchText(v *VPNTunnel) string {
	return fmt.Sprintf("vpn #%d %s %s", v.SessionNumber, v.OpenedByName, strings.Join(v.Routes, " "))
}

// VPNKey is a key a logos-vpn client connects with (docs:
// contracts/vpn-front, The key): one operator's, for one task. Its secret
// is kept hashed, as an API key's. Unused, it lapses at LapsesAt (a TTL
// index drops it); taken, Instance is the client's, LapsesAt goes, and it
// is that client's until it disconnects (core then drops it).
type VPNKey struct {
	field.DefaultField `bson:",inline"`

	KeyID       string    `bson:"key_id" json:"key_id"`
	SecretHash  string    `bson:"secret_hash" json:"-"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	UserID      uuid.UUID `bson:"user_id" json:"user_id"`
	Username    string    `bson:"username" json:"username"`
	// LapsesAt is when an unused key goes; nil once a client took it.
	LapsesAt *time.Time `bson:"lapses_at,omitempty" json:"lapses_at,omitempty"`
	// Instance is the client's that took it (VPNHello.Instance), and when.
	Instance string     `bson:"instance,omitempty" json:"instance,omitempty"`
	TakenAt  *time.Time `bson:"taken_at,omitempty" json:"taken_at,omitempty"`
}
