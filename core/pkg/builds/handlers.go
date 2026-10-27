package builds

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// The factory → core build operations, served on logos.core.rpc. A factory
// instance may only report the builds it is building: anything else is
// not_found, which tells it core no longer waits for that build.

// HandleProgress serves factory.build.progress.
func (s *Service) HandleProgress(ctx context.Context, req messaging.Envelope) (any, error) {
	var p protocol.BuildProgress
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, "malformed progress payload")
	}
	if err := p.Validate(); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, err.Error())
	}
	ok, err := s.repos.Builds.ReportStep(ctx, p.BuildID, req.Source.Instance, p.Step, p.Message, s.now())
	if err != nil {
		return nil, fmt.Errorf("record progress: %w", err)
	}
	if !ok {
		return nil, notWaiting(p.BuildID, req.Source.Instance)
	}
	return protocol.BuildProgressReply{BuildID: p.BuildID, Ack: true}, nil
}

// HandleResult serves factory.build.result. A failed result finishes the
// build at once; a succeeded one is recorded and its artifact checked in the
// background, so a large one never holds up the lifecycle calls that share
// this queue.
func (s *Service) HandleResult(ctx context.Context, req messaging.Envelope) (any, error) {
	var r protocol.BuildResult
	if err := json.Unmarshal(req.Payload, &r); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, "malformed result payload")
	}
	if err := r.Validate(); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, err.Error())
	}
	instance := req.Source.Instance
	b, err := s.repos.Builds.FindByBuildID(ctx, r.BuildID)
	if repository.IsNotFound(err) {
		return nil, notWaiting(r.BuildID, instance)
	}
	if err != nil {
		return nil, fmt.Errorf("load build: %w", err)
	}
	if b.Instance != instance {
		return nil, notWaiting(r.BuildID, instance)
	}
	recorded := protocol.BuildResultReply{BuildID: r.BuildID, Recorded: true}

	if b.State != models.BuildStateBuilding {
		// The same result again (the reply was lost) is answered the same
		// way; anything else came too late.
		if resultRecorded(b) {
			return recorded, nil
		}
		return nil, notWaiting(r.BuildID, instance)
	}

	if r.Status == protocol.BuildFailed {
		ok, err := s.repos.Builds.Finish(ctx, b.BuildID, []string{models.BuildStateBuilding}, repository.BuildOutcome{
			State: models.BuildStateFailed, Error: &models.BuildError{Code: r.Error.Code, Message: r.Error.Message},
			Warnings: r.Warnings, Log: r.Log, At: s.now(),
		})
		if err != nil {
			return nil, fmt.Errorf("record a failed build: %w", err)
		}
		if !ok {
			return nil, notWaiting(r.BuildID, instance)
		}
		s.logger.Info("build failed", zap.String("build_id", b.BuildID), zap.String("code", r.Error.Code), zap.String("reason", r.Error.Message))
		return recorded, nil
	}

	art := models.BuildArtifact{
		Filename: r.Artifact.Filename, Size: r.Artifact.Size, SHA256: r.Artifact.SHA256,
		MediaType: r.Artifact.MediaType, ObjectKey: artifactKey(b.OperationID, b.BuildID),
	}
	ok, err := s.repos.Builds.RecordResult(ctx, b.BuildID, instance, art, r.Capabilities, r.Warnings, r.Log, s.now())
	if err != nil {
		return nil, fmt.Errorf("record a build result: %w", err)
	}
	if !ok && !b.ResultReceived {
		return nil, notWaiting(r.BuildID, instance)
	}
	s.verifyAsync(b.BuildID)
	return recorded, nil
}

// resultRecorded reports whether a finished build ended with its factory's
// result, rather than one of core's own reasons (timeout, a lost factory, a
// cancel).
func resultRecorded(b *models.Build) bool {
	switch b.State {
	case models.BuildStateSucceeded:
		return true
	case models.BuildStateFailed:
		if b.Error == nil {
			return false
		}
		switch b.Error.Code {
		case models.BuildErrorTimeout, models.BuildErrorFactoryLost, models.BuildErrorSchemaChanged, models.BuildErrorRefused:
			return false
		case models.BuildErrorArtifactMismatch:
			return b.ResultReceived
		}
		return true
	}
	return false
}

func rpcErr(code, msg string) error { return &messaging.RPCError{Code: code, Message: msg} }

func notWaiting(buildID, instance string) error {
	return rpcErr(protocol.CodeNotFound, fmt.Sprintf("core does not wait for build %s from %s", buildID, instance))
}
