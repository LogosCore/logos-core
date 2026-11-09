package resolver

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// IMinionTaskResolver gives sessions' minions commands and reads what they
// answered (models.MinionTask; pkg/minionsync delivers them).
type IMinionTaskResolver interface {
	MinionTasks(ctx context.Context, sessionID string, last *int, before *string) (*model.MinionTaskPage, error)
	// Feedback is MinionTask.feedback: what its minion said of it.
	Feedback(ctx context.Context, t *model.MinionTask) ([]*model.MinionFeedback, error)
	MinionSessionCommands(ctx context.Context, sessionID string) ([]*model.BuildCommand, error)
	// DelegateServes are what a delegate task's command serves, as its
	// factory's latest schema says.
	DelegateServes(ctx context.Context, t *model.MinionTask) ([]protocol.ChannelVersions, error)
	QueueMinionTask(ctx context.Context, sessionID, command string, args any) (*model.MinionTask, error)
	CancelMinionTask(ctx context.Context, id string) (*model.MinionTask, error)
	SetMinionTunnelCap(ctx context.Context, taskID string, limit int) (*model.MinionTask, error)
	SetMinionTunnelRoutes(ctx context.Context, taskID string, routes []string) (*model.MinionTask, error)
	CreateMinionVPNKey(ctx context.Context, taskID string) (*model.MinionVPNKey, error)
	DisconnectMinionVPNClient(ctx context.Context, taskID, client string) (*model.MinionTask, error)
	MinionTask(ctx context.Context, id string) (*model.MinionTask, error)
	// Output is a task's output parts joined, as the minion sent them, and
	// Data their data merged: read only when a query asks (the selected
	// task's), the parent query having checked the caller.
	Output(ctx context.Context, taskID string) ([]byte, error)
	Data(ctx context.Context, taskID string) (any, error)
	// Document is the task's stored document as JSON, for debugging
	// (minion_task_document.go).
	Document(ctx context.Context, taskID string) (string, error)
	// File is a file_download task's file, as far as it came; nil for none.
	File(ctx context.Context, taskID string) (*model.MinionFile, error)
	// MinionFileStrings is a page of a kept file's printable strings.
	MinionFileStrings(ctx context.Context, fileID string, after, first, minLength *int, encoding, search *string) (*model.MinionFileStringPage, error)
	// The file browser (file_browser_resolver.go).
	FileBrowser(ctx context.Context, sessionID string) (*model.FsMachine, error)
	FileBrowserEntries(ctx context.Context, sessionID string, path, after *string, first *int, search, sort *string,
		desc, showDeleted, foldersOnly *bool) (*model.FsEntryPage, error)
	FileBrowserSearch(ctx context.Context, sessionID, query string, after *string, first *int, showDeleted *bool) (*model.FsEntryPage, error)
	MachineOfSession(ctx context.Context, sessionID string) (operationID, machine string, err error)
	// MinionStreamLink is a signed link to download a task's stream.
	MinionStreamLink(ctx context.Context, taskID string) (*model.MinionStreamLink, error)
	// Tunnels (minion_tunnel_resolver.go): a tunnel task's proxy, its
	// streams, an operation's operational ports.
	Tunnel(ctx context.Context, t *model.MinionTask) (*model.MinionTunnel, error)
	MinionTunnelStreams(ctx context.Context, taskID string, last *int, before, search, state *string) (*model.MinionTunnelStreamPage, error)
	OperationalPorts(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.OperationalPortConnection, error)
	// LoadPort is a task's port, for a subscriber: nil when it has none.
	LoadPort(ctx context.Context, taskID string) *model.OperationalPort
	// MinionListeners is a page of the operation's reverse port forwards'
	// listeners; LoadListener a task's, for a subscriber: nil for none.
	MinionListeners(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.MinionListenerConnection, error)
	LoadListener(ctx context.Context, taskID string) *model.MinionListener
	// VPNTunnels is a page of an operation's VPN tunnels; LoadVPNTunnel a
	// task's, for a subscriber: nil for none.
	VPNTunnels(ctx context.Context, operationID string, search *string, first *int, after *string, last *int, before *string) (*model.VPNTunnelConnection, error)
	LoadVPNTunnel(ctx context.Context, taskID string) *model.VPNTunnel
	// Load is one task as GraphQL, for a subscriber: nil when it cannot be
	// read.
	Load(ctx context.Context, taskID string) *model.MinionTask
	// SessionOf is the session a subscriber asks about, checked for its
	// role: the operation to filter events by.
	SessionOf(ctx context.Context, sessionID string) (*models.MinionSession, error)
}

// TaskBuilds is what the task resolver reads of builds.
type TaskBuilds interface {
	FindByBuildID(ctx context.Context, buildID uuid.UUID) (*models.Build, error)
}

// TaskSchemas is what it reads of factories: their build schemas.
type TaskSchemas interface {
	LatestBuildSchema(ctx context.Context, moduleName string) (*models.ModuleBuildSchema, error)
}

// TaskTargets finds a bind-call session's target (the minion target
// repository): a page core reaches only by calling it.
type TaskTargets interface {
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionTarget, error)
}

// TaskStreams signs the links streams download from
// (minionstreams.Service).
type TaskStreams interface {
	Link(taskID uuid.UUID) (string, time.Time)
	ReaderGone() time.Duration
}

type minionTaskResolver struct {
	tasks         repository.IMinionTaskRepository
	sessions      repository.IMinionSessionRepository
	builds        TaskBuilds
	schemas       TaskSchemas
	users         repository.IUserRepository
	operationRepo repository.IOperationRepository
	bus           eventbus.IEventBus
	files         TaskFiles
	streams       TaskStreams
	fileBrowser   TaskFileBrowser
	// targets: a bind-call session without one takes no tasks; nil, every
	// session takes them.
	targets TaskTargets
	// connector opens and closes bind-persistent targets' connections, for
	// connect and disconnect; nil, sessions have neither.
	connector TaskConnector
	// limits sets a session's max tasks, for max_tasks; nil, sessions have
	// none.
	limits MaxTasksSetter
	// feedback is minions' feedback, a task's on the task; nil, none.
	feedback repository.IMinionFeedbackRepository
	// creds resolves the credentials credential params name; nil, a
	// command with one takes no task.
	creds TaskCredentials
	// tunnels opens tunnel tasks' ports; nil, a tunnel command takes no
	// task.
	tunnels TaskTunnels
}

// TaskTunnels opens a tunnel task's operational port and closes it, and
// says what its proxy carried (miniontunnels.Service).
type TaskTunnels interface {
	Open(ctx context.Context, o miniontunnels.Opening) (int, error)
	Close(ctx context.Context, task uuid.UUID)
	// Address is the host operators reach the ports at; "" for the web
	// app's.
	Address() string
	View(ctx context.Context, task uuid.UUID) (miniontunnels.Summary, bool, error)
	Summary(task uuid.UUID) (miniontunnels.Summary, bool)
	Streams(ctx context.Context, task uuid.UUID, filter miniontunnels.StreamFilter, before uint64, limit int64) ([]models.MinionTunnelStream, error)
	Port(ctx context.Context, task uuid.UUID) (*models.OperationalPort, error)
	Ports(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.OperationalPort, error)
	CountPorts(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	// OpenReverse keeps a reverse port forward's listener for a task about
	// to be queued; Listener, Listeners and CountListeners read them.
	OpenReverse(ctx context.Context, o miniontunnels.Opening) error
	Listener(ctx context.Context, task uuid.UUID) (*models.MinionListener, error)
	Listeners(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionListener, error)
	CountListeners(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	// SetCap changes a running tunnel's bandwidth cap (0: none).
	SetCap(ctx context.Context, task uuid.UUID, limit int64) error
	// OpenVPN keeps a VPN tunnel for a task about to be queued (docs:
	// contracts/vpn-front); SetRoutes changes a running one's routes, VPN
	// reads its routes and clients, NewVPNKey makes a key for a client of
	// it, and DisconnectVPN ends one.
	OpenVPN(ctx context.Context, o miniontunnels.Opening) error
	SetRoutes(ctx context.Context, task uuid.UUID, routes []string) ([]string, error)
	VPN(task uuid.UUID) ([]string, []miniontunnels.VPNClientInfo, bool)
	NewVPNKey(ctx context.Context, task, user uuid.UUID, username string) (string, time.Time, error)
	DisconnectVPN(task uuid.UUID, instance, by string) error
	// VPNTunnel is a task's VPN tunnel as kept, VPNTunnels a page of an
	// operation's and CountVPNTunnels how many; VPNConnected its machines
	// connected now.
	VPNTunnel(ctx context.Context, task uuid.UUID) (*models.VPNTunnel, error)
	VPNTunnels(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.VPNTunnel, error)
	CountVPNTunnels(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	VPNConnected(task uuid.UUID) int
}

// TaskCredentials resolves the credentials a task's args name (credential
// params), each one of the operation's, of a type its param takes.
// Satisfied by *credentialrefs.Resolver.
type TaskCredentials interface {
	ResolveFields(ctx context.Context, operationID uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]protocol.Credential, error)
}

// TaskFiles finds a task's file, and reads a kept one's strings
// (minionfiles.Service).
type TaskFiles interface {
	File(ctx context.Context, taskID uuid.UUID) (*models.MinionFile, error)
	FileByID(ctx context.Context, fileID uuid.UUID) (*models.MinionFile, error)
	// TaskFile is an operation's task file, for a file param's value.
	TaskFile(ctx context.Context, operationID uuid.UUID, id string) (*models.MinionFile, error)
	Strings(ctx context.Context, f models.MinionFile, q minionfiles.StringsQuery) (minionfiles.StringsPage, error)
}

func NewMinionTaskResolver(tasks repository.IMinionTaskRepository, sessions repository.IMinionSessionRepository, builds TaskBuilds,
	schemas TaskSchemas, users repository.IUserRepository, operationRepo repository.IOperationRepository, bus eventbus.IEventBus,
	files TaskFiles, streams TaskStreams, fileBrowser TaskFileBrowser, targets TaskTargets, connector TaskConnector,
	limits MaxTasksSetter, feedback repository.IMinionFeedbackRepository, creds TaskCredentials, tunnels TaskTunnels) IMinionTaskResolver {
	return &minionTaskResolver{tasks: tasks, sessions: sessions, builds: builds, schemas: schemas, users: users,
		operationRepo: operationRepo, bus: bus, files: files, streams: streams, fileBrowser: fileBrowser, targets: targets, connector: connector,
		limits: limits, feedback: feedback, creds: creds, tunnels: tunnels}
}

const (
	defaultTaskPage = 50
	maxTaskPage     = 200
)

// session loads a session for role: viewer to read, operator to give or
// stop tasks (an agent key does neither).
func (r *minionTaskResolver) session(ctx context.Context, id string, role models.OperationRole) (*models.MinionSession, error) {
	if role != models.OperationRoleViewer && gqlctx.AuthFromContext(ctx).Agent != nil {
		return nil, moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid session ID")
	}
	s, err := r.sessions.FindBySessionID(ctx, sid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "session not found")
		}
		return nil, err
	}
	operation, err := gqlctx.LoadOperation(ctx, r.operationRepo, s.OperationID)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}
	if err := authorization.AuthorizeOperationRole(ctx, &operation, role); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *minionTaskResolver) SessionOf(ctx context.Context, sessionID string) (*models.MinionSession, error) {
	return r.session(ctx, sessionID, models.OperationRoleViewer)
}

func (r *minionTaskResolver) MinionTasks(ctx context.Context, sessionID string, last *int, before *string) (*model.MinionTaskPage, error) {
	s, err := r.session(ctx, sessionID, models.OperationRoleViewer)
	if err != nil {
		return nil, err
	}
	limit := defaultTaskPage
	if last != nil {
		limit = min(max(*last, 1), maxTaskPage)
	}
	var cursor *repository.MinionTaskCursor
	if before != nil && *before != "" {
		if cursor, err = decodeTaskCursor(*before); err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
		}
	}
	rows, err := r.tasks.ListBySession(ctx, s.SessionID, cursor, int64(limit+1))
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	page := &model.MinionTaskPage{Tasks: []*model.MinionTask{}}
	if len(rows) > limit {
		rows = rows[:limit]
		c := encodeTaskCursor(rows[limit-1])
		page.Before = &c
	}
	names := r.names(ctx, rows)
	for i := range rows {
		m, err := r.toModel(ctx, &rows[i], names)
		if err != nil {
			return nil, err
		}
		page.Tasks = append(page.Tasks, m)
	}
	return page, nil
}

// commandsOf are the commands of a session's build, as its factory's
// schema declares them: in the schema's order.
func (r *minionTaskResolver) commandsOf(ctx context.Context, s *models.MinionSession) (*protocol.BuildSchema, []protocol.BuildCommand, error) {
	b, err := r.builds.FindByBuildID(ctx, s.BuildID)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, nil, moduleGQLError(ctx, errCodeNotFound, "the session's build is gone: its commands are not known")
		}
		return nil, nil, fmt.Errorf("load the build: %w", err)
	}
	sc, err := r.schemas.LatestBuildSchema(ctx, b.ModuleName)
	if err != nil {
		return nil, nil, fmt.Errorf("load the build schema: %w", err)
	}
	if sc == nil {
		return nil, nil, moduleGQLError(ctx, errCodeNotFound, fmt.Sprintf("no %q factory declares a build schema: its commands are not known", b.ModuleName))
	}
	var out []protocol.BuildCommand
	for _, c := range sc.Commands {
		// help, connect and disconnect are core's own: a schema from before
		// they were reserved does not shadow them.
		if slices.Contains(b.Commands, c.ID) && !protocol.IsCoreCommand(c.ID) {
			switch {
			case protocol.IsTunnelAction(c.Action):
				c = tunnelCommand(sc.BuildSchema, c, b.Target)
			case protocol.IsDelegateAction(c.Action):
				// Core's own params (stop_idle) beside the command's.
				c.Params = append(slices.Clone(c.Params), protocol.TunnelCoreParams(c.Action)...)
			}
			out = append(out, c)
		}
	}
	if r.offersConnect(s) {
		out = append(out, connectCommands()...)
	}
	if r.offersMaxTasks(s) {
		out = append(out, maxTasksCommand())
	}
	return &sc.BuildSchema, append(out, helpCommand(out)), nil
}

func (r *minionTaskResolver) DelegateServes(ctx context.Context, t *model.MinionTask) ([]protocol.ChannelVersions, error) {
	s, err := r.session(ctx, t.SessionID, models.OperationRoleViewer)
	if err != nil {
		return nil, err
	}
	_, commands, err := r.commandsOf(ctx, s)
	if err != nil {
		return nil, err
	}
	for _, c := range commands {
		if c.ID == t.Command {
			return c.Serves, nil
		}
	}
	return nil, nil
}

func (r *minionTaskResolver) MinionSessionCommands(ctx context.Context, sessionID string) ([]*model.BuildCommand, error) {
	s, err := r.session(ctx, sessionID, models.OperationRoleViewer)
	if err != nil {
		return nil, err
	}
	schema, commands, err := r.commandsOf(ctx, s)
	if err != nil {
		return nil, err
	}
	out := make([]*model.BuildCommand, len(commands))
	for i, c := range commands {
		out[i] = toBuildCommandModel(*schema, c)
	}
	return out, nil
}

func (r *minionTaskResolver) QueueMinionTask(ctx context.Context, sessionID, command string, args any) (*model.MinionTask, error) {
	s, err := r.session(ctx, sessionID, models.OperationRoleOperator)
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	if args != nil {
		m, ok := args.(map[string]any)
		if !ok {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "args are an object of the command's params")
		}
		values = m
	}
	_, commands, err := r.commandsOf(ctx, s)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(commands, func(c protocol.BuildCommand) bool { return c.ID == command })
	if i < 0 {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("the session's minion has no %q command", command))
	}
	wire, problems := protocol.TaskArgs(commands[i].Params, values)
	if problems != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, problemsText(problems))
	}
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}
	if command == protocol.CommandHelp {
		topic, _ := protocol.ArgString(wire, "command")
		return r.answerHelp(ctx, s, uid, values, helpText(commands, topic))
	}
	if s.Closed(time.Now().UTC()) {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "the session is closed: its minion takes no tasks")
	}
	if command == protocol.CommandConnect || command == protocol.CommandDisconnect {
		return r.runConnect(ctx, s, uid, values, command)
	}
	if command == protocol.CommandMaxTasks {
		return r.runMaxTasks(ctx, s, uid, values, wire)
	}
	if err := overKind(ctx, s, commands[i]); err != nil {
		return nil, err
	}
	if err := r.reachable(ctx, s); err != nil {
		return nil, err
	}
	upload, err := r.offerFile(ctx, s, commands[i].Params, wire)
	if err != nil {
		return nil, err
	}
	credentialIDs, err := r.putCredentials(ctx, s.OperationID, commands[i].Params, wire)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	taskID := uuid.New()
	tunnel := protocol.IsTunnelAction(commands[i].Action)
	if tunnel {
		port, front, err := r.openTunnel(ctx, s, uid, taskID, commands[i], wire)
		if err != nil {
			return nil, err
		}
		if commands[i].Action == protocol.ActionReverseTunnel {
			// Where the minion listens, as core checked it (its port its
			// own: no operational port).
			values["bind"] = wire["bind"]
		} else {
			// What core puts in front, as it took it, and the port it
			// serves on (a VPN's none: its routes). A reverse tunnel's one
			// front no param names.
			values[protocol.ParamFront] = front
			if front == protocol.FrontVPN {
				values[protocol.ParamRoutes] = wire[protocol.ParamRoutes]
			} else {
				values[protocol.ParamPort] = port
			}
		}
		for _, key := range []string{protocol.ParamHost, protocol.ParamPath, protocol.ParamSource} {
			// As core read them: trimmed, a source as an address.
			if v, ok := wire[key]; ok && v != "" && (front == protocol.FrontForward || key == protocol.ParamSource) {
				values[key] = v
			}
		}
		if front == protocol.FrontTerminal {
			for _, key := range []string{protocol.ParamProgram, protocol.ParamArgs, protocol.ParamRows, protocol.ParamCols} {
				if v, ok := wire[key]; ok && v != "" {
					values[key] = v
				}
			}
		}
		// And when core stops it: a command line that leaves stop_idle out
		// runs by core's default for the action, as the form (which sends
		// every field) does.
		withDefault(values, commands[i].Params, protocol.ParamStopIdle)
	}
	if protocol.IsDelegateAction(commands[i].Action) {
		// When core stops it, as for a tunnel.
		withDefault(values, commands[i].Params, protocol.ParamStopIdle)
	}
	// On the wire the args go by their params' numbers.
	var wireBytes []byte
	if numbered := protocol.WireArgs(commands[i].Params, wire); len(numbered) > 0 {
		if wireBytes, err = protocol.MarshalCBOR(numbered); err != nil {
			if tunnel {
				r.tunnels.Close(ctx, taskID)
			}
			return nil, fmt.Errorf("encode the args: %w", err)
		}
	}
	t := &models.MinionTask{TaskID: taskID, OperationID: s.OperationID, SessionID: s.SessionID, Command: command,
		Op: commands[i].Op, Args: values, CredentialIDs: credentialIDs, WireArgs: wireBytes, Result: commands[i].Result, Action: commands[i].Action,
		IssuedBy: uid, State: models.MinionTaskQueued, QueuedAt: time.Now().UTC(), Upload: upload}
	if err := r.tasks.Insert(ctx, t); err != nil {
		if tunnel {
			r.tunnels.Close(ctx, taskID)
		}
		return nil, fmt.Errorf("queue the task: %w", err)
	}
	r.publish(t)
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
}

// terminalParam: one of a terminal's params, offered where the minion runs
// processes.
func terminalParam(key string) bool {
	return key == protocol.ParamProgram || key == protocol.ParamArgs || key == protocol.ParamRows || key == protocol.ParamCols
}

// tunnelCommand is a tunnel command as a build of target has it: what it
// opens narrowed by the target, and core's own params beside the command's
// (no num): its front, port, destination and cap, a path, a source address
// and TLS where its minion opens them, and the terminal front with its
// program, args and size where it runs processes.
func tunnelCommand(sc protocol.BuildSchema, c protocol.BuildCommand, target string) protocol.BuildCommand {
	c.Opens = sc.CommandOpens(c, target)
	params := slices.Clone(c.Params)
	for _, p := range protocol.TunnelCoreParams(c.Action) {
		switch {
		case p.Key == protocol.ParamPath && !c.CanOpen(protocol.OpenUnix) && !c.CanOpen(protocol.OpenPipe),
			p.Key == protocol.ParamSource && !c.CanOpen(protocol.OpenSource),
			p.Key == protocol.ParamTLS && !c.CanOpen(protocol.OpenTLS),
			terminalParam(p.Key) && !c.CanOpen(protocol.OpenProcess):
			continue
		}
		if p.Key == protocol.ParamFront && !c.CanOpen(protocol.OpenProcess) {
			// No terminal where the minion runs no processes.
			p.Options = slices.DeleteFunc(slices.Clone(p.Options), func(o protocol.EnumOption) bool { return o.Value == protocol.FrontTerminal })
		}
		params = append(params, p)
	}
	c.Params = params
	return c
}

// withDefault puts a param's default in a task's args where the operator
// gave the param no value: TaskArgs ran the task by that default, and its
// args now say so. A param the command does not declare, or declares with
// no default, stays out.
func withDefault(values map[string]any, params []protocol.FieldSpec, key string) {
	if v, ok := values[key]; ok && v != nil {
		return
	}
	if i := slices.IndexFunc(params, func(p protocol.FieldSpec) bool { return p.Key == key }); i >= 0 && params[i].Default != nil {
		values[key] = params[i].Default
	}
}

// openTunnel opens what a tunnel task about to be queued needs, by its
// front (its front param, its action's first when none): an operational
// port (its port arg: 0 for any free one) with core's SOCKS proxy, HTTP
// proxy or a forward to the task's host and to in front of it; or, for a
// reverse tunnel, the listener core keeps while the minion listens. It
// says the port (0 for a reverse tunnel) and the front.
func (r *minionTaskResolver) openTunnel(ctx context.Context, s *models.MinionSession, uid, taskID uuid.UUID, cmd protocol.BuildCommand, wire map[string]any) (int, string, error) {
	if r.tunnels == nil {
		return 0, "", moduleGQLError(ctx, errCodeBadUserInput, "core opens no tunnels")
	}
	fronts := protocol.TunnelFronts(cmd.Action)
	front, _ := protocol.ArgString(wire, protocol.ParamFront)
	if front == "" {
		front = fronts[0]
	}
	if !slices.Contains(fronts, front) {
		return 0, "", moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("front: one of %s", strings.Join(fronts, ", ")))
	}
	wire[protocol.ParamFront] = front
	asked, _ := protocol.ArgInt(wire, protocol.ParamPort)
	o := miniontunnels.Opening{Task: taskID, Session: s.SessionID, Operation: s.OperationID, By: uid, Port: int(max(min(asked, 65536), -1)),
		MaxStreams: miniontunnels.DefaultMaxStreams, Timeout: miniontunnels.DefaultConnectTimeout, SessionNumber: int(s.Number),
		ByName: gqlctx.AuthFromContext(ctx).Username}
	// A forward's destination: core checks it as a stream's. A tunnel's
	// forward goes to a path instead, a unix socket's or a pipe's, where
	// its minion opens it.
	path, _ := protocol.ArgString(wire, protocol.ParamPath)
	path = strings.TrimSpace(path)
	destination := func() error {
		if path != "" {
			kind := protocol.TunnelPathKind(path)
			open := map[int]string{protocol.TunnelKindUnix: protocol.OpenUnix, protocol.TunnelKindPipe: protocol.OpenPipe}[kind]
			if !cmd.CanOpen(open) {
				return moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("path: %s's minion opens no %s (its opens: %s)", cmd.ID,
					map[int]string{protocol.TunnelKindUnix: "unix sockets", protocol.TunnelKindPipe: "named pipes"}[kind], strings.Join(cmd.Opens, ", ")))
			}
			if err := protocol.TunnelPath(kind, path); err != nil {
				return moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("the forward's path: %v", err))
			}
			o.Kind, o.Path = kind, path
			wire[protocol.ParamPath] = path
			return nil
		}
		host, _ := protocol.ArgString(wire, protocol.ParamHost)
		to, _ := protocol.ArgInt(wire, protocol.ParamTo)
		dest := protocol.TunnelItem{T: protocol.ItemTunnel, Task: taskID[:], Stream: 1, Host: strings.TrimSpace(host), Port: uint64(max(to, 0))}
		if err := dest.Validate(); err != nil {
			return moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("the forward's destination (host, to; or a path): %v", err))
		}
		o.Host, o.HostPort = dest.Host, int(to)
		wire[protocol.ParamHost] = dest.Host
		return nil
	}
	if n, ok := protocol.ArgInt(wire, "max_streams"); ok && n > 0 {
		o.MaxStreams = int(min(n, 1<<16))
	}
	if d, ok := protocol.ArgDuration(wire, "timeout"); ok && d > 0 {
		o.Timeout = d
	}
	// Core's alone (no num): when to stop the tunnel no stream uses, and
	// how fast it may go.
	if d, ok := protocol.ArgDuration(wire, protocol.ParamStopIdle); ok && d > 0 {
		o.StopIdle = d
	}
	if c, ok := protocol.ArgInt(wire, protocol.ParamCap); ok && c > 0 {
		o.Cap = c
	}
	if cmd.Action == protocol.ActionReverseTunnel {
		// The minion listens; core takes each of its streams by the front,
		// and its destination stays core's (its params carry no num).
		bind, _ := protocol.ArgString(wire, "bind")
		listen, _ := protocol.ArgInt(wire, "listen")
		bind = strings.TrimSpace(bind)
		if err := protocol.TunnelListen(bind, listen); err != nil {
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("where the minion listens: %v", err))
		}
		udp, _ := protocol.ArgBool(wire, "udp")
		switch {
		case udp && !cmd.CanOpen(protocol.OpenUDP):
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("%s's minion listens for TCP alone (its opens)", cmd.ID))
		}
		// Its one front: a forward to host:to.
		if err := destination(); err != nil {
			return 0, "", err
		}
		o.Purpose, o.Front, o.Bind, o.Listen, o.UDP, o.Port = models.OperationalPurposeReverse, front, bind, int(listen), udp, 0
		wire["bind"] = bind
		if err := r.tunnels.OpenReverse(ctx, o); err != nil {
			if errors.Is(err, miniontunnels.ErrNoListeners) {
				return 0, "", moduleGQLError(ctx, errCodeBadUserInput, err.Error())
			}
			return 0, "", err
		}
		return 0, front, nil
	}
	// Where its TCP streams go from, and TLS to a forward's destination,
	// where the minion opens them. Each param applies by the front
	// (FieldSpec.When): TaskArgs refused it for another, and filled no
	// default.
	source, _ := protocol.ArgString(wire, protocol.ParamSource)
	useTLS, _ := protocol.ArgBool(wire, protocol.ParamTLS)
	if source = strings.TrimSpace(source); source != "" {
		ip := net.ParseIP(source)
		switch {
		case !cmd.CanOpen(protocol.OpenSource):
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("source: %s's minion takes no source address", cmd.ID))
		case ip == nil:
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, "source: an address of the minion's host, an IP")
		case path != "":
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, "source: a TCP connection's, not a path's")
		}
		o.Source = ip.String()
		wire[protocol.ParamSource] = o.Source
	}
	switch {
	case useTLS && path != "":
		return 0, "", moduleGQLError(ctx, errCodeBadUserInput, "tls: a forward's to a host and a port, not a path's")
	case useTLS && !cmd.CanOpen(protocol.OpenTLS):
		return 0, "", moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("tls: %s's minion speaks no TLS to a destination", cmd.ID))
	}
	o.TLS = useTLS
	if front == protocol.FrontVPN {
		return r.openVPN(ctx, cmd, o, wire)
	}
	program, _ := protocol.ArgString(wire, protocol.ParamProgram)
	argsLine, _ := protocol.ArgString(wire, protocol.ParamArgs)
	program, argsLine = strings.TrimSpace(program), strings.TrimSpace(argsLine)
	switch front {
	case protocol.FrontTerminal:
		if err := terminal(&o, cmd, wire, program, argsLine); err != nil {
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
	case protocol.FrontSOCKS:
		// Its UDP ASSOCIATE, when the minion opens udp; BIND, accept.
		o.Purpose, o.UDP, o.Accept = models.OperationalPurposeSOCKS, cmd.CanOpen(protocol.OpenUDP), cmd.CanOpen(protocol.OpenAccept)
	case protocol.FrontHTTP:
		o.Purpose = models.OperationalPurposeHTTP
	case protocol.FrontForward:
		if err := destination(); err != nil {
			return 0, "", err
		}
		o.Purpose = models.OperationalPurposeForward
	}
	port, err := r.tunnels.Open(ctx, o)
	if errors.Is(err, miniontunnels.ErrNoPort) {
		return 0, "", moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	if err != nil {
		return 0, "", err
	}
	wire[protocol.ParamPort] = int64(port)
	return port, front, nil
}

// openVPN keeps a VPN tunnel for the task (docs: contracts/vpn-front): its
// routes, checked and written as networks, and what its clients may open
// besides TCP, as its command opens udp and icmp. No operational port.
func (r *minionTaskResolver) openVPN(ctx context.Context, cmd protocol.BuildCommand, o miniontunnels.Opening, wire map[string]any) (int, string, error) {
	routes, err := protocol.TunnelRoutes(argStrings(wire[protocol.ParamRoutes]))
	if err != nil {
		return 0, "", moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	o.Routes = make([]string, len(routes))
	for i, p := range routes {
		o.Routes[i] = p.String()
	}
	wire[protocol.ParamRoutes] = o.Routes
	o.Purpose, o.Port = models.OperationalPurposeVPN, 0
	o.UDP, o.ICMP = cmd.CanOpen(protocol.OpenUDP), cmd.CanOpen(protocol.OpenICMP)
	if err := r.tunnels.OpenVPN(ctx, o); err != nil {
		if errors.Is(err, miniontunnels.ErrNoVPN) {
			return 0, "", moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
		return 0, "", err
	}
	return 0, protocol.FrontVPN, nil
}

// argStrings is a list arg's strings (a string_list's, as JSON or Go has
// it).
func argStrings(v any) []string {
	switch items := v.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, it := range items {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		// A command line's: comma-separated.
		var out []string
		for _, s := range strings.Split(items, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// terminal makes o a terminal's: the minion runs program (none: its
// host's shell) with args, a command line split as a shell's words, in a
// terminal of rows and cols on the raw port; where the command opens
// process.
func terminal(o *miniontunnels.Opening, cmd protocol.BuildCommand, wire map[string]any, program, argsLine string) error {
	if !cmd.CanOpen(protocol.OpenProcess) {
		return fmt.Errorf("front terminal: %s's minion runs no processes (its opens: %s)", cmd.ID, strings.Join(cmd.Opens, ", "))
	}
	args, err := protocol.TunnelArgs(argsLine)
	if err != nil {
		return err
	}
	rows, ok := protocol.ArgInt(wire, protocol.ParamRows)
	if !ok {
		rows = 24
	}
	cols, ok := protocol.ArgInt(wire, protocol.ParamCols)
	if !ok {
		cols = 80
	}
	if err := protocol.TunnelProcess(program, args, uint64(max(rows, 0)), uint64(max(cols, 0))); err != nil || rows < 1 {
		if err == nil {
			err = fmt.Errorf("a terminal is 1 to %d rows and cols", protocol.MaxTunnelTerm)
		}
		return fmt.Errorf("the terminal: %w", err)
	}
	o.Purpose, o.Program, o.Args, o.Rows, o.Cols = models.OperationalPurposeTerminal, program, args, int(rows), int(cols)
	// The task's args say what it runs with, defaults too (a command line
	// may leave the size out).
	wire[protocol.ParamProgram], wire[protocol.ParamArgs] = program, argsLine
	wire[protocol.ParamRows], wire[protocol.ParamCols] = rows, cols
	return nil
}

// overKind refuses a task of a command that does not work over the kind of
// connection the session's minion uses (its build schema's kinds): an
// operator switches the minion first. A session whose minion never said
// its connection takes it.
func overKind(ctx context.Context, s *models.MinionSession, c protocol.BuildCommand) error {
	if s.ConnKind == "" || c.OverKind(s.ConnKind) {
		return nil
	}
	return moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf(
		"%s works over %s connections; session #%d's minion uses connection %d, %s: switch it first",
		c.ID, strings.Join(c.Kinds, " or "), s.Number, s.Conn, s.ConnKind))
}

// reachable refuses a task core could never deliver: a bind session (a
// page core calls, a process core connects to) with no target. Nothing would call the page, so the
// task would wait for good; tasks queued while it had one wait, for a
// target added to the session to deliver. One whose target is unreachable
// takes tasks: core retries it, and Check now may reach it.
func (r *minionTaskResolver) reachable(ctx context.Context, s *models.MinionSession) error {
	if r.targets == nil || !protocol.IsBindKind(s.ConnKind) {
		return nil
	}
	_, err := r.targets.FindBySessionID(ctx, s.SessionID)
	switch {
	case repository.IsNotFound(err):
		return moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf(
			"session #%d has no target: core cannot reach its minion. Add a target first (the session's Connections, or its build's Targets)", s.Number))
	case err != nil:
		return fmt.Errorf("look for the session's target: %w", err)
	}
	return nil
}

// offerFile turns a file param's value, a task file's id, into the file
// offer the task carries (protocol.FileOffer: a transfer of its own, the
// file's name, size and SHA-256), checked against the param's accept and
// max_size; the upload it records. Nil for a command without a file param,
// or a task not given one.
func (r *minionTaskResolver) offerFile(ctx context.Context, s *models.MinionSession, params []protocol.FieldSpec, wire map[string]any) (*models.MinionTaskUpload, error) {
	i := slices.IndexFunc(params, func(p protocol.FieldSpec) bool { return p.Type == protocol.FieldFile || p.Type == protocol.FieldStream })
	if i < 0 {
		return nil, nil
	}
	p := params[i]
	id, ok := wire[p.Key].(string)
	if p.Type == protocol.FieldStream && !ok {
		// A stream without a name (uploaded with curl, which says none):
		// the path's last part.
		if id, ok = streamNameOf(wire); !ok {
			return nil, moduleGQLError(ctx, errCodeBadUserInput,
				p.Key+": no file picked, so the path names the file: end it with the file's name")
		}
	}
	if !ok {
		return nil, nil
	}
	if p.Type == protocol.FieldStream {
		// A stream: the file comes as its uploader sends it; its value is
		// the file's name.
		xfer := uuid.New()
		now := time.Now().UTC()
		wire[p.Key] = protocol.FileOffer{Xfer: xfer[:], Name: id, Stream: true}
		return &models.MinionTaskUpload{Name: id, Xfer: xfer[:], Stream: true, IdleSince: &now}, nil
	}
	if r.files == nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, p.Key+": core keeps no task files")
	}
	f, err := r.files.TaskFile(ctx, s.OperationID, id)
	if errors.Is(err, minionfiles.ErrNoTaskFile) {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, p.Key+": "+err.Error())
	}
	if err != nil {
		return nil, fmt.Errorf("load the task file: %w", err)
	}
	mediaType, _, _ := strings.Cut(f.ContentType, ";")
	if err := protocol.ValidateFile(p, protocol.BuildFile{ID: id, Name: f.Name, Size: f.Total, MediaType: mediaType}); err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("%s: %s: %v", p.Key, f.Name, err))
	}
	xfer := uuid.New()
	wire[p.Key] = protocol.FileOffer{Xfer: xfer[:], Name: f.Name, Size: uint64(f.Total), SHA256: f.SHA256}
	return &models.MinionTaskUpload{FileID: f.FileID, Name: f.Name, Size: f.Total, SHA256: f.SHA256, Xfer: xfer[:]}, nil
}

// streamNameOf is the name a stream without one takes: the last part of
// the task's path arg, by either separator (the host may be either kind);
// false for none, or a path that ends in a separator.
func streamNameOf(wire map[string]any) (string, bool) {
	path, _ := wire["path"].(string)
	name := path[strings.LastIndexAny(path, `/\`)+1:]
	if name == "" || name == "." || name == ".." || len(name) > 255 {
		return "", false
	}
	return name, true
}

// answerHelp keeps a help task, answered: it goes in as already started,
// so no get_tasks hands it to the minion, and ends with text as its
// output, as a minion's last result would. A closed session's commands
// can be read too.
func (r *minionTaskResolver) answerHelp(ctx context.Context, s *models.MinionSession, uid uuid.UUID, values map[string]any, text string) (*model.MinionTask, error) {
	now := time.Now().UTC()
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: protocol.CommandHelp,
		Args: values, IssuedBy: uid, State: models.MinionTaskRunning, QueuedAt: now, AckedAt: &now, StartedAt: &now}
	if err := r.tasks.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("keep the help task: %w", err)
	}
	ok := true
	after, _, err := r.tasks.AddResult(ctx, s.SessionID, t.TaskID, repository.MinionTaskResult{
		Seq: 1, Done: true, OK: &ok, Out: []byte(text), At: now,
	})
	if err != nil {
		return nil, fmt.Errorf("answer the help task: %w", err)
	}
	r.publish(after)
	return r.toModel(ctx, after, r.names(ctx, []models.MinionTask{*after}))
}

// problemsText is a TaskArgs report as one message, by param.
func problemsText(problems map[string]string) string {
	keys := make([]string, 0, len(problems))
	for k := range problems {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + problems[k]
	}
	return strings.Join(parts, "; ")
}

func (r *minionTaskResolver) CancelMinionTask(ctx context.Context, id string) (*model.MinionTask, error) {
	tid, err := uuid.Parse(id)
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
	if protocol.IsCoreCommand(t.Command) && !t.Finished() {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("core runs %s itself: it ends on its own", t.Command))
	}
	if t, err = r.tasks.RequestCancel(ctx, tid, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("cancel the task: %w", err)
	}
	r.publish(t)
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
}

func (r *minionTaskResolver) MinionTask(ctx context.Context, id string) (*model.MinionTask, error) {
	tid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := r.session(ctx, t.SessionID.String(), models.OperationRoleViewer); err != nil {
		return nil, err
	}
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
}

func (r *minionTaskResolver) parts(ctx context.Context, taskID string) ([]models.MinionTaskPart, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID")
	}
	parts, err := r.tasks.Parts(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("load the output: %w", err)
	}
	return parts, nil
}

func (r *minionTaskResolver) Output(ctx context.Context, taskID string) ([]byte, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID")
	}
	// Only the parts with output: a walk sends thousands without.
	parts, err := r.tasks.OutParts(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("load the output: %w", err)
	}
	var out []byte
	for _, p := range parts {
		out = append(out, p.Out...)
	}
	return out, nil
}

func (r *minionTaskResolver) Data(ctx context.Context, taskID string) (any, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("load the task: %w", err)
	}
	// A result kept whole on the task needs none of its parts.
	var parts []models.MinionTaskPart
	if !taskdata.Latest(t.Result) || len(t.LatestData) == 0 {
		if parts, err = r.parts(ctx, taskID); err != nil {
			return nil, err
		}
	}
	data, err := taskdata.Join(*t, parts)
	if err != nil {
		return nil, fmt.Errorf("join the data: %w", err)
	}
	return data, nil
}

func (r *minionTaskResolver) Document(ctx context.Context, taskID string) (string, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return "", fmt.Errorf("invalid task ID")
	}
	d, err := r.tasks.Document(ctx, tid)
	if err != nil {
		return "", fmt.Errorf("load the task: %w", err)
	}
	return documentJSON(d)
}

func (r *minionTaskResolver) File(ctx context.Context, taskID string) (*model.MinionFile, error) {
	if r.files == nil {
		return nil, nil
	}
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID")
	}
	f, err := r.files.File(ctx, tid)
	if err != nil || f == nil {
		return nil, err
	}
	out := &model.MinionFile{ID: f.FileID.String(), Name: f.Name, Size: int(f.Total), Received: int(f.Received),
		State: f.State, Err: strPtr(f.Err), ContentType: strPtr(f.ContentType), CreatedAt: f.CreateAt.UTC().Format(time.RFC3339Nano), DoneAt: taskTime(f.DoneAt)}
	if len(f.SHA256) > 0 {
		h := hex.EncodeToString(f.SHA256)
		out.Sha256 = &h
	}
	return out, nil
}

const (
	defaultStringsPage = 200
	maxStringsPage     = 1000
	defaultMinString   = 4
)

func (r *minionTaskResolver) MinionFileStrings(ctx context.Context, fileID string, after, first, minLength *int, encoding, search *string) (*model.MinionFileStringPage, error) {
	id, err := uuid.Parse(fileID)
	if err != nil || r.files == nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid file ID")
	}
	f, err := r.files.FileByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, moduleGQLError(ctx, errCodeNotFound, "file not found")
	}
	if _, err := r.session(ctx, f.SessionID.String(), models.OperationRoleViewer); err != nil {
		return nil, err
	}
	q := minionfiles.StringsQuery{First: defaultStringsPage, MinLength: defaultMinString}
	if after != nil {
		q.After = int64(*after)
	}
	if first != nil {
		q.First = min(max(*first, 1), maxStringsPage)
	}
	if minLength != nil {
		if *minLength < 2 || *minLength > 256 {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "minLength is 2 to 256")
		}
		q.MinLength = *minLength
	}
	if encoding != nil && *encoding != "" {
		if *encoding != minionfiles.EncodingASCII && *encoding != minionfiles.EncodingUTF16LE {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "encoding is ascii or utf16le")
		}
		q.Encoding = *encoding
	}
	if search != nil {
		q.Search = *search
	}
	if f.State != models.MinionFileComplete {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "the file is "+strings.ToLower(f.State))
	}
	if q.After < 0 || q.After > f.Total {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "after is past the file's end")
	}
	page, err := r.files.Strings(ctx, *f, q)
	if err != nil {
		return nil, fmt.Errorf("read the file's strings: %w", err)
	}
	out := &model.MinionFileStringPage{Strings: make([]*model.MinionFileString, 0, len(page.Strings)), Next: int(page.Next), Done: page.Done}
	for _, s := range page.Strings {
		out.Strings = append(out.Strings, &model.MinionFileString{Offset: int(s.Off), Length: int(s.Len), Encoding: s.Encoding, Text: s.Text})
	}
	return out, nil
}

func (r *minionTaskResolver) MinionStreamLink(ctx context.Context, taskID string) (*model.MinionStreamLink, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil || r.streams == nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "task not found")
		}
		return nil, err
	}
	// Downloading a stream takes a viewer, as any download; uploading one
	// puts a file on the host, an operator's.
	role := models.OperationRoleViewer
	if t.Upload != nil && t.Upload.Stream {
		role = models.OperationRoleOperator
	}
	if _, err := r.session(ctx, t.SessionID.String(), role); err != nil {
		return nil, err
	}
	streams := t.Result == protocol.ResultFileStream || (t.Upload != nil && t.Upload.Stream)
	if !streams || t.Finished() {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "the task streams nothing now")
	}
	path, exp := r.streams.Link(tid)
	return &model.MinionStreamLink{Path: path, ExpiresAt: exp.UTC().Format(time.RFC3339)}, nil
}

func (r *minionTaskResolver) Load(ctx context.Context, taskID string) *model.MinionTask {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		return nil
	}
	m, err := r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
	if err != nil {
		return nil
	}
	return m
}

func (r *minionTaskResolver) publish(t *models.MinionTask) {
	r.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(),
	}))
}

// names are the usernames of those who gave the tasks, in one read; a gone
// user has none.
func (r *minionTaskResolver) names(ctx context.Context, tasks []models.MinionTask) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	ids := make([]uuid.UUID, 0, len(tasks))
	for _, t := range tasks {
		if !slices.Contains(ids, t.IssuedBy) {
			ids = append(ids, t.IssuedBy)
		}
	}
	if len(ids) == 0 || r.users == nil {
		return out
	}
	users, err := r.users.FindByIDs(ctx, ids)
	if err != nil {
		return out
	}
	for _, u := range users {
		out[u.UserID] = u.Username
	}
	return out
}

// toModel is a task as GraphQL; its output and data are read when asked
// for (Output, Data).
func (r *minionTaskResolver) toModel(_ context.Context, t *models.MinionTask, names map[uuid.UUID]string) (*model.MinionTask, error) {
	m := &model.MinionTask{
		ID: t.TaskID.String(), Rev: int(t.Rev), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(), Command: t.Command,
		IssuedBy: t.IssuedBy.String(), State: t.State, QueuedAt: t.QueuedAt.UTC().Format(time.RFC3339Nano),
		SentAt: taskTime(t.SentAt), SendCount: t.SendCount, AckedAt: taskTime(t.AckedAt), StartedAt: taskTime(t.StartedAt),
		DoneAt: taskTime(t.DoneAt), CancelRequestedAt: taskTime(t.CancelRequestedAt), CancelReason: strPtr(t.CancelReason), Ok: t.OK, Err: strPtr(t.Err),
		OutputSize: int(t.OutSize), OutputTruncated: t.OutTruncated, Parts: t.Parts, DataParts: t.DataParts,
		Result: strPtr(t.Result), Action: strPtr(t.Action), DataSize: int(t.DataSize), DataTruncated: t.DataTruncated,
		DataProblem: strPtr(t.DataProblem), MissingParts: t.MissingParts(),
		FeedbackErrors: t.FeedbackErrors, FeedbackWarnings: t.FeedbackWarnings, As: toIdentityModel(t.As),
	}
	if name, ok := names[t.IssuedBy]; ok {
		m.IssuedByName = &name
	}
	if len(t.Args) > 0 {
		m.Args = map[string]any(t.Args)
	}
	if t.Exit != nil {
		exit := int(*t.Exit)
		m.Exit = &exit
	}
	if st := t.Stream; st != nil {
		m.Stream = &model.MinionTaskStream{Name: st.Name, Size: int(st.Size), Sent: int(st.Want), Delivered: int(st.Delivered),
			State: r.streamState(t), AnnouncedAt: st.AnnouncedAt.UTC().Format(time.RFC3339Nano), From: int(st.From),
			AttachedAt: taskTime(st.AttachedAt), ReaderAt: taskTime(st.ReaderAt)}
		if len(st.SHA256) > 0 {
			h := hex.EncodeToString(st.SHA256)
			m.Stream.Sha256 = &h
		}
	}
	if up := t.Upload; up != nil {
		m.Upload = &model.MinionTaskUpload{FileID: up.FileID.String(), Name: up.Name, Size: int(up.Size),
			Sha256: hex.EncodeToString(up.SHA256), Acked: int(up.Acked), State: uploadState(t),
			FirstAckAt: taskTime(up.FirstAckAt), LastAckAt: taskTime(up.LastAckAt),
			Stream: up.Stream, Sized: up.Sized || !up.Stream, Received: int(up.Received), From: int(up.From),
			AttachedAt: taskTime(up.AttachedAt), UploaderAt: taskTime(up.UploaderAt)}
		if up.Stream {
			st := r.uploaderState(t)
			m.Upload.UploaderState = &st
		}
		if up.Stream && up.FileID == uuid.Nil {
			m.Upload.FileID = ""
		}
	}
	return m, nil
}

// streamState is where a task's stream stands (MinionTaskStream.state).
func (r *minionTaskResolver) streamState(t *models.MinionTask) string {
	st := t.Stream
	gone := 30 * time.Second
	if r.streams != nil {
		gone = r.streams.ReaderGone()
	}
	switch {
	case t.State == models.MinionTaskDone:
		return "DONE"
	case t.State == models.MinionTaskCancelled:
		return "CANCELLED"
	case t.Finished():
		return "FAILED"
	case st.Ran && st.Delivered >= st.Size:
		// The downloader has it all; the minion ends the task at its next
		// check-in.
		return "DONE"
	case st.Reader != "" && st.ReaderAt != nil && st.ReaderAt.After(time.Now().Add(-gone)):
		return "STREAMING"
	case st.Ran:
		return "PAUSED"
	}
	return "WAITING"
}

// uploadState is where a task's upload stands (MinionTaskUpload.state).
func uploadState(t *models.MinionTask) string {
	up := t.Upload
	switch {
	case t.State == models.MinionTaskDone && t.DataProblem == "":
		return "DONE"
	case t.State == models.MinionTaskCancelled:
		return "CANCELLED"
	case t.Finished():
		return "FAILED"
	case up.FirstAckAt == nil:
		return "WAITING"
	case up.Acked < up.Size || (up.Stream && !up.Sized):
		return "SENDING"
	}
	return "SENT"
}

// uploaderState is where a stream's uploader stands
// (MinionTaskUpload.uploaderState).
func (r *minionTaskResolver) uploaderState(t *models.MinionTask) string {
	up := t.Upload
	gone := 30 * time.Second
	if r.streams != nil {
		gone = r.streams.ReaderGone()
	}
	switch {
	case up.Sized && up.Received >= up.Size:
		return "DONE"
	case t.Finished():
		return "STOPPED"
	case up.Uploader != "" && up.UploaderAt != nil && up.UploaderAt.After(time.Now().Add(-gone)):
		return "UPLOADING"
	case up.Received > 0:
		return "PAUSED"
	}
	return "WAITING"
}

func taskTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

// A page cursor: the last task's queue time (unix nanoseconds) and id.
func encodeTaskCursor(t models.MinionTask) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.QueuedAt.UnixNano(), 10) + ":" + t.TaskID.String()))
}

func decodeTaskCursor(s string) (*repository.MinionTaskCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	at, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, fmt.Errorf("cursor")
	}
	ns, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return nil, err
	}
	tid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	return &repository.MinionTaskCursor{QueuedAt: time.Unix(0, ns).UTC(), TaskID: tid}, nil
}

// putCredentials puts the credentials a task's credential params name in
// wire, in place of their ids: a minion has no way to look one up. Each one
// of the operation's, of a type its param takes; their ids come back, for
// the task to keep.
func (r *minionTaskResolver) putCredentials(ctx context.Context, op uuid.UUID, params []protocol.FieldSpec, wire map[string]any) ([]uuid.UUID, error) {
	ids := credentialrefs.IDs(params, wire)
	if len(ids) == 0 {
		return nil, nil
	}
	if r.creds == nil {
		return nil, errors.New("core resolves no credentials here")
	}
	given, err := r.creds.ResolveFields(ctx, op, params, wire)
	if err != nil {
		return nil, err
	}
	for _, p := range params {
		if p.Type != protocol.FieldCredential && p.Type != protocol.FieldCredentialList {
			continue
		}
		list, err := protocol.FieldCredentials(p, wire[p.Key], given)
		if err != nil {
			return nil, err
		}
		switch {
		case p.Type == protocol.FieldCredentialList:
			wire[p.Key] = list
		case len(list) == 1:
			wire[p.Key] = list[0]
		}
	}
	return ids, nil
}
