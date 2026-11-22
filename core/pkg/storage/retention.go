package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// Retention cleanups (docs: Storage, Retention cleanups) are of recordings
// alone: minion files and artifacts stay until their operation is deleted.
// A cleanup takes the recordings that ended at least an age ago, but those
// of tasks marked important: their files are deleted, their records kept,
// marked cleaned up (pkg/recordings, cleanup.go). Admins preview one (what
// it would take, by operation) and run it, for every operation or one; the
// retention policy, one for every operation, runs one each day once an
// admin turned it on. Each run is a storage action (cleanup): one at a
// time, in the background for as long as it takes, a batch at a time,
// what it found to take as it began and what it took kept as it goes; an
// admin's cancel stops it before its next recording. The bytes come back
// to the disk once reclaimed (Reclaim), or once SeaweedFS compacts their
// volumes on its own.

// Recordings are the recordings cleanups take (recordings.Service).
type Recordings interface {
	ToClean(ctx context.Context, before time.Time, operationID *uuid.UUID, limit int64) ([]models.Recording, error)
	ToCleanByOperation(ctx context.Context, before time.Time) ([]models.RecordingsToClean, error)
	CleanUp(ctx context.Context, recs []models.Recording, cleanup uuid.UUID) (int64, int64, error)
}

// MaxRetentionDays bounds a cleanup's age.
const MaxRetentionDays = 3650

// Errors the cleanups refuse with.
var (
	ErrBadAge           = fmt.Errorf("the age is a number of days, 1 to %d", MaxRetentionDays)
	ErrUnknownOperation = errors.New("no such operation")
	ErrNoRetention      = errors.New("core runs no retention cleanups")
)

// WithRetention gives the service the retention policy, and the recordings
// cleanups take; before Start.
func (s *Service) WithRetention(policy repository.IStorageRetentionRepository, recs Recordings) *Service {
	s.policy, s.recs = policy, recs
	return s
}

// Retention is the retention policy, and its runs: the last, and when the
// next is due (nil while it is off).
type Retention struct {
	Policy  models.StorageRetention
	LastRun *models.StorageAction
	NextRun *time.Time
}

// Retention is the policy as it is now.
func (s *Service) Retention(ctx context.Context) (Retention, error) {
	if s.policy == nil {
		return Retention{}, ErrNoRetention
	}
	p, err := s.policy.Get(ctx)
	if err != nil {
		return Retention{}, err
	}
	last, err := s.actions.Last(ctx, models.StorageActionCleanup, models.RetentionActor)
	if err != nil {
		return Retention{}, err
	}
	r := Retention{Policy: p, LastRun: last}
	if p.Enabled {
		next := s.nextRun(last, s.now())
		r.NextRun = &next
	}
	return r, nil
}

// SetRetention turns the policy on or off, at an age in days. Turned on,
// it is looked at now: its first cleanup starts, unless one ran within the
// day.
func (s *Service) SetRetention(ctx context.Context, actor models.ModuleActor, enabled bool, days int) (Retention, error) {
	if s.policy == nil {
		return Retention{}, ErrNoRetention
	}
	if days < 1 || days > MaxRetentionDays {
		return Retention{}, ErrBadAge
	}
	at := s.now()
	if err := s.policy.Set(ctx, models.StorageRetention{Enabled: enabled, OlderThanDays: days, UpdatedAt: &at, UpdatedBy: &actor}); err != nil {
		return Retention{}, err
	}
	s.logger.Info("storage: the retention policy was set", zap.Bool("enabled", enabled), zap.Int("older_than_days", days),
		zap.String("by", actor.Name))
	if enabled {
		select {
		case s.poke <- struct{}{}:
		default:
		}
	}
	return s.Retention(ctx)
}

// nextRun is when the policy's cleanup is due after last (none: now): a
// day after it began, or an hour after it ended when it failed, the sooner.
func (s *Service) nextRun(last *models.StorageAction, now time.Time) time.Time {
	if last == nil {
		return now
	}
	next := last.StartedAt.Add(s.cfg.RetentionEvery)
	if last.Status == models.StorageActionFailed && last.FinishedAt != nil {
		if retry := last.FinishedAt.Add(s.cfg.RetentionRetry); retry.Before(next) {
			next = retry
		}
	}
	return next
}

// retentionLoop runs the policy's cleanup when it is due: looked at now,
// every RetentionCheckEvery, and when an admin turns the policy on.
func (s *Service) retentionLoop() {
	defer s.wg.Done()
	t := time.NewTicker(s.cfg.RetentionCheckEvery)
	defer t.Stop()
	for {
		s.runPolicy(s.base)
		select {
		case <-s.base.Done():
			return
		case <-t.C:
		case <-s.poke:
		}
	}
}

// runPolicy starts the policy's cleanup, of every operation, when it is on
// and due. Another cleanup running, it waits for the next look.
func (s *Service) runPolicy(ctx context.Context) {
	p, err := s.policy.Get(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("storage: read the retention policy", zap.Error(err))
		}
		return
	}
	if !p.Enabled {
		return
	}
	last, err := s.actions.Last(ctx, models.StorageActionCleanup, models.RetentionActor)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("storage: read the retention policy's last cleanup", zap.Error(err))
		}
		return
	}
	if now := s.now(); (last != nil && last.Status == models.StorageActionRunning) || now.Before(s.nextRun(last, now)) {
		return
	}
	if _, err := s.startCleanup(ctx, models.RetentionActor, p.OlderThanDays, nil); err != nil && !errors.Is(err, ErrRunning) {
		s.logger.Warn("storage: start the retention policy's cleanup", zap.Error(err))
	}
}

// CleanupPreview is what a cleanup would take now: the recordings that
// ended before Before (OlderThanDays ago), but those of tasks marked
// important; in all, and a page of them by operation.
type CleanupPreview struct {
	OlderThanDays     int
	Before            time.Time
	Recordings, Bytes int64
	// Operations is the page, the most bytes first; Total how many
	// operations have any, More whether more follow the page.
	Operations []CleanupOperation
	Total      int
	More       bool
}

// CleanupOperation is what a cleanup would take of an operation's
// recordings. OperationName is "" for an operation gone.
type CleanupOperation struct {
	OperationID       uuid.UUID
	OperationName     string
	Recordings, Bytes int64
}

// CleanupCursor is o's place in a preview's list.
func CleanupCursor(o CleanupOperation) string {
	return encodeCursor(consumerCursor{Bytes: o.Bytes, Name: o.OperationName, Key: o.OperationID.String()})
}

// compareCleanups orders a preview's operations: the most bytes first,
// then by name and id.
func compareCleanups(a, b CleanupOperation) int {
	if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
		return c
	}
	if c := strings.Compare(a.OperationName, b.OperationName); c != 0 {
		return c
	}
	return strings.Compare(a.OperationID.String(), b.OperationID.String())
}

// CleanupPreview is what a cleanup at an age in days would take now: at
// most first operations after the cursor after (none: from the start).
func (s *Service) CleanupPreview(ctx context.Context, days int, after string, first int) (CleanupPreview, error) {
	if s.recs == nil {
		return CleanupPreview{}, ErrNoRetention
	}
	if days < 1 || days > MaxRetentionDays {
		return CleanupPreview{}, ErrBadAge
	}
	from, err := parseCursor(after)
	if err != nil {
		return CleanupPreview{}, err
	}
	before := s.before(days)
	groups, err := s.recs.ToCleanByOperation(ctx, before)
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("the recordings a cleanup takes: %w", err)
	}
	names := s.operationNames(ctx)
	out := CleanupPreview{OlderThanDays: days, Before: before, Operations: []CleanupOperation{}, Total: len(groups)}
	ops := make([]CleanupOperation, 0, len(groups))
	for _, g := range groups {
		out.Recordings, out.Bytes = out.Recordings+g.Recordings, out.Bytes+g.Bytes
		ops = append(ops, CleanupOperation{OperationID: g.OperationID, OperationName: names[g.OperationID], Recordings: g.Recordings,
			Bytes: g.Bytes})
	}
	slices.SortFunc(ops, compareCleanups)
	for _, o := range ops {
		if from != nil && !follows(o.Bytes, o.OperationName, o.OperationID.String(), *from) {
			continue
		}
		if len(out.Operations) == first {
			out.More = true
			break
		}
		out.Operations = append(out.Operations, o)
	}
	return out, nil
}

// before is the time the recordings a cleanup at an age in days takes
// ended before.
func (s *Service) before(days int) time.Time {
	return s.now().Add(-time.Duration(days) * 24 * time.Hour)
}

// CleanUp starts a cleanup of the recordings that ended at least an age in
// days ago, of one operation (nil: of every one), but those of tasks marked
// important: it answers with the action running, which takes them in the
// background.
func (s *Service) CleanUp(ctx context.Context, actor models.ModuleActor, days int, operationID *uuid.UUID) (*models.StorageAction, error) {
	if s.recs == nil {
		return nil, ErrNoRetention
	}
	if days < 1 || days > MaxRetentionDays {
		return nil, ErrBadAge
	}
	var op *models.Operation
	if operationID != nil {
		o, err := s.ops.FindByID(ctx, *operationID)
		if repository.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownOperation, *operationID)
		}
		if err != nil {
			return nil, err
		}
		op = &o
	}
	return s.startCleanup(ctx, actor, days, op)
}

// startCleanup keeps a cleanup's action, running, with what it finds to
// take now, and runs it.
func (s *Service) startCleanup(ctx context.Context, actor models.ModuleActor, days int, op *models.Operation) (*models.StorageAction, error) {
	now := s.now()
	before := s.before(days)
	a := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionCleanup, Bucket: s.cfg.Buckets[models.StoragePurposeRecordings],
		Actor: actor, Status: models.StorageActionRunning, StartedAt: now, OlderThanDays: days, Before: &before}
	if op != nil {
		a.OperationID, a.OperationName = &op.OperationID, op.Name
	}
	groups, err := s.recs.ToCleanByOperation(ctx, before)
	if err != nil {
		return nil, fmt.Errorf("the recordings a cleanup takes: %w", err)
	}
	for _, g := range groups {
		if op == nil || g.OperationID == op.OperationID {
			a.PlannedRecordings, a.PlannedBytes = a.PlannedRecordings+g.Recordings, a.PlannedBytes+g.Bytes
		}
	}
	if err := s.actions.Insert(ctx, a); err != nil {
		if errors.Is(err, repository.ErrStorageActionRunning) {
			return nil, fmt.Errorf("a cleanup: %w", ErrRunning)
		}
		return nil, err
	}
	s.logger.Info("storage: cleaning up recordings", zap.String("action_id", a.ActionID.String()), zap.Int("older_than_days", days),
		zap.String("operation", a.OperationName), zap.Int64("planned", a.PlannedRecordings), zap.String("by", actor.Name))
	out := *a
	stop := s.track(a.ActionID)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.untrack(a.ActionID)
		status, why := models.StorageActionSucceeded, ""
		err := s.cleanUp(stop, a)
		switch {
		case stop.Err() != nil || s.stopping(stop, a.ActionID):
			status, why = s.stoppedStatus()
		case err != nil:
			status, why = models.StorageActionFailed, err.Error()
		}
		s.step(a, "")
		s.finish(a, status, why, 0, 0)
	}()
	return &out, nil
}

// cleanUp takes a's recordings a batch at a time, keeping what it took as
// it goes, until none is left or it is to stop (stop: before its next
// recording); why it stopped short.
func (s *Service) cleanUp(stop context.Context, a *models.StorageAction) error {
	for {
		if s.stopping(stop, a.ActionID) {
			return nil
		}
		recs, err := s.recs.ToClean(stop, *a.Before, a.OperationID, int64(s.cfg.CleanupBatch))
		if err != nil {
			return fmt.Errorf("find the recordings to take: %w", err)
		}
		if len(recs) == 0 {
			return nil
		}
		s.step(a, fmt.Sprintf("taking recordings: %d taken of about %d", a.Recordings, a.PlannedRecordings))
		n, b, err := s.recs.CleanUp(stop, recs, a.ActionID)
		a.Recordings, a.Bytes = a.Recordings+n, a.Bytes+b
		s.step(a, fmt.Sprintf("taking recordings: %d taken of about %d", a.Recordings, a.PlannedRecordings))
		if err != nil {
			return err
		}
		if n == 0 {
			// The batch went to others meanwhile (a delete, the encoder):
			// asking again would bring the same.
			return nil
		}
	}
}
