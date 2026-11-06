package resolver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// Minion identities over GraphQL (minion_sessions.graphql): who a session's
// tasks run as (acting), who a task ran as (as), and a session's history.

func (r *minionSessionResolver) MinionIdentities(ctx context.Context, sessionID string, after *string, first *int) (*model.MinionIdentityPage, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid session ID")
	}
	s, err := r.sessions.FindBySessionID(ctx, sid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "session not found")
		}
		return nil, err
	}
	if _, err := r.viewer(ctx, s.OperationID.String()); err != nil {
		return nil, err
	}
	out := &model.MinionIdentityPage{Entries: []*model.MinionIdentityEvent{}}
	if r.identities == nil {
		return out, nil
	}
	n := int64(0)
	if first != nil {
		n = int64(*first)
	}
	entries, next, err := r.identities.List(ctx, s.SessionID, deref(after), n)
	if err != nil {
		if strings.Contains(err.Error(), "bad cursor") {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
		return nil, err
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, &model.MinionIdentityEvent{
			ID: e.EventID.String(), At: e.At.UTC().Format(time.RFC3339Nano), Scope: e.Scope, User: strPtr(e.User),
			Domain: strPtr(e.Domain), Integrity: strPtr(e.Integrity), How: strPtr(e.How), CredentialID: idPtr(e.CredentialID),
			TaskID: idPtr(e.TaskID),
		})
	}
	out.Next = strPtr(next)
	return out, nil
}

// toIdentityModel is an identity as GraphQL; nil for none.
func toIdentityModel(i *models.MinionIdentity) *model.MinionIdentity {
	if i == nil {
		return nil
	}
	return &model.MinionIdentity{User: strPtr(i.User), Domain: strPtr(i.Domain), Integrity: strPtr(i.Integrity), How: strPtr(i.How),
		CredentialID: idPtr(i.CredentialID), TaskID: idPtr(i.TaskID), Since: i.Since.UTC().Format(time.RFC3339Nano)}
}

func idPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
