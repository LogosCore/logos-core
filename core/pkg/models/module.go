package models

import (
	"time"

	"github.com/qiniu/qmgo/field"
)

// Module lifecycle status values (see contract module-lifecycle.md state machine).
const (
	ModuleStatusRegistered   = "registered"
	ModuleStatusDead         = "dead"
	ModuleStatusDeregistered = "deregistered"
)

// Module is one row per module instance in the durable registry —
// the system-of-record for "which module instances exist and their lifecycle
// state". Upserted on module.register (idempotent takeover). Instance is
// globally unique because heartbeat/deregister address an instance by id alone.
//
// Name vs Instance: Name is the module's hardcoded identity — its project/kind
// (e.g. "http", "telegram") baked into the implementation and shared by every
// running copy of that module. Instance is the unique id of one concrete
// deployment ("http-channel-1", "http-channel-2"). An operator may run many
// instances of the same Name; each carries its own unique Instance.
//
// Transposition *profiles* are NOT stored here — they remain channel-owned YAML
// on disk (ADR-0002). This collection holds only registration + liveness data.
type Module struct {
	field.DefaultField `bson:",inline"`

	Type        string `bson:"module_type" json:"module_type"` // channel | factory
	Name        string `bson:"module_name" json:"module_name"` // hardcoded module identity (http, telegram, ...); shared across all instances of that module
	Instance    string `bson:"instance" json:"instance"`       // self-assigned, unique per deployed instance
	Version     string `bson:"version" json:"version"`
	RPCQueue    string `bson:"rpc_queue" json:"rpc_queue"`                         // core→module callback queue
	Description string `bson:"description,omitempty" json:"description,omitempty"` // self-reported, human-facing

	Status          string         `bson:"status" json:"status"`
	RegisteredAt    time.Time      `bson:"registered_at" json:"registered_at"`
	LastHeartbeatAt *time.Time     `bson:"last_heartbeat_at,omitempty" json:"last_heartbeat_at,omitempty"`
	LastStatus      string         `bson:"last_status,omitempty" json:"last_status,omitempty"` // healthy|degraded
	LastMetrics     map[string]any `bson:"last_metrics,omitempty" json:"last_metrics,omitempty"`

	DeregisteredAt   *time.Time `bson:"deregistered_at,omitempty" json:"deregistered_at,omitempty"`
	DeregisterReason string     `bson:"deregister_reason,omitempty" json:"deregister_reason,omitempty"`
	DeclaredDeadAt   *time.Time `bson:"declared_dead_at,omitempty" json:"declared_dead_at,omitempty"`

	// --- Module management (see module_management.go) ---

	// Manifest is what the instance declared at its latest module.register;
	// nil when it declared none. Replaced (or cleared) by every register.
	Manifest     *ModuleManifest `bson:"manifest,omitempty" json:"manifest,omitempty"`
	ManifestHash string          `bson:"manifest_hash,omitempty" json:"manifest_hash,omitempty"`
	// AppliedRevision is the config revision the instance last reported it
	// processed (register, heartbeat, or a mgmt.config.apply reply).
	AppliedRevision int64 `bson:"applied_revision" json:"applied_revision"`
	// Rejected and RestartRequired come from the last mgmt.config.apply reply.
	Rejected        map[string]string `bson:"rejected,omitempty" json:"rejected,omitempty"`
	RestartRequired []string          `bson:"restart_required,omitempty" json:"restart_required,omitempty"`
	// LastApplyError is set when a push failed after all retries, and cleared
	// by the next successful apply.
	LastApplyError string     `bson:"last_apply_error,omitempty" json:"last_apply_error,omitempty"`
	LastApplyAt    *time.Time `bson:"last_apply_at,omitempty" json:"last_apply_at,omitempty"`

	// Connection is the minion connection schema the instance declared at its
	// latest module.register (channels only; see channel_endpoint.go); nil
	// when it declared none. Replaced (or cleared) by every register.
	Connection *ModuleConnectionSchema `bson:"connection,omitempty" json:"connection,omitempty"`

	// Build is the build schema the instance declared at its latest
	// module.register (factories only; see build.go); nil when it declared
	// none. Replaced (or cleared) by every register.
	Build *ModuleBuildSchema `bson:"build,omitempty" json:"build,omitempty"`
}
