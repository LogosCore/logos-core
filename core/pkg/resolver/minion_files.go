package resolver

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// An operation's minion files of a kind, every session's, as GraphQL lists
// them (docs: contracts/task-results-and-actions.md, screenshot): its
// screenshots' pictures, or the files its operators uploaded for tasks, a
// page at a time, newest first. Their bytes: GET /api/v1/minion-files/{id}
// (controller), a screenshot's thumbnail with ?thumb=1.

const (
	defaultMinionFilePage = 50
	maxMinionFilePage     = 200
)

// minionFileKinds are the kinds an operation's files are listed by.
var minionFileKinds = map[string]bool{models.MinionFileKindScreenshot: true, models.MinionFileKindTask: true}

// MinionFiles is a page of an operation's files of a kind, newest first, for
// a viewer of the operation.
func (r *minionTaskResolver) MinionFiles(ctx context.Context, operationID, kind string, last *int, before *string) (*model.MinionFilePage, error) {
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	if !minionFileKinds[kind] {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "kind is screenshot or task")
	}
	operation, err := gqlctx.LoadOperation(ctx, r.operationRepo, opID)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}
	if err := authorization.AuthorizeOperationRole(ctx, &operation, models.OperationRoleViewer); err != nil {
		return nil, err
	}
	page := &model.MinionFilePage{Files: []*model.MinionFile{}}
	if r.files == nil {
		return page, nil
	}
	limit := defaultMinionFilePage
	if last != nil {
		limit = min(max(*last, 1), maxMinionFilePage)
	}
	var cursor *repository.MinionFileCursor
	if before != nil && *before != "" {
		at, id, err := decodeTimeIDCursor(*before)
		if err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
		}
		cursor = &repository.MinionFileCursor{CreateAt: at, FileID: id}
	}
	files, err := r.files.Files(ctx, opID, kind, cursor, int64(limit)+1)
	if err != nil {
		return nil, fmt.Errorf("the operation's files: %w", err)
	}
	if len(files) > limit {
		files = files[:limit]
		next := encodeTimeIDCursor(files[len(files)-1].CreateAt, files[len(files)-1].FileID)
		page.Before = &next
	}
	for i := range files {
		page.Files = append(page.Files, minionFileModel(&files[i]))
	}
	return page, nil
}

// minionFileModel is a minion file as GraphQL gives it.
func minionFileModel(f *models.MinionFile) *model.MinionFile {
	out := &model.MinionFile{ID: f.FileID.String(), Name: f.Name, Kind: strPtr(f.Kind), Thumb: f.Thumb,
		Size: int(f.Total), Received: int(f.Received), State: f.State, Err: strPtr(f.Err), ContentType: strPtr(f.ContentType),
		Display: strPtr(f.Display), CreatedAt: f.CreateAt.UTC().Format(time.RFC3339Nano), DoneAt: taskTime(f.DoneAt)}
	if f.TaskID != uuid.Nil {
		id := f.TaskID.String()
		out.TaskID = &id
	}
	if f.SessionID != uuid.Nil {
		id := f.SessionID.String()
		out.SessionID = &id
	}
	if f.Width > 0 && f.Height > 0 {
		w, h := int(f.Width), int(f.Height)
		out.Width, out.Height = &w, &h
	}
	if len(f.SHA256) > 0 {
		h := hex.EncodeToString(f.SHA256)
		out.Sha256 = &h
	}
	return out
}
