package builds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/payloadcrypto"
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
	// Commands are the commands picked for the minion, by id; the target's
	// required ones are added. nil: the target's default ones.
	Commands    []string
	Connections []ConnectionInput
	// PersistedSession is the session the minion claims when it starts:
	// one of the operation's open sessions, for a factory whose minions
	// claim persisted sessions. nil: the minion opens new ones.
	PersistedSession *uuid.UUID
	// NewPersistedSession reserves a new session with the build for its
	// minion to claim (one per build: not with PersistedSession).
	NewPersistedSession bool
	// Failover is how the minion moves between its connections on its own;
	// nil: protocol.DefaultBuildFailover.
	Failover *protocol.BuildFailover
	// MaxTasks is how many tasks the minion runs at once as it starts, 0 to
	// protocol.MaxTasksLimit, for a factory whose minions take one; nil:
	// protocol.DefaultMaxTasks there, none elsewhere.
	MaxTasks *int
	// Compression turns the build's compression on or off, for a factory
	// that declares protocol.BuildSchema.Compression; nil: on there.
	Compression *bool
	Actor       models.ModuleActor
}

// artifactKey is where a build's artifact is uploaded.
func artifactKey(op, buildID uuid.UUID) string {
	return path.Join("artifacts", op.String(), buildID.String())
}

// CreateBuild validates a build against the factory kind's current schema and
// queues it: defaults filled in, the target's required commands added, each
// connection's settings resolved against its channel's current schema, a
// build id chosen. The scheduler then offers it to a factory instance.
func (s *Service) CreateBuild(ctx context.Context, op uuid.UUID, in BuildInput) (*models.Build, error) {
	moduleName := strings.TrimSpace(in.ModuleName)
	schema, err := s.schema(ctx, moduleName)
	if err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, invalid("no %q factory has declared a build schema", moduleName)
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
	c, probs, err := s.check(ctx, op, *schema, settings{
		Target: in.Target, Platform: in.Platform, Values: values, Commands: in.Commands, Connections: conns,
	})
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
	if in.PersistedSession != nil || in.NewPersistedSession {
		if probs == nil {
			probs = problems{}
		}
		switch {
		case in.PersistedSession != nil && in.NewPersistedSession:
			probs.add("persistedSession", "one persisted session per build: an existing one or a new one")
		case !schema.PersistedSessions:
			probs.add("persistedSession", "this factory's minions do not claim persisted sessions")
		case in.PersistedSession != nil:
			if err := s.checkPersistedSession(ctx, op, *in.PersistedSession, probs); err != nil {
				return nil, err
			}
		}
	}
	if in.MaxTasks != nil {
		if probs == nil {
			probs = problems{}
		}
		switch {
		case !schema.MaxTasks:
			probs.add("maxTasks", "this factory's minions take no max tasks")
		case *in.MaxTasks < 0 || *in.MaxTasks > protocol.MaxTasksLimit:
			probs.add("maxTasks", "0 to %d", protocol.MaxTasksLimit)
		}
	}
	if in.Compression != nil && *in.Compression && len(schema.Compression) == 0 {
		if probs == nil {
			probs = problems{}
		}
		probs.add("compression", "this factory's minions inflate nothing")
	}
	codec := schema.TargetCodec(c.target)
	if probs == nil {
		probs = problems{}
	}
	if s := codec.Suite; s != "" && !protocol.IsFactoryCodec(s) && !payloadcrypto.Supported(s) {
		probs.add("target", "the target's minions need payload crypto suite %q, which core does not implement", s)
	}
	if codec.Translated() && schema.Codec == nil {
		probs.add("target", "the factory names a codec of its own and declares no codec")
	}
	if err := probs.err("the build does not fit the factory's build schema"); err != nil {
		return nil, err
	}

	b := &models.Build{
		BuildID: uuid.New(), OperationID: op, ModuleName: moduleName, Description: description,
		Target: in.Target, Platform: in.Platform, SchemaVersion: schema.Version,
		Values:        protocol.ResolveFieldValues(c.fields, values),
		Commands:      c.commands,
		FileIDs:       c.fileIDs,
		CredentialIDs: c.credentialIDs,
		State:         models.BuildStateQueued,
		Deadline:      s.now().Add(s.cfg.Timeout),
		CreatedBy:     in.Actor, CreatedAt: s.now(),
		PersistedSessionID: in.PersistedSession,
	}
	if in.NewPersistedSession {
		sid := uuid.New()
		b.PersistedSessionID = &sid
	}
	failover := protocol.DefaultBuildFailover()
	if in.Failover != nil {
		failover = *in.Failover
	}
	b.Failover = (*models.BuildFailover)(&failover)
	if schema.MaxTasks {
		n := protocol.DefaultMaxTasks
		if in.MaxTasks != nil {
			n = *in.MaxTasks
		}
		b.MaxTasks = &n
	}
	// On by default where the factory's minions inflate.
	if len(schema.Compression) > 0 && (in.Compression == nil || *in.Compression) {
		b.Compression = schema.Compression[0]
	}
	// The target's codec (docs: Codecs). Core makes the keys of its own
	// suite now; a factory makes those of its own as the build is offered
	// (dispatch: factoryCrypto).
	if codec.Envelope != protocol.EnvelopeCBOR {
		b.Envelope = codec.Envelope
	}
	if codec.Translated() {
		decl := *schema.Codec
		b.Codec = &decl
	}
	if codec.Suite != "" && !protocol.IsFactoryCodec(codec.Suite) {
		if b.Crypto, err = payloadcrypto.NewKeys(codec.Suite); err != nil {
			return nil, err
		}
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
				Format:  protocol.ChannelEndpointExportV2,
				Name:    connectionName(conn, i),
				Channel: protocol.ExportChannel{ModuleName: conn.ModuleName, SchemaVersion: cs.Version},
				Kind:    c.kinds[i],
				Params:  protocol.ResolveFieldValues(cs.KindFields(c.kinds[i]), conn.Settings),
			},
			Values:   protocol.ResolveFieldValues(ch.KindFields(c.kinds[i]), conn.Values),
			Failover: conn.Failover,
			// What the channel carries on a connection of the kind: the
			// minion, and core, keep their messages within it.
			MaxMessage: cs.Kinds[c.kinds[i]].MaxMessage,
		})
	}

	// The request a factory gets must pass the factory's own check: the same
	// function, run here with the URLs the dispatch will sign.
	req, _, err := s.request(ctx, b, b.Deadline)
	if err != nil {
		return nil, err
	}
	if req.Crypto == nil && protocol.IsFactoryCodec(codec.Suite) {
		// The factory makes its suite's keys as the build is offered: the
		// request then carries them.
		req.Crypto = &protocol.BuildCrypto{Suite: codec.Suite}
	}
	if p := protocol.ValidateBuildRequest(*schema, req); p != nil {
		return nil, &ValidationError{Message: "the build does not fit the factory's build schema", Problems: p}
	}
	if err := s.repos.Builds.Insert(ctx, b); err != nil {
		return nil, fmt.Errorf("store build: %w", err)
	}
	switch {
	case in.NewPersistedSession:
		res := &models.MinionSessionReservation{SessionID: *b.PersistedSessionID, OperationID: op, BuildID: b.BuildID, CreatedAt: s.now()}
		if err := s.repos.Reservations.Insert(ctx, res); err != nil {
			return nil, fmt.Errorf("reserve the persisted session: %w", err)
		}
	case b.PersistedSessionID != nil:
		// A reserved session is not one yet: it is persisted when it opens.
		if err := s.repos.Sessions.SetPersisted(ctx, *b.PersistedSessionID); err != nil && !repository.IsNotFound(err) {
			s.logger.Warn("mark a session persisted", zap.String("session_id", b.PersistedSessionID.String()), zap.Error(err))
		}
	}
	s.logger.Info("build queued", zap.String("build_id", b.BuildID.String()), zap.String("factory", moduleName),
		zap.String("target", b.Target), zap.String("platform", b.Platform))
	s.wake()
	return b, nil
}

// Builds returns a page of an operation's builds, newest first: those filter
// lets through, past cursor, at most limit.
func (s *Service) Builds(ctx context.Context, op uuid.UUID, filter repository.BuildFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Build, error) {
	return s.repos.Builds.FindByOperationIDWithCursor(ctx, op, filter, cursor, limit, forward)
}

// CountBuilds counts an operation's builds filter lets through.
func (s *Service) CountBuilds(ctx context.Context, op uuid.UUID, filter repository.BuildFilter) (int64, error) {
	return s.repos.Builds.CountByOperationID(ctx, op, filter)
}

// Build returns one build, by its id: its minion's.
func (s *Service) Build(ctx context.Context, id string) (*models.Build, error) {
	buildID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return nil, ErrBuildNotFound
	}
	return s.build(ctx, buildID)
}

func (s *Service) build(ctx context.Context, buildID uuid.UUID) (*models.Build, error) {
	b, err := s.repos.Builds.FindByBuildID(ctx, buildID)
	if err != nil {
		return nil, notFound(err, ErrBuildNotFound)
	}
	return b, nil
}

// CancelBuild stops a queued or running build. A running one is also
// cancelled on its factory instance, best effort: core ignores whatever that
// instance still sends.
func (s *Service) CancelBuild(ctx context.Context, id string, actor models.ModuleActor) (*models.Build, error) {
	b, err := s.Build(ctx, id)
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
		s.logger.Info("build cancelled", zap.String("build_id", b.BuildID.String()), zap.String("by", actor.ID))
	}
	return s.build(ctx, b.BuildID)
}

// cancelOn tells a factory instance to stop a build, in the background.
func (s *Service) cancelOn(buildID uuid.UUID, instance string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CallTimeout+5*time.Second)
		defer cancel()
		mod, err := s.registry.FindByInstance(ctx, instance)
		if err != nil || mod.RPCQueue == "" {
			return
		}
		rep, err := s.rpc.Call(ctx, mod.RPCQueue, protocol.TypeFactoryBuildCancel, protocol.BuildCancelRequest{BuildID: buildID.String()}, s.cfg.CallTimeout)
		if err != nil || rep.Status != protocol.StatusOK {
			s.logger.Warn("factory did not confirm a build cancel", zap.String("build_id", buildID.String()),
				zap.String("instance", instance), zap.Error(err))
		}
	}()
}

// OpenArtifact streams a succeeded build's artifact.
func (s *Service) OpenArtifact(ctx context.Context, id string) (io.ReadCloser, *models.Build, error) {
	b, err := s.Build(ctx, id)
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
func (s *Service) DeleteArtifact(ctx context.Context, id string) (*models.Build, error) {
	b, err := s.Build(ctx, id)
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
	return s.build(ctx, b.BuildID)
}

// DeleteBuild removes a build, whatever its state: an unfinished one is
// cancelled first (and its factory told to stop), then its artifact, the
// minion it made and its record go. Build files stay: they belong to the
// operation, and become deletable when no build refers to them. A minion of
// the build that is already out cannot open a session any more: core no
// longer has its keys.
func (s *Service) DeleteBuild(ctx context.Context, id string, actor models.ModuleActor) error {
	b, err := s.Build(ctx, id)
	if err != nil {
		return err
	}
	if !b.Finished() {
		ok, err := s.repos.Builds.Finish(ctx, b.BuildID, []string{models.BuildStateQueued, models.BuildStateBuilding},
			repository.BuildOutcome{State: models.BuildStateCancelled, CancelledBy: &actor, At: s.now()})
		if err != nil {
			return fmt.Errorf("cancel build: %w", err)
		}
		if ok && b.State == models.BuildStateBuilding {
			s.cancelOn(b.BuildID, b.Instance)
		}
	}
	if err := s.store.Delete(ctx, artifactKey(b.OperationID, b.BuildID)); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	if err := s.repos.Minions.DeleteByBuildID(ctx, b.BuildID); err != nil {
		return fmt.Errorf("delete minion: %w", err)
	}
	// A session reserved with it and never claimed goes with it.
	if s.repos.Reservations != nil {
		if err := s.repos.Reservations.DeleteByBuildID(ctx, b.BuildID); err != nil {
			return fmt.Errorf("delete the reserved session: %w", err)
		}
	}
	if _, err := s.repos.Builds.Delete(ctx, b.BuildID); err != nil {
		return fmt.Errorf("delete build: %w", err)
	}
	s.logger.Info("build deleted", zap.String("build_id", b.BuildID.String()), zap.String("state", b.State), zap.String("by", actor.ID))
	return nil
}

// MaxBulkBuilds bounds the builds DeleteBuilds takes by id.
const MaxBulkBuilds = 500

// DeleteBuilds deletes many of an operation's builds as DeleteBuild does
// each: those with ids (at most MaxBulkBuilds; one not of the operation, or
// gone already, is skipped), or, ids nil, every one filter lets through. It
// returns how many went; on an error, those before it are gone.
func (s *Service) DeleteBuilds(ctx context.Context, op uuid.UUID, ids []string, filter *repository.BuildFilter, actor models.ModuleActor) (int, error) {
	n := 0
	if filter == nil {
		if len(ids) > MaxBulkBuilds {
			return 0, invalid("at most %d builds at once by id", MaxBulkBuilds)
		}
		for _, id := range ids {
			b, err := s.Build(ctx, id)
			if errors.Is(err, ErrBuildNotFound) || (err == nil && b.OperationID != op) {
				continue
			}
			if err != nil {
				return n, err
			}
			if err := s.DeleteBuild(ctx, id, actor); err != nil && !errors.Is(err, ErrBuildNotFound) {
				return n, err
			}
			n++
		}
		return n, nil
	}
	// Every match: the first page again and again, as each goes. One that
	// does not go (an error) stops it, so the loop always moves on.
	for {
		page, err := s.Builds(ctx, op, *filter, nil, 100, true)
		if err != nil {
			return n, fmt.Errorf("list builds: %w", err)
		}
		if len(page) == 0 {
			return n, nil
		}
		for _, b := range page {
			if err := s.DeleteBuild(ctx, b.BuildID.String(), actor); err != nil && !errors.Is(err, ErrBuildNotFound) {
				return n, err
			}
			n++
		}
	}
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
			s.logger.Warn("delete artifact with its operation", zap.String("build_id", b.BuildID.String()), zap.Error(err))
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
	if s.repos.Reservations != nil {
		if err := s.repos.Reservations.DeleteByOperationID(ctx, op); err != nil {
			return fmt.Errorf("delete reserved sessions: %w", err)
		}
	}
	return nil
}

// checkPersistedSession adds a problem when a build cannot persist session
// id: it is neither an open session of the operation nor one reserved in it.
func (s *Service) checkPersistedSession(ctx context.Context, op uuid.UUID, id uuid.UUID, probs problems) error {
	const key = "persistedSession"
	if s.repos.Sessions == nil {
		probs.add(key, "no session to persist")
		return nil
	}
	sess, err := s.repos.Sessions.FindBySessionID(ctx, id)
	switch {
	case repository.IsNotFound(err):
		// Or one reserved with another build, not claimed yet.
		if s.repos.Reservations == nil {
			probs.add(key, "no such session")
			return nil
		}
		res, rerr := s.repos.Reservations.FindBySessionID(ctx, id)
		switch {
		case repository.IsNotFound(rerr):
			probs.add(key, "no such session")
		case rerr != nil:
			return fmt.Errorf("load the reservation: %w", rerr)
		case res.OperationID != op:
			probs.add(key, "no such session")
		}
	case err != nil:
		return fmt.Errorf("load the session: %w", err)
	case sess.OperationID != op:
		probs.add(key, "no such session")
	case sess.Closed(s.now()):
		probs.add(key, "the session is closed")
	}
	return nil
}
