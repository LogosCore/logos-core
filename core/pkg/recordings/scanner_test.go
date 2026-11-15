package recordings

import (
	"strings"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/guacd"
)

// guacd's bytes cut anywhere: each cut, the scanner says where the last
// whole instruction ended, its syncs and its error once each, however the
// bytes came (characters of several bytes counted as one).
func TestScannerFramesInstructionsCutAnywhere(t *testing.T) {
	ins := []guacd.Instruction{
		{Opcode: "size", Args: []string{"0", "1024", "768"}},
		{Opcode: "name", Args: []string{"Рабочий стол — ☃"}},
		{Opcode: "sync", Args: []string{"1700000000123", "1"}},
		{Opcode: "img", Args: []string{"1", "14", "0", "image/png", "0", "0"}},
		{Opcode: "blob", Args: []string{"1", strings.Repeat("iVBORw0KGgo", 300)}},
		{Opcode: "end", Args: []string{"1"}},
		{Opcode: "", Args: []string{}},
		{Opcode: "sync", Args: []string{"1700000000456"}},
		{Opcode: "error", Args: []string{"Сервер отключился", "521"}},
		{Opcode: "syncs", Args: []string{"9"}},
	}
	var text string
	var ends []int
	for _, i := range ins {
		text += i.Encode()
		ends = append(ends, len(text))
	}
	wantEnd := func(upTo int) int {
		last := 0
		for _, e := range ends {
			if e <= upTo {
				last = e
			}
		}
		return last
	}
	for _, step := range []int{1, 2, 3, 7, 64, 4096, len(text)} {
		var syncs []int64
		var said string
		var status int
		s := scanner{onSync: func(ts int64) { syncs = append(syncs, ts) },
			onError: func(m string, st int) { said, status = m, st }}
		got := 0
		for off := 0; off < len(text); off += step {
			chunk := []byte(text[off:min(off+step, len(text))])
			if end := s.scan(chunk); end > 0 {
				got = off + end
			}
			if s.bad {
				t.Fatalf("step %d: bad at %d", step, off)
			}
			if w := wantEnd(off + len(chunk)); got != w {
				t.Fatalf("step %d at %d: last end %d, want %d", step, off, got, w)
			}
		}
		if len(syncs) != 2 || syncs[0] != 1700000000123 || syncs[1] != 1700000000456 {
			t.Errorf("step %d: syncs %v", step, syncs)
		}
		if said != "Сервер отключился" || status != 521 {
			t.Errorf("step %d: error %q %d", step, said, status)
		}
	}
}

// Bytes that are not Guacamole's: the scanner reads no more.
func TestScannerBad(t *testing.T) {
	for _, text := range []string{"4.sync,3.123x", "x.sync;", "4.sync;3x", "999999999999.x"} {
		var s scanner
		s.scan([]byte(text))
		if !s.bad {
			t.Errorf("%q: not bad", text)
		}
	}
}
