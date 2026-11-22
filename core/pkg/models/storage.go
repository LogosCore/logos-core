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
	// Status is StorageActionRunning until it ends, then Succeeded or
	// Failed (Err says why).
	Status     string     `bson:"status" json:"status"`
	Err        string     `bson:"err,omitempty" json:"err,omitempty"`
	StartedAt  time.Time  `bson:"started_at" json:"started_at"`
	FinishedAt *time.Time `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	// Freed is the bytes a reclaim took off the disk (the store's volumes
	// before, less after); Volumes the volumes a grow added.
	Freed   int64 `bson:"freed,omitempty" json:"freed,omitempty"`
	Volumes int   `bson:"volumes,omitempty" json:"volumes,omitempty"`

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
}

// Storage actions, and their states.
const (
	StorageActionReclaim = "reclaim"
	StorageActionGrow    = "grow"
	StorageActionCleanup = "cleanup"

	StorageActionRunning   = "running"
	StorageActionSucceeded = "succeeded"
	StorageActionFailed    = "failed"
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
