package resolver

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
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
	QueueMinionTask(ctx context.Context, sessionID, command string, args any) (*model.MinionTask, error)
	CancelMinionTask(ctx context.Context, id string) (*model.MinionTask, error)
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

// TaskTunnels opens a tunnel task's operational port, and closes it
// (miniontunnels.Service).
type TaskTunnels interface {
	Open(ctx context.Context, o miniontunnels.Opening) (int, error)
	Close(ctx context.Context, task uuid.UUID)
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
	tunnel := commands[i].Action == protocol.ActionTunnel
	if tunnel {
		port, err := r.openTunnel(ctx, s, uid, taskID, wire)
		if err != nil {
			return nil, err
		}
		// The task shows the port it serves on.
		values["port"] = port
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

// openTunnel takes an operational port for a tunnel task about to be
// queued (its port arg: 0 for any free one) and puts it in the task's
// args: core's SOCKS proxy listens there while the task runs.
func (r *minionTaskResolver) openTunnel(ctx context.Context, s *models.MinionSession, uid, taskID uuid.UUID, wire map[string]any) (int, error) {
	if r.tunnels == nil {
		return 0, moduleGQLError(ctx, errCodeBadUserInput, "core opens no tunnels")
	}
	asked, _ := protocol.ArgInt(wire, "port")
	o := miniontunnels.Opening{Task: taskID, Session: s.SessionID, Operation: s.OperationID, By: uid, Port: int(max(min(asked, 65536), -1)),
		MaxStreams: miniontunnels.DefaultMaxStreams, Timeout: miniontunnels.DefaultConnectTimeout}
	if n, ok := protocol.ArgInt(wire, "max_streams"); ok && n > 0 {
		o.MaxStreams = int(min(n, 1<<16))
	}
	if d, ok := protocol.ArgDuration(wire, "timeout"); ok && d > 0 {
		o.Timeout = d
	}
	port, err := r.tunnels.Open(ctx, o)
	if errors.Is(err, miniontunnels.ErrNoPort) {
		return 0, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	if err != nil {
		return 0, err
	}
	wire["port"] = int64(port)
	return port, nil
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
		DoneAt: taskTime(t.DoneAt), CancelRequestedAt: taskTime(t.CancelRequestedAt), Ok: t.OK, Err: strPtr(t.Err),
		OutputSize: int(t.OutSize), OutputTruncated: t.OutTruncated, Parts: t.Parts, DataParts: t.DataParts,
		Result: strPtr(t.Result), DataSize: int(t.DataSize), DataTruncated: t.DataTruncated,
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
