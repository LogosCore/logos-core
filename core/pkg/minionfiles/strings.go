package minionfiles

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// Strings finds the printable strings in a kept file, as strings(1) does:
// runs of printable ASCII (and tab), one byte a character or, in UTF-16LE,
// each followed by a zero byte. A page is read from a place in the file
// (StringsQuery.After) and says where the next starts, so a file of any
// size is read a piece at a time, nothing kept between pages. A page stops
// at First strings, or after reading stringsBudget bytes: with a search
// that matches little, a page may come back short, or empty, and not done.

// Encodings a string is found in.
const (
	EncodingASCII   = "ascii"
	EncodingUTF16LE = "utf16le"
)

const (
	// stringsBudget is how much of the file one page reads at most.
	stringsBudget = 8 << 20
	// maxStringLen cuts a run this many characters long: the rest is the
	// next string. It keeps a page's place close behind where it read to.
	maxStringLen = 4096
)

type StringsQuery struct {
	// After is where in the file to read from: 0, or a page's Next.
	After int64
	// First is how many strings at most.
	First int
	// MinLength is the fewest characters a string has.
	MinLength int
	// Encoding is EncodingASCII or EncodingUTF16LE; empty for both.
	Encoding string
	// Search keeps the strings that hold it, any case.
	Search string
}

// FoundString is a string in the file: Len bytes from Off.
type FoundString struct {
	Off      int64
	Len      int64
	Encoding string
	Text     string
}

type StringsPage struct {
	Strings []FoundString
	// Next is where the next page reads from; the file's size when Done.
	Next int64
	Done bool
}

// Strings reads a page of a kept file's strings.
func (s *Service) Strings(ctx context.Context, f models.MinionFile, q StringsQuery) (StringsPage, error) {
	if f.State != models.MinionFileComplete {
		return StringsPage{}, fmt.Errorf("the file is %s", f.State)
	}
	if q.After < 0 || q.After > f.Total {
		return StringsPage{}, fmt.Errorf("after is past the file's %d bytes", f.Total)
	}
	if q.After == f.Total {
		return StringsPage{Next: f.Total, Done: true}, nil
	}
	body, _, err := s.store.Get(ctx, objectKey(f.FileID))
	if err != nil {
		return StringsPage{}, err
	}
	defer body.Close()
	if sk, ok := body.(io.Seeker); ok {
		_, err = sk.Seek(q.After, io.SeekStart)
	} else {
		_, err = io.CopyN(io.Discard, body, q.After)
	}
	if err != nil {
		return StringsPage{}, fmt.Errorf("read from %d: %w", q.After, err)
	}
	return scanStrings(bufio.NewReaderSize(body, 64<<10), q, stringsBudget)
}

// run is a string being read.
type run struct {
	start int64 // -1: none
	text  []byte
}

// half is a UTF-16 character's first byte, read; at is where (-1: none).
type half struct {
	at int64
	b  byte
}

func printable(b byte) bool { return b >= 0x20 && b <= 0x7e || b == '\t' }

// scanStrings reads r, which starts at q.After, for at most budget bytes.
// A page's place is the earliest string still being read when it stopped
// (or the first byte of a UTF-16 character half read): the next page reads
// that string whole, and only the strings that start before it are this
// page's, so none is cut by a page's end or found twice.
func scanStrings(r io.ByteReader, q StringsQuery, budget int64) (StringsPage, error) {
	ascii := q.Encoding == "" || q.Encoding == EncodingASCII
	utf16 := q.Encoding == "" || q.Encoding == EncodingUTF16LE
	needle := strings.ToLower(q.Search)
	var found []FoundString
	emit := func(r *run, enc string, width int64) {
		if r.start >= 0 && len(r.text) >= q.MinLength {
			text := string(r.text)
			if needle == "" || strings.Contains(strings.ToLower(text), needle) {
				found = append(found, FoundString{Off: r.start, Len: int64(len(r.text)) * width, Encoding: enc, Text: text})
			}
		}
		r.start, r.text = -1, r.text[:0]
	}
	a := run{start: -1}
	// One UTF-16 reading for each alignment: characters from even places,
	// and from odd ones.
	u := [2]run{{start: -1}, {start: -1}}
	h := [2]half{{at: -1}, {at: -1}}

	pos, end := q.After, q.After+budget
	// A string the page began with goes on being read past the budget:
	// it is no longer than maxStringLen, and a page has to get somewhere.
	holds := func() bool {
		return a.start == q.After || u[0].start == q.After || u[1].start == q.After || h[0].at == q.After || h[1].at == q.After
	}
	done := false
	for (len(found) < q.First && pos < end) || holds() {
		b, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			done = true
			break
		}
		if err != nil {
			return StringsPage{}, err
		}
		if ascii {
			if printable(b) {
				if a.start < 0 {
					a.start = pos
				}
				a.text = append(a.text, b)
				if len(a.text) == maxStringLen {
					emit(&a, EncodingASCII, 1)
				}
			} else {
				emit(&a, EncodingASCII, 1)
			}
		}
		if utf16 {
			i := pos & 1
			// This byte ends the character the other alignment began a
			// byte ago,
			if o := &h[1-i]; o.at >= 0 {
				if b == 0 {
					if u[1-i].start < 0 {
						u[1-i].start = o.at
					}
					u[1-i].text = append(u[1-i].text, o.b)
					if len(u[1-i].text) == maxStringLen {
						emit(&u[1-i], EncodingUTF16LE, 2)
					}
				} else {
					emit(&u[1-i], EncodingUTF16LE, 2)
				}
				o.at = -1
			}
			// and begins one in its own.
			if printable(b) {
				h[i] = half{at: pos, b: b}
			} else {
				emit(&u[i], EncodingUTF16LE, 2)
			}
		}
		pos++
	}

	next := pos
	if done {
		emit(&a, EncodingASCII, 1)
		emit(&u[0], EncodingUTF16LE, 2)
		emit(&u[1], EncodingUTF16LE, 2)
	} else {
		for _, at := range []int64{a.start, u[0].start, u[1].start, h[0].at, h[1].at} {
			if at >= 0 && at < next {
				next = at
			}
		}
	}
	page := StringsPage{Next: next, Done: done}
	for _, f := range found {
		if done || f.Off < next {
			page.Strings = append(page.Strings, f)
		}
	}
	sort.SliceStable(page.Strings, func(i, j int) bool { return page.Strings[i].Off < page.Strings[j].Off })
	return page, nil
}
