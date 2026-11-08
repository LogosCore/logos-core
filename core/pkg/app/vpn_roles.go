package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// vpnRoles says whether a VPN key's operator still is one of its
// operation's (miniontunnels.VPNRoles): read as each client connects, and
// every minute while it is.
type vpnRoles struct {
	users repository.IUserRepository
	ops   repository.IOperationRepository
}

func (r vpnRoles) IsOperator(ctx context.Context, operation, user uuid.UUID) bool {
	u, err := r.users.FindByID(ctx, user)
	if err != nil || !u.Active {
		return false
	}
	op, err := r.ops.FindByID(ctx, operation)
	if err != nil {
		return false
	}
	ctx = gqlctx.WithAuthInfo(ctx, gqlctx.AuthInfo{UserID: user.String(), Username: u.Username, Roles: u.Roles})
	return authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleOperator) == nil
}
