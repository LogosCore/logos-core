package builds

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// BuildInput is what an operator asks to build.
type BuildInput struct {
	// Description is what the build is for, required: the operators' note.
	// Core keeps it with the build record and never puts it in the build
	// request, so it cannot end up in an artifact.
	Description string
	ModuleName  string
	Target      string
	Platform    string
	Values      map[string]any
	Connections []ConnectionInput
	Actor       models.ModuleActor
}

// artifactKey is where a build's artifact is uploaded.
func artifactKey(op uuid.UUID, buildID string) string {
	return path.Join("artifacts", op.String(), buildID)
}

// CreateBuild validates a build against the factory kind's current schema and
// queues it: defaults filled in, each connection's settings resolved against
// its channel's current schema, a minion id chosen. The scheduler then offers
// it to a factory instance.
func (s *Service) CreateBuild(ctx context.Context, op uuid.UUID, in BuildInput) (*models.Build, error) {
	moduleName := strings.TrimSpace(in.ModuleName)
	schema, err := s.schema(ctx, moduleName)
	if err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, invalid("no %q factory has declared a build schema", moduleName)
	}
	if len(schema.Crypto) > 0 {
		return nil, invalid("the %q factory's minions need payload crypto (%s), which core does not implement yet",
			moduleName, strings.Join(schema.Crypto, ", "))
	}
	insts, err := s.instances(ctx, moduleName, schema.Version)
	if err != nil {
		return nil, err
	}
	if len(insts) == 0 {
		return nil, invalid("no running %q factory declares build schema version %d", moduleName, schema.Version)
	}

	values := compact(in.Values)
	conns := compactConnections(in.Connections)
	c, probs, err := s.check(ctx, op, *schema, settings{Target: in.Target, Platform: in.Platform, Values: values, Connections: conns})
	if err != nil {
		return nil, err
	}
	description := strings.TrimSpace(in.Description)
	switch {
	case description == "":
		probs.add("description", "a description is required")
	case utf8.RuneCountInString(description) > MaxDescriptionLength:
		probs.add("description", "longer than %d characters", MaxDescriptionLength)
	}
	if err := probs.err("the build does not fit the factory's build schema"); err != nil {
		return nil, err
	}

	b := &models.Build{
		BuildID: protocol.NewULID(), OperationID: op, ModuleName: moduleName, Description: description,
		Target: in.Target, Platform: in.Platform, SchemaVersion: schema.Version,
		Values:    protocol.ResolveFieldValues(c.fields, values),
		FileIDs:   c.fileIDs,
		MinionID:  uuid.NewString(),
		State:     models.BuildStateQueued,
		Deadline:  s.now().Add(s.cfg.Timeout),
		CreatedBy: in.Actor, CreatedAt: s.now(),
	}
	for i, conn := range conns {
		if conn.Description != "" && b.ConnectionDescriptions == nil {
			b.ConnectionDescriptions = make([]string, len(conns))
		}
		if b.ConnectionDescriptions != nil {
			b.ConnectionDescriptions[i] = conn.Description
		}
		cs := c.channels[i]
		ch, _ := schema.Channel(conn.ModuleName)
		b.Connections.Items = append(b.Connections.Items, protocol.BuildConnection{
			Endpoint: protocol.ChannelEndpointExport{
				Format:  protocol.ChannelEndpointExportV1,
				Name:    connectionName(conn, i),
				Channel: protocol.ExportChannel{ModuleName: conn.ModuleName, SchemaVersion: cs.Version},
				Params:  protocol.ResolveConnectionValues(*cs, conn.Settings),
			},
			Values: protocol.ResolveFieldValues(ch.Fields, conn.Values),
		})
	}

	// The request a factory gets must pass the factory's own check: the same
	// function, run here with the URLs the dispatch will sign.
	req, err := s.request(ctx, b, b.Deadline)
	if err != nil {
		return nil, err
	}
	if p := protocol.ValidateBuildRequest(*schema, req); p != nil {
		return nil, &ValidationError{Message: "the build does not fit the factory's build schema", Problems: p}
	}
	if err := s.repos.Builds.Insert(ctx, b); err != nil {
		return nil, fmt.Errorf("store build: %w", err)
	}
	s.logger.Info("build queued", zap.String("build_id", b.BuildID), zap.String("factory", moduleName),
		zap.String("target", b.Target), zap.String("platform", b.Platform))
	s.wake()
	return b, nil
}

// Builds returns an operation's builds, newest first, at most limit (0: all).
func (s *Service) Builds(ctx context.Context, op uuid.UUID, limit int64) ([]models.Build, error) {
	return s.repos.Builds.List(ctx, op, limit)
}

// Build returns one build.
func (s *Service) Build(ctx context.Context, buildID string) (*models.Build, error) {
	b, err := s.repos.Builds.FindByBuildID(ctx, strings.TrimSpace(buildID))
	if err != nil {
		return nil, notFound(err, ErrBuildNotFound)
	}
	return b, nil
}

// CancelBuild stops a queued or running build. A running one is also
// cancelled on its factory instance, best effort: core ignores whatever that
// instance still sends.
func (s *Service) CancelBuild(ctx context.Context, buildID string, actor models.ModuleActor) (*models.Build, error) {
	b, err := s.Build(ctx, buildID)
	if err != nil {
		return nil, err
	}
	if b.Finished() {
		return nil, invalid("the build already finished (%s)", b.State)
	}
	ok, err := s.repos.Builds.Finish(ctx, b.BuildID, []string{models.BuildStateQueued, models.BuildStateBuilding},
		repository.BuildOutcome{State: models.BuildStateCancelled, CancelledBy: &actor, At: s.now()})
	if err != nil {
		return nil, fmt.Errorf("cancel build: %w", err)
	}
	if ok && b.State == models.BuildStateBuilding {
		s.cancelOn(b.BuildID, b.Instance)
	}
	if ok {
		s.logger.Info("build cancelled", zap.String("build_id", b.BuildID), zap.String("by", actor.ID))
	}
	return s.Build(ctx, b.BuildID)
}

// cancelOn tells a factory instance to stop a build, in the background.
func (s *Service) cancelOn(buildID, instance string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CallTimeout+5*time.Second)
		defer cancel()
		mod, err := s.registry.FindByInstance(ctx, instance)
		if err != nil || mod.RPCQueue == "" {
			return
		}
		rep, err := s.rpc.Call(ctx, mod.RPCQueue, protocol.TypeFactoryBuildCancel, protocol.BuildCancelRequest{BuildID: buildID}, s.cfg.CallTimeout)
		if err != nil || rep.Status != protocol.StatusOK {
			s.logger.Warn("factory did not confirm a build cancel", zap.String("build_id", buildID),
				zap.String("instance", instance), zap.Error(err))
		}
	}()
}

// OpenArtifact streams a succeeded build's artifact.
func (s *Service) OpenArtifact(ctx context.Context, buildID string) (io.ReadCloser, *models.Build, error) {
	b, err := s.Build(ctx, buildID)
	if err != nil {
		return nil, nil, err
	}
	if b.State != models.BuildStateSucceeded || b.Artifact == nil || b.Artifact.Deleted {
		return nil, b, invalid("the build has no artifact to download")
	}
	r, _, err := s.store.Get(ctx, b.Artifact.ObjectKey)
	if err != nil {
		return nil, b, fmt.Errorf("open artifact: %w", err)
	}
	return r, b, nil
}

// DeleteArtifact removes a finished build's artifact bytes; the build record
// stays.
func (s *Service) DeleteArtifact(ctx context.Context, buildID string) (*models.Build, error) {
	b, err := s.Build(ctx, buildID)
	if err != nil {
		return nil, err
	}
	if !b.Finished() || b.Artifact == nil || b.Artifact.Deleted {
		return nil, invalid("the build has no artifact to delete")
	}
	if err := s.store.Delete(ctx, b.Artifact.ObjectKey); err != nil {
		return nil, fmt.Errorf("delete artifact: %w", err)
	}
	if err := s.repos.Builds.MarkArtifactDeleted(ctx, b.BuildID); err != nil {
		return nil, fmt.Errorf("record artifact deletion: %w", err)
	}
	return s.Build(ctx, b.BuildID)
}

// Minions returns an operation's minions, newest first.
func (s *Service) Minions(ctx context.Context, op uuid.UUID) ([]models.Minion, error) {
	return s.repos.Minions.List(ctx, op)
}

// DeleteOperationData removes everything builds keep for an operation: its
// builds and their artifacts, files and minions.
func (s *Service) DeleteOperationData(ctx context.Context, op uuid.UUID) error {
	builds, err := s.repos.Builds.List(ctx, op, 0)
	if err != nil {
		return fmt.Errorf("list builds: %w", err)
	}
	for _, b := range builds {
		if !b.Finished() {
			_, _ = s.repos.Builds.Finish(ctx, b.BuildID, []string{b.State},
				repository.BuildOutcome{State: models.BuildStateCancelled, At: s.now()})
			if b.State == models.BuildStateBuilding {
				s.cancelOn(b.BuildID, b.Instance)
			}
		}
		if err := s.store.Delete(ctx, artifactKey(op, b.BuildID)); err != nil {
			s.logger.Warn("delete artifact with its operation", zap.String("build_id", b.BuildID), zap.Error(err))
		}
	}
	files, err := s.repos.Files.List(ctx, op)
	if err != nil {
		return fmt.Errorf("list build files: %w", err)
	}
	for _, f := range files {
		if err := s.store.Delete(ctx, f.ObjectKey); err != nil {
			s.logger.Warn("delete build file with its operation", zap.String("file_id", f.FileID.String()), zap.Error(err))
		}
	}
	for name, del := range map[string]func(context.Context, uuid.UUID) error{
		"builds":  s.repos.Builds.DeleteByOperationID,
		"files":   s.repos.Files.DeleteByOperationID,
		"minions": s.repos.Minions.DeleteByOperationID,
	} {
		if err := del(ctx, op); err != nil {
			return fmt.Errorf("delete build %s: %w", name, err)
		}
	}
	return nil
}
