package resolver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// An operation's folders of sessions (models.MinionSessionFolder): its
// operators share them, so an operator makes, changes and fills them, and a
// viewer reads them. A folder's sessions stay when it goes, in All.

func toFolderModel(f models.MinionSessionFolder) *model.MinionSessionFolder {
	return &model.MinionSessionFolder{ID: f.FolderID.String(), OperationID: f.OperationID.String(), Name: f.Name,
		Icon: strPtr(f.Icon), Emoji: strPtr(f.Emoji), Color: strPtr(f.Color), Order: f.Order}
}

// operatorOf loads an operation and checks the caller is its operator.
func (r *minionSessionResolver) operatorOf(ctx context.Context, op uuid.UUID) error {
	if gqlctx.AuthFromContext(ctx).Agent != nil {
		return moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	operation, err := gqlctx.LoadOperation(ctx, r.operationRepo, op)
	if err != nil {
		return fmt.Errorf("operation not found: %w", err)
	}
	return authorization.AuthorizeOperationRole(ctx, &operation, models.OperationRoleOperator)
}

// operatorFolder loads a folder its caller may change: their operation's,
// as its operator.
func (r *minionSessionResolver) operatorFolder(ctx context.Context, id string) (*models.MinionSessionFolder, error) {
	fid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid folder ID")
	}
	f, err := r.folders.FindByID(ctx, fid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "folder not found")
		}
		return nil, err
	}
	if err := r.operatorOf(ctx, f.OperationID); err != nil {
		return nil, err
	}
	return f, nil
}

func folderFields(ctx context.Context, in model.MinionSessionFolderInput) (name, icon, emoji, color string, err error) {
	name, icon, emoji, color = strings.TrimSpace(in.Name), deref(in.Icon), deref(in.Emoji), deref(in.Color)
	if why := models.ValidateSessionFolder(name, icon, emoji, color); why != "" {
		return "", "", "", "", moduleGQLError(ctx, errCodeBadUserInput, why)
	}
	return name, icon, emoji, color, nil
}

// foldersChanged tells the operation's clients its folders changed: their
// lists refetch (the event names no session).
func (r *minionSessionResolver) foldersChanged(op uuid.UUID) {
	r.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(eventbus.MinionSessionEventPayload{OperationID: op.String()}))
}

func (r *minionSessionResolver) MinionSessionFolders(ctx context.Context, operationID string) ([]*model.MinionSessionFolder, error) {
	op, err := r.viewer(ctx, operationID)
	if err != nil {
		return nil, err
	}
	list, err := r.folders.ListByOperation(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("list the folders: %w", err)
	}
	out := make([]*model.MinionSessionFolder, len(list))
	for i, f := range list {
		out[i] = toFolderModel(f)
	}
	return out, nil
}

func (r *minionSessionResolver) CreateMinionSessionFolder(ctx context.Context, operationID string, in model.MinionSessionFolderInput) (*model.MinionSessionFolder, error) {
	op, err := uuid.Parse(operationID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	if err := r.operatorOf(ctx, op); err != nil {
		return nil, err
	}
	name, icon, emoji, color, err := folderFields(ctx, in)
	if err != nil {
		return nil, err
	}
	if n, err := r.folders.Count(ctx, op); err != nil {
		return nil, err
	} else if n >= models.MaxSessionFolders {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("an operation has at most %d folders", models.MaxSessionFolders))
	}
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}
	order, err := r.folders.NextOrder(ctx, op)
	if err != nil {
		return nil, err
	}
	f := models.MinionSessionFolder{FolderID: uuid.New(), OperationID: op, Name: name, Icon: icon, Emoji: emoji, Color: color,
		Order: order, CreatedAt: time.Now().UTC(), CreatedBy: uid}
	if err := r.folders.Create(ctx, &f); err != nil {
		return nil, fmt.Errorf("create the folder: %w", err)
	}
	r.foldersChanged(op)
	return toFolderModel(f), nil
}

func (r *minionSessionResolver) UpdateMinionSessionFolder(ctx context.Context, id string, in model.MinionSessionFolderInput) (*model.MinionSessionFolder, error) {
	f, err := r.operatorFolder(ctx, id)
	if err != nil {
		return nil, err
	}
	name, icon, emoji, color, err := folderFields(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := r.folders.Update(ctx, f.FolderID, name, icon, emoji, color); err != nil {
		return nil, fmt.Errorf("change the folder: %w", err)
	}
	f.Name, f.Icon, f.Emoji, f.Color = name, icon, emoji, color
	r.foldersChanged(f.OperationID)
	return toFolderModel(*f), nil
}

func (r *minionSessionResolver) MoveMinionSessionFolder(ctx context.Context, id string, before *string) ([]*model.MinionSessionFolder, error) {
	f, err := r.operatorFolder(ctx, id)
	if err != nil {
		return nil, err
	}
	list, err := r.folders.ListByOperation(ctx, f.OperationID)
	if err != nil {
		return nil, err
	}
	// The order without it, then it back in before the one named (or last).
	order := make([]models.MinionSessionFolder, 0, len(list))
	for _, x := range list {
		if x.FolderID != f.FolderID {
			order = append(order, x)
		}
	}
	at := len(order)
	if before != nil {
		bid, err := uuid.Parse(*before)
		if err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid folder ID")
		}
		for i, x := range order {
			if x.FolderID == bid {
				at = i
			}
		}
	}
	order = append(order[:at], append([]models.MinionSessionFolder{*f}, order[at:]...)...)
	out := make([]*model.MinionSessionFolder, len(order))
	for i := range order {
		if order[i].Order != i {
			if err := r.folders.SetOrder(ctx, order[i].FolderID, i); err != nil {
				return nil, fmt.Errorf("order the folders: %w", err)
			}
			order[i].Order = i
		}
		out[i] = toFolderModel(order[i])
	}
	r.foldersChanged(f.OperationID)
	return out, nil
}

func (r *minionSessionResolver) DeleteMinionSessionFolder(ctx context.Context, id string) (bool, error) {
	f, err := r.operatorFolder(ctx, id)
	if err != nil {
		return false, err
	}
	// Its sessions first: one never names a folder that is gone.
	if _, err := r.sessions.ClearFolder(ctx, f.OperationID, f.FolderID); err != nil {
		return false, fmt.Errorf("empty the folder: %w", err)
	}
	if err := r.folders.Delete(ctx, f.FolderID); err != nil {
		return false, fmt.Errorf("delete the folder: %w", err)
	}
	r.foldersChanged(f.OperationID)
	return true, nil
}

func (r *minionSessionResolver) SetMinionSessionsFolder(ctx context.Context, operationID string, ids []string, matching *model.MinionSessionMatchInput, folderID *string) (int, error) {
	op, err := uuid.Parse(operationID)
	if err != nil {
		return 0, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	if err := r.operatorOf(ctx, op); err != nil {
		return 0, err
	}
	var folder *uuid.UUID
	if folderID != nil {
		fid, err := uuid.Parse(*folderID)
		if err != nil {
			return 0, moduleGQLError(ctx, errCodeBadUserInput, "invalid folder ID")
		}
		f, err := r.folders.FindByID(ctx, fid)
		if err != nil || f.OperationID != op {
			return 0, moduleGQLError(ctx, errCodeNotFound, "folder not found")
		}
		folder = &fid
	}
	targets, q, err := bulkTargets(ctx, ids, matching)
	if err != nil {
		return 0, err
	}
	var n int64
	if q != nil {
		n, err = r.sessions.SetFolderMatching(ctx, op, *q, folder)
	} else {
		n, err = r.sessions.SetFolderMany(ctx, op, targets, folder)
	}
	if err != nil {
		return 0, fmt.Errorf("move the sessions: %w", err)
	}
	// One event naming no session: every list refetches (a folder's
	// membership is no column a row's patch could tell).
	if n > 0 {
		r.foldersChanged(op)
	}
	return int(n), nil
}

// folderOf reads a list's folder argument: nil for All.
func folderOf(ctx context.Context, folderID *string) (*uuid.UUID, error) {
	if folderID == nil || *folderID == "" {
		return nil, nil
	}
	fid, err := uuid.Parse(*folderID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid folder ID")
	}
	return &fid, nil
}

// uuidPtr is an id as GraphQL has it: nil for none.
func uuidPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
