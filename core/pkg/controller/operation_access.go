package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// Operation access for the REST handlers, which sit outside the GraphQL
// directive that guards the same operations there. The wiki image, file and
// transfer controllers each used to carry their own copy of this.

// callerHasOperationRole reports whether the caller may act on operation opID
// with at least role: app admins always may, anyone else by their membership
// — the same rule the GraphQL @hasPermission directive applies, including
// the synthetic Public operation every authenticated caller can use.
// Operators may upload attachments; viewers may read them.
func callerHasOperationRole(c *gin.Context, ops gqlctx.OperationFinder, opID uuid.UUID, role models.OperationRole) bool {
	if isAppAdminFromContext(c) {
		return true
	}
	op, err := gqlctx.LoadOperation(c.Request.Context(), ops, opID)
	if err != nil {
		return false
	}
	return callerHasRoleIn(c, &op, role)
}

// callerHasRoleIn is callerHasOperationRole for an operation already loaded.
func callerHasRoleIn(c *gin.Context, op *models.Operation, role models.OperationRole) bool {
	if isAppAdminFromContext(c) {
		return true
	}
	rolesSlice, _ := c.Get("roles")
	ctx := gqlctx.WithAuthInfo(c.Request.Context(), gqlctx.AuthInfo{
		UserID:   c.GetString("userID"),
		Username: c.GetString("username"),
		Roles:    toStringSlice(rolesSlice),
	})
	return authorization.AuthorizeOperationRole(ctx, op, role) == nil
}

func isAppAdminFromContext(c *gin.Context) bool {
	raw, _ := c.Get("roles")
	for _, r := range toStringSlice(raw) {
		if r == "admin" {
			return true
		}
	}
	return false
}

func toStringSlice(v any) []string {
	s, _ := v.([]string)
	return s
}
