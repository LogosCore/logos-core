package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// StorageAction is a maintenance action an admin ran on the object store
// (pkg/storage): reclaiming its deleted bytes, or adding a volume to a
// bucket. Kept as the Storage page's activity.
type StorageAction struct {
	field.DefaultField `bson:",inline"`

	ActionID uuid.UUID `bson:"action_id" json:"action_id"`
	// Action is StorageActionReclaim (every bucket's volumes) or
	// StorageActionGrow (Bucket's).
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
}

// Storage actions, and their states.
const (
	StorageActionReclaim = "reclaim"
	StorageActionGrow    = "grow"

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
