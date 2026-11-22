package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Factory commands (docs: contracts/factory-commands): commands a factory
// runs itself, never a minion, which every session of its builds has. A
// session lists them beside its build's commands; a task of one waits in core
// (FACTORY) for an instance of its factory to take it.

// TaskFactoryCommands runs factory commands' tasks (factorycommands.Service):
// one queued is offered to its factory, an operator's stop stops it.
type TaskFactoryCommands interface {
	Queued(t *models.MinionTask)
	StopTask(ctx context.Context, t *models.MinionTask) (*models.MinionTask, error)
}

// WithTaskFactoryCommands gives the resolver what runs factory commands'
// tasks: without it, a session has none.
func WithTaskFactoryCommands(r IMinionTaskResolver, f TaskFactoryCommands) IMinionTaskResolver {
	if tr, ok := r.(*minionTaskResolver); ok {
		tr.factory = f
	}
	return r
}

// factoryBuildCommand is a factory command as a session lists its commands:
// no op, no targets, nothing for a minion.
func factoryBuildCommand(fc protocol.FactoryCommand) protocol.BuildCommand {
	return protocol.BuildCommand{ID: fc.ID, Label: fc.Label, Description: fc.Description, Params: fc.Params, Result: fc.Result}
}

// factoryCommandOf is a factory command of sc by id: none for a command's id
// (a schema keeps the two apart) or core's own.
func factoryCommandOf(sc *protocol.BuildSchema, id string) (protocol.FactoryCommand, bool) {
	if sc == nil || models.IsCoreCommand(id) {
		return protocol.FactoryCommand{}, false
	}
	if _, ok := sc.Command(id); ok {
		return protocol.FactoryCommand{}, false
	}
	return sc.FactoryCommand(id)
}

// factoryIDs are the ids of commands that are sc's factory commands.
func factoryIDs(sc *protocol.BuildSchema, commands []protocol.BuildCommand) []string {
	var out []string
	for _, c := range commands {
		if _, ok := factoryCommandOf(sc, c.ID); ok {
			out = append(out, c.ID)
		}
	}
	return out
}

// toFactoryCommandModel is a factory command as GraphQL shows it: a command
// marked factory, with no targets.
func toFactoryCommandModel(sc protocol.BuildSchema, fc protocol.FactoryCommand) *model.BuildCommand {
	m := toBuildCommandModel(sc, factoryBuildCommand(fc))
	m.Factory, m.Targets = true, []string{}
	return m
}

// queueFactory checks a factory command's task as an operator's (its args,
// the task files and credentials they name) and keeps it: it waits in core
// (FACTORY) for an instance of its factory to take it. A check that fails is
// a GraphQL error with the reason; the task is not published.
func (r *minionTaskResolver) queueFactory(ctx context.Context, s *models.MinionSession, fc protocol.FactoryCommand, values map[string]any,
	uid uuid.UUID) (*models.MinionTask, error) {
	b, err := r.builds.FindByBuildID(ctx, s.BuildID)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "the session's build is gone: its factory would get no build")
		}
		return nil, fmt.Errorf("load the build: %w", err)
	}
	args, problems := fc.Args(values)
	if problems != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, problemsText(problems))
	}
	credentialIDs, err := r.factoryCredentials(ctx, s.OperationID, fc.Params, args)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	files, err := r.factoryFiles(ctx, s.OperationID, fc.Params, args)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: fc.ID, Args: values,
		CredentialIDs: credentialIDs, Result: fc.Result, IssuedBy: uid, State: models.MinionTaskFactory, QueuedAt: now,
		LineRest: lineRest(fc.Params), Factory: &models.MinionFactoryRun{Module: b.ModuleName, Args: args, Files: files, OfferAt: &now}}
	t.LineNames = r.lineNames(ctx, s, fc.Params, t.Args)
	t.LineJSON = lineJSON(fc.Params, t.Args)
	if err := r.tasks.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("queue the task: %w", err)
	}
	return t, nil
}

// factoryCredentials are the credentials a factory command's args name
// (credential params), each one of the operation's of a type its param
// takes: the factory gets them with its call.
func (r *minionTaskResolver) factoryCredentials(ctx context.Context, op uuid.UUID, params []protocol.FieldSpec, args map[string]any) ([]uuid.UUID, error) {
	ids := credentialrefs.IDs(params, args)
	if len(ids) == 0 {
		return nil, nil
	}
	if r.creds == nil {
		return nil, errors.New("core resolves no credentials here")
	}
	if _, err := r.creds.ResolveFields(ctx, op, params, args); err != nil {
		return nil, err
	}
	return ids, nil
}

// factoryFiles are the task files a factory command's args name (its file
// param), each one of the operation's that the param takes: the factory gets
// them by link.
func (r *minionTaskResolver) factoryFiles(ctx context.Context, op uuid.UUID, params []protocol.FieldSpec, args map[string]any) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, p := range params {
		id, ok := args[p.Key].(string)
		if p.Type != protocol.FieldFile || !ok || id == "" {
			continue
		}
		f, err := r.taskFile(ctx, op, id)
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
		out = append(out, f.FileID)
	}
	return out, nil
}

// toFactoryModel is where a factory command's task stands, as GraphQL shows
// it.
func toFactoryModel(f *models.MinionFactoryRun) *model.MinionTaskFactory {
	return &model.MinionTaskFactory{Module: f.Module, Instance: strPtr(f.Instance), AcceptedAt: taskTime(f.AcceptedAt),
		ReportAt: taskTime(f.ReportAt), Waiting: strPtr(f.Waiting), WaitingSince: taskTime(f.WaitingSince), ResultAt: taskTime(f.ResultAt)}
}
