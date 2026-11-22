// Package storage is the Storage page's: the object store core keeps its
// files in (SeaweedFS), as admins see and maintain it. Core reads SeaweedFS's
// master (when SEAWEEDFS_MASTER_URL is set: its disk, volume slots and
// buckets) and sums its own records of what it keeps in each bucket, once a
// minute, into a snapshot it serves; it warns of what would make writes
// fail (no free slot, a bucket with no writable volume, a full disk), and
// runs two maintenance actions: reclaiming deleted files' space (core
// compacts the volumes one at a time, reclaim.go) and adding a volume to a
// bucket. It runs retention cleanups of recordings too, an admin's or the
// retention policy's each day (retention.go). Each action is kept
// (models.StorageAction) with who ran it and how it went; a reclaim and a
// cleanup run for as long as they take, keep their progress as they go, and
// stop when an admin cancels them.
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
	// core reads its own records alone, and runs no action. MasterGRPC is
	// its gRPC address (host:port), a reclaim's: empty, MasterURL's host on
	// its port plus 10000.
	MasterURL  string
	MasterGRPC string
	// Buckets are core's buckets, by purpose (models.StoragePurpose...).
	Buckets map[string]string
	// Interval is how often core reads the store (a minute); CallTimeout a
	// call's to SeaweedFS (10 s), but a compaction's, which takes as long as
	// it does.
	Interval    time.Duration
	CallTimeout time.Duration
	// ReclaimThreshold is the share of a volume deleted files must hold for a
	// reclaim to compact it (0.1). SeaweedFS compacts past its own (0.3) on
	// its own. A volume the master did not compact, still past it, is asked
	// again ReclaimRetryWait later (10 s), ReclaimTries times in all (3);
	// ReclaimFirstTries (30: five minutes) until the reclaim's first volume
	// settles, as SeaweedFS's own vacuum, paused, may still be ending the
	// volume it was on (its pass holds the master's one vacuum lock).
	ReclaimThreshold  float64
	ReclaimRetryWait  time.Duration
	ReclaimTries      int
	ReclaimFirstTries int
	// FewSlots warns when fewer volume slots are free (3); DiskLowPercent
	// when a disk has less free (10); ReclaimableRatio and ReclaimableMin
	// tell of a bucket whose deleted files hold that share of it, and that
	// much (0.3, 64 MiB).
	FewSlots         int
	DiskLowPercent   float64
	ReclaimableRatio float64
	ReclaimableMin   int64

	// RetentionCheckEvery is how often core looks whether the retention
	// policy's cleanup is due (10 minutes): a day after the last began
	// (RetentionEvery), sooner after one that failed, an hour after it ended
	// (RetentionRetry). CleanupBatch is how many recordings a cleanup reads
	// at once (50).
	RetentionCheckEvery time.Duration
	RetentionEvery      time.Duration
	RetentionRetry      time.Duration
	CleanupBatch        int
}

// DefaultConfig is Config's defaults, with no master and no buckets.
func DefaultConfig() Config {
	return Config{Interval: time.Minute, CallTimeout: 10 * time.Second, ReclaimThreshold: 0.1, ReclaimRetryWait: 10 * time.Second,
		ReclaimTries: 3, ReclaimFirstTries: 30, FewSlots: 3, DiskLowPercent: 10, ReclaimableRatio: 0.3, ReclaimableMin: 64 << 20,
		RetentionCheckEvery: 10 * time.Minute, RetentionEvery: 24 * time.Hour, RetentionRetry: time.Hour, CleanupBatch: 50}
}

// Errors the actions refuse with.
var (
	ErrNotConfigured  = errors.New("core is not set to reach SeaweedFS's master (SEAWEEDFS_MASTER_URL)")
	ErrUnknownBucket  = errors.New("no such bucket")
	ErrRunning        = errors.New("it is running already")
	ErrUnknownAction  = errors.New("no such action")
	ErrNotRunning     = errors.New("it is not running: it ended")
	ErrNotCancellable = errors.New("it ends at once: there is nothing to stop")
)

// Operations names operations, for the breakdown and the cleanups.
type Operations interface {
	FindAll(ctx context.Context, search string, offset, limit int64, memberID *uuid.UUID) ([]models.Operation, error)
	FindByID(ctx context.Context, id uuid.UUID) (models.Operation, error)
}

// Service serves the store's snapshot, and runs its actions.
type Service struct {
	cfg Config
	sw  *seaweed // nil: no master
	// rpc is the master over gRPC, a reclaim's: nil with no master, or
	// rpcErr says why it could not be made.
	rpc     *masterRPC
	rpcErr  error
	usage   repository.IStorageUsageRepository
	actions repository.IStorageActionRepository
	ops     Operations
	logger  *zap.Logger
	now     func() time.Time

	// policy and recs are the retention cleanups' (WithRetention): nil,
	// there are none. poke has the policy looked at now.
	policy repository.IStorageRetentionRepository
	recs   Recordings
	poke   chan struct{}

	// base is the service's context: Stop ends it, and a reclaim or a
	// cleanup running with it. runs are each running one's stop: Cancel
	// ends it.
	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	runMu  sync.Mutex
	runs   map[uuid.UUID]context.CancelFunc

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
	if cfg.ReclaimThreshold <= 0 {
		cfg.ReclaimThreshold = def.ReclaimThreshold
	}
	if cfg.ReclaimRetryWait <= 0 {
		cfg.ReclaimRetryWait = def.ReclaimRetryWait
	}
	if cfg.ReclaimTries <= 0 {
		cfg.ReclaimTries = def.ReclaimTries
	}
	if cfg.ReclaimFirstTries <= 0 {
		cfg.ReclaimFirstTries = def.ReclaimFirstTries
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
	if cfg.RetentionCheckEvery <= 0 {
		cfg.RetentionCheckEvery = def.RetentionCheckEvery
	}
	if cfg.RetentionEvery <= 0 {
		cfg.RetentionEvery = def.RetentionEvery
	}
	if cfg.RetentionRetry <= 0 {
		cfg.RetentionRetry = def.RetentionRetry
	}
	if cfg.CleanupBatch <= 0 {
		cfg.CleanupBatch = def.CleanupBatch
	}
	s := &Service{cfg: cfg, usage: usage, actions: actions, ops: ops, logger: logger, now: time.Now, poke: make(chan struct{}, 1),
		runs: map[uuid.UUID]context.CancelFunc{}}
	if master := strings.TrimRight(strings.TrimSpace(cfg.MasterURL), "/"); master != "" {
		s.sw = &seaweed{master: master, http: &http.Client{}}
		addr, err := masterGRPCAddr(master, strings.TrimSpace(cfg.MasterGRPC))
		if err == nil {
			s.rpc, err = newMasterRPC(addr)
		}
		if err != nil {
			s.rpcErr = err
			logger.Warn("storage: SeaweedFS's master over gRPC: no reclaim runs", zap.Error(err))
		}
	}
	s.base, s.cancel = context.WithCancel(context.Background())
	return s
}

// Start fails the actions a core that stopped left running, and resumes
// SeaweedFS's own vacuum where a reclaim left it paused; then reads the store
// now and every Interval, and runs the retention policy when due.
func (s *Service) Start() {
	if ended, err := s.actions.EndRunning(s.base, coreStopped, s.now()); err != nil {
		s.logger.Warn("storage: end the actions left running", zap.Error(err))
	} else if len(ended) > 0 {
		s.logger.Info("storage: ended the actions left running", zap.Int("actions", len(ended)))
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.resumeLeftVacuum()
	}()
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
	if s.policy != nil && s.recs != nil {
		s.wg.Add(1)
		go s.retentionLoop()
	}
}

// Stop ends the reads, and a reclaim or a cleanup running, and waits for
// them.
func (s *Service) Stop() {
	s.cancel()
	s.wg.Wait()
	if s.rpc != nil {
		s.rpc.close()
	}
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
	if len(usage) == 0 {
		return map[uuid.UUID]string{models.PublicOperationID: models.PublicOperationName}
	}
	return s.operationNames(ctx)
}

// operationNames are every operation's names.
func (s *Service) operationNames(ctx context.Context) map[uuid.UUID]string {
	names := map[uuid.UUID]string{models.PublicOperationID: models.PublicOperationName}
	if s.ops == nil {
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
