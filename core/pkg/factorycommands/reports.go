package factorycommands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// The factory → core reports, served on logos.core.rpc. An instance reports
// only the tasks it runs: anything else is not_found, which tells it core no
// longer waits for the task (stopped, or another's), and it stops it.

// HandleOutput serves factory.command.output: what a task's command printed,
// kept as its output once per seq.
func (s *Service) HandleOutput(ctx context.Context, req messaging.Envelope) (any, error) {
	var o protocol.FactoryCommandOutput
	if err := json.Unmarshal(req.Payload, &o); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, "malformed output payload")
	}
	if err := o.Validate(); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, err.Error())
	}
	// Validate checked the task id is a UUID.
	after, kept, err := s.tasks.FactoryOutput(ctx, uuid.MustParse(o.TaskID), req.Source.Instance, o.Seq, o.Out, s.now())
	if repository.IsNotFound(err) {
		return nil, notWaiting(o.TaskID, req.Source.Instance)
	}
	if err != nil {
		return nil, fmt.Errorf("record output: %w", err)
	}
	if kept && len(o.Out) > 0 {
		s.publish(after)
	}
	return protocol.FactoryCommandOutputReply{TaskID: o.TaskID, Seq: o.Seq, Ack: true}, nil
}

// HandleResult serves factory.command.result: how a task ended, with its
// data (inline, or a file the factory put, which core takes and drops) and
// the files the factory put that are the task's own. A result sent again is
// answered as the first was. Data or files a result names wrongly fail the
// task, saying why: the factory's code would send them so again.
func (s *Service) HandleResult(ctx context.Context, req messaging.Envelope) (any, error) {
	var r protocol.FactoryCommandResult
	if err := json.Unmarshal(req.Payload, &r); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, "malformed result payload")
	}
	if err := r.Validate(); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, err.Error())
	}
	instance := req.Source.Instance
	recorded := protocol.FactoryCommandResultReply{TaskID: r.TaskID, Recorded: true}
	t, err := s.tasks.FindByTaskID(ctx, uuid.MustParse(r.TaskID)) // checked by Validate
	if repository.IsNotFound(err) {
		return nil, notWaiting(r.TaskID, instance)
	}
	if err != nil {
		return nil, fmt.Errorf("load the task: %w", err)
	}
	switch {
	case t.Factory == nil || t.Factory.Instance != instance:
		return nil, notWaiting(r.TaskID, instance)
	case t.Finished() && t.Factory.ResultAt != nil:
		// The same result again (its reply was lost).
		return recorded, nil
	case t.State != models.MinionTaskFactory:
		return nil, notWaiting(r.TaskID, instance)
	}

	res, drop, why := s.result(ctx, t, r)
	if why == "" && len(r.Files) > 0 && s.files != nil {
		ids := make([]uuid.UUID, len(r.Files))
		for i, id := range r.Files {
			ids[i] = uuid.MustParse(id) // checked by Validate
		}
		if err := s.files.AdoptFactoryFiles(ctx, t, ids); err != nil {
			why = "files: " + err.Error()
		}
	}
	if why != "" {
		s.fail(ctx, t, why, false)
		return recorded, nil
	}
	after, err := s.tasks.FinishFactory(ctx, t.TaskID, instance, res)
	if repository.IsNotFound(err) {
		// Ended meanwhile: stopped, or core gave up on it.
		return nil, notWaiting(r.TaskID, instance)
	}
	if err != nil {
		return nil, fmt.Errorf("record the result: %w", err)
	}
	for _, id := range drop {
		if err := s.files.DropTaskFile(ctx, id); err != nil {
			s.logger.Warn("factory commands: drop a file the factory put", zap.String("file_id", id.String()), zap.Error(err))
		}
	}
	s.logger.Info("factory command's task ended", zap.String("task_id", t.TaskID.String()), zap.String("state", after.State))
	s.ended(ctx, after, false)
	return recorded, nil
}

// result is what a result ends its task with, its data taken (inline, or the
// file it names, to drop once kept) and checked against the task's result
// within its budget; why says what in it core cannot take.
func (s *Service) result(ctx context.Context, t *models.MinionTask, r protocol.FactoryCommandResult) (repository.FactoryResult,
	[]uuid.UUID, string) {
	res := repository.FactoryResult{State: models.MinionTaskFailed, OK: r.OK, Err: r.Err, Exit: r.Exit, At: s.now()}
	if r.State == protocol.EndDone && r.OK {
		res.State = models.MinionTaskDone
	}
	budget := max(int64(models.MaxMinionTaskOutput)-t.OutSize-t.DataSize, 0)
	data := []byte(r.Data)
	var drop []uuid.UUID
	if r.DataFile != "" {
		b, id, cut, err := s.takeFile(ctx, t, uuid.MustParse(r.DataFile), budget) // checked by Validate
		if err != nil {
			return res, nil, "data_file: " + err.Error()
		}
		drop = append(drop, id)
		if cut {
			res.DataCut = true
			return res, drop, ""
		}
		data = b
	}
	if len(data) == 0 {
		return res, drop, ""
	}
	cbor, err := taskdata.FromJSON(data)
	switch {
	case err != nil:
		res.Problem = "data: " + err.Error()
	case int64(len(cbor)) > budget:
		res.DataCut = true
	default:
		res.Data = cbor
		seq := uint64(max(t.Factory.Seq, 0)) //nolint:gosec // not negative
		if err := taskdata.Check(t.Result, seq, cbor); err != nil {
			res.Problem = fmt.Sprintf("part %d: %v", seq, err)
		}
	}
	return res, drop, ""
}

// takeFile reads a file the factory put for the task, max bytes at most (cut:
// it had more).
func (s *Service) takeFile(ctx context.Context, t *models.MinionTask, id uuid.UUID, max int64) ([]byte, uuid.UUID, bool, error) {
	if s.files == nil {
		return nil, uuid.Nil, false, errNotPut
	}
	f, err := s.files.FileByID(ctx, id)
	if err != nil {
		return nil, uuid.Nil, false, err
	}
	if f == nil || f.Kind != models.MinionFileKindTask || f.ForTask != t.TaskID || f.State != models.MinionFileComplete {
		return nil, uuid.Nil, false, errNotPut
	}
	body, _, err := s.files.Open(ctx, f.FileID)
	if err != nil {
		return nil, uuid.Nil, false, err
	}
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, uuid.Nil, false, err
	}
	if int64(len(b)) > max {
		return nil, f.FileID, true, nil
	}
	return b, f.FileID, false, nil
}

func rpcErr(code, msg string) error { return &messaging.RPCError{Code: code, Message: msg} }

func notWaiting(task, instance string) error {
	return rpcErr(protocol.CodeNotFound, fmt.Sprintf("core does not wait for task %s from %s", task, instance))
}
