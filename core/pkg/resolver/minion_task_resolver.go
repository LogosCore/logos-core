package resolver

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// IMinionTaskResolver gives sessions' minions commands and reads what they
// answered (models.MinionTask; pkg/minionsync delivers them).
type IMinionTaskResolver interface {
	MinionTasks(ctx context.Context, sessionID string, last *int, before *string) (*model.MinionTaskPage, error)
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

type minionTaskResolver struct {
	tasks         repository.IMinionTaskRepository
	sessions      repository.IMinionSessionRepository
	builds        TaskBuilds
	schemas       TaskSchemas
	users         repository.IUserRepository
	operationRepo repository.IOperationRepository
	bus           eventbus.IEventBus
}

func NewMinionTaskResolver(tasks repository.IMinionTaskRepository, sessions repository.IMinionSessionRepository, builds TaskBuilds,
	schemas TaskSchemas, users repository.IUserRepository, operationRepo repository.IOperationRepository, bus eventbus.IEventBus) IMinionTaskResolver {
	return &minionTaskResolver{tasks: tasks, sessions: sessions, builds: builds, schemas: schemas, users: users,
		operationRepo: operationRepo, bus: bus}
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
		// help is core's own: a schema from before it was reserved does not
		// shadow it.
		if slices.Contains(b.Commands, c.ID) && c.ID != protocol.CommandHelp {
			out = append(out, c)
		}
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
	wireBytes, err := protocol.MarshalCBOR(wire)
	if err != nil {
		return nil, fmt.Errorf("encode the args: %w", err)
	}
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: command,
		Args: values, WireArgs: wireBytes, Handler: commands[i].Handler, IssuedBy: uid, State: models.MinionTaskQueued,
		QueuedAt: time.Now().UTC()}
	if err := r.tasks.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("queue the task: %w", err)
	}
	r.publish(t)
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
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
	parts, err := r.parts(ctx, taskID)
	if err != nil {
		return nil, err
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
	parts, err := r.parts(ctx, taskID)
	if err != nil {
		return nil, err
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
		ID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(), Command: t.Command,
		IssuedBy: t.IssuedBy.String(), State: t.State, QueuedAt: t.QueuedAt.UTC().Format(time.RFC3339Nano),
		SentAt: taskTime(t.SentAt), SendCount: t.SendCount, AckedAt: taskTime(t.AckedAt), StartedAt: taskTime(t.StartedAt),
		DoneAt: taskTime(t.DoneAt), CancelRequestedAt: taskTime(t.CancelRequestedAt), Ok: t.OK, Err: strPtr(t.Err),
		OutputSize: int(t.OutSize), OutputTruncated: t.OutTruncated, Parts: t.Parts,
		Handler: strPtr(t.Handler), DataSize: int(t.DataSize), DataTruncated: t.DataTruncated,
		DataProblem: strPtr(t.DataProblem), MissingParts: t.MissingParts(),
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
	return m, nil
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
