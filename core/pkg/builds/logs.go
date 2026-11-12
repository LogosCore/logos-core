package builds

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// A build's live log (factory.build.log): the factory sends its output as it
// builds, in numbered chunks of lines, each line with its plan step. Core
// keeps the chunks (build_logs) and the app reads them a page at a time
// (Log). A factory that sends no live log sends its output with the result
// instead (Build.Log), which the app gets as one page of lines without steps.

const (
	// MaxBuildLogBytes is how much of a build's live log core keeps: past
	// it, the oldest chunks go (Build.LogTrimmed counts their lines).
	MaxBuildLogBytes = 8 << 20
	// logPageChunks is how many chunks a page of the log holds at most.
	logPageChunks = 20
)

// HandleLog serves factory.build.log.
func (s *Service) HandleLog(ctx context.Context, req messaging.Envelope) (any, error) {
	var c protocol.BuildLogChunk
	if err := json.Unmarshal(req.Payload, &c); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, "malformed log payload")
	}
	if err := c.Validate(); err != nil {
		return nil, rpcErr(protocol.CodeValidationFailed, err.Error())
	}
	instance := req.Source.Instance
	id := uuid.MustParse(c.BuildID) // checked by Validate
	b, err := s.repos.Builds.FindByBuildID(ctx, id)
	if err != nil || b.Instance != instance || b.State != models.BuildStateBuilding {
		return nil, notWaiting(c.BuildID, instance)
	}
	chunk := &models.BuildLogChunk{BuildID: id, OperationID: b.OperationID, Seq: c.Seq, Dropped: c.Dropped, ReceivedAt: s.now()}
	chunk.Lines = make([]models.BuildLogLine, len(c.Lines))
	for i, l := range c.Lines {
		chunk.Lines[i] = models.BuildLogLine{Step: l.Step, At: l.At, Text: l.Text}
		chunk.Bytes += int64(len(l.Text))
	}
	added, err := s.repos.Logs.Append(ctx, chunk)
	if err != nil {
		return nil, fmt.Errorf("store a build log chunk: %w", err)
	}
	ack := protocol.BuildLogReply{BuildID: c.BuildID, Seq: c.Seq, Ack: true}
	if !added {
		return ack, nil // the same chunk again: its reply was lost
	}
	kept, ok, err := s.repos.Builds.AddLog(ctx, id, instance, chunk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("count a build log chunk: %w", err)
	}
	if !ok {
		return nil, notWaiting(c.BuildID, instance) // it ended meanwhile
	}
	if over := kept - MaxBuildLogBytes; over > 0 {
		bytes, lines, err := s.repos.Logs.TrimStart(ctx, id, over)
		if bytes > 0 {
			if terr := s.repos.Builds.TrimmedLog(ctx, id, bytes, lines); terr != nil && err == nil {
				err = terr
			}
		}
		if err != nil {
			s.logger.Warn("trim a build log", zap.String("build_id", c.BuildID), zap.Error(err))
		}
	}
	return ack, nil
}

// LogPage is a page of a build's log.
type LogPage struct {
	// Live is set when the factory sent its log as it built, by step;
	// otherwise Lines are the log of its result (or none yet).
	Live  bool
	Lines []LogLine
	// Cursor is where the next page starts (Log's after).
	Cursor int64
	// More is set when the log goes on past this page.
	More bool
	// Trimmed is how many lines core dropped from the log's start.
	Trimmed int64
}

// LogLine is a line of a build's log, or a mark where lines are missing.
type LogLine struct {
	Step string     // the plan step it was written in; "" for none
	At   *time.Time // nil for a result's log
	Text string
	// Missing is set on a mark: that many lines are missing here, 0 when
	// how many is not known (a chunk that never arrived).
	Missing *int64
}

// Log is a page of b's log after the chunk numbered after (0: from the
// start). For a build whose factory sent no live log, it is the result's log
// whole, Cursor staying after.
func (s *Service) Log(ctx context.Context, b *models.Build, after int64) (LogPage, error) {
	if !b.LogStreamed {
		page := LogPage{Cursor: after}
		if b.Log != "" {
			for _, line := range strings.Split(strings.TrimSuffix(b.Log, "\n"), "\n") {
				page.Lines = append(page.Lines, LogLine{Text: strings.TrimSuffix(line, "\r")})
			}
		}
		return page, nil
	}
	chunks, err := s.repos.Logs.Page(ctx, b.BuildID, max(after, 0), logPageChunks)
	if err != nil {
		return LogPage{}, fmt.Errorf("read the build log: %w", err)
	}
	page := LogPage{Live: true, Cursor: after, More: len(chunks) == logPageChunks, Trimmed: b.LogTrimmed}
	for _, c := range chunks {
		// A number skipped is a chunk that never arrived, unless the
		// log's start was trimmed and this page is its start.
		if c.Seq > page.Cursor+1 && (page.Cursor > 0 || b.LogTrimmed == 0) {
			page.Lines = append(page.Lines, LogLine{Missing: new(int64)})
		}
		if c.Dropped > 0 {
			page.Lines = append(page.Lines, LogLine{Missing: &c.Dropped})
		}
		for _, l := range c.Lines {
			page.Lines = append(page.Lines, LogLine{Step: l.Step, At: &l.At, Text: l.Text})
		}
		page.Cursor = c.Seq
	}
	return page, nil
}
