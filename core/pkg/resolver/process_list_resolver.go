package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/processlist"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// A process_list task's processes as its view pages them (docs:
// contracts/task-results-and-actions.md, process_list): read from the
// task's parts, ordered, searched and paged by core (pkg/processlist).

// At most a page of 2000: after a fold the view reads again, in one page,
// as many rows as it had, so it stays where it was.
const (
	defaultProcessPage = 50
	maxProcessPage     = 2000
)

// MinionTaskProcesses is a page of a process_list task's processes, for a
// viewer of its session's operation; this session's minion's own marked by
// its latest hello's pid; folded as fold and flip say.
func (r *minionTaskResolver) MinionTaskProcesses(ctx context.Context, taskID string, search *string, order *model.MinionProcessOrder,
	desc *bool, first *int, after *string, fold *model.MinionProcessFold, flip []int) (*model.MinionProcessConnection, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "task not found")
		}
		return nil, err
	}
	s, err := r.session(ctx, t.SessionID.String(), models.OperationRoleViewer)
	if err != nil {
		return nil, err
	}
	if t.Result != protocol.ResultProcessList {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "the task's result is not a process_list")
	}
	q := processlist.Query{Order: processlist.OrderTree, First: defaultProcessPage, MinionPID: s.Hello.PID}
	if first != nil {
		if *first <= 0 {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "'first' must be positive")
		}
		q.First = min(*first, maxProcessPage)
	}
	if search != nil {
		q.Search = *search
	}
	if order != nil {
		q.Order = processlist.Order(*order)
	}
	if desc != nil {
		q.Desc = *desc
	}
	if after != nil {
		q.After = *after
	}
	if fold != nil {
		q.Fold = processlist.Fold(*fold)
	}
	if len(flip) > 0 {
		q.Flip = make(map[uint64]bool, len(flip))
		for _, pid := range flip {
			if pid >= 0 {
				q.Flip[uint64(pid)] = true
			}
		}
	}
	parts, err := r.parts(ctx, taskID)
	if err != nil {
		return nil, err
	}
	sep, procs, err := processlist.Read(parts)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "the task's data does not fit process_list: "+err.Error())
	}
	page, err := processlist.List(procs, q)
	if errors.Is(err, processlist.ErrBadCursor) {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
	}
	if err != nil {
		return nil, err
	}
	out := &model.MinionProcessConnection{Edges: []*model.MinionProcessEdge{}, TotalCount: page.Total, Sep: sep, Hidden: page.Hidden,
		SystemTrees: page.SystemTrees,
		PageInfo:    &pagination.PageInfo{HasNextPage: page.More, HasPreviousPage: q.After != ""}}
	marks := r.callerProcessMarks(ctx)
	for i, row := range page.Rows {
		node := processModel(row)
		node.Mark = r.processMarkFor(marks, row.Name)
		_, node.BuiltinMark = r.builtinMarks.Of(row.Name)
		out.Edges = append(out.Edges, &model.MinionProcessEdge{Node: node, Cursor: page.Cursor(i)})
	}
	if n := len(out.Edges); n > 0 {
		out.PageInfo.StartCursor, out.PageInfo.EndCursor = &out.Edges[0].Cursor, &out.Edges[n-1].Cursor
	}
	return out, nil
}

// WithTaskProcessMarks gives the task resolver the users' marks on
// processes by name, and the deployment's own (builtins): process lists show
// the caller's, else the built-in one, and setMinionProcessMark sets theirs.
func WithTaskProcessMarks(r IMinionTaskResolver, marks repository.IMinionProcessMarkRepository, builtins models.BuiltinProcessMarks) IMinionTaskResolver {
	if tr, ok := r.(*minionTaskResolver); ok {
		tr.processMarks, tr.builtinMarks = marks, builtins
	}
	return r
}

// callerProcessMarks are the caller's marks on processes, by key
// (models.ProcessMarkKey): read once for a page. None when they do not read:
// the list shows unmarked rather than failing.
func (r *minionTaskResolver) callerProcessMarks(ctx context.Context) map[string]models.MinionProcessMark {
	if r.processMarks == nil {
		return nil
	}
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil
	}
	marks, err := r.processMarks.ForUser(ctx, uid)
	if err != nil {
		logger.From(ctx).Warn("process marks: read", zap.Error(err))
		return nil
	}
	return marks
}

// processMarkFor is the mark the processes of a name show the caller: their
// own (an empty one, a clearing: none, the built-in mark hidden too); else
// the name's built-in mark; nil for none.
func (r *minionTaskResolver) processMarkFor(own map[string]models.MinionProcessMark, name string) *model.MinionProcessMark {
	if m, ok := own[models.ProcessMarkKey(name)]; ok {
		if m.Empty() {
			return nil
		}
		return &model.MinionProcessMark{Color: strPtr(m.Color), Icon: strPtr(m.Icon), Emoji: strPtr(m.Emoji)}
	}
	if b, ok := r.builtinMarks.Of(name); ok {
		return &model.MinionProcessMark{Color: strPtr(b.Color), Icon: strPtr(b.Icon), Emoji: strPtr(b.Emoji), Builtin: true}
	}
	return nil
}

// markerOf is the caller of a mark's change and the name it is for: a
// person (not an agent key: a mark is a person's), a name of 1 to 255
// bytes.
func (r *minionTaskResolver) markerOf(ctx context.Context, name string) (uuid.UUID, string, error) {
	if gqlctx.AuthFromContext(ctx).Agent != nil || r.processMarks == nil {
		return uuid.Nil, "", moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > models.MaxProcessMarkName {
		return uuid.Nil, "", moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("name: a process's name, 1 to %d bytes", models.MaxProcessMarkName))
	}
	uid, err := callerUserID(ctx)
	return uid, name, err
}

// SetMinionProcessMark marks the processes of a name for the caller alone,
// in every process list, in place of the name's built-in mark. An empty
// mark clears it: kept, where it hides a built-in mark; removed, where there
// is none to hide. It returns the mark that shows, nil for none.
func (r *minionTaskResolver) SetMinionProcessMark(ctx context.Context, name string, mark *model.MinionSessionMarkInput) (*model.MinionProcessMark, error) {
	uid, name, err := r.markerOf(ctx, name)
	if err != nil {
		return nil, err
	}
	color, icon, emoji := markOf(mark)
	if why := models.ValidateSessionMark(color, icon, emoji); why != "" {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "mark: "+why)
	}
	own := models.MinionProcessMark{Name: name, Color: color, Icon: icon, Emoji: emoji}
	if _, builtin := r.builtinMarks.Of(name); own.Empty() && !builtin {
		err = r.processMarks.Remove(ctx, uid, name)
	} else {
		err = r.processMarks.Set(ctx, uid, name, color, icon, emoji)
	}
	if err != nil {
		return nil, fmt.Errorf("mark the processes: %w", err)
	}
	return r.processMarkFor(map[string]models.MinionProcessMark{models.ProcessMarkKey(name): own}, name), nil
}

// ResetMinionProcessMark takes the caller's own mark on a name away, or
// their clearing of it: the name shows its built-in mark again, which it
// returns (nil for a name with none).
func (r *minionTaskResolver) ResetMinionProcessMark(ctx context.Context, name string) (*model.MinionProcessMark, error) {
	uid, name, err := r.markerOf(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := r.processMarks.Remove(ctx, uid, name); err != nil {
		return nil, fmt.Errorf("reset the processes' mark: %w", err)
	}
	return r.processMarkFor(nil, name), nil
}

// processModel is a row as GraphQL has it: its start time RFC 3339, its x
// by the JSON mapping of task data.
func processModel(row processlist.Row) *model.MinionProcess {
	p := row.Process
	m := &model.MinionProcess{Pid: int(p.PID), Name: p.Name, Path: strPtr(p.Path), Cmdline: strPtr(p.Cmdline), User: strPtr(p.User),
		Integrity: strPtr(p.Integrity), Arch: strPtr(p.Arch), State: strPtr(p.State), Depth: row.Depth, Children: row.Children,
		Minion: row.Minion, Context: row.Context, Ppid: intPtrOf(p.PPID), Session: intPtrOf(p.Session), Threads: intPtrOf(p.Threads),
		Memory: intPtrOf(p.Memory), CPUTime: intPtrOf(p.CPUTime), Last: row.Last, Guides: append([]bool{}, row.Guides...), Hidden: row.Hidden,
		Stem: row.Stem}
	if p.Started != nil {
		at := time.Unix(*p.Started, 0).UTC().Format(time.RFC3339)
		m.Started = &at
	}
	if len(p.X) > 0 {
		if x, ok := taskdata.Value(p.X).(map[string]any); ok {
			m.X = x
		}
	}
	return m
}

// intPtrOf is a count as GraphQL's Int; nil for none.
func intPtrOf(n *uint64) *int {
	if n == nil {
		return nil
	}
	v := int(*n)
	return &v
}
