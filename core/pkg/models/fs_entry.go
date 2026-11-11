package models

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
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
	// A folder a task could not read (permission denied): why, when and
	// by whom; cleared by a later listing of it.
	ListErr   string     `bson:"list_err,omitempty" json:"list_err,omitempty"`
	ListErrAt *time.Time `bson:"list_err_at,omitempty" json:"list_err_at,omitempty"`
	ListErrBy *FsTaskRef `bson:"list_err_by,omitempty" json:"list_err_by,omitempty"`

	// Downloaded: transfers core keeps the file of, a download's or a
	// stream's kept as it passed (Stream). Streamed: a stream core kept no
	// copy of (over its file size cap, or not passed through whole): it
	// went to the downloader alone.
	Downloaded []FsTransfer `bson:"downloaded,omitempty" json:"downloaded,omitempty"`
	Uploaded   []FsTransfer `bson:"uploaded,omitempty" json:"uploaded,omitempty"`
	Streamed   []FsTransfer `bson:"streamed,omitempty" json:"streamed,omitempty"`

	// Volume: a volume's files are here (a drive's root, a mount point),
	// as the newest volumes task of the machine said; none once a later
	// one no longer had it.
	Volume *FsVolume `bson:"volume,omitempty" json:"volume,omitempty"`
}

// FsVolume is a volume as a volumes task listed it (protocol.Volume): its
// kind, what it is mounted from, its label, file system, size and free
// space, and the task that listed it.
type FsVolume struct {
	Kind     string    `bson:"kind" json:"kind"`
	Device   string    `bson:"device,omitempty" json:"device,omitempty"`
	Label    string    `bson:"label,omitempty" json:"label,omitempty"`
	FS       string    `bson:"fs,omitempty" json:"fs,omitempty"`
	Size     *int64    `bson:"size,omitempty" json:"size,omitempty"`
	Free     *int64    `bson:"free,omitempty" json:"free,omitempty"`
	ReadOnly bool      `bson:"read_only,omitempty" json:"read_only,omitempty"`
	NoMedia  bool      `bson:"no_media,omitempty" json:"no_media,omitempty"`
	SeenAt   time.Time `bson:"seen_at" json:"seen_at"`
	SeenBy   FsTaskRef `bson:"seen_by" json:"seen_by"`
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

// An entry's lists of transfers (FsEntry).
const (
	FsDownloaded = "downloaded"
	FsUploaded   = "uploaded"
	FsStreamed   = "streamed"
)

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

// FsListing is where a task's listing stands as its pieces come (a
// ListingPiece's id within the task; docs: contracts/minion-messages.md,
// Listings): when core began it, the folder once its first piece came,
// the pieces applied to the snapshot, and its last. Kept a while after.
type FsListing struct {
	TaskID uuid.UUID `bson:"task_id"`
	ID     uint64    `bson:"id"`
	// At is when core took its first piece: the listing's time in the
	// snapshot, every piece's.
	At     time.Time `bson:"at"`
	Path   string    `bson:"path,omitempty"`
	Sep    string    `bson:"sep,omitempty"`
	Parent string    `bson:"parent,omitempty"`
	// Last is the last piece's number, once it came.
	Last    *uint64 `bson:"last,omitempty"`
	Partial bool    `bson:"partial,omitempty"`
	Err     string  `bson:"err,omitempty"`
	// Applied are the pieces in the snapshot; Whole, that what the folder
	// no longer has was marked gone (once).
	Applied []uint64 `bson:"applied,omitempty"`
	Whole   bool     `bson:"whole,omitempty"`
	// Pending are pieces that came before the first, which names the
	// folder: applied when it comes.
	Pending   []FsPendingPiece `bson:"pending,omitempty"`
	CreatedAt time.Time        `bson:"created_at"`
}

// FsPendingPiece is a listing's piece kept until its folder is known.
type FsPendingPiece struct {
	Part    uint64              `bson:"part"`
	Entries []protocol.DirEntry `bson:"entries,omitempty"`
}

// Complete: every piece up to the last applied.
func (l FsListing) Complete() bool {
	if l.Last == nil {
		return false
	}
	for p := uint64(0); p <= *l.Last; p++ {
		if !slices.Contains(l.Applied, p) {
			return false
		}
	}
	return true
}
