package builds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// maxOffersPerTick bounds the queued builds one tick offers, so a backlog
// cannot starve the rest of the tick.
const maxOffersPerTick = 20

// verifyGiveUp is how long past the deadline core keeps trying to read an
// artifact it could not check (the store not answering), before failing.
const verifyGiveUp = time.Hour

// Tick runs one scheduler pass: it fails builds that ran out of time or lost
// their factory instance, resumes artifact checks, and offers queued builds
// to factories. The loop calls it; tests call it directly.
func (s *Service) Tick(ctx context.Context) {
	s.expire(ctx)
	for range maxOffersPerTick {
		b, err := s.repos.Builds.ClaimQueued(ctx, s.now(), s.now().Add(s.cfg.Lease))
		if err != nil {
			s.logger.Warn("claim a queued build", zap.Error(err))
			return
		}
		if b == nil {
			return
		}
		s.offer(ctx, b)
	}
}

// expire fails what core no longer waits for and restarts interrupted
// artifact checks.
func (s *Service) expire(ctx context.Context) {
	unfinished, err := s.repos.Builds.ListUnfinished(ctx)
	if err != nil {
		s.logger.Warn("list unfinished builds", zap.Error(err))
		return
	}
	if len(unfinished) == 0 {
		return
	}
	active, err := s.registry.ListActive(ctx)
	if err != nil {
		s.logger.Warn("list registered modules", zap.Error(err))
		return
	}
	live := map[string]bool{}
	for _, m := range active {
		live[m.Instance] = true
	}
	now := s.now()
	for i := range unfinished {
		b := &unfinished[i]
		switch {
		case b.State == models.BuildStateBuilding && b.ResultReceived:
			s.verifyAsync(b.BuildID)
		case now.After(b.Deadline):
			msg := "the build ran out of time"
			if b.State == models.BuildStateQueued {
				msg = "no factory instance took the build in time"
			}
			s.fail(ctx, b, models.BuildErrorTimeout, msg)
		case b.State == models.BuildStateBuilding && !live[b.Instance]:
			s.fail(ctx, b, models.BuildErrorFactoryLost, fmt.Sprintf("factory instance %s stopped during the build", b.Instance))
		}
	}
}

// fail finishes an unfinished build as failed with core's own reason.
func (s *Service) fail(ctx context.Context, b *models.Build, code, msg string) {
	ok, err := s.repos.Builds.Finish(ctx, b.BuildID, []string{b.State}, repository.BuildOutcome{
		State: models.BuildStateFailed, Error: &models.BuildError{Code: code, Message: msg}, At: s.now(),
	})
	if err != nil {
		s.logger.Warn("fail a build", zap.String("build_id", b.BuildID.String()), zap.Error(err))
		return
	}
	if ok {
		s.logger.Info("build failed", zap.String("build_id", b.BuildID.String()), zap.String("code", code), zap.String("reason", msg))
	}
}

// offer offers a claimed queued build to the kind's instances in turn: the
// first to accept builds it, a busy or unreachable one passes it on. When
// none takes it, it stays queued for the next tick.
//
// The build is marked BUILDING on an instance before it is offered there: a
// factory starts, and reports, as soon as it accepts, which can be before its
// reply reaches core. One that does not take it puts the build back.
func (s *Service) offer(ctx context.Context, b *models.Build) {
	release := func() {
		if err := s.repos.Builds.ReleaseClaim(ctx, b.BuildID); err != nil {
			s.logger.Warn("release a build claim", zap.String("build_id", b.BuildID.String()), zap.Error(err))
		}
	}
	insts, err := s.instances(ctx, b.ModuleName, b.SchemaVersion)
	if err != nil {
		s.logger.Warn("offer a build", zap.String("build_id", b.BuildID.String()), zap.Error(err))
		release()
		return
	}
	if len(insts) == 0 {
		schema, err := s.schema(ctx, b.ModuleName)
		if err == nil && schema != nil && schema.Version != b.SchemaVersion {
			s.fail(ctx, b, models.BuildErrorSchemaChanged, fmt.Sprintf(
				"the %q factory now declares build schema version %d, not %d: build again", b.ModuleName, schema.Version, b.SchemaVersion))
			return
		}
		release() // its instances may be restarting
		return
	}

	deadline := s.now().Add(s.cfg.Timeout)
	req, err := s.request(ctx, b, deadline)
	if err != nil {
		s.logger.Warn("prepare a build request", zap.String("build_id", b.BuildID.String()), zap.Error(err))
		release()
		return
	}
	start := int(s.next.Add(1))
	for i := range insts {
		inst := insts[(start+i)%len(insts)]
		ok, err := s.repos.Builds.MarkBuilding(ctx, b.BuildID, inst.Instance, inst.Version, deadline, req.Artifact.MaxSize, s.now())
		if err != nil || !ok {
			return // cancelled meanwhile
		}
		if s.offerTo(ctx, b, inst, req) {
			return
		}
		// Not taken there: back in the queue with its queue deadline (b is
		// the build as claimed), unless the instance already reported a
		// result (its acceptance was lost on the way back).
		if ok, err := s.repos.Builds.Unoffer(ctx, b.BuildID, inst.Instance, b.Deadline, s.now().Add(s.cfg.Lease)); err != nil || !ok {
			return
		}
	}
	release()
}

// offerTo offers a build marked BUILDING on inst there. False when inst did
// not take it: busy, unreachable, or an answer that is not an acceptance. A
// refusal fails the build, and counts as taken (nothing more to do).
func (s *Service) offerTo(ctx context.Context, b *models.Build, inst models.Module, req protocol.BuildRequest) bool {
	rep, err := s.rpc.Call(ctx, inst.RPCQueue, protocol.TypeFactoryBuild, req, s.cfg.CallTimeout)
	if err != nil {
		s.logger.Debug("factory did not answer a build offer", zap.String("instance", inst.Instance), zap.Error(err))
		return false
	}
	if rep.Status != protocol.StatusOK {
		code, msg := protocol.CodeInternalError, "no reason given"
		if rep.Error != nil {
			code, msg = rep.Error.Code, rep.Error.Message
		}
		if code == protocol.CodeBusy {
			return false
		}
		b.State = models.BuildStateBuilding
		s.fail(ctx, b, models.BuildErrorRefused, fmt.Sprintf("factory instance %s refused the build: %s: %s", inst.Instance, code, msg))
		return true
	}
	var accepted protocol.BuildReply
	if json.Unmarshal(rep.Payload, &accepted) != nil || !accepted.Accepted || accepted.BuildID != b.BuildID.String() {
		return false
	}
	s.setPlan(ctx, b.BuildID, inst.Instance, accepted.Steps)
	s.logger.Info("build started", zap.String("build_id", b.BuildID.String()), zap.String("instance", inst.Instance),
		zap.Int("steps", len(accepted.Steps)))
	return true
}

// setPlan stores the plan an instance accepted a build with. Without a valid
// one the build is still built, and shows as a single step.
func (s *Service) setPlan(ctx context.Context, buildID uuid.UUID, instance string, steps []protocol.BuildStep) {
	if err := protocol.ValidateBuildSteps(steps); err != nil {
		s.logger.Warn("a factory accepted a build without a valid plan", zap.String("build_id", buildID.String()),
			zap.String("instance", instance), zap.Error(err))
		return
	}
	plan := make([]models.BuildPlanStep, len(steps))
	for i, st := range steps {
		plan[i] = models.BuildPlanStep{ID: st.ID, Label: st.Label}
	}
	if _, err := s.repos.Builds.SetPlan(ctx, buildID, instance, plan); err != nil {
		s.logger.Warn("store a build's plan", zap.String("build_id", buildID.String()), zap.Error(err))
	}
}

// request is the factory.build request of b, with URLs valid until deadline.
func (s *Service) request(ctx context.Context, b *models.Build, deadline time.Time) (protocol.BuildRequest, error) {
	expiry := time.Until(deadline) + time.Minute
	req := protocol.BuildRequest{
		BuildID: b.BuildID.String(), Target: b.Target, Platform: b.Platform,
		SchemaVersion: b.SchemaVersion, Values: b.Values, Commands: b.Commands, Connections: b.Connections.Items,
		Crypto: b.Crypto, Deadline: deadline, Actor: b.CreatedBy.Protocol(),
	}
	if len(b.FileIDs) > 0 {
		req.Files = make(map[string]protocol.BuildFile, len(b.FileIDs))
	}
	for _, id := range b.FileIDs {
		f, err := s.file(ctx, b.OperationID, id)
		if err != nil {
			return req, fmt.Errorf("build file %s: %w", id, err)
		}
		pf := f.Protocol()
		if pf.URL, err = s.signer.GetURL(ctx, f.ObjectKey, expiry); err != nil {
			return req, err
		}
		req.Files[pf.ID] = pf
	}
	upload, err := s.signer.PutURL(ctx, artifactKey(b.OperationID, b.BuildID), expiry)
	if err != nil {
		return req, err
	}
	req.Artifact = protocol.ArtifactUpload{UploadURL: upload, MaxSize: s.cfg.ArtifactMaxSize}
	return req, nil
}

// verifyAsync checks a received artifact in the background, once per build
// per process.
func (s *Service) verifyAsync(buildID uuid.UUID) {
	if _, busy := s.verifying.LoadOrStore(buildID, true); busy {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.verifying.Delete(buildID)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.verify(ctx, buildID)
	}()
}

// verify reads the uploaded artifact and compares it with what the factory
// reported. Only then does the build succeed and the minion exist.
func (s *Service) verify(ctx context.Context, buildID uuid.UUID) {
	b, err := s.repos.Builds.FindByBuildID(ctx, buildID)
	if err != nil || b.State != models.BuildStateBuilding || !b.ResultReceived || b.Artifact == nil {
		return
	}
	size, sum, err := s.digest(ctx, b.Artifact.ObjectKey, b.Artifact.Size)
	switch {
	case blob.IsNotFound(err):
		s.fail(ctx, b, models.BuildErrorArtifactMismatch, "the factory reported an artifact it did not upload")
		return
	case err != nil:
		if s.now().After(b.Deadline.Add(verifyGiveUp)) {
			s.fail(ctx, b, models.BuildErrorArtifactMismatch, "the artifact could not be read to check it")
			return
		}
		s.logger.Warn("check a build artifact; will retry", zap.String("build_id", buildID.String()), zap.Error(err))
		return
	case size != b.Artifact.Size:
		_ = s.store.Delete(ctx, b.Artifact.ObjectKey)
		s.fail(ctx, b, models.BuildErrorArtifactMismatch, fmt.Sprintf(
			"the stored artifact is %d bytes; the factory reported %d", size, b.Artifact.Size))
		return
	case sum != b.Artifact.SHA256:
		_ = s.store.Delete(ctx, b.Artifact.ObjectKey)
		s.fail(ctx, b, models.BuildErrorArtifactMismatch, fmt.Sprintf(
			"the stored artifact's SHA-256 is %s; the factory reported %s", sum, b.Artifact.SHA256))
		return
	}
	ok, err := s.repos.Builds.Finish(ctx, buildID, []string{models.BuildStateBuilding},
		repository.BuildOutcome{State: models.BuildStateSucceeded, At: s.now()})
	if err != nil || !ok {
		return // cancelled meanwhile, or another core instance finished it
	}
	m := &models.Minion{
		BuildID: b.BuildID, OperationID: b.OperationID, ModuleName: b.ModuleName,
		Target: b.Target, Platform: b.Platform, Commands: b.Commands, CreatedBy: b.CreatedBy, CreatedAt: s.now(),
	}
	if err := s.repos.Minions.Insert(ctx, m); err != nil {
		s.logger.Error("record a built minion", zap.String("build_id", buildID.String()), zap.Error(err))
	}
	s.logger.Info("build succeeded", zap.String("build_id", buildID.String()))
}

// digest reads an object of want bytes: its size and SHA-256. A larger one
// is not read past want plus one byte, nor at all when the store says it is
// larger, and comes back with no SHA-256: what the factory reports bounds
// what core reads.
func (s *Service) digest(ctx context.Context, key string, want int64) (int64, string, error) {
	r, info, err := s.store.Get(ctx, key)
	if err != nil {
		return 0, "", err
	}
	defer r.Close()
	if info.ContentLength > want {
		return info.ContentLength, "", nil
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, want+1))
	if err != nil {
		return 0, "", err
	}
	if n > want {
		return n, "", nil
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
