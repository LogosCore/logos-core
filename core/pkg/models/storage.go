package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// StorageAction is a maintenance action run on the object store
// (pkg/storage): reclaiming its deleted bytes, adding a volume to a bucket,
// or a retention cleanup of recordings, an admin's or the retention
// policy's (RetentionActor). Kept as the Storage page's activity.
type StorageAction struct {
	field.DefaultField `bson:",inline"`

	ActionID uuid.UUID `bson:"action_id" json:"action_id"`
	// Action is StorageActionReclaim (every bucket's volumes),
	// StorageActionGrow (Bucket's) or StorageActionCleanup (the recordings
	// bucket's).
	Action string `bson:"action" json:"action"`
	Bucket string `bson:"bucket,omitempty" json:"bucket,omitempty"`
	// Actor is who ran it.
	Actor ModuleActor `bson:"actor" json:"actor"`
	// Status is StorageActionRunning until it ends, then Succeeded, Failed
	// (Err says why) or Cancelled (CancelledBy).
	Status     string     `bson:"status" json:"status"`
	Err        string     `bson:"err,omitempty" json:"err,omitempty"`
	StartedAt  time.Time  `bson:"started_at" json:"started_at"`
	FinishedAt *time.Time `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	// Freed is the bytes a reclaim took off the disk so far (each volume's
	// before, less after); Volumes the volumes a grow added.
	Freed   int64 `bson:"freed,omitempty" json:"freed,omitempty"`
	Volumes int   `bson:"volumes,omitempty" json:"volumes,omitempty"`

	// Step is what a running reclaim or cleanup does now, in a few words,
	// and since when.
	Step   string     `bson:"step,omitempty" json:"step,omitempty"`
	StepAt *time.Time `bson:"step_at,omitempty" json:"step_at,omitempty"`
	// CancelRequestedAt and CancelledBy: an admin asked a running reclaim
	// or cleanup to stop. It stops before its next step (a volume, a
	// recording), and ends Cancelled.
	CancelRequestedAt *time.Time   `bson:"cancel_requested_at,omitempty" json:"cancel_requested_at,omitempty"`
	CancelledBy       *ModuleActor `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`

	// A reclaim's: the volumes it compacts, as it found them, the most
	// deleted first, each as it went. VacuumPaused: SeaweedFS's own vacuum
	// is paused for it, until it ends (or, for a core that stopped first,
	// until core starts again).
	ReclaimVolumes []StorageReclaimVolume `bson:"reclaim_volumes,omitempty" json:"reclaim_volumes,omitempty"`
	VacuumPaused   bool                   `bson:"vacuum_paused,omitempty" json:"vacuum_paused,omitempty"`

	// A cleanup's (docs: Storage, Retention cleanups): the age it takes
	// recordings at, in days, and so the time they ended before; the
	// operation it covers, named as it was then (none: every operation);
	// the recordings it took and their bytes, kept as it goes.
	OlderThanDays int        `bson:"older_than_days,omitempty" json:"older_than_days,omitempty"`
	Before        *time.Time `bson:"before,omitempty" json:"before,omitempty"`
	OperationID   *uuid.UUID `bson:"operation_id,omitempty" json:"operation_id,omitempty"`
	OperationName string     `bson:"operation_name,omitempty" json:"operation_name,omitempty"`
	Recordings    int64      `bson:"recordings,omitempty" json:"recordings,omitempty"`
	Bytes         int64      `bson:"bytes,omitempty" json:"bytes,omitempty"`
	// PlannedRecordings and PlannedBytes are what a cleanup found to take
	// as it began: it takes what still matches as it goes.
	PlannedRecordings int64 `bson:"planned_recordings,omitempty" json:"planned_recordings,omitempty"`
	PlannedBytes      int64 `bson:"planned_bytes,omitempty" json:"planned_bytes,omitempty"`
}

// StorageReclaimVolume is one volume a reclaim compacts: where it is, what
// it held as the reclaim found it, and how its compaction went.
type StorageReclaimVolume struct {
	ID uint32 `bson:"id" json:"id"`
	// Bucket is its collection ("": SeaweedFS's own files); Server the
	// volume server it is read on (host:port).
	Bucket string `bson:"bucket" json:"bucket"`
	Server string `bson:"server" json:"server"`
	// Size is its bytes as the reclaim found it, Deleted those deleted
	// files held.
	Size    int64 `bson:"size" json:"size"`
	Deleted int64 `bson:"deleted" json:"deleted"`
	// State is a ReclaimVolume... state; Freed the bytes its compaction
	// took off the disk; Msg why it was skipped, failed or stopped.
	State      string     `bson:"state" json:"state"`
	Freed      int64      `bson:"freed,omitempty" json:"freed,omitempty"`
	Msg        string     `bson:"msg,omitempty" json:"msg,omitempty"`
	StartedAt  *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt *time.Time `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
}

// A reclaim's volume, as it goes.
const (
	ReclaimVolumeWaiting    = "waiting"
	ReclaimVolumeCompacting = "compacting"
	ReclaimVolumeCompacted  = "compacted"
	// ReclaimVolumeSkipped: it needed no compaction once reached (Msg).
	ReclaimVolumeSkipped = "skipped"
	ReclaimVolumeFailed  = "failed"
	// ReclaimVolumeStopped: the reclaim stopped before it, or as SeaweedFS
	// compacted it (Msg).
	ReclaimVolumeStopped = "stopped"
)

// StorageActionProgress is how far a running reclaim or cleanup came: what
// it does now, and what it did so far.
type StorageActionProgress struct {
	Step           string
	StepAt         *time.Time
	Freed          int64
	Recordings     int64
	Bytes          int64
	ReclaimVolumes []StorageReclaimVolume
}

// Storage actions, and their states.
const (
	StorageActionReclaim = "reclaim"
	StorageActionGrow    = "grow"
	StorageActionCleanup = "cleanup"

	StorageActionRunning   = "running"
	StorageActionSucceeded = "succeeded"
	StorageActionFailed    = "failed"
	StorageActionCancelled = "cancelled"
)

// StorageUsage is what core's records say it keeps in the object store for
// one operation, of one kind of one of its purposes: their objects and
// bytes. OperationID is uuid.Nil for what is no operation's (skills).
type StorageUsage struct {
	Purpose     string    `bson:"purpose" json:"purpose"`
	Kind        string    `bson:"kind" json:"kind"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	Objects     int64     `bson:"objects" json:"objects"`
	Bytes       int64     `bson:"bytes" json:"bytes"`
}

// What core keeps in its buckets (StorageUsage.Purpose): each its own
// bucket's, unless the settings name one bucket for several.
const (
	StoragePurposeMinionFiles = "minion_files" // MINION_FILE_BUCKET
	StoragePurposeRecordings  = "recordings"   // DESKTOP_RECORDING_BUCKET
	StoragePurposeBuilds      = "builds"       // BUILD_BUCKET
	StoragePurposeWikiImages  = "wiki_images"  // WIKI_IMAGE_BUCKET
	StoragePurposeWikiFiles   = "wiki_files"   // WIKI_FILE_BUCKET
	StoragePurposeSkills      = "skills"       // SKILL_BUCKET
)

// StorageRetention is the retention policy (docs: Storage, Retention
// cleanups), one for every operation: on, core runs a cleanup each day of
// the recordings that ended OlderThanDays ago, but those of tasks marked
// important. Off until an admin turns it on.
type StorageRetention struct {
	Enabled       bool `bson:"enabled" json:"enabled"`
	OlderThanDays int  `bson:"older_than_days" json:"older_than_days"`
	// UpdatedAt and UpdatedBy are who set it last, and when: nil before
	// anyone did.
	UpdatedAt *time.Time   `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
	UpdatedBy *ModuleActor `bson:"updated_by,omitempty" json:"updated_by,omitempty"`
}

// DefaultRetentionDays is the retention policy's age before an admin sets
// one, and the age a cleanup is offered at.
const DefaultRetentionDays = 30

// RetentionActor runs the retention policy's cleanups.
var RetentionActor = ModuleActor{Type: "system", ID: "retention_policy", Name: "Retention policy"}
