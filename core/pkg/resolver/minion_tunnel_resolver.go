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
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Tunnel tasks' proxies as the web app shows them (docs:
// contracts/minion-messages/tunnels.md, What core keeps and shows): a
// task's tunnel (its port, its streams' numbers), its streams a page at a
// time, and the operation's operational ports in use.

const (
	defaultTunnelStreamPage = 50
	maxTunnelStreamPage     = 200
)

// Tunnel is a tunnel task's tunnel: live while core listens for it, what
// it came to once it ended. Its front, port and destination are its
// args' (core put them there when it queued the task).
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
	reverse := t.Action != nil && *t.Action == protocol.ActionReverseTunnel
	front, _ := protocol.ArgString(args, protocol.ParamFront)
	if front == "" {
		front = protocol.FrontSOCKS
		if reverse {
			front = protocol.FrontForward
		}
	}
	port := sum.Port
	if !listening {
		if p, ok := protocol.ArgInt(args, protocol.ParamPort); ok {
			port = int(p)
		}
	}
	capNow := sum.Cap
	if !listening {
		capNow, _ = protocol.ArgInt(args, protocol.ParamCap)
	}
	m := &model.MinionTunnel{Port: port, Address: strPtr(r.tunnels.Address()), Front: front, Reverse: reverse, Listening: listening,
		Open: sum.Open, Streams: int(sum.Streams), Kept: miniontunnels.KeptStreams, Up: int(sum.Up), Down: int(sum.Down),
		StopsAt: taskTime(stopsAt(sum)), Cap: int(max(capNow, 0))}
	if listening {
		// Its session's link, as its streams' acks measure it.
		if sum.RTT > 0 {
			ms := int(sum.RTT.Milliseconds())
			m.RttMs = &ms
		}
		ms := int(sum.ResendAfter.Milliseconds())
		m.ResendAfterMs = &ms
	}
	// Its limits, as core took them when it queued the task: its args',
	// else the defaults (openTunnel).
	m.MaxStreams = miniontunnels.DefaultMaxStreams
	if n, ok := protocol.ArgInt(args, "max_streams"); ok && n > 0 {
		m.MaxStreams = int(min(n, 1<<16))
	}
	m.TimeoutMs = int(miniontunnels.DefaultConnectTimeout.Milliseconds())
	if d, ok := protocol.ArgDuration(args, "timeout"); ok && d > 0 {
		m.TimeoutMs = int(d.Milliseconds())
	}
	if front == protocol.FrontForward || protocol.IsDesktopFront(front) {
		// Its destination, as its args say (core checked them when it
		// queued the task): a host and a port, or a path.
		host, _ := protocol.ArgString(args, protocol.ParamHost)
		path, _ := protocol.ArgString(args, protocol.ParamPath)
		if to, ok := protocol.ArgInt(args, protocol.ParamTo); ok && host != "" && path == "" {
			dest := net.JoinHostPort(host, strconv.FormatInt(to, 10))
			m.To = &dest
		}
		m.Path = strPtr(path)
		tlsOn, _ := protocol.ArgBool(args, protocol.ParamTLS)
		m.TLS = tlsOn && path == ""
	}
	if !reverse {
		source, _ := protocol.ArgString(args, protocol.ParamSource)
		m.Source = strPtr(source)
	}
	m.SocksBind = listening && front == protocol.FrontSOCKS && sum.Accept
	if protocol.IsDesktopFront(front) {
		m.Desktop = r.tunnels.Desktops()
		m.Login, m.LoginProblem = r.desktopLogin(ctx, t.OperationID, args)
	}
	if front == protocol.FrontTerminal {
		// Its program and size, as its args say (core put the defaults
		// there when it queued the task).
		program, _ := protocol.ArgString(args, protocol.ParamProgram)
		line, _ := protocol.ArgString(args, protocol.ParamArgs)
		m.Program, m.Args = strPtr(program), strPtr(line)
		if rows, ok := protocol.ArgInt(args, protocol.ParamRows); ok {
			n := int(rows)
			m.Rows = &n
		}
		if cols, ok := protocol.ArgInt(args, protocol.ParamCols); ok {
			n := int(cols)
			m.Cols = &n
		}
	}
	m.VpnClients = []*model.MinionVPNClient{}
	if front == protocol.FrontVPN {
		// No operational port: its clients come over core's own address.
		m.Port, m.Address = 0, nil
		routes := argStrings(args[protocol.ParamRoutes])
		if live, clients, ok := r.tunnels.VPN(id); ok {
			routes = live
			for _, c := range clients {
				m.VpnClients = append(m.VpnClients, &model.MinionVPNClient{ID: c.ID, Name: c.Name, User: c.User, Machine: c.Machine, Os: c.OS,
					Arch: c.Arch, Version: c.Version, From: c.From, Since: c.Since.UTC().Format(time.RFC3339Nano), Connected: c.Connected,
					Open: c.Open, Streams: int(c.Streams), Up: int(c.Up), Down: int(c.Down)})
			}
		}
		m.Routes = routes
	}
	if reverse {
		// No operational port: the minion listens, where its args ask and
		// where it says it does.
		m.Port, m.Address = 0, nil
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

// SetMinionTunnelCap changes a running tunnel task's bandwidth cap, bytes a
// second each way (0: none), for an operator of its session's operation.
func (r *minionTaskResolver) SetMinionTunnelCap(ctx context.Context, taskID string, limit int) (*model.MinionTask, error) {
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
	if _, err := r.session(ctx, t.SessionID.String(), models.OperationRoleOperator); err != nil {
		return nil, err
	}
	switch {
	case !protocol.IsTunnelAction(t.Action):
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "not a tunnel task")
	case limit < 0:
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "a cap is bytes a second, 0 for none")
	case r.tunnels == nil:
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "core opens no tunnels")
	}
	if err := r.tunnels.SetCap(ctx, tid, int64(limit)); err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "the tunnel is not running")
		}
		return nil, fmt.Errorf("set the tunnel's cap: %w", err)
	}
	r.publish(t)
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
}

// vpnTask is a VPN tunnel task, for an operator of its session's
// operation.
func (r *minionTaskResolver) vpnTask(ctx context.Context, taskID string) (*models.MinionTask, uuid.UUID, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, tid, moduleGQLError(ctx, errCodeBadUserInput, "invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, tid, moduleGQLError(ctx, errCodeNotFound, "task not found")
		}
		return nil, tid, err
	}
	if _, err := r.session(ctx, t.SessionID.String(), models.OperationRoleOperator); err != nil {
		return nil, tid, err
	}
	front, _ := protocol.ArgString(t.Args, protocol.ParamFront)
	switch {
	case t.Action != protocol.ActionTunnel || front != protocol.FrontVPN:
		return nil, tid, moduleGQLError(ctx, errCodeBadUserInput, "not a VPN tunnel task")
	case r.tunnels == nil:
		return nil, tid, moduleGQLError(ctx, errCodeBadUserInput, "core opens no tunnels")
	}
	return t, tid, nil
}

// notRunning is a VPN tunnel's error when core holds none of it.
func notRunning(ctx context.Context, err error, what string) error {
	if repository.IsNotFound(err) {
		return moduleGQLError(ctx, errCodeBadUserInput, "the VPN tunnel is not running")
	}
	return fmt.Errorf("%s: %w", what, err)
}

// SetMinionTunnelRoutes changes a running VPN tunnel's routes, for an
// operator of its session's operation: kept with the task's args too.
func (r *minionTaskResolver) SetMinionTunnelRoutes(ctx context.Context, taskID string, routes []string) (*model.MinionTask, error) {
	t, tid, err := r.vpnTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if _, err := protocol.TunnelRoutes(routes); err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	kept, err := r.tunnels.SetRoutes(ctx, tid, routes)
	if err != nil {
		return nil, notRunning(ctx, err, "set the VPN's routes")
	}
	if err := r.tasks.SetArg(ctx, tid, protocol.ParamRoutes, kept); err != nil {
		return nil, fmt.Errorf("keep the VPN's routes: %w", err)
	}
	t.Args[protocol.ParamRoutes] = kept
	r.publish(t)
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
}

// CreateMinionVPNKey makes the caller a key for a client of a running VPN
// tunnel, for an operator of its session's operation.
func (r *minionTaskResolver) CreateMinionVPNKey(ctx context.Context, taskID string) (*model.MinionVPNKey, error) {
	_, tid, err := r.vpnTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	auth := gqlctx.AuthFromContext(ctx)
	uid, err := uuid.Parse(auth.UserID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid user")
	}
	key, lapses, err := r.tunnels.NewVPNKey(ctx, tid, uid, auth.Username)
	if err != nil {
		return nil, notRunning(ctx, err, "make a VPN key")
	}
	return &model.MinionVPNKey{Key: key, LapsesAt: lapses.UTC().Format(time.RFC3339Nano)}, nil
}

// DisconnectMinionVPNClient ends a client of a VPN tunnel, for an operator
// of its session's operation, saying who did.
func (r *minionTaskResolver) DisconnectMinionVPNClient(ctx context.Context, taskID, client string) (*model.MinionTask, error) {
	t, tid, err := r.vpnTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := r.tunnels.DisconnectVPN(tid, client, gqlctx.AuthFromContext(ctx).Username); err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "no such client")
		}
		return nil, err
	}
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
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

// tunnelKindNames are a stream's kinds past TCP and UDP, as GraphQL names
// them.
var tunnelKindNames = map[int]string{
	protocol.TunnelKindUnix: "unix", protocol.TunnelKindPipe: "pipe", protocol.TunnelKindProcess: "process", protocol.TunnelKindAccept: "accept",
	protocol.TunnelKindICMP: "icmp",
}

// tunnelStreamModel is a stream as GraphQL has it.
func tunnelStreamModel(s *models.MinionTunnelStream) *model.MinionTunnelStream {
	m := &model.MinionTunnelStream{Stream: int(s.Stream), Client: s.Client, Host: s.Host, Port: s.Port, Path: strPtr(s.Path), UDP: s.UDP,
		OpenedAt: s.OpenedAt.UTC().Format(time.RFC3339Nano),
		State:    "OPENING", Bound: strPtr(s.Bound), Msg: strPtr(s.Msg), EndedAt: taskTime(s.EndedAt), End: strPtr(s.End), Up: int(s.Up), Down: int(s.Down),
		Resends: int(s.Resends), Probes: int(s.Probes), WindowUp: int(s.WindowUp), WindowDown: int(s.WindowDown)}
	if name, ok := tunnelKindNames[s.Kind]; ok {
		m.Kind = &name
	}
	if s.Kind == protocol.TunnelKindProcess {
		m.Program, m.Terminal = strPtr(s.Program), s.Terminal
		if s.Exit != nil {
			n := int(*s.Exit)
			m.Exit = &n
		}
	}
	m.Came = strPtr(s.Came)
	if s.Result != nil {
		name := protocol.TunnelResultName(*s.Result)
		m.Result, m.State = &name, "OPEN"
	}
	if s.EndedAt != nil {
		m.State = "ENDED"
	}
	return m
}

// portOperations are the operations a Ports list covers: the one named,
// for a viewer of it at least; none named, every operation the caller is a
// member of (the Ports page with no operation, as Findings without one).
// Its rows say their operation.
func (r *minionTaskResolver) portOperations(ctx context.Context, operationID *string) ([]uuid.UUID, error) {
	if operationID == nil || *operationID == "" {
		uid, err := uuid.Parse(gqlctx.AuthFromContext(ctx).UserID)
		if err != nil {
			return nil, fmt.Errorf("invalid caller ID: %w", err)
		}
		ops, err := r.operationRepo.FindByMemberID(ctx, uid)
		if err != nil {
			return nil, fmt.Errorf("list the caller's operations: %w", err)
		}
		gqlctx.PrimeOperations(ctx, ops)
		ids := make([]uuid.UUID, len(ops))
		for i := range ops {
			ids[i] = ops[i].OperationID
		}
		return ids, nil
	}
	opID, err := uuid.Parse(*operationID)
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
	return []uuid.UUID{opID}, nil
}

// operationName is the name of a Ports row's operation, from the
// request's operation cache (portOperations fills it for a page); "" for
// one that cannot be read, or without operations to read.
func (r *minionTaskResolver) operationName(ctx context.Context, id uuid.UUID) string {
	if r.operationRepo == nil {
		return ""
	}
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, id)
	if err != nil {
		return ""
	}
	return op.Name
}

// OperationalPorts is a page of the operational ports in use of an
// operation, or of every operation the caller is a member of
// (portOperations): each one's task's tunnel as it is now.
func (r *minionTaskResolver) OperationalPorts(ctx context.Context, operationID *string, search *string, first *int, after *string, last *int, before *string) (*model.OperationalPortConnection, error) {
	opIDs, err := r.portOperations(ctx, operationID)
	if err != nil {
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
	total, err := r.tunnels.CountPorts(ctx, opIDs, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.tunnels.Ports(ctx, opIDs, q, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(p *models.OperationalPort) string { return repository.OperationalPortCursor(p) },
		func(p *models.OperationalPort, cursor string) *model.OperationalPortEdge {
			return &model.OperationalPortEdge{Node: r.portModel(ctx, p), Cursor: cursor}
		})
	return &model.OperationalPortConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

// portModel is a port as GraphQL has it: its task's tunnel as it is now.
func (r *minionTaskResolver) portModel(ctx context.Context, p *models.OperationalPort) *model.OperationalPort {
	var to *string
	if p.Host != "" {
		dest := net.JoinHostPort(p.Host, strconv.Itoa(p.HostPort))
		to = &dest
	}
	node := &model.OperationalPort{ID: p.TaskID.String(), OperationID: p.OperationID.String(), OperationName: r.operationName(ctx, p.OperationID), Port: p.Port, Front: p.Purpose, To: to, Path: strPtr(p.Path), Program: strPtr(p.Program), Address: strPtr(r.tunnels.Address()),
		SessionID: p.SessionID.String(), SessionNumber: p.SessionNumber, TaskID: p.TaskID.String(), OpenedBy: p.OpenedBy.String(),
		OpenedByName: strPtr(p.OpenedByName), OpenedAt: p.OpenedAt.UTC().Format(time.RFC3339Nano), Streams: int(p.Opened)}
	if sum, ok := r.tunnels.Summary(p.TaskID); ok {
		node.Listening, node.Open, node.Streams = true, sum.Open, int(max(sum.Streams, p.Opened))
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

// MinionListeners is a page of the reverse port forwards' listeners of an
// operation, or of every operation the caller is a member of
// (portOperations): each one's task's tunnel as it is now.
func (r *minionTaskResolver) MinionListeners(ctx context.Context, operationID *string, search *string, first *int, after *string, last *int, before *string) (*model.MinionListenerConnection, error) {
	opIDs, err := r.portOperations(ctx, operationID)
	if err != nil {
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
	total, err := r.tunnels.CountListeners(ctx, opIDs, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.tunnels.Listeners(ctx, opIDs, q, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(l *models.MinionListener) string { return repository.MinionListenerCursor(l) },
		func(l *models.MinionListener, cursor string) *model.MinionListenerEdge {
			return &model.MinionListenerEdge{Node: r.listenerModel(ctx, l), Cursor: cursor}
		})
	return &model.MinionListenerConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

// listenerModel is a listener as GraphQL has it: its task's tunnel as it is
// now.
func (r *minionTaskResolver) listenerModel(ctx context.Context, l *models.MinionListener) *model.MinionListener {
	var to *string
	if l.Host != "" {
		dest := net.JoinHostPort(l.Host, strconv.Itoa(l.HostPort))
		to = &dest
	}
	front := l.Front
	if front == "" {
		front = protocol.FrontForward
	}
	node := &model.MinionListener{ID: l.TaskID.String(), OperationID: l.OperationID.String(), OperationName: r.operationName(ctx, l.OperationID), Bind: l.Bind, Listen: l.Listen, Bound: strPtr(l.Bound), UDP: l.UDP, Front: front,
		To: to, SessionID: l.SessionID.String(), SessionNumber: l.SessionNumber,
		TaskID: l.TaskID.String(), OpenedBy: l.OpenedBy.String(), OpenedByName: strPtr(l.OpenedByName),
		OpenedAt: l.OpenedAt.UTC().Format(time.RFC3339Nano), Streams: int(l.Opened)}
	if sum, ok := r.tunnels.Summary(l.TaskID); ok {
		node.Open, node.Streams = sum.Open, int(max(sum.Streams, l.Opened))
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
	return r.listenerModel(ctx, l)
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
	return r.portModel(ctx, p)
}

// VPNTunnels is a page of the VPN tunnels of an operation, or of every
// operation the caller is a member of (portOperations): each one's task's
// tunnel as it is now.
func (r *minionTaskResolver) VPNTunnels(ctx context.Context, operationID *string, search *string, first *int, after *string, last *int, before *string) (*model.VPNTunnelConnection, error) {
	opIDs, err := r.portOperations(ctx, operationID)
	if err != nil {
		return nil, err
	}
	empty := &model.VPNTunnelConnection{Edges: []*model.VPNTunnelEdge{}, PageInfo: &pagination.PageInfo{}}
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
	total, err := r.tunnels.CountVPNTunnels(ctx, opIDs, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.tunnels.VPNTunnels(ctx, opIDs, q, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(v *models.VPNTunnel) string { return repository.VPNTunnelCursor(v) },
		func(v *models.VPNTunnel, cursor string) *model.VPNTunnelEdge {
			return &model.VPNTunnelEdge{Node: r.vpnTunnelModel(ctx, v), Cursor: cursor}
		})
	return &model.VPNTunnelConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

// vpnTunnelModel is a VPN tunnel as GraphQL has it: its task's tunnel as
// it is now.
func (r *minionTaskResolver) vpnTunnelModel(ctx context.Context, v *models.VPNTunnel) *model.VPNTunnel {
	node := &model.VPNTunnel{ID: v.TaskID.String(), OperationID: v.OperationID.String(), OperationName: r.operationName(ctx, v.OperationID), Routes: v.Routes, SessionID: v.SessionID.String(), SessionNumber: v.SessionNumber,
		TaskID: v.TaskID.String(), OpenedBy: v.OpenedBy.String(), OpenedByName: strPtr(v.OpenedByName),
		OpenedAt: v.OpenedAt.UTC().Format(time.RFC3339Nano), Streams: int(v.Opened)}
	if node.Routes == nil {
		node.Routes = []string{}
	}
	if sum, ok := r.tunnels.Summary(v.TaskID); ok {
		node.Listening, node.Open, node.Streams = true, sum.Open, int(max(sum.Streams, v.Opened))
		node.StopsAt = taskTime(stopsAt(sum))
		node.Clients = r.tunnels.VPNConnected(v.TaskID)
		if routes, _, ok := r.tunnels.VPN(v.TaskID); ok {
			node.Routes = routes
		}
	}
	return node
}

// LoadVPNTunnel is a task's VPN tunnel as GraphQL has it, for a subscriber
// (whose operation the subscription checked): nil when it has none.
func (r *minionTaskResolver) LoadVPNTunnel(ctx context.Context, taskID string) *model.VPNTunnel {
	id, err := uuid.Parse(taskID)
	if err != nil || r.tunnels == nil {
		return nil
	}
	v, err := r.tunnels.VPNTunnel(ctx, id)
	if err != nil {
		return nil
	}
	return r.vpnTunnelModel(ctx, v)
}

// desktopLogin is the credential a desktop's args name (its login), as
// its view shows it: its name (nil for none, or one gone), and why the
// desktop could not log in with it (nil when it could, or it has none),
// as opening one would say: gone, of a type a desktop does not take, or
// lacking what its type needs. One read of the credential, no more.
func (r *minionTaskResolver) desktopLogin(ctx context.Context, operationID string, args map[string]any) (login, problem *string) {
	raw, _ := protocol.ArgString(args, protocol.ParamCredential)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	op, opErr := uuid.Parse(operationID)
	switch {
	case err != nil || opErr != nil:
		return nil, strPtr("no credential's id")
	case r.creds == nil:
		return nil, strPtr("core resolves no credentials here")
	}
	name, why, err := r.creds.Named(ctx, op, id, desktopLoginTypes())
	if err != nil {
		logger.From(ctx).Warn("A desktop's login: read its credential", zap.String("credential_id", raw), zap.Error(err))
		return nil, strPtr("core could not read the credential now")
	}
	return strPtr(name), strPtr(why)
}

// desktopLoginTypes are the credential types a desktop logs in with: its
// login param's (protocol.ParamCredential).
func desktopLoginTypes() []string {
	for _, f := range protocol.TunnelCoreParams(protocol.ActionTunnel) {
		if f.Key == protocol.ParamCredential {
			return f.Accept
		}
	}
	return nil
}
