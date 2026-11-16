package recordings

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A terminal's recording: an asciicast (v2), which asciinema plays. A JSON
// header line (its size, when it began, a title), then an event a line,
// [seconds since it began, code, data]: "o" what the process wrote, "i"
// what was typed, "r" a new size ("COLSxROWS"). Each event's data is
// text: bytes cut inside a UTF-8 character go with the next of their
// direction, and bytes that are not UTF-8 are U+FFFD (the format holds
// text alone).

// termState is what a terminal's Recorder keeps between events. Its fields
// are the Recorder's mu's.
type termState struct {
	// start is when it began, on the service's clock; last the time of its
	// last event, in milliseconds since.
	start time.Time
	last  int64
	// carry holds what ended inside a UTF-8 character: the output's [0],
	// the input's [1].
	carry [2][]byte
}

// asciicastHeader is an asciicast v2's first line.
type asciicastHeader struct {
	Version   int    `json:"version"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Timestamp int64  `json:"timestamp"`
	Title     string `json:"title,omitempty"`
}

// Its size when a terminal says none: a raw port's default.
const (
	defaultCols = 80
	defaultRows = 24
)

func (r *Recorder) beginTerminal() {
	r.term.start = r.s.clock()
	cols, rows := r.rec.Width, r.rec.Height
	if cols < 1 || rows < 1 {
		cols, rows = defaultCols, defaultRows
	}
	h := asciicastHeader{Version: 2, Width: cols, Height: rows, Timestamp: r.rec.StartedAt.Unix(), Title: r.title()}
	b, _ := json.Marshal(h)
	r.keep(append(b, '\n'))
}

// title is its header's: who, and the stream.
func (r *Recorder) title() string {
	parts := []string{"terminal"}
	if r.rec.Client != "" {
		parts = append(parts, r.rec.Client)
	}
	if r.rec.Stream > 0 {
		parts = append(parts, "stream "+strconv.FormatUint(r.rec.Stream, 10))
	}
	return strings.Join(parts, " · ")
}

// Output records what the terminal's process wrote: b as it came.
func (r *Recorder) Output(b []byte) { r.text(0, "o", b) }

// Input records what was typed in the terminal: b as it came.
func (r *Recorder) Input(b []byte) {
	if r == nil || !r.rec.Keys {
		return
	}
	r.text(1, "i", b)
}

// Resize records the terminal's new size.
func (r *Recorder) Resize(cols, rows int) {
	if r == nil || cols < 1 || rows < 1 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return
	}
	r.event("r", strconv.Itoa(cols)+"x"+strconv.Itoa(rows))
}

// text records b, one direction's bytes, as an event of code: its whole
// UTF-8 characters, holding back one it ends inside of.
func (r *Recorder) text(dir int, code string, b []byte) {
	if r == nil || len(b) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return
	}
	data := b
	if c := r.term.carry[dir]; len(c) > 0 {
		data = append(c, b...)
	}
	whole, rest := splitUTF8(data)
	r.term.carry[dir] = append(r.term.carry[dir][:0:0], rest...)
	if len(whole) > 0 {
		r.event(code, string(whole))
	}
}

// event adds one event line, timed now. Called with mu held.
func (r *Recorder) event(code, data string) {
	since := r.s.clock().Sub(r.term.start)
	ms := since.Milliseconds()
	if ms < r.term.last {
		since, ms = time.Duration(r.term.last)*time.Millisecond, r.term.last
	}
	r.term.last = ms
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(strconv.FormatFloat(since.Seconds(), 'f', 6, 64))
	b.WriteString(`,"`)
	b.WriteString(code)
	b.WriteString(`",`)
	b.WriteString(jsonText(data))
	b.WriteString("]\n")
	r.keep([]byte(b.String()))
}

// jsonText is s as a JSON string: bytes that are not UTF-8 as U+FFFD, and
// <, > and & as themselves.
func jsonText(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// splitUTF8 cuts b before a UTF-8 character it ends inside of: whole, and
// the start of that character (rest, at most 3 bytes; nil for none).
func splitUTF8(b []byte) (whole, rest []byte) {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax+1; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i], b[i:]
			}
			break
		}
	}
	return b, nil
}
