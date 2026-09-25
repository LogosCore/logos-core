package controller

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// operationsByID is an OperationFinder over a fixed set of operations.
type operationsByID map[uuid.UUID]models.Operation

func (f operationsByID) FindByID(_ context.Context, id uuid.UUID) (models.Operation, error) {
	if models.IsPublicOperation(id) {
		return models.SynthesizePublicOperation(), nil
	}
	op, ok := f[id]
	if !ok {
		return models.Operation{}, errors.New("operation not found")
	}
	return op, nil
}

func callerContext(userID uuid.UUID, roles ...string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set("userID", userID.String())
	c.Set("roles", roles)
	return c
}

// TestCallerHasOperationRole pins the access rule the wiki image, file and
// transfer handlers share. Each used to carry its own copy; a wrong answer
// here serves one operation's attachments to another's members.
func TestCallerHasOperationRole(t *testing.T) {
	viewer, stranger := uuid.New(), uuid.New()
	opID := uuid.New()
	ops := operationsByID{opID: {
		OperationID: opID,
		Members:     []models.OperationMember{{UserID: viewer, Role: models.OperationRoleViewer}},
	}}

	for _, tc := range []struct {
		name   string
		caller *gin.Context
		opID   uuid.UUID
		role   models.OperationRole
		want   bool
	}{
		{"a member reads", callerContext(viewer, "user"), opID, models.OperationRoleViewer, true},
		{"a viewer cannot upload", callerContext(viewer, "user"), opID, models.OperationRoleOperator, false},
		{"a non-member cannot read", callerContext(stranger, "user"), opID, models.OperationRoleViewer, false},
		{"an unknown operation grants nothing", callerContext(viewer, "user"), uuid.New(), models.OperationRoleViewer, false},
		{"an app admin needs no membership", callerContext(stranger, "admin"), uuid.New(), models.OperationRoleOperator, true},
		{"Public is open to every caller", callerContext(stranger, "user"), models.PublicOperationID, models.OperationRoleOperator, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := callerHasOperationRole(tc.caller, ops, tc.opID, tc.role); got != tc.want {
				t.Errorf("callerHasOperationRole = %v, want %v", got, tc.want)
			}
		})
	}
}
