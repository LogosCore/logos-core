package resolver

import (
	"context"
	"maps"
	"strings"
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

// fakeProcessMarks are users' marks on process names, in memory, keyed as
// the repository keys them: an empty one kept, as Set keeps it.
type fakeProcessMarks struct {
	marks map[uuid.UUID]map[string]models.MinionProcessMark
}

func (f *fakeProcessMarks) Set(_ context.Context, uid uuid.UUID, name, color, icon, emoji string) error {
	key := models.ProcessMarkKey(name)
	if f.marks[uid] == nil {
		f.marks[uid] = map[string]models.MinionProcessMark{}
	}
	f.marks[uid][key] = models.MinionProcessMark{UserID: uid, Key: key, Name: name, Color: color, Icon: icon, Emoji: emoji}
	return nil
}

func (f *fakeProcessMarks) Remove(_ context.Context, uid uuid.UUID, name string) error {
	delete(f.marks[uid], models.ProcessMarkKey(name))
	return nil
}

func (f *fakeProcessMarks) ForUser(_ context.Context, uid uuid.UUID) (map[string]models.MinionProcessMark, error) {
	return f.marks[uid], nil
}

func (f *fakeProcessMarks) RemoveOrphans(context.Context) (int64, error) { return 0, nil }

// A user's mark on a process name shows on the processes of that name,
// whatever its case, in that user's lists alone; an empty mark removes it.
// A mark that does not check, a name of none or past 255 bytes, and an agent
// key, are refused.
func TestMinionProcessMarks(t *testing.T) {
	ann, bob := uuid.New(), uuid.New()
	op := models.Operation{OperationID: uuid.New(), Members: []models.OperationMember{
		{UserID: ann, Role: models.OperationRoleViewer}, {UserID: bob, Role: models.OperationRoleViewer}}}
	session := models.MinionSession{SessionID: uuid.New(), OperationID: op.OperationID}
	task := models.MinionTask{TaskID: uuid.New(), SessionID: session.SessionID, OperationID: op.OperationID, Result: protocol.ResultProcessList}
	one := uint64(1)
	data, err := protocol.ResultData(protocol.ProcessList{Sep: "\\", Processes: []protocol.Process{
		{PID: 4, Name: "System"}, {PID: 900, PPID: &one, Name: "firefox.exe"}, {PID: 901, PPID: &one, Name: "Firefox.EXE"}}})
	if err != nil {
		t.Fatal(err)
	}
	marks := &fakeProcessMarks{marks: map[uuid.UUID]map[string]models.MinionProcessMark{}}
	r := &minionTaskResolver{tasks: taskParts{oneTask: oneTask{task: task}, parts: []models.MinionTaskPart{{Seq: 0, DataCBOR: data}}},
		sessions: oneSession{session: session}, processMarks: marks,
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) { return op, nil }}}
	as := func(user uuid.UUID) context.Context {
		return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: user.String(), Roles: []string{"user"}})
	}
	marked := func(user uuid.UUID) map[string]string {
		page, err := r.MinionTaskProcesses(as(user), task.TaskID.String(), nil, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range page.Edges {
			if m := e.Node.Mark; m != nil {
				out[e.Node.Name] = deref(m.Color) + deref(m.Emoji) + deref(m.Icon)
			}
		}
		return out
	}

	red, fox := "red-strong", "🦊"
	got, err := r.SetMinionProcessMark(as(ann), " FIREFOX.exe ", &model.MinionSessionMarkInput{Color: &red, Emoji: &fox})
	if err != nil || got == nil || deref(got.Color) != red || deref(got.Emoji) != fox || got.Icon != nil {
		t.Fatalf("set: %+v %v", got, err)
	}
	if m := marks.marks[ann]["firefox.exe"]; m.Name != "FIREFOX.exe" {
		t.Fatalf("kept as %+v", m)
	}
	if got := marked(ann); !maps.Equal(got, map[string]string{"firefox.exe": red + fox, "Firefox.EXE": red + fox}) {
		t.Fatalf("ann's list: %v", got)
	}
	if got := marked(bob); len(got) != 0 {
		t.Fatalf("bob sees ann's marks: %v", got)
	}
	if got, err := r.SetMinionProcessMark(as(ann), "firefox.exe", nil); err != nil || got != nil || len(marked(ann)) != 0 {
		t.Fatalf("cleared: %+v %v, list %v", got, err, marked(ann))
	}

	purple, flame := "purple-mega", "flame"
	for name, c := range map[string]struct {
		name string
		mark *model.MinionSessionMarkInput
	}{
		"a color it has not":   {"x", &model.MinionSessionMarkInput{Color: &purple}},
		"an icon and an emoji": {"x", &model.MinionSessionMarkInput{Icon: &flame, Emoji: &fox}},
		"no name":              {"  ", &model.MinionSessionMarkInput{Color: &red}},
		"a name past 255":      {strings.Repeat("x", 256), &model.MinionSessionMarkInput{Color: &red}},
	} {
		if _, err := r.SetMinionProcessMark(as(ann), c.name, c.mark); codeOf(err) != errCodeBadUserInput {
			t.Errorf("%s: %v", name, err)
		}
	}
	agent := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: ann.String(), Roles: []string{"user"},
		Agent: &gqlctx.AgentInfo{AgentKeyID: "a-1"}})
	if _, err := r.SetMinionProcessMark(agent, "x", &model.MinionSessionMarkInput{Color: &red}); codeOf(err) != errCodeForbidden {
		t.Errorf("an agent key marked: %v", err)
	}
}

// A built-in mark shows on its name's processes to every user who has none
// of their own, said built-in; one's own mark takes its place; clearing it
// shows none, kept as an empty mark (another user still sees the built-in);
// a reset goes back to it. Clearing a name with no built-in mark keeps
// nothing.
func TestMinionProcessBuiltinMarks(t *testing.T) {
	builtins, skipped, err := models.ParseBuiltinProcessMarks([]byte(`[{"name": "System", "color": "gold", "emoji": "⚙️"}]`))
	if err != nil || len(skipped) != 0 {
		t.Fatal(err, skipped)
	}

	ann, bob := uuid.New(), uuid.New()
	op := models.Operation{OperationID: uuid.New(), Members: []models.OperationMember{
		{UserID: ann, Role: models.OperationRoleViewer}, {UserID: bob, Role: models.OperationRoleViewer}}}
	session := models.MinionSession{SessionID: uuid.New(), OperationID: op.OperationID}
	task := models.MinionTask{TaskID: uuid.New(), SessionID: session.SessionID, OperationID: op.OperationID, Result: protocol.ResultProcessList}
	data, err := protocol.ResultData(protocol.ProcessList{Sep: "\\", Processes: []protocol.Process{{PID: 4, Name: "System"}, {PID: 812, Name: "sshd"}}})
	if err != nil {
		t.Fatal(err)
	}
	marks := &fakeProcessMarks{marks: map[uuid.UUID]map[string]models.MinionProcessMark{}}
	r := &minionTaskResolver{tasks: taskParts{oneTask: oneTask{task: task}, parts: []models.MinionTaskPart{{Seq: 0, DataCBOR: data}}},
		sessions: oneSession{session: session}, processMarks: marks, builtinMarks: builtins,
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) { return op, nil }}}
	as := func(user uuid.UUID) context.Context {
		return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: user.String(), Roles: []string{"user"}})
	}
	// Each row as "<color><emoji>" and "*" for a built-in, "-" for none; and
	// whether its name has a built-in mark.
	shown := func(user uuid.UUID) map[string]string {
		page, err := r.MinionTaskProcesses(as(user), task.TaskID.String(), nil, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range page.Edges {
			v := "-"
			if m := e.Node.Mark; m != nil {
				v = deref(m.Color) + deref(m.Emoji) + map[bool]string{true: "*", false: ""}[m.Builtin]
			}
			out[e.Node.Name] = v + map[bool]string{true: " has", false: ""}[e.Node.BuiltinMark]
		}
		return out
	}

	if got := shown(ann); !maps.Equal(got, map[string]string{"System": "gold⚙️* has", "sshd": "-"}) {
		t.Fatalf("the built-in: %v", got)
	}
	blue := "blue-soft"
	if got, err := r.SetMinionProcessMark(as(ann), "system", &model.MinionSessionMarkInput{Color: &blue}); err != nil || got == nil || got.Builtin ||
		deref(got.Color) != blue || !maps.Equal(shown(ann), map[string]string{"System": "blue-soft has", "sshd": "-"}) {
		t.Fatalf("ann's own: %+v %v, %v", got, err, shown(ann))
	}
	if got, err := r.SetMinionProcessMark(as(ann), "SYSTEM", nil); err != nil || got != nil ||
		!maps.Equal(shown(ann), map[string]string{"System": "- has", "sshd": "-"}) || !marks.marks[ann]["system"].Empty() {
		t.Fatalf("cleared: %+v %v, %v", got, err, shown(ann))
	}
	if got := shown(bob); got["System"] != "gold⚙️* has" {
		t.Fatalf("bob: %v", got)
	}
	if got, err := r.ResetMinionProcessMark(as(ann), "System"); err != nil || got == nil || !got.Builtin || deref(got.Color) != "gold" ||
		shown(ann)["System"] != "gold⚙️* has" {
		t.Fatalf("reset: %+v %v, %v", got, err, shown(ann))
	}
	if _, err := r.SetMinionProcessMark(as(ann), "sshd", nil); err != nil || len(marks.marks[ann]) != 0 {
		t.Fatalf("a name with none, cleared: %v %v", marks.marks[ann], err)
	}
	if got, err := r.ResetMinionProcessMark(as(ann), "sshd"); err != nil || got != nil {
		t.Fatalf("a name with none, reset: %+v %v", got, err)
	}
}
