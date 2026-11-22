package composites

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"go.uber.org/zap"
)

// finish ends the task as its factory's finish says, once what was under
// way has ended. Its message or err takes the status line's place. A
// command with no op of its own ends with its own output and data (inline,
// or a file the factory put, which core takes and drops), or a subtask's
// result; one with an op keeps its own op's.
func (s *Service) finish(ctx context.Context, lk *look) error {
	t, f := lk.t, lk.t.Composite.Finish
	if f == nil {
		f = &models.MinionComposeFinish{OK: false, Err: "it ended with no finish"}
	}
	state := models.MinionTaskDone
	if !f.OK {
		state = models.MinionTaskFailed
	}
	line := f.Message
	if !f.OK {
		line = f.Err
	}
	u := repository.ComposeUpdate{Unset: []string{"status"}}
	if line != "" {
		u = repository.ComposeUpdate{Set: map[string]any{"status": line}}
	}
	if _, err := s.tasks.UpdateComposite(ctx, t.TaskID, u); err != nil {
		return ignoreNotFound(err)
	}
	ok := f.OK
	if lk.cmd.HasOp() || (f.ResultOf == "" && len(f.Out) == 0 && f.OutFile == "" && len(f.Data) == 0 && f.DataFile == "") {
		return s.end(ctx, lk, state, &ok, f.Err)
	}
	res := repository.CompositeResult{State: state, OK: f.OK, Err: f.Err, At: s.now()}
	var drop []uuid.UUID
	if f.ResultOf != "" {
		id, err := uuid.Parse(f.ResultOf)
		if err != nil {
			return s.end(ctx, lk, models.MinionTaskFailed, new(bool), "result_of names no subtask")
		}
		sub, err := s.tasks.Subtask(ctx, t.TaskID, id)
		if err != nil {
			return s.end(ctx, lk, models.MinionTaskFailed, new(bool), "result_of names no subtask")
		}
		res.From = sub
	} else {
		budget := int64(models.MaxMinionTaskOutput)
		out := f.Out
		if f.OutFile != "" {
			b, id, cut, err := s.takeFile(ctx, t, f.OutFile, budget)
			if err != nil {
				return s.end(ctx, lk, models.MinionTaskFailed, new(bool), "out_file: "+err.Error())
			}
			out, res.OutCut = b, cut
			drop = append(drop, id)
		}
		budget -= int64(len(out))
		data := f.Data
		if f.DataFile != "" {
			b, id, cut, err := s.takeFile(ctx, t, f.DataFile, budget)
			if err != nil {
				return s.end(ctx, lk, models.MinionTaskFailed, new(bool), "data_file: "+err.Error())
			}
			if cut {
				b, res.DataCut = nil, true
			}
			data = b
			drop = append(drop, id)
		}
		res.Out = out
		if len(data) > 0 {
			cbor, err := taskdata.FromJSON(data)
			switch {
			case err != nil:
				res.Problem = "data: " + err.Error()
			case int64(len(cbor)) > budget:
				res.DataCut = true
			default:
				res.Data = cbor
				if err := taskdata.Check(t.Result, 0, cbor); err != nil {
					res.Problem = fmt.Sprintf("part 0: %v", err)
				}
			}
		}
	}
	after, err := s.tasks.FinishComposite(ctx, t.TaskID, res)
	if err != nil {
		return ignoreNotFound(err)
	}
	if res.From != nil && s.files != nil {
		if err := s.files.TakeFiles(ctx, res.From.TaskID, t.TaskID); err != nil {
			s.logger.Warn("composites: take a subtask's files", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		}
	}
	for _, id := range drop {
		if err := s.files.DropTaskFile(ctx, id); err != nil {
			s.logger.Warn("composites: drop a file the factory put", zap.String("file_id", id.String()), zap.Error(err))
		}
	}
	s.ended(after)
	return nil
}

// putFile is a file the factory put for the task, by its id.
func (s *Service) putFile(ctx context.Context, t *models.MinionTask, id string) (*models.MinionFile, error) {
	fid, err := uuid.Parse(id)
	if err != nil || s.files == nil {
		return nil, errors.New("no file put for the task")
	}
	f, err := s.files.FileByID(ctx, fid)
	if err != nil {
		return nil, err
	}
	if f == nil || f.Kind != models.MinionFileKindTask || f.ForTask != t.TaskID || f.State != models.MinionFileComplete {
		return nil, errors.New("no file put for the task")
	}
	return f, nil
}

// takeFile reads a file the factory put for the task, max bytes at most
// (cut: it had more).
func (s *Service) takeFile(ctx context.Context, t *models.MinionTask, id string, max int64) ([]byte, uuid.UUID, bool, error) {
	f, err := s.putFile(ctx, t, id)
	if err != nil {
		return nil, uuid.Nil, false, err
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
		return b[:max], f.FileID, true, nil
	}
	return b, f.FileID, false, nil
}
