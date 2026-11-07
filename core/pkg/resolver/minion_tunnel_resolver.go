package resolver

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Tunnel tasks' proxies as the web app shows them (docs:
// contracts/minion-messages/tunnels.md, What core keeps and shows): a
// task's tunnel (its port, its streams' numbers), its streams a page at a
// time, and the operation's operational ports in use.

const (
	defaultTunnelStreamPage = 50
	maxTunnelStreamPage     = 200
)

// Tunnel is a tunnel task's proxy: live while core listens for it, from its
// streams' records once it ended. The task's port is its args' (core put it
// there when it queued the task).
func (r *minionTaskResolver) Tunnel(ctx context.Context, t *model.MinionTask) (*model.MinionTunnel, error) {
	if r.tunnels == nil {
		return nil, nil
	}
	id, err := uuid.Parse(t.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID")
	}
	sum, listening, err := r.tunnels.View(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("the task's tunnel: %w", err)
	}
	args, _ := t.Args.(map[string]any)
	port := sum.Port
	if !listening {
		if p, ok := protocol.ArgInt(args, "port"); ok {
			port = int(p)
		}
	}
	m := &model.MinionTunnel{Port: port, Address: strPtr(r.tunnels.Address()), Purpose: models.OperationalPurposeSOCKS, Listening: listening,
		Open: sum.Open, Streams: int(sum.Streams), Kept: miniontunnels.KeptStreams, Up: int(sum.Up), Down: int(sum.Down), StopsAt: taskTime(stopsAt(sum))}
	if t.Action != nil && (*t.Action == protocol.ActionPortForward || *t.Action == protocol.ActionReversePortForward) {
		// Its destination, as its args say (core checked them when it
		// queued the task).
		m.Purpose = models.OperationalPurposeForward
		host, _ := protocol.ArgString(args, "host")
		if to, ok := protocol.ArgInt(args, "to"); ok && host != "" {
			dest := net.JoinHostPort(host, strconv.FormatInt(to, 10))
			m.To = &dest
		}
	}
	if t.Action != nil && *t.Action == protocol.ActionReversePortForward {
		// No operational port: the minion listens, where its args ask and
		// where it says it does.
		m.Purpose, m.Port, m.Address = models.OperationalPurposeReverse, 0, nil
		bind, _ := protocol.ArgString(args, "bind")
		m.Bind = strPtr(bind)
		if l, ok := protocol.ArgInt(args, "listen"); ok {
			n := int(l)
			m.Listen = &n
		}
		m.Bound = strPtr(sum.Bound)
		m.Listening = listening && sum.Bound != ""
		udp, _ := protocol.ArgBool(args, "udp")
		m.UDP = &udp
	}
	return m, nil
}

// MinionTunnelStreams is a page of a tunnel task's streams, the latest
// first, for a member of its session's operation.
func (r *minionTaskResolver) MinionTunnelStreams(ctx context.Context, taskID string, last *int, before, search, state *string) (*model.MinionTunnelStreamPage, error) {
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
	if _, err := r.session(ctx, t.SessionID.String(), models.OperationRoleViewer); err != nil {
		return nil, err
	}
	page := &model.MinionTunnelStreamPage{Streams: []*model.MinionTunnelStream{}}
	if r.tunnels == nil || !protocol.IsTunnelAction(t.Action) {
		return page, nil
	}
	limit := defaultTunnelStreamPage
	if last != nil {
		limit = min(max(*last, 1), maxTunnelStreamPage)
	}
	var from uint64
	if before != nil && *before != "" {
		if from, err = strconv.ParseUint(*before, 10, 64); err != nil || from == 0 {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
		}
	}
	filter := miniontunnels.StreamFilter{}
	if search != nil {
		filter.Search = strings.TrimSpace(*search)
	}
	if state != nil {
		switch strings.ToUpper(*state) {
		case "OPEN":
			filter.State = miniontunnels.StreamsOpen
		case "ENDED":
			filter.State = miniontunnels.StreamsEnded
		case "":
		default:
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "state is OPEN or ENDED")
		}
	}
	recs, err := r.tunnels.Streams(ctx, tid, filter, from, int64(limit)+1)
	if err != nil {
		return nil, fmt.Errorf("the task's streams: %w", err)
	}
	if len(recs) > limit {
		recs = recs[:limit]
		next := strconv.FormatUint(recs[len(recs)-1].Stream, 10)
		page.Before = &next
	}
	for i := range recs {
		page.Streams = append(page.Streams, tunnelStreamModel(&recs[i]))
	}
	return page, nil
}

// tunnelStreamModel is a stream as GraphQL has it.
func tunnelStreamModel(s *models.MinionTunnelStream) *model.MinionTunnelStream {
	m := &model.MinionTunnelStream{Stream: int(s.Stream), Client: s.Client, Host: s.Host, Port: s.Port, UDP: s.UDP, OpenedAt: s.OpenedAt.UTC().Format(time.RFC3339Nano),
		State: "OPENING", Bound: strPtr(s.Bound), Msg: strPtr(s.Msg), EndedAt: taskTime(s.EndedAt), End: strPtr(s.End), Up: int(s.Up), Down: int(s.Down)}
	if s.Result != nil {
		name := protocol.TunnelResultName(*s.Result)
		m.Result, m.State = &name, "OPEN"
	}
	if s.EndedAt != nil {
		m.State = "ENDED"
	}
	return m
}

// OperationalPorts is a page of an operation's operational ports in use,
// for a member of it: each one's task's tunnel as it is now.
func (r *minionTaskResolver) OperationalPorts(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.OperationalPortConnection, error) {
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, opID)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}
	if err := authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleViewer); err != nil {
		return nil, err
	}
	empty := &model.OperationalPortConnection{Edges: []*model.OperationalPortEdge{}, PageInfo: &pagination.PageInfo{}}
	if r.tunnels == nil {
		return empty, nil
	}
	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("invalid pagination args: %v", err))
	}
	q := ""
	if search != nil {
		q = strings.TrimSpace(*search)
	}
	total, err := r.tunnels.CountPorts(ctx, opID, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.tunnels.Ports(ctx, opID, q, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(p *models.OperationalPort) string { return repository.OperationalPortCursor(p) },
		func(p *models.OperationalPort, cursor string) *model.OperationalPortEdge {
			return &model.OperationalPortEdge{Node: r.portModel(p), Cursor: cursor}
		})
	return &model.OperationalPortConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

// portModel is a port as GraphQL has it: its task's tunnel as it is now.
func (r *minionTaskResolver) portModel(p *models.OperationalPort) *model.OperationalPort {
	var to *string
	if p.Host != "" {
		dest := net.JoinHostPort(p.Host, strconv.Itoa(p.HostPort))
		to = &dest
	}
	node := &model.OperationalPort{ID: p.TaskID.String(), Port: p.Port, Purpose: p.Purpose, To: to, Address: strPtr(r.tunnels.Address()),
		SessionID: p.SessionID.String(), SessionNumber: p.SessionNumber, TaskID: p.TaskID.String(), OpenedBy: p.OpenedBy.String(),
		OpenedByName: strPtr(p.OpenedByName), OpenedAt: p.OpenedAt.UTC().Format(time.RFC3339Nano), Streams: int(p.Streams)}
	if sum, ok := r.tunnels.Summary(p.TaskID); ok {
		node.Listening, node.Open, node.Streams = true, sum.Open, int(max(sum.Streams, p.Streams))
		node.StopsAt = taskTime(stopsAt(sum))
	}
	return node
}

// stopsAt is when core stops a tunnel unless a stream opens first; nil for
// not now.
func stopsAt(sum miniontunnels.Summary) *time.Time {
	if sum.StopsAt.IsZero() {
		return nil
	}
	return &sum.StopsAt
}

// MinionListeners is a page of an operation's reverse port forwards'
// listeners, for a member of it: each one's task's tunnel as it is now.
func (r *minionTaskResolver) MinionListeners(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.MinionListenerConnection, error) {
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, opID)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}
	if err := authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleViewer); err != nil {
		return nil, err
	}
	empty := &model.MinionListenerConnection{Edges: []*model.MinionListenerEdge{}, PageInfo: &pagination.PageInfo{}}
	if r.tunnels == nil {
		return empty, nil
	}
	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("invalid pagination args: %v", err))
	}
	q := ""
	if search != nil {
		q = strings.TrimSpace(*search)
	}
	total, err := r.tunnels.CountListeners(ctx, opID, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.tunnels.Listeners(ctx, opID, q, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(l *models.MinionListener) string { return repository.MinionListenerCursor(l) },
		func(l *models.MinionListener, cursor string) *model.MinionListenerEdge {
			return &model.MinionListenerEdge{Node: r.listenerModel(l), Cursor: cursor}
		})
	return &model.MinionListenerConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

// listenerModel is a listener as GraphQL has it: its task's tunnel as it is
// now.
func (r *minionTaskResolver) listenerModel(l *models.MinionListener) *model.MinionListener {
	node := &model.MinionListener{ID: l.TaskID.String(), Bind: l.Bind, Listen: l.Listen, Bound: strPtr(l.Bound), UDP: l.UDP,
		To: net.JoinHostPort(l.Host, strconv.Itoa(l.HostPort)), SessionID: l.SessionID.String(), SessionNumber: l.SessionNumber,
		TaskID: l.TaskID.String(), OpenedBy: l.OpenedBy.String(), OpenedByName: strPtr(l.OpenedByName),
		OpenedAt: l.OpenedAt.UTC().Format(time.RFC3339Nano), Streams: int(l.Streams)}
	if sum, ok := r.tunnels.Summary(l.TaskID); ok {
		node.Open, node.Streams = sum.Open, int(max(sum.Streams, l.Streams))
		if sum.Bound != "" {
			node.Bound = strPtr(sum.Bound)
		}
		node.Listening = sum.Bound != ""
		node.StopsAt = taskTime(stopsAt(sum))
	}
	return node
}

// LoadListener is a task's reverse port forward's listener as GraphQL has
// it, for a subscriber (whose operation the subscription checked): nil when
// it has none.
func (r *minionTaskResolver) LoadListener(ctx context.Context, taskID string) *model.MinionListener {
	id, err := uuid.Parse(taskID)
	if err != nil || r.tunnels == nil {
		return nil
	}
	l, err := r.tunnels.Listener(ctx, id)
	if err != nil {
		return nil
	}
	return r.listenerModel(l)
}

// LoadPort is a task's port as GraphQL has it, for a subscriber (whose
// operation the subscription checked): nil when it has none.
func (r *minionTaskResolver) LoadPort(ctx context.Context, taskID string) *model.OperationalPort {
	id, err := uuid.Parse(taskID)
	if err != nil || r.tunnels == nil {
		return nil
	}
	p, err := r.tunnels.Port(ctx, id)
	if err != nil {
		return nil
	}
	return r.portModel(p)
}
