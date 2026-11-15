package recordings

import (
	"strconv"
	"strings"
)

// scanner frames guacd's bytes into instructions as they come, cut
// anywhere (the relay's reads): where each instruction ends, so what core
// adds to a recording goes between two, never inside one; and the args of
// the two a recording reads: sync's timestamp (the time of guacd's frames)
// and error's message and status (why a desktop ended). Lengths are in
// characters, as the Guacamole protocol counts them: a character is a byte
// that does not go on the one before it (UTF-8).
type scanner struct {
	state int
	// n is an element's length as its digits come; left the characters
	// of the element still to come.
	n, left int
	// elem is the element's index in its instruction: 0, the opcode.
	elem int
	// op is the opcode, as far as it is kept; args the args of an
	// instruction read (sync, error), each cut at maxArg.
	op   []byte
	args [2][]byte
	// bad: the bytes are not Guacamole's as the scanner reads them; it
	// reads no more.
	bad bool

	onSync  func(timestamp int64)
	onError func(message string, status int)
}

const (
	stLength = iota
	stValue
	stSep
)

const (
	// maxOp is the longest opcode kept: longer ones are none it reads.
	maxOp = 8
	// maxArg is the most of an arg kept.
	maxArg = 512
	// maxLength bounds an element: guacd's are far shorter (a blob's 8 KiB
	// of base64, an instruction's image at most the screen's).
	maxLength = 1 << 28
)

// scan reads b, the next of guacd's bytes: the offset past the last
// instruction b ends, 0 when it ends none (its bytes go on one that
// began before, or one that goes on after). Once bad, it reads nothing.
func (s *scanner) scan(b []byte) int {
	end := 0
	for i := 0; i < len(b); i++ {
		if s.bad {
			return end
		}
		c := b[i]
		switch s.state {
		case stLength:
			switch {
			case c >= '0' && c <= '9':
				s.n = s.n*10 + int(c-'0')
				if s.n > maxLength {
					s.bad = true
				}
			case c == '.':
				s.left, s.n = s.n, 0
				s.state = stValue
				if s.left == 0 {
					s.state = stSep
				}
			default:
				s.bad = true
			}
		case stValue:
			if s.left > 0 && !s.keeps() {
				// A run of ASCII characters, a byte each: the bulk of a
				// recording (images in base64), passed over at once.
				j, lim := i, min(len(b), i+s.left)
				for j < lim && b[j] < 0x80 {
					j++
				}
				if j > i {
					s.left -= j - i
					i = j - 1
					continue
				}
			}
			if c&0xC0 == 0x80 {
				// A byte that goes on the character before.
				s.keep(c)
				continue
			}
			if s.left == 0 {
				// Past the element's last character: its separator.
				s.state = stSep
				i--
				continue
			}
			s.left--
			s.keep(c)
		case stSep:
			switch c {
			case ',':
				s.elem++
				s.state = stLength
			case ';':
				s.ended()
				end = i + 1
				s.state = stLength
			default:
				s.bad = true
			}
		}
	}
	return end
}

// keeps reports whether the element being read is one kept: the opcode,
// or an arg of an instruction read.
func (s *scanner) keeps() bool {
	switch s.elem {
	case 0:
		return true
	case 1:
		return s.reads()
	case 2:
		return s.opIs("error")
	}
	return false
}

// reads reports whether the instruction's args are read.
func (s *scanner) reads() bool { return s.opIs("sync") || s.opIs("error") }

func (s *scanner) opIs(op string) bool { return string(s.op) == op }

func (s *scanner) keep(c byte) {
	switch {
	case s.elem == 0:
		if len(s.op) <= maxOp {
			s.op = append(s.op, c)
		}
	case s.elem <= 2 && s.keeps():
		if a := &s.args[s.elem-1]; len(*a) < maxArg {
			*a = append(*a, c)
		}
	}
}

// ended is an instruction's end: the one read says what it said.
func (s *scanner) ended() {
	switch {
	case s.opIs("sync") && s.elem >= 1 && s.onSync != nil:
		if ts, err := strconv.ParseInt(string(s.args[0]), 10, 64); err == nil {
			s.onSync(ts)
		}
	case s.opIs("error") && s.onError != nil:
		status, _ := strconv.Atoi(string(s.args[1]))
		s.onError(strings.ToValidUTF8(string(s.args[0]), ""), status)
	}
	s.op, s.args[0], s.args[1] = s.op[:0], s.args[0][:0], s.args[1][:0]
	s.elem = 0
}
