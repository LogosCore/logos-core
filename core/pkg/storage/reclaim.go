package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// A reclaim gives the disk back the space deleted files hold. Core compacts
// the volumes itself, one at a time, through SeaweedFS's master over gRPC
// (masterrpc.go): every volume whose deleted files hold ReclaimThreshold of
// it or more, as the master last heard of it, the most deleted first (the
// most space first, should it stop). Meanwhile SeaweedFS's own vacuum is
// paused: the master runs one vacuum at a time, and would leave core's
// calls doing nothing. Each volume is read on its volume server before and
// after (its /status, as it is now): compacted when its CompactRevision
// moved, with the bytes it freed; skipped when it needs none, or is
// read-only; failed, saying why, when the master did not compact it,
// ReclaimTries times asked (ReclaimFirstTries until a first volume
// settles: SeaweedFS's own vacuum, paused, ends the volume it is on
// first, holding the master's lock meanwhile). A reclaim runs for as long
// as it takes, keeping each volume as it goes; an admin's cancel stops it
// before its next volume, the one under way ending as SeaweedFS ends it.

// coreStopped is why a run that core's stop cut short ends failed.
const coreStopped = "core stopped before it ended"

// Reclaim starts compacting the volumes deleted files hold ReclaimThreshold
// of or more: it answers with the action running, which goes on in the
// background (Cancel stops it).
func (s *Service) Reclaim(ctx context.Context, actor models.ModuleActor) (*models.StorageAction, error) {
	if s.sw == nil {
		return nil, ErrNotConfigured
	}
	now := s.now()
	a := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionReclaim, Actor: actor,
		Status: models.StorageActionRunning, StartedAt: now, Step: "reading the volumes", StepAt: &now}
	if err := s.actions.Insert(ctx, a); err != nil {
		if errors.Is(err, repository.ErrStorageActionRunning) {
			return nil, fmt.Errorf("a reclaim: %w", ErrRunning)
		}
		return nil, err
	}
	s.logger.Info("storage: reclaiming deleted files' space", zap.String("action_id", a.ActionID.String()), zap.String("by", actor.Name))
	out := *a
	stop := s.track(a.ActionID)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.untrack(a.ActionID)
		status, why := s.reclaim(stop, a)
		s.finish(a, status, why, a.Freed, 0)
	}()
	return &out, nil
}

// Action is one action as it is now: ErrUnknownAction for none.
func (s *Service) Action(ctx context.Context, id uuid.UUID) (*models.StorageAction, error) {
	a, err := s.actions.Get(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrUnknownAction
	}
	return a, err
}

// Cancel asks a running reclaim or cleanup to stop: it stops before its next
// step (a volume, a recording), what it did kept, and ends cancelled.
// ErrUnknownAction for none, ErrNotRunning for one that ended,
// ErrNotCancellable for a grow, which ends at once.
func (s *Service) Cancel(ctx context.Context, actor models.ModuleActor, id uuid.UUID) (*models.StorageAction, error) {
	a, err := s.actions.RequestCancel(ctx, id, actor, s.now())
	switch {
	case repository.IsNotFound(err):
		return nil, ErrUnknownAction
	case errors.Is(err, repository.ErrStorageActionNotRunning):
		return nil, ErrNotRunning
	case errors.Is(err, repository.ErrStorageActionNotCancellable):
		return nil, ErrNotCancellable
	case err != nil:
		return nil, err
	}
	s.runMu.Lock()
	stop := s.runs[id]
	s.runMu.Unlock()
	if stop != nil {
		stop()
	}
	s.logger.Info("storage: an action was asked to stop", zap.String("action", a.Action), zap.String("action_id", id.String()),
		zap.String("by", actor.Name))
	return a, nil
}

// track keeps a run's stop, which Cancel ends; core's stop ends it too.
func (s *Service) track(id uuid.UUID) context.Context {
	ctx, cancel := context.WithCancel(s.base)
	s.runMu.Lock()
	s.runs[id] = cancel
	s.runMu.Unlock()
	return ctx
}

func (s *Service) untrack(id uuid.UUID) {
	s.runMu.Lock()
	cancel := s.runs[id]
	delete(s.runs, id)
	s.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// stopping reports whether a run stops before its next step: its stop ended
// (Cancel here, or core's stop), or its record says an admin asked (through
// another core).
func (s *Service) stopping(stop context.Context, id uuid.UUID) bool {
	if stop.Err() != nil {
		return true
	}
	a, err := s.actions.Get(stop, id)
	return err == nil && a.CancelRequestedAt != nil
}

// stoppedStatus is how a run cut short ends: failed when core stopped,
// cancelled when an admin asked.
func (s *Service) stoppedStatus() (string, string) {
	if s.base.Err() != nil {
		return models.StorageActionFailed, coreStopped
	}
	return models.StorageActionCancelled, ""
}

// reclaim runs a: its plan, SeaweedFS's own vacuum paused around its
// volumes. It answers how it ended, and why.
func (s *Service) reclaim(stop context.Context, a *models.StorageAction) (string, string) {
	plan, err := s.planReclaim(s.base)
	if err != nil {
		if s.base.Err() != nil {
			return models.StorageActionFailed, coreStopped
		}
		return models.StorageActionFailed, fmt.Sprintf("read the volumes: %v", err)
	}
	a.ReclaimVolumes = plan
	if len(plan) == 0 {
		s.step(a, "")
		return models.StorageActionSucceeded, ""
	}
	if s.rpc == nil {
		s.step(a, "")
		return models.StorageActionFailed, s.rpcErr.Error()
	}
	if s.stopping(stop, a.ActionID) {
		s.stopRest(a, 0)
		return s.stoppedStatus()
	}
	s.step(a, "pausing SeaweedFS's own vacuum")
	if err := s.callRPC(s.rpc.disableVacuum); err != nil {
		s.stopRest(a, 0)
		return models.StorageActionFailed, err.Error()
	}
	s.setVacuumPaused(a, true)
	status, why := s.compactAll(stop, a)
	s.step(a, "resuming SeaweedFS's own vacuum")
	if err := s.resumeVacuum(); err != nil {
		note := fmt.Sprintf("SeaweedFS's own vacuum is still paused (%v): core resumes it as it starts again, SeaweedFS's master as it restarts", err)
		if why != "" {
			note = why + "; " + note
		}
		if status == models.StorageActionSucceeded {
			status = models.StorageActionFailed
		}
		return status, note
	}
	s.setVacuumPaused(a, false)
	return status, why
}

// planReclaim is what a reclaim compacts: each volume not read-only whose
// deleted files hold ReclaimThreshold of it or more, the most deleted
// first. A volume with copies is read on its first server by name.
func (s *Service) planReclaim(ctx context.Context) ([]models.StorageReclaimVolume, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	defer cancel()
	vs, err := s.sw.volStatus(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uint32]models.StorageReclaimVolume{}
	for _, racks := range vs.Volumes.DataCenters {
		for _, nodes := range racks {
			for node, vols := range nodes {
				for _, v := range vols {
					if v.ReadOnly || !s.overThreshold(v) {
						continue
					}
					if p, ok := byID[v.ID]; ok && p.Server <= node {
						continue
					}
					byID[v.ID] = models.StorageReclaimVolume{ID: v.ID, Bucket: v.Collection, Server: node, Size: v.Size,
						Deleted: v.DeletedByteCount, State: models.ReclaimVolumeWaiting}
				}
			}
		}
	}
	plan := slices.SortedFunc(maps.Values(byID), func(a, b models.StorageReclaimVolume) int {
		if c := cmp.Compare(b.Deleted, a.Deleted); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if plan == nil {
		plan = []models.StorageReclaimVolume{}
	}
	return plan, nil
}

// overThreshold reports whether deleted files hold ReclaimThreshold of v or
// more.
func (s *Service) overThreshold(v volume) bool {
	return v.Size > 0 && v.DeletedByteCount > 0 && float64(v.DeletedByteCount) >= s.cfg.ReclaimThreshold*float64(v.Size)
}

// compactAll compacts a's volumes in their order, keeping each as it goes,
// until all are done or it is to stop.
func (s *Service) compactAll(stop context.Context, a *models.StorageAction) (string, string) {
	vols := a.ReclaimVolumes
	failed := 0
	// settled: a volume was compacted, or failed, asked over and over:
	// SeaweedFS's own vacuum is surely over.
	settled := false
	for i := range vols {
		if s.stopping(stop, a.ActionID) {
			s.stopRest(a, i)
			return s.stoppedStatus()
		}
		v := &vols[i]
		at := s.now()
		v.State, v.StartedAt = models.ReclaimVolumeCompacting, &at
		s.step(a, fmt.Sprintf("compacting volume %d of %s (%d of %d)", v.ID, bucketText(v.Bucket), i+1, len(vols)))
		tries := s.cfg.ReclaimFirstTries
		if settled {
			tries = s.cfg.ReclaimTries
		}
		state, freed, msg, asked := s.compactVolume(a, v, i, tries)
		if asked && (state == models.ReclaimVolumeCompacted || state == models.ReclaimVolumeFailed) {
			settled = true
		}
		end := s.now()
		v.State, v.Freed, v.Msg, v.FinishedAt = state, freed, msg, &end
		a.Freed += freed
		if state == models.ReclaimVolumeFailed {
			failed++
		}
		s.save(a)
	}
	if failed > 0 {
		return models.StorageActionFailed, fmt.Sprintf("%d of %d volumes were not compacted: each says why", failed, len(vols))
	}
	return models.StorageActionSucceeded, ""
}

// stopRest marks the volumes from i on that the reclaim did not reach.
func (s *Service) stopRest(a *models.StorageAction, i int) {
	for j := i; j < len(a.ReclaimVolumes); j++ {
		if v := &a.ReclaimVolumes[j]; v.State == models.ReclaimVolumeWaiting {
			v.State, v.Msg = models.ReclaimVolumeStopped, "the reclaim stopped before it"
		}
	}
	s.step(a, "")
}

// compactVolume has the master compact v, the i-th of a's volumes, asking
// it at most tries times: how it went, the bytes it freed, why it was
// skipped, failed or stopped, and whether the master was asked at all. Its
// calls end only with core's stop: a compaction takes as long as it does.
func (s *Service) compactVolume(a *models.StorageAction, v *models.StorageReclaimVolume, i, tries int) (string, int64, string, bool) {
	before, err := s.liveVolume(v)
	if err != nil {
		return models.ReclaimVolumeFailed, 0, fmt.Sprintf("its volume server did not answer: %v", err), false
	}
	switch {
	case before.ReadOnly:
		return models.ReclaimVolumeSkipped, 0, "it is read-only: SeaweedFS compacts no read-only volume", false
	case !s.overThreshold(before):
		return models.ReclaimVolumeSkipped, 0, "deleted files hold less than a tenth of it now", false
	}
	for try := 1; ; try++ {
		if err := s.rpc.vacuumVolume(s.base, s.cfg.ReclaimThreshold, v.ID, v.Bucket); err != nil {
			if s.base.Err() != nil {
				return models.ReclaimVolumeStopped, 0, "core stopped as SeaweedFS compacted it: SeaweedFS ends it on its own", true
			}
			return models.ReclaimVolumeFailed, 0, err.Error(), true
		}
		after, err := s.liveVolume(v)
		if err != nil {
			return models.ReclaimVolumeFailed, 0, fmt.Sprintf("its volume server did not answer after SeaweedFS's master did: %v", err), true
		}
		if after.CompactRevision != before.CompactRevision {
			return models.ReclaimVolumeCompacted, max(before.Size-after.Size, 0), "", true
		}
		if !s.overThreshold(after) {
			return models.ReclaimVolumeSkipped, 0, "deleted files hold less than a tenth of it now", true
		}
		if try >= tries {
			return models.ReclaimVolumeFailed, 0, fmt.Sprintf("SeaweedFS did not compact it, asked %d times: its master skips a volume short of "+
				"its copies, and a volume server fails a compaction it has no room for (one needs room for a copy of the volume). "+
				"SeaweedFS's master's log says which", try), true
		}
		s.step(a, fmt.Sprintf("waiting to compact volume %d of %s (%d of %d): SeaweedFS's master did not, its own vacuum ending the "+
			"volume it is on maybe; asking again (%d of %d)", v.ID, bucketText(v.Bucket), i+1, len(a.ReclaimVolumes), try+1, tries))
		select {
		case <-s.base.Done():
			return models.ReclaimVolumeStopped, 0, "core stopped before SeaweedFS compacted it", true
		case <-time.After(s.cfg.ReclaimRetryWait):
		}
	}
}

// liveVolume is v as its volume server has it now.
func (s *Service) liveVolume(v *models.StorageReclaimVolume) (volume, error) {
	ctx, cancel := context.WithTimeout(s.base, s.cfg.CallTimeout)
	defer cancel()
	return s.sw.nodeVolume(ctx, v.Server, v.ID)
}

// callRPC makes a quick call to the master (pausing or resuming its
// vacuum), bounded by CallTimeout.
func (s *Service) callRPC(call func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(s.base, s.cfg.CallTimeout)
	defer cancel()
	return call(ctx)
}

// resumeVacuum resumes SeaweedFS's own vacuum, as core stops too: three
// tries, a second apart.
func (s *Service) resumeVacuum() error {
	var err error
	for try := 1; try <= 3; try++ {
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CallTimeout)
		err = s.rpc.enableVacuum(ctx)
		cancel()
		if err == nil {
			return nil
		}
		if try < 3 {
			time.Sleep(time.Second)
		}
	}
	return err
}

// resumeLeftVacuum resumes SeaweedFS's own vacuum where a reclaim left it
// paused: its core stopped first, or the master did not answer it.
func (s *Service) resumeLeftVacuum() {
	paused, err := s.actions.VacuumPaused(s.base)
	if err != nil {
		s.logger.Warn("storage: read the reclaims that paused SeaweedFS's own vacuum", zap.Error(err))
		return
	}
	if len(paused) == 0 || s.rpc == nil {
		return
	}
	if err := s.resumeVacuum(); err != nil {
		s.logger.Warn("storage: SeaweedFS's own vacuum stays paused: its master did not answer", zap.Error(err))
		return
	}
	for _, a := range paused {
		if err := s.actions.SetVacuumPaused(s.base, a.ActionID, false); err != nil {
			s.logger.Warn("storage: keep that SeaweedFS's own vacuum was resumed", zap.String("action_id", a.ActionID.String()), zap.Error(err))
		}
	}
	s.logger.Info("storage: resumed SeaweedFS's own vacuum, which a reclaim had paused", zap.Int("reclaims", len(paused)))
}

// setVacuumPaused keeps on a that it paused SeaweedFS's own vacuum, or
// resumed it.
func (s *Service) setVacuumPaused(a *models.StorageAction, paused bool) {
	a.VacuumPaused = paused
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CallTimeout)
	defer cancel()
	if err := s.actions.SetVacuumPaused(ctx, a.ActionID, paused); err != nil {
		s.logger.Warn("storage: keep whether SeaweedFS's own vacuum is paused", zap.String("action_id", a.ActionID.String()),
			zap.Bool("paused", paused), zap.Error(err))
	}
}

// step keeps what a does now ("": nothing more), with its progress.
func (s *Service) step(a *models.StorageAction, step string) {
	now := s.now()
	a.Step, a.StepAt = step, &now
	if step == "" {
		a.StepAt = nil
	}
	s.save(a)
}

// save keeps a's progress, as core stops too.
func (s *Service) save(a *models.StorageAction) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CallTimeout)
	defer cancel()
	p := models.StorageActionProgress{Step: a.Step, StepAt: a.StepAt, Freed: a.Freed, Recordings: a.Recordings, Bytes: a.Bytes,
		ReclaimVolumes: slices.Clone(a.ReclaimVolumes)}
	if err := s.actions.Progress(ctx, a.ActionID, p); err != nil {
		s.logger.Warn("storage: keep how far an action came", zap.String("action_id", a.ActionID.String()), zap.Error(err))
	}
}
