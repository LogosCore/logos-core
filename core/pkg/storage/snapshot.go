package storage

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// Snapshot is the object store as core last read it: SeaweedFS's state
// (when core is set to reach its master), what core's records say it keeps
// in each bucket, and the warnings they give.
type Snapshot struct {
	At        time.Time
	SeaweedFS SeaweedFS
	Disks     []Disk
	Buckets   []Bucket
	Warnings  []Warning
	// RecordsErr is why core could not read its own records; empty when it
	// could.
	RecordsErr string
	// consumers are each bucket's consumers (by name), the largest first.
	consumers map[string][]Consumer
}

// SeaweedFS is the store's state, as its master says.
type SeaweedFS struct {
	// Configured: core has the master's URL (SEAWEEDFS_MASTER_URL);
	// Reachable: it answered, else Err says why.
	Configured bool
	Reachable  bool
	Err        string
	Version    string
	Leader     string
	// Slots are the volumes the volume servers have room for (about one a
	// GB of free disk), FreeSlots those not taken: a bucket that needs a
	// new volume when none is free fails its writes.
	Slots     int
	FreeSlots int
}

// Disk is one of a volume server's disks.
type Disk struct {
	// Node is the volume server (host:port), Dir its directory there; Err
	// says why its disks could not be read (no Dir then).
	Node string
	Dir  string
	Err  string
	// Total, Used and Free are the disk's bytes, whatever uses them.
	Total, Used, Free int64
	// Volumes are the server's volumes, MaxVolumes the ones it has room
	// for.
	Volumes, MaxVolumes int
}

// Bucket is one of the store's buckets: SeaweedFS's numbers for it (a
// bucket is a collection), and core's records of what it keeps there.
type Bucket struct {
	// Name "" is SeaweedFS's own: volumes written outside any bucket.
	Name string
	// Purposes are what core keeps there (models.StoragePurpose...); none:
	// not core's.
	Purposes []string
	// Volumes are its volumes, Writable those new files go to, ReadOnly
	// those full or set read-only: none (Written false) before its first
	// write.
	Volumes, Writable, ReadOnly int
	// Size is its volumes' bytes on disk, Deleted the part of them deleted
	// files hold until reclaimed (Reclaim space).
	Size, Deleted int64
	// Objects and ObjectBytes are what core's records say it keeps there.
	Objects, ObjectBytes int64
}

// Written reports whether SeaweedFS has volumes for the bucket.
func (b Bucket) Written() bool { return b.Volumes > 0 }

// Core reports whether core keeps something in the bucket.
func (b Bucket) Core() bool { return len(b.Purposes) > 0 }

// Warning is something about the store an admin should know: Level
// LevelError (writes fail, or core cannot tell), LevelWarning (they will
// soon), LevelInfo (space to win back). Bucket names the bucket it is
// about, when one.
type Warning struct {
	Level   string
	Code    string
	Bucket  string
	Message string
}

// Warning levels, the worst first.
const (
	LevelError   = "error"
	LevelWarning = "warning"
	LevelInfo    = "info"
)

// Consumer is what core keeps in a bucket for one operation, of one kind:
// the Storage page's breakdown. OperationID is uuid.Nil for what is no
// operation's (skills); OperationName "" for an operation gone.
type Consumer struct {
	OperationID   uuid.UUID
	OperationName string
	Purpose, Kind string
	Objects       int64
	Bytes         int64
}

// key tells consumers of a bucket apart.
func (c Consumer) key() string { return c.OperationID.String() + "/" + c.Purpose + "/" + c.Kind }

// purposeOrder is the order core's buckets show in.
var purposeOrder = []string{models.StoragePurposeMinionFiles, models.StoragePurposeRecordings, models.StoragePurposeBuilds,
	models.StoragePurposeWikiImages, models.StoragePurposeWikiFiles, models.StoragePurposeSkills}

// seaweedState is what SeaweedFS said for a snapshot; err is the master's
// failure (none of the rest then).
type seaweedState struct {
	dir     dirStatus
	vols    volStatus
	cluster clusterStatus
	// nodes are the data nodes' disks, nodeErrs why one could not be read.
	nodes    map[string]nodeStatus
	nodeErrs map[string]string
	err      error
}

// buildSnapshot makes a snapshot of what SeaweedFS said (nil: core is not
// set to reach it) and what core's records say (usage, or usageErr), with
// operations' names.
func buildSnapshot(at time.Time, cfg Config, sw *seaweedState, usage []models.StorageUsage, usageErr error,
	names map[uuid.UUID]string) Snapshot {
	snap := Snapshot{At: at, consumers: map[string][]Consumer{}}
	byName := map[string]*Bucket{}
	bucket := func(name string) *Bucket {
		b, ok := byName[name]
		if !ok {
			b = &Bucket{Name: name}
			byName[name] = b
		}
		return b
	}
	for _, p := range purposeOrder {
		if name := cfg.Buckets[p]; name != "" {
			b := bucket(name)
			b.Purposes = append(b.Purposes, p)
		}
	}

	if sw != nil {
		snap.SeaweedFS.Configured = true
		if sw.err != nil {
			snap.SeaweedFS.Err = sw.err.Error()
		} else {
			snap.SeaweedFS.Reachable = true
			snap.SeaweedFS.Version = sw.dir.Version
			snap.SeaweedFS.Leader = sw.cluster.Leader
			snap.SeaweedFS.Slots, snap.SeaweedFS.FreeSlots = sw.dir.Topology.Max, sw.dir.Topology.Free
			for _, v := range sw.vols.all() {
				b := bucket(v.Collection)
				b.Volumes++
				b.Size += v.Size
				b.Deleted += v.DeletedByteCount
				if v.ReadOnly {
					b.ReadOnly++
				}
			}
			for _, l := range sw.dir.Topology.Layouts {
				bucket(l.Collection).Writable += len(l.Writables)
			}
			for _, dc := range sw.dir.Topology.DataCenters {
				for _, rack := range dc.Racks {
					for _, n := range rack.DataNodes {
						snap.Disks = append(snap.Disks, nodeDisks(n.URL, n.Volumes, n.Max, sw)...)
					}
				}
			}
		}
	}

	if usageErr != nil {
		snap.RecordsErr = usageErr.Error()
	}
	for _, u := range usage {
		name := cfg.Buckets[u.Purpose]
		if name == "" {
			continue
		}
		b := bucket(name)
		b.Objects += u.Objects
		b.ObjectBytes += u.Bytes
		snap.consumers[name] = append(snap.consumers[name], Consumer{OperationID: u.OperationID, OperationName: names[u.OperationID],
			Purpose: u.Purpose, Kind: u.Kind, Objects: u.Objects, Bytes: u.Bytes})
	}
	for name := range snap.consumers {
		slices.SortFunc(snap.consumers[name], compareConsumers)
	}

	for _, b := range byName {
		snap.Buckets = append(snap.Buckets, *b)
	}
	slices.SortFunc(snap.Buckets, compareBuckets)
	snap.Warnings = warnings(cfg, snap)
	return snap
}

// nodeDisks are a data node's disks as its /status said, or one Disk saying
// why they could not be read.
func nodeDisks(node string, volumes, maxVolumes int, sw *seaweedState) []Disk {
	if why, failed := sw.nodeErrs[node]; failed {
		return []Disk{{Node: node, Err: why, Volumes: volumes, MaxVolumes: maxVolumes}}
	}
	var out []Disk
	for _, d := range sw.nodes[node].DiskStatuses {
		out = append(out, Disk{Node: node, Dir: d.Dir, Total: d.All, Used: d.Used, Free: d.Free, Volumes: volumes, MaxVolumes: maxVolumes})
	}
	return out
}

// compareBuckets orders buckets: core's in purposeOrder, then the others by
// name, SeaweedFS's own ("") last.
func compareBuckets(a, b Bucket) int {
	rank := func(x Bucket) int {
		switch {
		case x.Core():
			return slices.Index(purposeOrder, x.Purposes[0])
		case x.Name == "":
			return len(purposeOrder) + 1
		}
		return len(purposeOrder)
	}
	if c := cmp.Compare(rank(a), rank(b)); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}

// compareConsumers orders a bucket's consumers: the largest first, then by
// operation and kind.
func compareConsumers(a, b Consumer) int {
	if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
		return c
	}
	if c := strings.Compare(a.OperationName, b.OperationName); c != 0 {
		return c
	}
	return strings.Compare(a.key(), b.key())
}

// warnings are what an admin should know of snap: SeaweedFS out of reach,
// slots running out, a bucket that cannot be written, a disk running full,
// deleted bytes worth reclaiming, records core cannot read. The worst
// first.
func warnings(cfg Config, snap Snapshot) []Warning {
	var out []Warning
	add := func(level, code, bucket, format string, args ...any) {
		out = append(out, Warning{Level: level, Code: code, Bucket: bucket, Message: fmt.Sprintf(format, args...)})
	}
	sw := snap.SeaweedFS
	if snap.RecordsErr != "" {
		add(LevelWarning, "records_unreadable", "", "Core cannot read its records of what it keeps: %s.", snap.RecordsErr)
	}
	switch {
	case !sw.Configured:
	case !sw.Reachable:
		add(LevelError, "seaweedfs_unreachable", "", "Core cannot reach SeaweedFS's master: %s. The buckets show what core's records say alone.", sw.Err)
	default:
		switch {
		case sw.FreeSlots == 0:
			add(LevelError, "no_free_slots", "", "No free volume slots: a bucket that needs a new volume fails its writes. "+
				"SeaweedFS has a slot for about every GB of free disk on its volume servers: free disk there.")
		case sw.FreeSlots < cfg.FewSlots:
			add(LevelWarning, "few_free_slots", "", "Only %d of %d volume slots are free: once none is, a bucket that needs a new volume "+
				"fails its writes. Free disk on the volume servers.", sw.FreeSlots, sw.Slots)
		}
		for _, b := range snap.Buckets {
			switch {
			case b.Name == "" || b.Writable > 0 || (!b.Written() && !b.Core()):
			case !b.Written() && sw.FreeSlots == 0:
				add(LevelError, "bucket_not_writable", b.Name, "%s has no volume yet, and no slot is free for its first: its first write fails.", b.Name)
			case !b.Written():
			case sw.FreeSlots == 0:
				add(LevelError, "bucket_not_writable", b.Name, "%s has no writable volume, and no slot is free for one: its writes fail.", b.Name)
			default:
				add(LevelWarning, "bucket_not_writable", b.Name, "%s has no writable volume: SeaweedFS adds one at its next write, "+
					"or add one now.", b.Name)
			}
		}
		for _, d := range snap.Disks {
			switch {
			case d.Err != "":
				add(LevelWarning, "disks_unreadable", "", "Core cannot read the disks of volume server %s: %s.", d.Node, d.Err)
			case d.Total > 0 && float64(d.Free)*100 < cfg.DiskLowPercent*float64(d.Total):
				add(LevelWarning, "disk_low", "", "%s on volume server %s has %.0f%% free (%s of %s).", d.Dir, d.Node,
					float64(d.Free)*100/float64(d.Total), bytesText(d.Free), bytesText(d.Total))
			}
		}
		for _, b := range snap.Buckets {
			if b.Deleted >= cfg.ReclaimableMin && float64(b.Deleted) >= cfg.ReclaimableRatio*float64(b.Size) {
				add(LevelInfo, "reclaimable", b.Name, "%s of %s's %s on disk are deleted files: reclaiming space frees them.",
					bytesText(b.Deleted), bucketText(b.Name), bytesText(b.Size))
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Warning) int { return cmp.Compare(levelRank(a.Level), levelRank(b.Level)) })
	return out
}

func levelRank(level string) int {
	return slices.Index([]string{LevelError, LevelWarning, LevelInfo}, level)
}

// bucketText is a bucket's name in a sentence.
func bucketText(name string) string {
	if name == "" {
		return "SeaweedFS's own files"
	}
	return name
}

// bytesText is a size as people read it: 512 B, 1.5 KiB, 2.3 GiB.
func bytesText(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
