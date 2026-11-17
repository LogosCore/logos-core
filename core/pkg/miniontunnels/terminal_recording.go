package miniontunnels

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A terminal's sessions are recorded (docs: contracts/minion-messages/
// tunnels.md, Recordings): each stream of a terminal task, a web
// terminal's or a raw port client's, from its open to its end, what the
// process wrote, what was typed and the terminal's sizes, by
// TerminalRecordings (recordings.Service). A session it does not record
// (its store failed) runs all the same.

// TerminalRecorder records one terminal session (recordings.Recorder).
type TerminalRecorder interface {
	// Output is what the process wrote, Input what was typed: bytes as
	// they came.
	Output(b []byte)
	Input(b []byte)
	// Resize is the terminal's new size.
	Resize(cols, rows int)
	// EndWith ends it (end: models.RecordingEnd...; msg: the process's
	// exit), Fail one that did not open (msg: why); each returns once its
	// last part is kept.
	EndWith(end, msg string)
	Fail(msg string)
}

// TerminalBeginning is a terminal session about to open.
type TerminalBeginning struct {
	Task   uuid.UUID
	Stream uint64
	// Client is who, as the streams list says it; User the operator of a
	// web terminal (none on the raw port).
	Client string
	User   TerminalUser
	// Cols and Rows: its size as it opens.
	Cols, Rows int
}

// TerminalUser is the operator who opens a web terminal.
type TerminalUser struct {
	ID   uuid.UUID
	Name string
}

// TerminalRecordings begins terminal sessions' recordings: nil for one it
// does not record.
type TerminalRecordings interface {
	BeginTerminal(ctx context.Context, b TerminalBeginning) TerminalRecorder
}

// WithTerminalRecordings records the terminals' sessions: without, none
// is.
func (s *Service) WithTerminalRecordings(r TerminalRecordings) *Service {
	s.recordings = r
	return s
}

// recordingBeginWait bounds a recording's start: its session waits for it.
const recordingBeginWait = 10 * time.Second

// recordTerminal begins the recording of a terminal's stream, before it
// runs: what it wrote, typed or said from then on is recorded. A stream
// that ended meanwhile ends its recording as it did.
func (s *Service) recordTerminal(st *stream, user TerminalUser) {
	if s.recordings == nil {
		return
	}
	s.mu.Lock()
	b := TerminalBeginning{Task: st.t.task, Stream: st.id, Client: st.rec.Client, User: user, Cols: st.cols, Rows: st.rows}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), recordingBeginWait)
	r := s.recordings.BeginTerminal(ctx, b)
	cancel()
	if r == nil {
		return
	}
	s.mu.Lock()
	if !st.gone {
		st.recorder = r
		s.mu.Unlock()
		return
	}
	end, msg := recordingEnd(st)
	s.mu.Unlock()
	r.EndWith(end, msg)
}

// endRecording ends a dropped stream's recording, as it ended, without
// holding the service up. Called with mu held.
func (s *Service) endRecording(st *stream) {
	r := st.recorder
	if r == nil {
		return
	}
	st.recorder = nil
	end, msg := recordingEnd(st)
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		// What was read, written or resized before the end is recorded
		// first: the client is closed, so each returns.
		st.recording.Wait()
		r.EndWith(end, msg)
	}()
}

// recordOne is the recorder for one read, write or resize about to go,
// counted until it is recorded (recording.Done): nil when the stream is
// not recorded. Called with mu held.
func (st *stream) recordOne() TerminalRecorder {
	if st.recorder == nil {
		return nil
	}
	st.recording.Add(1)
	return st.recorder
}

// recordingEnd is how a terminal's recording ended, by its stream's end:
// the process's (its exit), its client's, the task's, core's, or the
// minion's. Called with mu held.
func recordingEnd(st *stream) (end, msg string) {
	switch st.rec.End {
	case models.TunnelEndDone:
		if st.rec.Exit != nil {
			msg = fmt.Sprintf("exit %d", *st.rec.Exit)
		}
		return models.RecordingEndProcess, msg
	case models.TunnelEndClientReset:
		return models.RecordingEndClosed, ""
	case models.TunnelEndTaskEnded:
		return models.RecordingEndTaskEnded, ""
	case models.TunnelEndCoreStopped:
		return models.RecordingEndCoreStopped, ""
	case models.TunnelEndNotOpened, models.TunnelEndNoReply:
		return models.RecordingEndNotOpened, "the process did not start"
	}
	return models.RecordingEndReset, ""
}

// failRecording ends the recording of a terminal whose process did not
// start, saying why as its client was told (note, core's line). Called
// without mu.
func (s *Service) failRecording(st *stream, note string) {
	s.mu.Lock()
	r := st.recorder
	st.recorder = nil
	s.mu.Unlock()
	if r != nil {
		r.Fail(strings.TrimSuffix(strings.TrimPrefix(note, "[logos: "), "]"))
	}
}
