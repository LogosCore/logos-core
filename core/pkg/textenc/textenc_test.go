package textenc

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

func encode(t *testing.T, id, s string) []byte {
	t.Helper()
	e, ok := Lookup(id)
	if !ok || e.enc == nil {
		return []byte(s)
	}
	b, err := e.enc.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encode %q as %s: %v", s, id, err)
	}
	return b
}

func TestDecode(t *testing.T) {
	cp866 := []byte{0x8f, 0xe0, 0xa8, 0xa2, 0xa5, 0xe2} // "Привет"
	if got := Decode("ibm866", cp866); got != "Привет" {
		t.Errorf("ibm866: %q", got)
	}
	// UTF-8, the default, for none and for one unknown: what is not
	// UTF-8 stands as U+FFFD.
	for _, id := range []string{"", UTF8, "ebcdic-klingon"} {
		// E0 A8 A2 ("\u0440\u0438") happens to be UTF-8 too, U+0A22; a run of
		// bytes that are not is one U+FFFD.
		if got := Decode(id, cp866); got != "\uFFFD\u0A22\uFFFD" {
			t.Errorf("%q: %q", id, got)
		}
		if got := Decode(id, []byte("dir ✓")); got != "dir ✓" {
			t.Errorf("%q: %q", id, got)
		}
	}
	if got := Decode("utf-16le", encode(t, "utf-16le", "C:\\Пользователи\r\n")); got != "C:\\Пользователи\r\n" {
		t.Errorf("utf-16le: %q", got)
	}
}

func TestEveryEncodingRoundTrips(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range All() {
		if seen[e.ID] {
			t.Errorf("%s twice", e.ID)
		}
		seen[e.ID] = true
		if e.ID != strings.ToLower(e.ID) {
			t.Errorf("%s: ids are lower case", e.ID)
		}
		// Each reads back what it wrote.
		in := "Volume Serial Number is 1234-ABCD"
		if got := Decode(e.ID, encode(t, e.ID, in)); got != in {
			t.Errorf("%s: %q", e.ID, got)
		}
	}
}

func TestSample(t *testing.T) {
	head := strings.Repeat("ascii line\n", 400) // 4400 bytes
	tail := string([]byte{0x8f, 0xe0, 0xa8, 0xa2, 0xa5, 0xe2}) + "\n"
	b := []byte(head + "C:\\" + tail)
	s := Sample(b)
	// From its line's start, on a multiple of 4.
	start := bytes.Index(b, s)
	if start%4 != 0 || start > len(head) || len(head)-start >= 4 {
		t.Fatalf("starts at %d, its line at %d", start, len(head))
	}
	if !bytes.HasSuffix(s, []byte(tail)) {
		t.Errorf("sample %q", s)
	}

	// All ASCII: from the start, SampleMax at most.
	if s := Sample([]byte(head)); len(s) != SampleMax || !bytes.HasPrefix([]byte(head), s) {
		t.Errorf("ASCII sample of %d bytes", len(s))
	}

	// A long line: a quarter before its first byte that is not ASCII.
	long := []byte(strings.Repeat("x", 5000) + "\xe9")
	if s := Sample(long); !bytes.Contains(s, []byte{0xe9}) || len(s) > SampleMax {
		t.Errorf("long line: %d bytes, the byte in it: %v", len(s), bytes.Contains(s, []byte{0xe9}))
	}

	// UTF-16LE: its NULs start the sample, aligned so it reads.
	u16, _ := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder().Bytes([]byte(head[:44] + "Привет\r\n"))
	if got := Decode("utf-16le", Sample(u16)); !strings.HasSuffix(got, "Привет\r\n") || strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("utf-16le sample: %q", got)
	}
}

func TestDecodingsFindTheEncoding(t *testing.T) {
	text := "Том в устройстве C не имеет метки.\r\n Серийный номер тома: 1234-ABCD\r\n"
	cp866, _ := charmap.CodePage866.NewEncoder().Bytes([]byte(text))
	var got Decoding
	for _, d := range Decodings(cp866) {
		if d.ID == "ibm866" {
			got = d
		}
		if d.ID == UTF8 && d.Unreadable == 0 {
			t.Errorf("CP866 read as UTF-8 with nothing unreadable: %q", d.Text)
		}
		if d.ID == "iso-8859-1" && d.Unreadable == 0 {
			t.Errorf("CP866 read as ISO-8859-1 with no C1 controls: %q", d.Text)
		}
	}
	if got.Text != text || got.Unreadable != 0 {
		t.Errorf("ibm866: %q, %d unreadable", got.Text, got.Unreadable)
	}
	if n := len(Decodings(cp866)); n != len(All()) {
		t.Errorf("%d decodings of %d encodings", n, len(All()))
	}
}

func TestDecodingsCountLostLines(t *testing.T) {
	// ASCII read as UTF-16 is every character valid: its lines, lost,
	// are what says it is not.
	ascii := []byte("uid=1000(user) gid=1000(user)\nline two\nline three\n")
	u16, _ := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder().Bytes([]byte("Привет\r\nмир\r\n"))
	for _, c := range []struct {
		name  string
		b     []byte
		check map[string]bool // encoding: whether something is unreadable
	}{
		{"ascii", ascii, map[string]bool{UTF8: false, "ibm866": false, "utf-16le": true, "utf-16be": true, "utf-32le": true}},
		{"utf-16le", u16, map[string]bool{"utf-16le": false, UTF8: true, "ibm866": true}},
	} {
		for _, d := range Decodings(c.b) {
			if want, ok := c.check[d.ID]; ok && (d.Unreadable > 0) != want {
				t.Errorf("%s read as %s: %d unreadable in %q", c.name, d.ID, d.Unreadable, d.Text)
			}
		}
	}
}

func TestDecodingsLeaveTheCutCharacterOut(t *testing.T) {
	// A sample cut inside a character: none of it is unreadable.
	b := []byte(strings.Repeat("я", SampleMax)) // 2 bytes each in UTF-8
	b = append([]byte("x"), b...)               // the cut falls inside one
	for _, d := range Decodings(b) {
		if d.ID == UTF8 && d.Unreadable != 0 {
			t.Errorf("UTF-8: %d unreadable", d.Unreadable)
		}
	}
	u16, _ := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder().Bytes([]byte(strings.Repeat("я", SampleMax)))
	u16 = append([]byte("x\x00"), u16...)
	for _, d := range Decodings(u16) {
		if d.ID == "utf-16le" && d.Unreadable != 0 {
			t.Errorf("UTF-16LE: %d unreadable", d.Unreadable)
		}
	}
}
