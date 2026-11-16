package recordings

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// Recorder records one desktop or one terminal session as core relays it,
// in its recording's format: a desktop's guacd bytes and the web app's
// mouse and keys (desktop.go: FromGuacd, FromWeb), a terminal's output,
// input and sizes (terminal.go: Output, Input, Resize), until it ends
// (End, EndWith, Fail). What comes is held whole (an instruction, an
// event) and kept as parts by its loop. Its methods are safe from the
// relay's goroutines at once, and do nothing on a nil Recorder (one
// unrecorded) or one ended.
type Recorder struct {
	s   *Service
	rec models.Recording

	mu sync.Mutex
	// buf holds whole instructions or events, in order, not kept yet; held
	// counts what the loop took from buf and has not kept yet (a part the
	// store has not taken): buf and held stay within MaxHeld together.
	buf  []byte
	held int
	// lost counts the bytes dropped.
	lost  int64
	ended bool
	end   string
	msg   string

	// A desktop's (desktop.go), and a terminal's (terminal.go).
	desk deskState
	term termState

	// full: buf grew past a part; closing: it ended; done: its last part
	// is kept and its record says it ended.
	full    chan struct{}
	closing chan struct{}
	done    chan struct{}
}

func newRecorder(s *Service, rec *models.Recording) *Recorder {
	r := &Recorder{s: s, rec: *rec, full: make(chan struct{}, 1), closing: make(chan struct{}), done: make(chan struct{})}
	if rec.FileFormat() == models.RecordingFormatAsciicast {
		r.beginTerminal()
	} else {
		r.beginDesktop()
	}
	return r
}

// ID is the recording's.
func (r *Recorder) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.rec.RecordingID
}

// keep adds whole instructions or events to buf, all of them or, past
// MaxHeld (with what the loop holds), none (lost). Called with mu held.
func (r *Recorder) keep(parts ...[]byte) {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	if len(r.buf)+r.held+n > r.s.cfg.MaxHeld {
		r.lost += int64(n)
		return
	}
	for _, p := range parts {
		r.buf = append(r.buf, p...)
	}
	if len(r.buf) >= r.s.cfg.PartMax {
		select {
		case r.full <- struct{}{}:
		default:
		}
	}
}

// End ends the recording, as its desktop or terminal ended (end: how,
// models.RecordingEnd...): what it holds is kept, and its record says it
// ended; it returns once it does. A desktop's own end carries guacd's
// error, if it sent one.
func (r *Recorder) End(end string) { r.close(end, "") }

// EndWith ends it as End does, saying msg (a terminal's process's exit).
func (r *Recorder) EndWith(end, msg string) { r.close(end, msg) }

// Fail ends a recording whose desktop or terminal did not open (msg:
// why): nothing recorded.
func (r *Recorder) Fail(msg string) { r.close(models.RecordingEndNotOpened, msg) }

func (r *Recorder) close(end, msg string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		<-r.done
		return
	}
	r.ended, r.end, r.msg = true, end, msg
	if end == models.RecordingEndDesktop && r.desk.said != "" {
		r.msg = r.desk.said
	}
	r.mu.Unlock()
	close(r.closing)
	<-r.done
}

// take is what to keep next: buf's instructions or events, with how long
// the recording plays so far; they count as held until the loop says
// (holding).
func (r *Recorder) take() (data []byte, dur, lost int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, r.buf = r.buf, nil
	r.held += len(data)
	if r.rec.FileFormat() == models.RecordingFormatAsciicast {
		dur = r.term.last
	} else if r.desk.synced {
		dur = r.desk.last - r.desk.first
	}
	return data, dur, r.lost
}

// holding is what the loop holds now that a put is done: n bytes the store
// did not take, which it tries again with the next part.
func (r *Recorder) holding(n int) {
	r.mu.Lock()
	r.held = n
	r.mu.Unlock()
}

// loop keeps the recording's parts as they fill, and its numbers with each
// (its core's word that it records it), until it ends.
func (r *Recorder) loop() {
	defer close(r.done)
	defer func() {
		r.s.mu.Lock()
		delete(r.s.active, r.rec.RecordingID)
		r.s.mu.Unlock()
	}()
	tick := time.NewTicker(r.s.cfg.PartEvery)
	defer tick.Stop()
	id := r.rec.RecordingID
	var (
		held  []byte
		parts int
		size  int64
		// dropped: parts the store never took (lost with what take says).
		dropped int64
	)
	for {
		last := false
		select {
		case <-tick.C:
		case <-r.full:
		case <-r.closing:
			last = true
		}
		data, dur, lost := r.take()
		if len(held) > 0 {
			data = append(held, data...)
			held = nil
		}
		if len(data) > 0 {
			tries := 1
			if last {
				tries = 3
			}
			switch err := r.put(parts, data, tries); {
			case err == nil:
				parts, size = parts+1, size+int64(len(data))
				r.holding(0)
			case !last && len(data) <= r.s.cfg.MaxHeld:
				// Tried again at the head of the next part.
				held = data
				r.holding(len(held))
			default:
				dropped += int64(len(data))
				r.holding(0)
				r.s.logger.Warn("A recording lost a part: the store did not take it", zap.String("recording_id", id.String()),
					zap.Int("bytes", len(data)), zap.Error(err))
			}
		}
		now := r.s.now()
		set := bson.M{"parts": parts, "size": size, "duration_ms": dur, "lost": lost + dropped, "seen_at": now}
		if last {
			r.mu.Lock()
			end, msg := r.end, r.msg
			r.mu.Unlock()
			set["state"], set["end"], set["ended_at"] = models.RecordingStateSaved, end, now
			if end == models.RecordingEndNotOpened {
				set["state"] = models.RecordingStateFailed
			}
			if msg != "" {
				set["msg"] = msg
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.s.cfg.PutWait)
		err := r.s.repo.Save(ctx, id, set)
		cancel()
		if err != nil {
			r.s.logger.Warn("A recording: keep its numbers", zap.String("recording_id", id.String()), zap.Error(err))
		}
		if last {
			ctx, cancel := context.WithTimeout(context.Background(), r.s.cfg.PutWait)
			t, err := r.s.tasks.AddRecording(ctx, r.rec.TaskID, 0, 0)
			cancel()
			if err == nil {
				r.s.publish(t)
			}
			r.s.logger.Info("A recording ended", zap.String("recording_id", id.String()), zap.String("front", r.rec.Front),
				zap.String("end", set["end"].(string)), zap.Int("parts", parts), zap.Int64("size", size), zap.Int64("lost", lost+dropped))
			return
		}
	}
}

// put keeps part n, trying it up to tries times, within PutWait in all.
func (r *Recorder) put(n int, data []byte, tries int) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.s.cfg.PutWait)
	defer cancel()
	var err error
	for i := 0; i < tries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(time.Duration(i) * r.s.cfg.RetryWait):
			}
		}
		err = r.s.store.Put(ctx, partKey(r.rec.RecordingID, n), bytes.NewReader(data), int64(len(data)), "application/octet-stream")
		if err == nil {
			return nil
		}
	}
	return err
}
