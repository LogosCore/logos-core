// Package guacd speaks to guacd, Apache Guacamole's proxy daemon (docs:
// contracts/minion-messages/tunnels.md, Desktops). Core opens a desktop's
// session there (Open: select, args, size, audio, video, image, timezone,
// name, connect, ready), then relays it: guacd's bytes to the web app's
// Guacamole client as they come, cut where UTF-8 allows (CutUTF8), and
// the client's instructions to guacd, but for its tunnel's own (opcode "",
// a ping: answered).
package guacd

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Instruction is one of the Guacamole protocol's: an opcode and its args.
type Instruction struct {
	Opcode string
	Args   []string
}

// Limits on an instruction read: guacd's are far smaller (a blob's data is
// 8 KiB of base64), and the web app sends one at a time.
const (
	// MaxElement is the most characters one element holds.
	MaxElement = 1 << 16
	// MaxElements is the most elements an instruction has, its opcode one.
	MaxElements = 256
	// maxDigits bounds an element's length prefix.
	maxDigits = 6
)

// ErrIncomplete: the text ends inside an instruction.
var ErrIncomplete = errors.New("an incomplete instruction")

// Encode is the instruction on the wire: each element its length in
// characters (Unicode code points), a dot and itself, the elements comma
// separated, a semicolon last: "4.size,4.1024,3.768;".
func (i Instruction) Encode() string {
	var b strings.Builder
	for n, e := range append([]string{i.Opcode}, i.Args...) {
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(utf8.RuneCountInString(e)))
		b.WriteByte('.')
		b.WriteString(e)
	}
	b.WriteByte(';')
	return b.String()
}

// String is the instruction for a log: its opcode and args.
func (i Instruction) String() string {
	return i.Opcode + "(" + strings.Join(i.Args, ", ") + ")"
}

// Parse reads the whole instructions text holds (a web app's message):
// ErrIncomplete when it ends inside one, another error when it is not
// Guacamole's.
func Parse(text string) ([]Instruction, error) {
	var out []Instruction
	for text != "" {
		ins, rest, err := parseOne(text)
		if err != nil {
			return out, err
		}
		out = append(out, ins)
		text = rest
	}
	return out, nil
}

func parseOne(text string) (Instruction, string, error) {
	var elems []string
	for {
		dot := strings.IndexByte(text, '.')
		if dot < 0 {
			if len(text) > maxDigits {
				return Instruction{}, "", errors.New("an element's length too long")
			}
			return Instruction{}, "", ErrIncomplete
		}
		n, err := length(text[:dot])
		if err != nil {
			return Instruction{}, "", err
		}
		text = text[dot+1:]
		// n characters, then a comma or a semicolon.
		end, ok := runeOffset(text, n)
		if !ok {
			return Instruction{}, "", ErrIncomplete
		}
		elems = append(elems, text[:end])
		if len(elems) > MaxElements {
			return Instruction{}, "", fmt.Errorf("more than %d elements", MaxElements)
		}
		if end == len(text) {
			return Instruction{}, "", ErrIncomplete
		}
		sep := text[end]
		text = text[end+1:]
		switch sep {
		case ';':
			return Instruction{Opcode: elems[0], Args: elems[1:]}, text, nil
		case ',':
		default:
			return Instruction{}, "", fmt.Errorf("%q after an element", sep)
		}
	}
}

// length is an element's length prefix: digits, at most MaxElement.
func length(digits string) (int, error) {
	if digits == "" || len(digits) > maxDigits {
		return 0, fmt.Errorf("an element's length %q", digits)
	}
	n := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("an element's length %q", digits)
		}
		n = n*10 + int(c-'0')
	}
	if n > MaxElement {
		return 0, fmt.Errorf("an element of %d characters, past %d", n, MaxElement)
	}
	return n, nil
}

// runeOffset is the byte offset past the first n characters of s; false
// when s holds fewer.
func runeOffset(s string, n int) (int, bool) {
	off := 0
	for i := 0; i < n; i++ {
		if off >= len(s) {
			return 0, false
		}
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
	}
	return off, true
}

// Read reads one instruction from r: guacd's, as its session opens.
func Read(r *bufio.Reader) (Instruction, error) {
	var elems []string
	for {
		digits, err := r.ReadString('.')
		if err != nil {
			return Instruction{}, err
		}
		n, err := length(digits[:len(digits)-1])
		if err != nil {
			return Instruction{}, err
		}
		var b strings.Builder
		for i := 0; i < n; i++ {
			c, _, err := r.ReadRune()
			if err != nil {
				return Instruction{}, err
			}
			b.WriteRune(c)
		}
		elems = append(elems, b.String())
		if len(elems) > MaxElements {
			return Instruction{}, fmt.Errorf("more than %d elements", MaxElements)
		}
		sep, err := r.ReadByte()
		if err != nil {
			return Instruction{}, err
		}
		switch sep {
		case ';':
			return Instruction{Opcode: elems[0], Args: elems[1:]}, nil
		case ',':
		default:
			return Instruction{}, fmt.Errorf("%q after an element", sep)
		}
	}
}

// CutUTF8 splits b where a text message may end: whole is b but for a
// character its end cuts in two, rest that character's first bytes, which
// go with the next message.
func CutUTF8(b []byte) (whole, rest []byte) {
	// A character is at most 4 bytes: look back as far for its start.
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return b, nil
			}
			return b[:i], b[i:]
		}
	}
	return b, nil
}
