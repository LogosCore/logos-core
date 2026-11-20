package composites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// endOf is an end as a call tells it: what ran (its args, defaults filled
// in), how it ended, and what it returned, small inline, larger by a link
// signed until expires (docs: What travels, and how).
func (s *Service) endOf(ctx context.Context, lk *look, p models.MinionComposeEnd, expires time.Time) (protocol.ComposeEnd, error) {
	if p.TaskID == nil {
		// A subtask that never became a task: refused, or dropped while it
		// waited.
		return protocol.ComposeEnd{Command: p.Command, Args: s.args(lk, p.Command, p.Args), Key: p.Key, As: p.As,
			State: protocol.EndState(p.State), Err: p.Err}, nil
	}
	if p.OwnOp {
		t, c := lk.t, lk.t.Composite
		args := t.Args
		if c.OwnArgs != nil {
			args = c.OwnArgs
		}
		e := protocol.ComposeEnd{TaskID: t.TaskID.String(), OwnOp: true, Command: t.Command, Args: s.args(lk, t.Command, args),
			State: protocol.EndFailed, Err: "its own op's end was not kept"}
		if oe := c.OwnEnd; oe != nil {
			e.State, e.OK, e.Err = protocol.EndState(oe.State), oe.OK, oe.Err
			if e.State == protocol.EndDone {
				e.Exit = oe.Exit
			}
		}
		if e.State == protocol.EndFailed && e.Err == "" {
			e.Err = "core ended it"
		}
		if e.State != protocol.EndRefused {
			if err := s.result(ctx, t, &e, expires); err != nil {
				return e, err
			}
		}
		return e, nil
	}
	sub, err := s.tasks.FindByTaskID(ctx, *p.TaskID)
	if repository.IsNotFound(err) {
		return protocol.ComposeEnd{TaskID: p.TaskID.String(), Command: p.Command, State: protocol.EndFailed, Err: "the task is gone"}, nil
	}
	if err != nil {
		return protocol.ComposeEnd{}, fmt.Errorf("a subtask: %w", err)
	}
	e := protocol.ComposeEnd{TaskID: sub.TaskID.String(), Command: sub.Command, Args: s.args(lk, sub.Command, sub.Args),
		Key: sub.SubtaskKey, As: sub.SubtaskAs, Err: sub.Err}
	switch {
	case sub.State == models.MinionTaskDone:
		e.State, e.OK, e.Exit = protocol.EndDone, true, sub.Exit
	case sub.State == models.MinionTaskCancelled:
		e.State = protocol.EndCancelled
	case sub.DoneSeq != nil:
		// Its minion sent its last part, not ok.
		e.State, e.Exit = protocol.EndDone, sub.Exit
	default:
		// Core ended it: too large for the connection, lost by its minion.
		e.State = protocol.EndFailed
		if e.Err == "" {
			e.Err = "core ended it"
		}
	}
	if err := s.result(ctx, sub, &e, expires); err != nil {
		return e, err
	}
	return e, nil
}

// result puts in e what task returned: its data as the web app gets it,
// its output and its file, each inline when small, by link when not.
func (s *Service) result(ctx context.Context, t *models.MinionTask, e *protocol.ComposeEnd, expires time.Time) error {
	parts, err := s.tasks.Parts(ctx, t.TaskID)
	if err != nil {
		return fmt.Errorf("a task's parts: %w", err)
	}
	data, err := dataJSON(*t, parts)
	if err != nil {
		return err
	}
	switch {
	case data == nil:
	case len(data) <= protocol.MaxComposeInline:
		e.Data = data
	default:
		e.DataURL = s.links.Sign(DataPath(t.TaskID), expires)
	}
	if out := joinOut(parts); len(out) > protocol.MaxComposeInline {
		e.OutURL = s.links.Sign(OutPath(t.TaskID), expires)
	} else if len(out) > 0 {
		e.Out = out
	}
	if s.files != nil {
		f, err := s.files.File(ctx, t.TaskID)
		if err != nil {
			return fmt.Errorf("a task's file: %w", err)
		}
		// Kept whole, and checked: a file core could not check (no SHA-256)
		// is not listed.
		if f != nil && f.State == models.MinionFileComplete && len(f.SHA256) == sha256.Size {
			e.Files = []protocol.BuildFile{s.buildFile(t, f, expires)}
		}
	}
	e.Truncated = t.OutTruncated || t.DataTruncated
	e.Incomplete = t.MissingParts() > 0
	return nil
}

// buildFile is a task's file as an end lists it: its name (the last part
// of a path on its host), size and SHA-256, and a link to it.
func (s *Service) buildFile(t *models.MinionTask, f *models.MinionFile, expires time.Time) protocol.BuildFile {
	name := f.Name[strings.LastIndexAny(f.Name, `/\`)+1:]
	if name == "" {
		name = f.FileID.String()
	}
	mediaType, _, _ := strings.Cut(f.ContentType, ";")
	return protocol.BuildFile{ID: f.FileID.String(), Name: name, Size: f.Total, SHA256: hex.EncodeToString(f.SHA256),
		MediaType: strings.TrimSpace(mediaType), URL: s.links.Sign(FilePath(t.TaskID, f.FileID), expires)}
}

// dataJSON is a task's data as the web app gets it (its parts joined by its
// result's rule), when it is a JSON object: nil for none.
func dataJSON(t models.MinionTask, parts []models.MinionTaskPart) ([]byte, error) {
	v, err := taskdata.Join(t, parts)
	if err != nil {
		return nil, fmt.Errorf("a task's data: %w", err)
	}
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("a task's data: %w", err)
	}
	if trimmed := bytes.TrimLeft(raw, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, nil
	}
	return raw, nil
}

// joinOut is the parts' output, in seq order.
func joinOut(parts []models.MinionTaskPart) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p.Out...)
	}
	return out
}
