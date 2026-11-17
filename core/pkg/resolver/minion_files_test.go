package resolver

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// fakeKindFiles are an operation's files in memory, listed as the
// repository's ListByKind lists them: newest first, after a cursor.
type fakeKindFiles struct {
	TaskFiles
	files []models.MinionFile
}

func (f *fakeKindFiles) Files(_ context.Context, op uuid.UUID, kind string, before *repository.MinionFileCursor, limit int64) ([]models.MinionFile, error) {
	var out []models.MinionFile
	for _, x := range f.files {
		after := before == nil || x.CreateAt.Before(before.CreateAt) ||
			(x.CreateAt.Equal(before.CreateAt) && x.FileID.String() < before.FileID.String())
		if x.OperationID == op && x.Kind == kind && after {
			out = append(out, x)
		}
	}
	slices.SortFunc(out, func(a, b models.MinionFile) int {
		if c := b.CreateAt.Compare(a.CreateAt); c != 0 {
			return c
		}
		return -compareStrings(a.FileID.String(), b.FileID.String())
	})
	return out[:min(int64(len(out)), limit)], nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// An operation's screenshots, every session's, a page at a time, newest
// first, for a viewer of the operation: each with its task, session, size
// in pixels and thumbnail. Only the kinds core marks are asked for.
func TestMinionFiles(t *testing.T) {
	viewer := uuid.New()
	op := models.Operation{OperationID: uuid.New(), Members: []models.OperationMember{{UserID: viewer, Role: models.OperationRoleViewer}}}
	at := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	files := &fakeKindFiles{}
	for i := range 3 {
		files.files = append(files.files, models.MinionFile{FileID: uuid.New(), OperationID: op.OperationID, SessionID: uuid.New(),
			TaskID: uuid.New(), Kind: models.MinionFileKindScreenshot, Name: "screen.png", Total: 1000, Received: 1000,
			State: models.MinionFileComplete, ContentType: "image/png", Width: 1920, Height: 1080, Thumb: i != 1})
		files.files[i].CreateAt = at.Add(time.Duration(i) * time.Minute)
	}
	// A download is no screenshot.
	files.files = append(files.files, models.MinionFile{FileID: uuid.New(), OperationID: op.OperationID, Name: "/etc/hosts"})
	r := &minionTaskResolver{files: files,
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) { return op, nil }}}
	as := func(user uuid.UUID) context.Context {
		ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: user.String(), Roles: []string{"user"}})
		return gqlctx.WithOperationMemo(ctx)
	}

	two := 2
	page, err := r.MinionFiles(as(viewer), op.OperationID.String(), models.MinionFileKindScreenshot, &two, nil)
	if err != nil || len(page.Files) != 2 || page.Files[0].ID != files.files[2].FileID.String() ||
		page.Files[1].ID != files.files[1].FileID.String() || page.Before == nil {
		t.Fatalf("first page %+v %v", page, err)
	}
	got := page.Files[0]
	if got.Kind == nil || *got.Kind != "screenshot" || got.Width == nil || *got.Width != 1920 || *got.Height != 1080 ||
		!got.Thumb || page.Files[1].Thumb || got.TaskID == nil || *got.TaskID != files.files[2].TaskID.String() ||
		got.SessionID == nil || got.Display != nil {
		t.Errorf("a screenshot %+v", got)
	}
	page, err = r.MinionFiles(as(viewer), op.OperationID.String(), models.MinionFileKindScreenshot, &two, page.Before)
	if err != nil || len(page.Files) != 1 || page.Files[0].ID != files.files[0].FileID.String() || page.Before != nil {
		t.Fatalf("last page %+v %v", page, err)
	}

	bad := "x"
	if _, err := r.MinionFiles(as(viewer), op.OperationID.String(), models.MinionFileKindScreenshot, nil, &bad); err == nil {
		t.Error("a bad cursor")
	}
	if _, err := r.MinionFiles(as(viewer), op.OperationID.String(), "download", nil, nil); err == nil {
		t.Error("a kind core does not mark")
	}
	if _, err := r.MinionFiles(as(uuid.New()), op.OperationID.String(), models.MinionFileKindScreenshot, nil, nil); err == nil {
		t.Error("listed for one not in the operation")
	}
}
