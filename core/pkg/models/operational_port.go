package models

import (
	"time"

	"github.com/google/uuid"
)

// OperationalPort is one of core's operational ports in use
// (contracts/minion-messages/tunnels.md, Operational ports): the task it
// serves, and for what. It goes back to the range when the task ends; a
// restart opens again the ports of tasks still running.
type OperationalPort struct {
	Port        int       `bson:"port" json:"port"`
	Purpose     string    `bson:"purpose" json:"purpose"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	OpenedBy    uuid.UUID `bson:"opened_by" json:"opened_by"`
	OpenedAt    time.Time `bson:"opened_at" json:"opened_at"`
	// Streams is how many streams the task has opened, the last one's
	// number: a stream's is never used again in the task, a restart's
	// streams' neither.
	Streams uint64 `bson:"streams" json:"streams"`
	// MaxStreams and Timeout are the task's max_streams and timeout args:
	// how many streams may be open at once, and how long the minion tries
	// a connect.
	MaxStreams int           `bson:"max_streams,omitempty" json:"max_streams,omitempty"`
	Timeout    time.Duration `bson:"timeout,omitempty" json:"timeout,omitempty"`
}

// OperationalPurposeSOCKS: a tunnel task's SOCKS proxy.
const OperationalPurposeSOCKS = "socks"

// MinionTunnelStream is one stream of a tunnel task, as the operation's
// record: who reached what through the minion, when, and how much went
// each way. The bytes are never kept.
type MinionTunnelStream struct {
	TaskID      uuid.UUID `bson:"task_id" json:"task_id"`
	Stream      uint64    `bson:"stream" json:"stream"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	// Client is the address of the tool that asked for it (the SOCKS
	// client's).
	Client string `bson:"client" json:"client"`
	// Host and Port: the destination, as the tool named it.
	Host     string    `bson:"host" json:"host"`
	Port     int       `bson:"port" json:"port"`
	OpenedAt time.Time `bson:"opened_at" json:"opened_at"`
	// Result is the minion's reply (protocol.TunnelOpen, ...): nil while
	// it has not come. Bound is the minion's socket's address when open;
	// Msg why not.
	Result *int   `bson:"result,omitempty" json:"result,omitempty"`
	Bound  string `bson:"bound,omitempty" json:"bound,omitempty"`
	Msg    string `bson:"msg,omitempty" json:"msg,omitempty"`
	// EndedAt and End: when and how it ended (TunnelEnd...).
	EndedAt *time.Time `bson:"ended_at,omitempty" json:"ended_at,omitempty"`
	End     string     `bson:"end,omitempty" json:"end,omitempty"`
	// Up: bytes the tool sent the destination; Down: back.
	Up   int64 `bson:"up" json:"up"`
	Down int64 `bson:"down" json:"down"`
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
