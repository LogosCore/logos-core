// Package textenc reads a task's output, the bytes a minion sent as its
// host gave them, as text: as UTF-8, or as the encoding an operator picked
// for the session (a Windows host's cmd.exe writes its OEM code page,
// CP866 on a Russian one). Decodings reads a piece of it in every encoding
// it knows, for the operator to pick from, as CyberChef's Text Encoding
// Brute Force does.
package textenc

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"
)

// UTF8 is the default encoding's id: a session without one reads as it.
const UTF8 = "utf-8"

// Encoding is one encoding output can be read as: ID is what a session
// keeps, Name what an operator sees.
type Encoding struct {
	ID   string
	Name string
	enc  encoding.Encoding
}

// all is every encoding, in the order the table lists them before it sorts:
// Unicode, then the code pages a Windows console writes (OEM), Windows'
// own (ANSI), ISO 8859, the other single-byte ones, the CJK multi-byte ones
// and EBCDIC. Ids are lower case, IANA's names where there is one.
var all = []Encoding{
	{UTF8, "UTF-8", nil},
	{"utf-16le", "UTF-16LE", unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)},
	{"utf-16be", "UTF-16BE", unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)},
	{"utf-32le", "UTF-32LE", utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM)},
	{"utf-32be", "UTF-32BE", utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM)},
	{"ibm437", "OEM United States (437)", charmap.CodePage437},
	{"ibm850", "OEM Western European (850)", charmap.CodePage850},
	{"ibm852", "OEM Central European (852)", charmap.CodePage852},
	{"ibm855", "OEM Cyrillic (855)", charmap.CodePage855},
	{"ibm858", "OEM Western European + euro (858)", charmap.CodePage858},
	{"ibm860", "OEM Portuguese (860)", charmap.CodePage860},
	{"ibm862", "OEM Hebrew (862)", charmap.CodePage862},
	{"ibm863", "OEM French Canadian (863)", charmap.CodePage863},
	{"ibm865", "OEM Nordic (865)", charmap.CodePage865},
	{"ibm866", "OEM Russian (866)", charmap.CodePage866},
	{"windows-874", "Windows Thai (874)", charmap.Windows874},
	{"windows-1250", "Windows Central European (1250)", charmap.Windows1250},
	{"windows-1251", "Windows Cyrillic (1251)", charmap.Windows1251},
	{"windows-1252", "Windows Western European (1252)", charmap.Windows1252},
	{"windows-1253", "Windows Greek (1253)", charmap.Windows1253},
	{"windows-1254", "Windows Turkish (1254)", charmap.Windows1254},
	{"windows-1255", "Windows Hebrew (1255)", charmap.Windows1255},
	{"windows-1256", "Windows Arabic (1256)", charmap.Windows1256},
	{"windows-1257", "Windows Baltic (1257)", charmap.Windows1257},
	{"windows-1258", "Windows Vietnamese (1258)", charmap.Windows1258},
	{"iso-8859-1", "ISO-8859-1 Western European", charmap.ISO8859_1},
	{"iso-8859-2", "ISO-8859-2 Central European", charmap.ISO8859_2},
	{"iso-8859-3", "ISO-8859-3 South European", charmap.ISO8859_3},
	{"iso-8859-4", "ISO-8859-4 North European", charmap.ISO8859_4},
	{"iso-8859-5", "ISO-8859-5 Cyrillic", charmap.ISO8859_5},
	{"iso-8859-6", "ISO-8859-6 Arabic", charmap.ISO8859_6},
	{"iso-8859-7", "ISO-8859-7 Greek", charmap.ISO8859_7},
	{"iso-8859-8", "ISO-8859-8 Hebrew", charmap.ISO8859_8},
	{"iso-8859-9", "ISO-8859-9 Turkish", charmap.ISO8859_9},
	{"iso-8859-10", "ISO-8859-10 Nordic", charmap.ISO8859_10},
	{"iso-8859-13", "ISO-8859-13 Baltic", charmap.ISO8859_13},
	{"iso-8859-14", "ISO-8859-14 Celtic", charmap.ISO8859_14},
	{"iso-8859-15", "ISO-8859-15 Western European + euro", charmap.ISO8859_15},
	{"iso-8859-16", "ISO-8859-16 South-Eastern European", charmap.ISO8859_16},
	{"koi8-r", "KOI8-R Russian", charmap.KOI8R},
	{"koi8-u", "KOI8-U Ukrainian", charmap.KOI8U},
	{"macintosh", "Mac Roman", charmap.Macintosh},
	{"x-mac-cyrillic", "Mac Cyrillic", charmap.MacintoshCyrillic},
	{"shift_jis", "Japanese Shift_JIS (932)", japanese.ShiftJIS},
	{"euc-jp", "Japanese EUC-JP", japanese.EUCJP},
	{"iso-2022-jp", "Japanese ISO-2022-JP", japanese.ISO2022JP},
	{"gbk", "Chinese Simplified GBK (936)", simplifiedchinese.GBK},
	{"gb18030", "Chinese Simplified GB18030", simplifiedchinese.GB18030},
	{"hz-gb-2312", "Chinese Simplified HZ-GB-2312", simplifiedchinese.HZGB2312},
	{"big5", "Chinese Traditional Big5 (950)", traditionalchinese.Big5},
	{"euc-kr", "Korean EUC-KR (949)", korean.EUCKR},
	{"ibm037", "EBCDIC US-Canada (037)", charmap.CodePage037},
	{"ibm1047", "EBCDIC Latin 1 (1047)", charmap.CodePage1047},
	{"ibm01140", "EBCDIC US-Canada + euro (1140)", charmap.CodePage1140},
}

// All is every encoding output can be read as.
func All() []Encoding { return all }

// Lookup is the encoding id names; false for one this package does not know.
func Lookup(id string) (Encoding, bool) {
	for _, e := range all {
		if e.ID == id {
			return e, true
		}
	}
	return Encoding{}, false
}

// Decode is b read as the encoding id names (UTF-8 for "" or one unknown):
// what cannot be read stands as U+FFFD.
func Decode(id string, b []byte) string {
	e, ok := Lookup(id)
	if !ok || e.enc == nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	out, err := e.enc.NewDecoder().Bytes(b)
	if err != nil {
		// The decoders replace what they cannot read; an error is
		// theirs alone. Read it as UTF-8 rather than show nothing.
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(out)
}

// SampleMax is the most of the output Decodings reads in each encoding.
const SampleMax = 2048

// Sample is the piece of b Decodings reads: from the start of the line
// with its first byte that is not ASCII (or a NUL: UTF-16 and UTF-32 text
// is full of them), where encodings differ, SampleMax bytes at most. It
// starts on a multiple of 4 bytes from b's start, as UTF-16 and UTF-32
// text does.
func Sample(b []byte) []byte {
	first := bytes.IndexFunc(b, func(r rune) bool { return r >= utf8.RuneSelf || r == 0 })
	if first < 0 {
		first = 0
	}
	start := 0
	if nl := bytes.LastIndexByte(b[:first], '\n'); nl >= 0 {
		start = nl + 1
	}
	// A long line: the bytes before the first not ASCII keep a quarter.
	if first-start > SampleMax/4 {
		start = first - SampleMax/4
	}
	start &^= 3
	return b[start:min(len(b), start+SampleMax)]
}

// Decoding is a sample read in one encoding: its text, and how much of it
// is a sign the encoding is not the output's: characters that could not be
// read (U+FFFD) or are control characters but a tab, a line end or a form
// feed (a NUL in UTF-16 text read as one byte a character; C1's, CP866's
// letters read as ISO-8859-1), and line breaks it lost.
type Decoding struct {
	Encoding
	Text       string
	Unreadable int
}

// Decodings is b's sample (Sample) read in every encoding, in All's order.
// The sample's last character, cut by its end, is left out rather than
// shown as unreadable.
func Decodings(b []byte) []Decoding {
	sample := Sample(b)
	cut := len(sample) < len(b)
	breaks := bytes.Count(sample, []byte{'\n'})
	out := make([]Decoding, 0, len(all))
	for _, e := range all {
		text := decodeSample(e, sample, cut)
		n := unreadable(text)
		// Lines read as one: ASCII read as UTF-16 is every character
		// valid (CJK, mostly), its line feeds inside them. Real UTF-16
		// output keeps its own line feeds, and an ASCII-compatible
		// encoding all of them.
		if breaks > 0 && !strings.Contains(text, "\n") {
			n += breaks
		}
		out = append(out, Decoding{Encoding: e, Text: text, Unreadable: n})
	}
	return out
}

func decodeSample(e Encoding, sample []byte, cut bool) string {
	if e.enc == nil {
		if cut {
			sample = trimPartialUTF8(sample)
		}
		return strings.ToValidUTF8(string(sample), "�")
	}
	if !cut {
		return Decode(e.ID, sample)
	}
	// Not at the end: a character the cut split waits for bytes that
	// never come, and is left out.
	dst := make([]byte, 4*len(sample)+16)
	n, _, err := e.enc.NewDecoder().Transform(dst, sample, false)
	if err != nil && !errors.Is(err, transform.ErrShortSrc) {
		return Decode(e.ID, sample)
	}
	return string(dst[:n])
}

// trimPartialUTF8 drops a UTF-8 character the end cut, its first bytes.
func trimPartialUTF8(b []byte) []byte {
	for i := 1; i <= utf8.UTFMax-1 && i <= len(b); i++ {
		c := b[len(b)-i]
		if c < utf8.RuneSelf {
			return b
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			return b
		}
	}
	return b
}

func unreadable(s string) int {
	n := 0
	for _, r := range s {
		if r == utf8.RuneError || (r < 0x20 && r != '\t' && r != '\n' && r != '\r' && r != '\f') || (r >= 0x7f && r <= 0x9f) {
			n++
		}
	}
	return n
}
