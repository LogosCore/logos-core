package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// FsEntry is a path a machine was seen to have (docs:
// contracts/file-browser.md): one entry of the snapshot core keeps of each
// machine's file system, made from its sessions' tasks' results as they end.
// Machine is the snapshot's key within the operation (FsMachineKey).
type FsEntry struct {
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	Machine     string    `bson:"machine" json:"machine"`
	// Path is as the host writes it; PathKey compares (lowercase on a
	// Windows host). Parent ("" for a root) and Name likewise.
	Path      string `bson:"path" json:"path"`
	PathKey   string `bson:"path_key" json:"-"`
	Parent    string `bson:"parent" json:"parent"`
	ParentKey string `bson:"parent_key" json:"-"`
	Name      string `bson:"name" json:"name"`
	// NameKey is the name in lowercase, for searching and sorting.
	NameKey string `bson:"name_key" json:"-"`
	Sep     string `bson:"sep" json:"sep"`

	// What was last known of it (a dir_listing entry's).
	Type    string `bson:"type" json:"type"`
	DirRank int    `bson:"dir_rank" json:"-"` // 0 a folder, 1 the rest: folders first
	// SizeKey and ModKey sort and page by size and modified: never missing
	// (-1 and math.MinInt64 for unknown).
	SizeKey  int64  `bson:"size_key" json:"-"`
	ModKey   int64  `bson:"mod_key" json:"-"`
	Size     *int64 `bson:"size,omitempty" json:"size,omitempty"`
	Modified *int64 `bson:"modified,omitempty" json:"modified,omitempty"`
	Mode     string `bson:"mode,omitempty" json:"mode,omitempty"`
	Owner    string `bson:"owner,omitempty" json:"owner,omitempty"`
	Group    string `bson:"group,omitempty" json:"group,omitempty"`
	Target   string `bson:"target,omitempty" json:"target,omitempty"`
	Hidden   bool   `bson:"hidden,omitempty" json:"hidden,omitempty"`

	// ChangedAt is the end of the task whose change the entry holds: it
	// takes only newer ones.
	ChangedAt time.Time `bson:"changed_at" json:"changed_at"`
	// Known: a task said what it is (a listing, a transfer); an entry made
	// only as a parent on the way to one is not.
	Known  bool       `bson:"known,omitempty" json:"known,omitempty"`
	SeenAt *time.Time `bson:"seen_at,omitempty" json:"seen_at,omitempty"`
	SeenBy *FsTaskRef `bson:"seen_by,omitempty" json:"seen_by,omitempty"`
	GoneAt *time.Time `bson:"gone_at,omitempty" json:"gone_at,omitempty"`
	GoneBy *FsTaskRef `bson:"gone_by,omitempty" json:"gone_by,omitempty"`
	// GoneHow: FsGoneDeleted, or FsGoneNotListed.
	GoneHow string `bson:"gone_how,omitempty" json:"gone_how,omitempty"`

	// A folder: when its children were last listed, and by whom.
	ListedAt *time.Time `bson:"listed_at,omitempty" json:"listed_at,omitempty"`
	ListedBy *FsTaskRef `bson:"listed_by,omitempty" json:"listed_by,omitempty"`

	Downloaded []FsTransfer `bson:"downloaded,omitempty" json:"downloaded,omitempty"`
	Uploaded   []FsTransfer `bson:"uploaded,omitempty" json:"uploaded,omitempty"`
}

// FsTaskRef is the task that said something of an entry, and its session.
type FsTaskRef struct {
	TaskID    uuid.UUID `bson:"task_id" json:"task_id"`
	SessionID uuid.UUID `bson:"session_id" json:"session_id"`
	// Number and User are the session's, then: what the browser shows.
	Number int64  `bson:"number" json:"number"`
	User   string `bson:"user,omitempty" json:"user,omitempty"`
}

// FsTransfer is a file a task moved: downloaded (FileID the kept file's,
// none for a stream) or uploaded (By the operator).
type FsTransfer struct {
	TaskID uuid.UUID  `bson:"task_id" json:"task_id"`
	FileID *uuid.UUID `bson:"file_id,omitempty" json:"file_id,omitempty"`
	Size   int64      `bson:"size" json:"size"`
	SHA256 []byte     `bson:"sha256,omitempty" json:"sha256,omitempty"`
	Stream bool       `bson:"stream,omitempty" json:"stream,omitempty"`
	By     *uuid.UUID `bson:"by,omitempty" json:"by,omitempty"`
	At     time.Time  `bson:"at" json:"at"`
}

// How an entry went.
const (
	FsGoneDeleted   = "deleted"
	FsGoneNotListed = "not_listed"
)

// FsMachineKey is the snapshot a session's tasks feed: its machine's id,
// or, for a session without a lasting one (none, or ephemeral), its own.
func FsMachineKey(s MinionSession) string {
	if id := s.Hello.MachineID; id != "" && !strings.HasPrefix(id, "ephemeral:") {
		return id
	}
	return "session:" + s.SessionID.String()
}
