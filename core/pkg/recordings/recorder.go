package recordings

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/guacd"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// Recorder records one desktop as core relays it: guacd's bytes
// (FromGuacd), the web app's mouse and keys (FromWeb), until it ends (End,
// Fail). Its methods are safe from the relay's two goroutines at once,
// and do nothing on a nil Recorder (a desktop unrecorded) or one ended.
type Recorder struct {
	s   *Service
	rec models.DesktopRecording

	mu sync.Mutex
	// buf holds whole instructions, in order, not kept yet; pending the
	// bytes of guacd's instruction that has not ended.
	buf     []byte
	pending []byte
	scan    scanner
	// synced: guacd sent a sync; first and last are the timestamps of its
	// first and last, lastAt when the last came (on the service's clock).
	synced      bool
	first, last int64
	lastAt      time.Time
	// lost counts the bytes dropped; unreadable: guacd's bytes stopped
	// being Guacamole's as the scanner reads them (none since recorded).
	lost       int64
	unreadable bool
	// said is guacd's error, the last it sent.
	said  string
	ended bool
	end   string
	msg   string

	// full: buf grew past a part; closing: it ended; done: its last part
	// is kept and its record says it ended.
	full    chan struct{}
	closing chan struct{}
	done    chan struct{}
}

func newRecorder(s *Service, rec *models.DesktopRecording) *Recorder {
	r := &Recorder{s: s, rec: *rec, full: make(chan struct{}, 1), closing: make(chan struct{}), done: make(chan struct{})}
	r.scan.onSync = r.synced0
	r.scan.onError = func(message string, _ int) { r.said = message }
	return r
}

// ID is the recording's.
func (r *Recorder) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.rec.RecordingID
}

// synced0 is a sync of guacd's: its frames' time. Called with mu held.
func (r *Recorder) synced0(ts int64) {
	if !r.synced {
		r.synced, r.first = true, ts
	}
	r.last, r.lastAt = ts, r.s.clock()
}

// FromGuacd records what guacd sent the web app: b as it came, cut
// anywhere.
func (r *Recorder) FromGuacd(b []byte) {
	if r == nil || len(b) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended || r.unreadable {
		return
	}
	end := r.scan.scan(b)
	if r.scan.bad {
		// Not Guacamole's as core reads it: what came since its last
		// whole instruction, and anything after, is not recorded.
		r.unreadable = true
		r.s.logger.Warn("A desktop's recording stops: guacd's bytes do not read as instructions",
			zap.String("recording_id", r.rec.RecordingID.String()))
		return
	}
	if end == 0 {
		r.pending = append(r.pending, b...)
		return
	}
	// The instructions b ends: the first began in what came before.
	r.keep(r.pending, b[:end])
	r.pending = append(r.pending[:0], b[end:]...)
}

// FromWeb records an instruction the web app sent guacd: its mouse and
// keys, each with a timestamp on guacd's clock (its last sync's, and the
// time since), where it came. Only once guacd synced: its player reads
// none before its first frame.
func (r *Recorder) FromWeb(ins guacd.Instruction) {
	if r == nil || !r.rec.Keys {
		return
	}
	var want int
	switch ins.Opcode {
	case "mouse":
		want = 3
	case "key":
		want = 2
	default:
		return
	}
	if len(ins.Args) < want {
		return
	}
	for _, a := range ins.Args[:want] {
		if _, err := strconv.Atoi(a); err != nil {
			return
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended || r.unreadable || !r.synced {
		return
	}
	ts := r.last + r.s.clock().Sub(r.lastAt).Milliseconds()
	args := append(append([]string{}, ins.Args[:want]...), strconv.FormatInt(ts, 10))
	r.keep([]byte(guacd.Instruction{Opcode: ins.Opcode, Args: args}.Encode()))
}

// keep adds whole instructions to buf, all of them or, past MaxHeld, none
// (lost). Called with mu held.
func (r *Recorder) keep(parts ...[]byte) {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	if len(r.buf)+n > r.s.cfg.MaxHeld {
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

// End ends the recording, as its desktop ended (end: how,
// models.DesktopRecordingEnd...): what it holds is kept, and its record
// says it ended; it returns once it does. An end of the desktop's own
// carries guacd's error, if it sent one.
func (r *Recorder) End(end string) { r.close(end, "") }

// Fail ends a recording whose desktop did not open (msg: why): nothing
// recorded.
func (r *Recorder) Fail(msg string) { r.close(models.DesktopRecordingEndNotOpened, msg) }

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
	if end == models.DesktopRecordingEndDesktop && r.said != "" {
		r.msg = r.said
	}
	r.mu.Unlock()
	close(r.closing)
	<-r.done
}

// take is what to keep next: buf's instructions, with how far it came.
func (r *Recorder) take() (data []byte, dur, lost int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, r.buf = r.buf, nil
	if r.synced {
		dur = r.last - r.first
	}
	return data, dur, r.lost
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
			if err := r.put(parts, data, tries); err != nil {
				if !last && len(data) <= r.s.cfg.MaxHeld {
					held = data
				} else {
					dropped += int64(len(data))
					r.s.logger.Warn("A desktop's recording lost a part: the store did not take it", zap.String("recording_id", id.String()),
						zap.Int("bytes", len(data)), zap.Error(err))
				}
			} else {
				parts, size = parts+1, size+int64(len(data))
			}
		}
		now := r.s.now()
		set := bson.M{"parts": parts, "size": size, "duration_ms": dur, "lost": lost + dropped, "seen_at": now}
		if last {
			r.mu.Lock()
			end, msg := r.end, r.msg
			r.mu.Unlock()
			set["state"], set["end"], set["ended_at"] = models.DesktopRecordingSaved, end, now
			if end == models.DesktopRecordingEndNotOpened {
				set["state"] = models.DesktopRecordingFailed
			}
			if msg != "" {
				set["msg"] = msg
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.s.cfg.PutWait)
		err := r.s.repo.Save(ctx, id, set)
		cancel()
		if err != nil {
			r.s.logger.Warn("A desktop's recording: keep its numbers", zap.String("recording_id", id.String()), zap.Error(err))
		}
		if last {
			if t, err := r.s.tasks.AddRecording(context.Background(), r.rec.TaskID, 0, 0); err == nil {
				r.s.publish(t)
			}
			r.s.logger.Info("A desktop's recording ended", zap.String("recording_id", id.String()), zap.String("end", set["end"].(string)),
				zap.Int("parts", parts), zap.Int64("size", size), zap.Int64("lost", lost+dropped))
			return
		}
	}
}

// put keeps part n, trying it up to tries times.
func (r *Recorder) put(n int, data []byte, tries int) error {
	var err error
	for i := 0; i < tries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * r.s.cfg.RetryWait)
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.s.cfg.PutWait)
		err = r.s.store.Put(ctx, partKey(r.rec.RecordingID, n), bytes.NewReader(data), int64(len(data)), "application/octet-stream")
		cancel()
		if err == nil {
			return nil
		}
	}
	return err
}
