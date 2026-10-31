package minionfiles

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
)

func utf16le(s string) []byte {
	var b []byte
	for i := 0; i < len(s); i++ {
		b = append(b, s[i], 0)
	}
	return b
}

func scanAll(t *testing.T, data []byte, q StringsQuery) []FoundString {
	t.Helper()
	q.First = 1 << 30
	p, err := scanStrings(bytes.NewReader(data), q, int64(len(data))+1)
	if err != nil || !p.Done || p.Next != int64(len(data)) {
		t.Fatalf("scan: %+v %v", p, err)
	}
	return p.Strings
}

func TestScanStrings(t *testing.T) {
	var data []byte
	data = append(data, 0x01, 0x02)
	data = append(data, "hello world"...) // 2
	data = append(data, 0xff)
	data = append(data, utf16le("Wide One")...) // 14, even
	data = append(data, 0xfe, 0x7f, 0x00)       // 0x7f is not printable
	data = append(data, 0x01)
	data = append(data, utf16le("Odd")...) // 34, odd
	data = append(data, 0xfe, 0xfe, 'a', 'b', 0xfe)
	got := scanAll(t, data, StringsQuery{MinLength: 3})
	want := []FoundString{
		{Off: 2, Len: 11, Encoding: EncodingASCII, Text: "hello world"},
		{Off: 14, Len: 16, Encoding: EncodingUTF16LE, Text: "Wide One"},
		{Off: 34, Len: 6, Encoding: EncodingUTF16LE, Text: "Odd"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := scanAll(t, data, StringsQuery{MinLength: 3, Encoding: EncodingASCII, Search: "WORLD"}); len(got) != 1 || got[0].Off != 2 {
		t.Fatalf("ascii, searched: %+v", got)
	}
	if got := scanAll(t, data, StringsQuery{MinLength: 2, Encoding: EncodingASCII}); len(got) != 2 || got[1].Text != "ab" {
		t.Fatalf("min 2: %+v", got)
	}
}

// A run past maxStringLen comes in pieces.
func TestScanStringsLong(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, maxStringLen*2+5)
	got := scanAll(t, data, StringsQuery{MinLength: 4, Encoding: EncodingASCII})
	if len(got) != 3 || got[1].Off != maxStringLen || got[2].Len != 5 {
		t.Fatalf("got %d: %+v", len(got), got[len(got)-1])
	}
}

// Read a page at a time, however small, a file gives the strings it gives
// read whole: none cut, none twice.
func TestScanStringsPaged(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 64<<10)
	for i := 0; i < len(data); {
		switch rng.Intn(4) {
		case 0:
			n := 1 + rng.Intn(40)
			for j := 0; j < n && i < len(data); j++ {
				data[i] = byte(0x20 + rng.Intn(95))
				i++
			}
		case 1:
			n := 1 + rng.Intn(30)
			for j := 0; j < n && i+1 < len(data); j++ {
				data[i], data[i+1] = byte(0x20+rng.Intn(95)), 0
				i += 2
			}
			i++
		default:
			data[i] = byte(rng.Intn(256))
			i++
		}
	}
	for _, q := range []StringsQuery{{MinLength: 4}, {MinLength: 2, Search: "a"}, {MinLength: 3, Encoding: EncodingUTF16LE}} {
		whole := scanAll(t, data, q)
		for _, step := range []struct {
			first  int
			budget int64
		}{{1, 1 << 20}, {7, 300}, {1000, 97}} {
			var paged []FoundString
			q := q
			q.First = step.first
			for after, pages := int64(0), 0; ; pages++ {
				q.After = after
				p, err := scanStrings(bytes.NewReader(data[after:]), q, step.budget)
				if err != nil || pages > len(data) {
					t.Fatalf("page at %d: %v", after, err)
				}
				paged = append(paged, p.Strings...)
				if p.Done {
					break
				}
				if p.Next <= after {
					t.Fatalf("stuck at %d", after)
				}
				after = p.Next
			}
			if !reflect.DeepEqual(paged, whole) {
				t.Fatalf("%+v %+v: %d strings paged, %d whole", q, step, len(paged), len(whole))
			}
		}
	}
}
