package recordings

import (
	"strconv"
	"time"

	"github.com/logoscore/logos-core/core/pkg/guacd"
	"go.uber.org/zap"
)

// A desktop's recording: the format guacd's own recordings have, which
// Guacamole's player plays. The protocol's instructions as guacd sent them
// to the web app, with the operator's mouse and keys each where it came
// (mouse x, y, mask and key keysym, pressed, each with a timestamp on
// guacd's clock, from its last sync).

// deskState is what a desktop's Recorder reads of guacd's bytes. Its
// fields are the Recorder's mu's.
type deskState struct {
	// pending holds the bytes of guacd's instruction that has not ended.
	pending []byte
	scan    scanner
	// synced: guacd sent a sync; first and last are the timestamps of its
	// first and last, lastAt when the last came (on the service's clock).
	synced      bool
	first, last int64
	lastAt      time.Time
	// unreadable: guacd's bytes stopped being Guacamole's as the scanner
	// reads them (none since recorded).
	unreadable bool
	// said is guacd's error, the last it sent.
	said string
}

func (r *Recorder) beginDesktop() {
	r.desk.scan.onSync = r.synced0
	r.desk.scan.onError = func(message string, _ int) { r.desk.said = message }
}

// synced0 is a sync of guacd's: its frames' time. Called with mu held.
func (r *Recorder) synced0(ts int64) {
	d := &r.desk
	if !d.synced {
		d.synced, d.first = true, ts
	}
	d.last, d.lastAt = ts, r.s.clock()
}

// FromGuacd records what guacd sent the web app: b as it came, cut
// anywhere.
func (r *Recorder) FromGuacd(b []byte) {
	if r == nil || len(b) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d := &r.desk
	if r.ended || d.unreadable {
		return
	}
	end := d.scan.scan(b)
	if d.scan.bad {
		// Not Guacamole's as core reads it: what came since its last
		// whole instruction, and anything after, is not recorded; the
		// instructions b ended before it are.
		if end > 0 {
			r.keep(d.pending, b[:end])
		}
		d.pending = nil
		d.unreadable = true
		r.s.logger.Warn("A desktop's recording stops: guacd's bytes do not read as instructions",
			zap.String("recording_id", r.rec.RecordingID.String()))
		return
	}
	if end == 0 {
		d.pending = append(d.pending, b...)
		return
	}
	// The instructions b ends: the first began in what came before.
	r.keep(d.pending, b[:end])
	d.pending = append(d.pending[:0], b[end:]...)
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
	d := &r.desk
	if r.ended || d.unreadable || !d.synced {
		return
	}
	ts := d.last + r.s.clock().Sub(d.lastAt).Milliseconds()
	args := append(append([]string{}, ins.Args[:want]...), strconv.FormatInt(ts, 10))
	r.keep([]byte(guacd.Instruction{Opcode: ins.Opcode, Args: args}.Encode()))
}
