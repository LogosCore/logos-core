package resolver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// Minion sessions over GraphQL (minion_sessions.graphql): read-only, opened
// by pkg/minionsync. Viewers of an operation read them.

// IMinionSessionResolver is the business logic behind minion_sessions.graphql.
type IMinionSessionResolver interface {
	MinionSessions(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.MinionSessionConnection, error)
	// Load reads one session for a subscription event, already authorized
	// by the subscription's filter; nil when it cannot be read.
	Load(ctx context.Context, sessionID string) *model.MinionSession
}

type minionSessionResolver struct {
	sessions      repository.IMinionSessionRepository
	operationRepo repository.IOperationRepository
}

func NewMinionSessionResolver(sessions repository.IMinionSessionRepository, operationRepo repository.IOperationRepository) IMinionSessionResolver {
	return &minionSessionResolver{sessions: sessions, operationRepo: operationRepo}
}

func (r *minionSessionResolver) MinionSessions(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.MinionSessionConnection, error) {
	op, err := uuid.Parse(operationID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	operation, err := gqlctx.LoadOperation(ctx, r.operationRepo, op)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}
	if err := authorization.AuthorizeOperationRole(ctx, &operation, models.OperationRoleViewer); err != nil {
		return nil, err
	}
	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, fmt.Errorf("invalid pagination args: %w", err)
	}
	q := strings.TrimSpace(deref(search))
	total, err := r.sessions.CountByOperationID(ctx, op, q)
	if err != nil {
		return nil, fmt.Errorf("count sessions: %w", err)
	}
	rows, err := r.sessions.FindByOperationIDWithCursor(ctx, op, q, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	edges, pageInfo := pagination.BuildEdges(rows, args, repository.MinionSessionCursor,
		func(s *models.MinionSession, cursor string) *model.MinionSessionEdge {
			return &model.MinionSessionEdge{Node: toMinionSessionModel(s), Cursor: cursor}
		})
	return &model.MinionSessionConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

func (r *minionSessionResolver) Load(ctx context.Context, sessionID string) *model.MinionSession {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return nil
	}
	s, err := r.sessions.FindBySessionID(ctx, id)
	if err != nil {
		return nil
	}
	return toMinionSessionModel(s)
}

func toMinionSessionModel(s *models.MinionSession) *model.MinionSession {
	h := s.Hello
	out := &model.MinionSession{
		ID: s.SessionID.String(), OperationID: s.OperationID.String(), BuildID: s.BuildID.String(),
		BuildDescription: s.BuildDescription, ModuleName: s.ModuleName, Target: s.Target, Platform: s.Platform,
		Staged: s.Staged, Pid: int(h.PID), Os: h.OS, Arch: h.Arch, Host: strPtr(h.Host), User: strPtr(h.User),
		Domain: strPtr(h.Domain), Proc: strPtr(h.Proc), Integrity: strPtr(h.Integrity), Addrs: orEmpty(h.Addrs),
		Version: strPtr(h.Version), CreatedAt: s.CreatedAt.Format(time.RFC3339), LastSeenAt: timePtr(s.LastSeenAt),
	}
	if len(h.X) > 0 {
		out.X = h.X
	}
	return out
}
