package resolver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/processlist"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
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
	for i, row := range page.Rows {
		out.Edges = append(out.Edges, &model.MinionProcessEdge{Node: processModel(row), Cursor: page.Cursor(i)})
	}
	if n := len(out.Edges); n > 0 {
		out.PageInfo.StartCursor, out.PageInfo.EndCursor = &out.Edges[0].Cursor, &out.Edges[n-1].Cursor
	}
	return out, nil
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
