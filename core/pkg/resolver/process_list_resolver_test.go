package resolver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// taskParts is oneTask with its parts.
type taskParts struct {
	oneTask
	parts []models.MinionTaskPart
}

func (o taskParts) Parts(context.Context, uuid.UUID) ([]models.MinionTaskPart, error) {
	return o.parts, nil
}

// A process_list task's processes, a page at a time, for a viewer of its
// operation: this session's minion marked by its hello's pid, its start
// time RFC 3339, its x by the JSON mapping; ordered and searched; refused
// for one not in the operation, for a task of another result, a page of
// none and a cursor that does not read.
func TestMinionTaskProcesses(t *testing.T) {
	viewer := uuid.New()
	op := models.Operation{OperationID: uuid.New(), Members: []models.OperationMember{{UserID: viewer, Role: models.OperationRoleViewer}}}
	session := models.MinionSession{SessionID: uuid.New(), OperationID: op.OperationID, Hello: models.MinionHello{PID: 812}}
	task := models.MinionTask{TaskID: uuid.New(), SessionID: session.SessionID, OperationID: op.OperationID, Result: protocol.ResultProcessList}
	one, started := uint64(1), int64(1793347920)
	data, err := protocol.ResultData(protocol.ProcessList{Sep: "/", Processes: []protocol.Process{
		{PID: 1, Name: "systemd", User: "root"},
		{PID: 812, PPID: &one, Name: "bob-minion", Started: &started, Integrity: "root", X: map[string]any{"cgroup": []byte{1, 2}}},
		{PID: 900, PPID: &one, Name: "bash"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := &minionTaskResolver{tasks: taskParts{oneTask: oneTask{task: task}, parts: []models.MinionTaskPart{{Seq: 0, DataCBOR: data}}},
		sessions:      oneSession{session: session},
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) { return op, nil }}}
	as := func(user uuid.UUID) context.Context {
		return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: user.String(), Roles: []string{"user"}})
	}

	two := 2
	page, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, &two, nil, nil, nil)
	if err != nil || page.TotalCount != 3 || page.Sep != "/" || len(page.Edges) != 2 || !page.PageInfo.HasNextPage || page.PageInfo.HasPreviousPage {
		t.Fatalf("first page %+v %v", page, err)
	}
	root, minion := page.Edges[0].Node, page.Edges[1].Node
	if root.Name != "systemd" || root.Depth != 0 || root.Children != 2 || root.Minion || root.Started != nil || root.Ppid != nil {
		t.Fatalf("the root %+v", root)
	}
	if minion.Pid != 812 || !minion.Minion || minion.Depth != 1 || *minion.Started != "2026-10-30T08:12:00Z" || *minion.Integrity != "root" ||
		*minion.Ppid != 1 || minion.X.(map[string]any)["cgroup"].(map[string]any)["$bytes"] != "AQI=" {
		t.Fatalf("the minion %+v", minion)
	}
	rest, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, &two, page.PageInfo.EndCursor, nil, nil)
	if err != nil || len(rest.Edges) != 1 || rest.Edges[0].Node.Name != "bash" || rest.PageInfo.HasNextPage {
		t.Fatalf("the page after %+v %v", rest, err)
	}
	// The tree's lines across pages: the minion, the first of systemd's
	// two, and bash, its last on the next page; a root's none.
	if root.Last || root.Guides == nil || len(root.Guides) != 0 || minion.Last || len(minion.Guides) != 0 || !rest.Edges[0].Node.Last {
		t.Fatalf("lines: root %v %v, minion %v %v, bash %v", root.Last, root.Guides, minion.Last, minion.Guides, rest.Edges[0].Node.Last)
	}
	// Folded: systemd (its pid flipped), its two hidden; Fold All folds it
	// too, its flip unfolding it.
	none, all := model.MinionProcessFoldNone, model.MinionProcessFoldAll
	folded, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, nil, nil, &none, []int{1})
	if err != nil || len(folded.Edges) != 1 || folded.Edges[0].Node.Hidden != 2 || folded.Hidden != 2 || folded.TotalCount != 3 ||
		folded.Edges[0].Node.Stem || folded.SystemTrees != 0 {
		t.Fatalf("folded %+v %v", folded, err)
	}
	if unfolded, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, nil, nil, &all, []int{1}); err != nil ||
		len(unfolded.Edges) != 3 || !unfolded.Edges[0].Node.Stem || unfolded.Hidden != 0 {
		t.Fatalf("fold all, systemd flipped %+v %v", unfolded, err)
	}
	byName, yes, search := model.MinionProcessOrderName, true, "b"
	sorted, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), &search, &byName, &yes, nil, nil, nil, nil)
	if err != nil || sorted.TotalCount != 2 || sorted.Edges[0].Node.Name != "bob-minion" || sorted.Edges[1].Node.Name != "bash" {
		t.Fatalf("searched, by name the other way %+v %v", sorted, err)
	}

	if _, err := r.MinionTaskProcesses(as(uuid.New()), task.TaskID.String(), nil, nil, nil, nil, nil, nil, nil); err == nil {
		t.Error("listed for one not in the operation")
	}
	zero, bad := 0, "x"
	if _, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, &zero, nil, nil, nil); codeOf(err) != errCodeBadUserInput {
		t.Errorf("first 0: %v", err)
	}
	if _, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, nil, &bad, nil, nil); codeOf(err) != errCodeBadUserInput {
		t.Errorf("a bad cursor: %v", err)
	}
	other := task
	other.Result = protocol.ResultVolumes
	r.tasks = taskParts{oneTask: oneTask{task: other}}
	if _, err := r.MinionTaskProcesses(as(viewer), task.TaskID.String(), nil, nil, nil, nil, nil, nil, nil); codeOf(err) != errCodeBadUserInput {
		t.Errorf("a volumes task: %v", err)
	}
}
