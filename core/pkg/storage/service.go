// Package storage is the Storage page's: the object store core keeps its
// files in (SeaweedFS), as admins see and maintain it. Core reads SeaweedFS's
// master (when SEAWEEDFS_MASTER_URL is set: its disk, volume slots and
// buckets) and sums its own records of what it keeps in each bucket, once a
// minute, into a snapshot it serves; it warns of what would make writes
// fail (no free slot, a bucket with no writable volume, a full disk), and
// runs two maintenance actions: reclaiming deleted files' space (the
// master's vacuum, every bucket's volumes) and adding a volume to a bucket.
// Each action is kept (models.StorageAction) with who ran it and how it
// went.
package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// Config is how the service reads and maintains the store.
type Config struct {
	// MasterURL is SeaweedFS's master (http://seaweedfs-master:9333); empty:
	// core reads its own records alone, and runs no action.
	MasterURL string
	// Buckets are core's buckets, by purpose (models.StoragePurpose...).
	Buckets map[string]string
	// Interval is how often core reads the store (a minute); CallTimeout a
	// call's to SeaweedFS (10 s); ReclaimTimeout how long a reclaim may run
	// (30 minutes).
	Interval       time.Duration
	CallTimeout    time.Duration
	ReclaimTimeout time.Duration
	// ReclaimThreshold is the share of a volume deleted files must hold for a
	// reclaim to compact it (0.1). SeaweedFS compacts past its own (0.3) on
	// its own.
	ReclaimThreshold float64
	// FewSlots warns when fewer volume slots are free (3); DiskLowPercent
	// when a disk has less free (10); ReclaimableRatio and ReclaimableMin
	// tell of a bucket whose deleted files hold that share of it, and that
	// much (0.3, 64 MiB).
	FewSlots         int
	DiskLowPercent   float64
	ReclaimableRatio float64
	ReclaimableMin   int64
}

// DefaultConfig is Config's defaults, with no master and no buckets.
func DefaultConfig() Config {
	return Config{Interval: time.Minute, CallTimeout: 10 * time.Second, ReclaimTimeout: 30 * time.Minute, ReclaimThreshold: 0.1,
		FewSlots: 3, DiskLowPercent: 10, ReclaimableRatio: 0.3, ReclaimableMin: 64 << 20}
}

// Errors the actions refuse with.
var (
	ErrNotConfigured = errors.New("core is not set to reach SeaweedFS's master (SEAWEEDFS_MASTER_URL)")
	ErrUnknownBucket = errors.New("no such bucket")
	ErrRunning       = errors.New("it is running already")
)

// Operations names operations, for the breakdown.
type Operations interface {
	FindAll(ctx context.Context, search string, offset, limit int64, memberID *uuid.UUID) ([]models.Operation, error)
}

// Service serves the store's snapshot, and runs its actions.
type Service struct {
	cfg     Config
	sw      *seaweed // nil: no master
	usage   repository.IStorageUsageRepository
	actions repository.IStorageActionRepository
	ops     Operations
	logger  *zap.Logger
	now     func() time.Time

	// base is the service's context: Stop ends it, and a reclaim running
	// with it.
	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// building serializes the snapshot's builds; mu guards snap.
	building sync.Mutex
	mu       sync.RWMutex
	snap     *Snapshot
}

// New makes the service; Start begins its reads.
func New(cfg Config, usage repository.IStorageUsageRepository, actions repository.IStorageActionRepository, ops Operations,
	logger *zap.Logger) *Service {
	def := DefaultConfig()
	if cfg.Interval <= 0 {
		cfg.Interval = def.Interval
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = def.CallTimeout
	}
	if cfg.ReclaimTimeout <= 0 {
		cfg.ReclaimTimeout = def.ReclaimTimeout
	}
	if cfg.ReclaimThreshold <= 0 {
		cfg.ReclaimThreshold = def.ReclaimThreshold
	}
	if cfg.FewSlots <= 0 {
		cfg.FewSlots = def.FewSlots
	}
	if cfg.DiskLowPercent <= 0 {
		cfg.DiskLowPercent = def.DiskLowPercent
	}
	if cfg.ReclaimableRatio <= 0 {
		cfg.ReclaimableRatio = def.ReclaimableRatio
	}
	if cfg.ReclaimableMin <= 0 {
		cfg.ReclaimableMin = def.ReclaimableMin
	}
	s := &Service{cfg: cfg, usage: usage, actions: actions, ops: ops, logger: logger, now: time.Now}
	if master := strings.TrimRight(strings.TrimSpace(cfg.MasterURL), "/"); master != "" {
		s.sw = &seaweed{master: master, http: &http.Client{}}
	}
	s.base, s.cancel = context.WithCancel(context.Background())
	return s
}

// Start fails the actions a core that stopped left running, then reads the
// store now and every Interval.
func (s *Service) Start() {
	if n, err := s.actions.EndRunning(s.base, "core stopped before it ended", s.now()); err != nil {
		s.logger.Warn("storage: end the actions left running", zap.Error(err))
	} else if n > 0 {
		s.logger.Info("storage: ended the actions left running", zap.Int64("actions", n))
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(s.cfg.Interval)
		defer t.Stop()
		for {
			s.Refresh(s.base)
			select {
			case <-s.base.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Stop ends the reads and a reclaim running, and waits for them.
func (s *Service) Stop() {
	s.cancel()
	s.wg.Wait()
}

// Status is the latest snapshot: one read at once when there is none yet.
func (s *Service) Status(ctx context.Context) Snapshot {
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()
	if snap != nil {
		return *snap
	}
	return s.Refresh(ctx)
}

// Consumers is a page of what core keeps in bucket, from the latest
// snapshot (Snapshot.Consumers).
func (s *Service) Consumers(ctx context.Context, bucket, search, after string, first int) (ConsumerPage, error) {
	return s.Status(ctx).Consumers(bucket, search, after, first)
}

// Refresh reads the store and core's records now, and keeps the snapshot.
func (s *Service) Refresh(ctx context.Context) Snapshot {
	s.building.Lock()
	defer s.building.Unlock()
	var sw *seaweedState
	if s.sw != nil {
		st := s.readSeaweed(ctx)
		sw = &st
	}
	usage, usageErr := s.usage.Usage(ctx)
	snap := buildSnapshot(s.now(), s.cfg, sw, usage, usageErr, s.names(ctx, usage))
	s.mu.Lock()
	s.snap = &snap
	s.mu.Unlock()
	return snap
}

// readSeaweed is what the master says, and each data node's disks.
func (s *Service) readSeaweed(ctx context.Context) seaweedState {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	defer cancel()
	st := seaweedState{nodes: map[string]nodeStatus{}, nodeErrs: map[string]string{}}
	var err error
	if st.dir, err = s.sw.dirStatus(ctx); err != nil {
		st.err = err
		return st
	}
	if st.vols, err = s.sw.volStatus(ctx); err != nil {
		st.err = err
		return st
	}
	if st.cluster, err = s.sw.clusterStatus(ctx); err != nil {
		st.err = err
		return st
	}
	for _, dc := range st.dir.Topology.DataCenters {
		for _, rack := range dc.Racks {
			for _, n := range rack.DataNodes {
				ns, err := s.sw.nodeStatus(ctx, n.URL)
				if err != nil {
					st.nodeErrs[n.URL] = err.Error()
					continue
				}
				st.nodes[n.URL] = ns
			}
		}
	}
	return st
}

// names are the operations' names the usage names.
func (s *Service) names(ctx context.Context, usage []models.StorageUsage) map[uuid.UUID]string {
	names := map[uuid.UUID]string{models.PublicOperationID: models.PublicOperationName}
	if len(usage) == 0 || s.ops == nil {
		return names
	}
	ops, err := s.ops.FindAll(ctx, "", 0, 100_000, nil)
	if err != nil {
		s.logger.Warn("storage: read the operations' names", zap.Error(err))
		return names
	}
	for _, op := range ops {
		names[op.OperationID] = op.Name
	}
	return names
}

// Actions is a page of the actions run, the latest first, and how many there
// are in all.
func (s *Service) Actions(ctx context.Context, cursor *pagination.Cursor, limit int64, forward bool) ([]models.StorageAction, int64, error) {
	page, err := s.actions.FindWithCursor(ctx, cursor, limit, forward)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.actions.Count(ctx)
	return page, total, err
}

// Reclaim starts compacting every volume deleted files hold past
// ReclaimThreshold of, freeing their space on disk: it answers with the
// action running, which ends in the background (the master answers once
// every volume is done).
func (s *Service) Reclaim(ctx context.Context, actor models.ModuleActor) (*models.StorageAction, error) {
	if s.sw == nil {
		return nil, ErrNotConfigured
	}
	a := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionReclaim, Actor: actor,
		Status: models.StorageActionRunning, StartedAt: s.now()}
	if err := s.actions.Insert(ctx, a); err != nil {
		if errors.Is(err, repository.ErrStorageActionRunning) {
			return nil, fmt.Errorf("a reclaim: %w", ErrRunning)
		}
		return nil, err
	}
	s.logger.Info("storage: reclaiming deleted files' space", zap.String("action_id", a.ActionID.String()), zap.String("by", actor.Name))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.base, s.cfg.ReclaimTimeout)
		defer cancel()
		before, beforeErr := s.onDisk(ctx)
		err := s.sw.vacuum(ctx, s.cfg.ReclaimThreshold)
		status, why, freed := models.StorageActionSucceeded, "", int64(0)
		switch {
		case s.base.Err() != nil:
			status, why = models.StorageActionFailed, "core stopped before it ended"
		case err != nil:
			status, why = models.StorageActionFailed, err.Error()
		default:
			if after, afterErr := s.onDisk(ctx); beforeErr == nil && afterErr == nil {
				freed = max(before-after, 0)
			}
		}
		s.finish(a, status, why, freed, 0)
	}()
	return a, nil
}

// Grow adds a writable volume to bucket, one of core's or one SeaweedFS has:
// the action as it ended (a failure says SeaweedFS's reason, as when no slot
// is free).
func (s *Service) Grow(ctx context.Context, actor models.ModuleActor, bucket string) (*models.StorageAction, error) {
	if s.sw == nil {
		return nil, ErrNotConfigured
	}
	if !s.known(ctx, bucket) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBucket, bucket)
	}
	a := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionGrow, Bucket: bucket, Actor: actor,
		Status: models.StorageActionRunning, StartedAt: s.now()}
	if err := s.actions.Insert(ctx, a); err != nil {
		if errors.Is(err, repository.ErrStorageActionRunning) {
			return nil, fmt.Errorf("adding a volume to %s: %w", bucket, ErrRunning)
		}
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	defer cancel()
	n, err := s.sw.grow(callCtx, bucket, 1)
	status, why := models.StorageActionSucceeded, ""
	if err != nil {
		status, why = models.StorageActionFailed, err.Error()
	}
	return s.finish(a, status, why, 0, n), nil
}

// finish ends a and reads the store again: a as it ended (as it started,
// when it could not be kept).
func (s *Service) finish(a *models.StorageAction, status, why string, freed int64, volumes int) *models.StorageAction {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CallTimeout)
	defer cancel()
	after, err := s.actions.Finish(ctx, a.ActionID, status, why, freed, volumes, s.now())
	if err != nil {
		s.logger.Warn("storage: keep an action's end", zap.String("action_id", a.ActionID.String()), zap.Error(err))
		after = a
	}
	s.logger.Info("storage: action ended", zap.String("action", a.Action), zap.String("bucket", a.Bucket),
		zap.String("status", status), zap.String("err", why), zap.Int64("freed", freed), zap.Int("volumes", volumes))
	s.Refresh(ctx)
	return after
}

// onDisk is every volume's bytes on disk.
func (s *Service) onDisk(ctx context.Context) (int64, error) {
	vols, err := s.sw.volStatus(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, v := range vols.all() {
		total += v.Size
	}
	return total, nil
}

// known reports whether bucket is one of core's or one SeaweedFS has; never
// SeaweedFS's own ("").
func (s *Service) known(ctx context.Context, bucket string) bool {
	if bucket == "" {
		return false
	}
	for _, b := range s.Status(ctx).Buckets {
		if b.Name == bucket {
			return true
		}
	}
	return false
}
