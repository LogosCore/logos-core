package resolver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/composites"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// A composite command's subtasks and its own op (docs: contracts/
// task-outsourcing), queued for its factory's decisions by core's
// composites service: checked as an operator's task is, by the same code
// (queue). A check that fails is a refusal (composites.Refusal): the
// subtask, or the op, ends refused, saying why.

// QueueSubtask queues one of parent's subtasks under its task id, issued by
// whoever queued parent: its command one the composite uses that the
// session has.
func (r *minionTaskResolver) QueueSubtask(ctx context.Context, parent *models.MinionTask, sub models.MinionSubtask) (*models.MinionTask, error) {
	if t, err := r.tasks.FindByTaskID(ctx, sub.TaskID); err == nil {
		return t, nil
	} else if !repository.IsNotFound(err) {
		return nil, err
	}
	s, sc, commands, err := r.compositeSession(ctx, parent)
	if err != nil {
		return nil, err
	}
	pc, ok := sc.Command(parent.Command)
	if !ok || !slices.Contains(pc.Uses, sub.Command) {
		return nil, &composites.Refusal{Reason: fmt.Sprintf("%q is not one of the commands %s uses", sub.Command, parent.Command)}
	}
	i := slices.IndexFunc(commands, func(c protocol.BuildCommand) bool { return c.ID == sub.Command })
	if i < 0 || protocol.IsCoreCommand(sub.Command) {
		return nil, &composites.Refusal{Reason: fmt.Sprintf("the session's minion has no %q command", sub.Command)}
	}
	cmd := commands[i]
	values := map[string]any{}
	if sub.Args != nil {
		values = maps.Clone(map[string]any(sub.Args))
	}
	wire, problems := protocol.TaskArgs(cmd.Params, values)
	if problems != nil {
		return nil, &composites.Refusal{Reason: problemsText(problems)}
	}
	if s.Closed(time.Now().UTC()) {
		return nil, &composites.Refusal{Reason: "the session is closed: its minion takes no tasks"}
	}
	t, err := r.queue(ctx, s, cmd, values, wire, queueing{by: parent.IssuedBy, files: r.compositeFile(parent), parent: parent, sub: &sub})
	if err != nil {
		return nil, refusal(err)
	}
	r.publish(t)
	return t, nil
}

// SendOwnOp sends t's own op with args (t's own when nil), checked as an
// operator's: t goes to its minion, queued. One sent already comes back as
// it is.
func (r *minionTaskResolver) SendOwnOp(ctx context.Context, t *models.MinionTask, args map[string]any) (*models.MinionTask, error) {
	if t.Composite == nil || t.Composite.OwnOp != string(protocol.OwnOpNotRun) {
		return t, nil
	}
	s, _, commands, err := r.compositeSession(ctx, t)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(commands, func(c protocol.BuildCommand) bool { return c.ID == t.Command })
	if i < 0 || !commands[i].HasOp() {
		return nil, &composites.Refusal{Reason: fmt.Sprintf("the session's minion has no %q op", t.Command)}
	}
	cmd := commands[i]
	values := map[string]any(maps.Clone(t.Args))
	if args != nil {
		values = maps.Clone(args)
	}
	if values == nil {
		values = map[string]any{}
	}
	wire, problems := protocol.TaskArgs(cmd.Params, values)
	if problems != nil {
		return nil, &composites.Refusal{Reason: problemsText(problems)}
	}
	if s.Closed(time.Now().UTC()) {
		return nil, &composites.Refusal{Reason: "the session is closed: its minion takes no tasks"}
	}
	if err := overKind(ctx, s, cmd); err != nil {
		return nil, refusal(err)
	}
	if err := r.reachable(ctx, s); err != nil {
		return nil, refusal(err)
	}
	upload, err := r.offerFile(ctx, s, cmd.Params, wire, r.compositeFile(t), false)
	if err != nil {
		return nil, refusal(err)
	}
	credentialIDs, err := r.putCredentials(ctx, s.OperationID, cmd.Params, wire)
	if err != nil {
		return nil, &composites.Refusal{Reason: err.Error()}
	}
	var wireBytes []byte
	if numbered := protocol.WireArgs(cmd.Params, wire); len(numbered) > 0 {
		if wireBytes, err = protocol.MarshalCBOR(numbered); err != nil {
			return nil, fmt.Errorf("encode the args: %w", err)
		}
	}
	var own models.JSONMap
	if args != nil {
		own = models.JSONMap(values)
	}
	after, err := r.tasks.SendOwnOp(ctx, t.TaskID, cmd.Op, wireBytes, upload, credentialIDs, own, time.Now().UTC())
	if repository.IsNotFound(err) {
		return r.tasks.FindByTaskID(ctx, t.TaskID)
	}
	if err != nil {
		return nil, err
	}
	r.publish(after)
	return after, nil
}

// compositeSession is a composite task's session, its factory's schema, and
// the commands the session has.
func (r *minionTaskResolver) compositeSession(ctx context.Context, t *models.MinionTask) (*models.MinionSession, *protocol.BuildSchema,
	[]protocol.BuildCommand, error) {
	s, err := r.sessions.FindBySessionID(ctx, t.SessionID)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, nil, nil, &composites.Refusal{Reason: "its session is gone"}
		}
		return nil, nil, nil, err
	}
	sc, commands, err := r.commandsOf(ctx, s)
	if err != nil {
		return nil, nil, nil, refusal(err)
	}
	return s, sc, commands, nil
}

// compositeFile is how a file param of t's subtasks, and of its own op,
// names its file: one of the operation's task files (an operator's, or one
// t's factory put for it), or a file t, or one of its subtasks, got.
func (r *minionTaskResolver) compositeFile(t *models.MinionTask) fileLookup {
	return func(ctx context.Context, operationID uuid.UUID, id string) (*models.MinionFile, error) {
		f, err := r.taskFile(ctx, operationID, id)
		if !errors.Is(err, minionfiles.ErrNoTaskFile) {
			return f, err
		}
		fid, perr := uuid.Parse(id)
		if perr != nil || r.files == nil {
			return nil, minionfiles.ErrNoTaskFile
		}
		f, ferr := r.files.FileByID(ctx, fid)
		if ferr != nil || f == nil || f.OperationID != operationID || f.State != models.MinionFileComplete {
			return nil, minionfiles.ErrNoTaskFile
		}
		if f.TaskID == t.TaskID {
			return f, nil
		}
		got, terr := r.tasks.FindByTaskID(ctx, f.TaskID)
		if terr != nil || got.ParentID == nil || *got.ParentID != t.TaskID {
			return nil, minionfiles.ErrNoTaskFile
		}
		return f, nil
	}
}

// refusal is a queueing check's GraphQL error as a refusal, saying why;
// any other error as it is.
func refusal(err error) error {
	var gql *gqlerror.Error
	if errors.As(err, &gql) {
		return &composites.Refusal{Reason: gql.Message}
	}
	return err
}

// MinionSubtasks are a composite task's subtasks, in the order they were
// queued: the caller a viewer of its session's operation.
func (r *minionTaskResolver) MinionSubtasks(ctx context.Context, taskID string) ([]*model.MinionTask, error) {
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
	out := []*model.MinionTask{}
	if t.Composite == nil {
		return out, nil
	}
	subs, err := r.tasks.ListSubtasks(ctx, t.TaskID)
	if err != nil {
		return nil, fmt.Errorf("list the subtasks: %w", err)
	}
	names := r.names(ctx, subs)
	for i := range subs {
		m, err := r.toModel(ctx, &subs[i], names)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// compositeModel is a composite task's record as GraphQL: its subtasks
// under way counted now.
func (r *minionTaskResolver) compositeModel(ctx context.Context, t *models.MinionTask) *model.MinionComposite {
	c := t.Composite
	m := &model.MinionComposite{Status: strPtr(c.Status), OwnOp: strPtr(c.OwnOp), Phase: strPtr(c.Phase), Calls: c.Seq,
		TimerAt: taskTime(c.TimerAt), SilentSince: taskTime(c.SilentSince), StoppedAt: taskTime(c.StoppedAt)}
	if c.OwnArgs != nil {
		m.OwnArgs = map[string]any(c.OwnArgs)
	}
	if e := c.OwnEnd; e != nil {
		m.OwnEnd = &model.MinionComposeOwnEnd{State: e.State, Ok: e.OK, Err: strPtr(e.Err), At: e.At.UTC().Format(time.RFC3339Nano)}
		if e.Exit != nil {
			exit := int(*e.Exit)
			m.OwnEnd.Exit = &exit
		}
	}
	under := len(c.Waiting)
	if !t.Finished() {
		if n, err := r.tasks.CountOpenSubtasks(ctx, t.TaskID); err == nil {
			under += n
		}
	}
	m.SubtasksUnderWay, m.SubtasksEnded = under, max(c.Subtasks-under, 0)
	return m
}
