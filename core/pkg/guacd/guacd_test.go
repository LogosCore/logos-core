package guacd

import (
	"bufio"
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEncodeParse(t *testing.T) {
	ins := []Instruction{
		{Opcode: "size", Args: []string{"1024", "768", "96"}},
		{Opcode: "clipboard", Args: []string{"héllo, wörld; 🦅"}}, // lengths in characters
		{Opcode: "audio"},
		{Opcode: "", Args: []string{"ping", "1731600000000"}},
	}
	if got := ins[0].Encode(); got != "4.size,4.1024,3.768,2.96;" {
		t.Fatalf("encode %q", got)
	}
	if got := ins[1].Encode(); got != "9.clipboard,15.héllo, wörld; 🦅;" {
		t.Fatalf("encode %q", got)
	}
	var text string
	for _, i := range ins {
		text += i.Encode()
	}
	got, err := Parse(text)
	if err != nil || len(got) != len(ins) {
		t.Fatalf("parse %v: %v", got, err)
	}
	for n := range ins {
		if got[n].Opcode != ins[n].Opcode || !slices.Equal(got[n].Args, ins[n].Args) && len(ins[n].Args)+len(got[n].Args) > 0 {
			t.Errorf("%d: %v, want %v", n, got[n], ins[n])
		}
	}
	// What is not whole, or not Guacamole's.
	for text, want := range map[string]error{
		"4.size,4.10":   ErrIncomplete,
		"4.size,4.1024": ErrIncomplete,
		"4.size":        ErrIncomplete,
		"4.size,":       ErrIncomplete,
		"12":            ErrIncomplete,
	} {
		if _, err := Parse(text); !errors.Is(err, want) {
			t.Errorf("%q: %v", text, err)
		}
	}
	for _, text := range []string{"x.size;", "4.size:", "4.sizeX;", "1234567.a;", "-1.a;", "99999999999;"} {
		if _, err := Parse(text); err == nil || errors.Is(err, ErrIncomplete) {
			t.Errorf("%q: %v", text, err)
		}
	}
}

func TestRead(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("4.args,13.VERSION_1_5_0,8.hostname;5.ready,5.$abcd;"))
	a, err := Read(r)
	if err != nil || a.Opcode != "args" || !slices.Equal(a.Args, []string{"VERSION_1_5_0", "hostname"}) {
		t.Fatalf("%v %v", a, err)
	}
	b, err := Read(r)
	if err != nil || b.Opcode != "ready" || b.Args[0] != "$abcd" {
		t.Fatalf("%v %v", b, err)
	}
	if _, err := Read(r); err == nil {
		t.Fatal("read past the end")
	}
}

func TestCutUTF8(t *testing.T) {
	eagle := []byte("🦅") // 4 bytes
	for n := range 5 {
		b := append([]byte("4.blob,"), eagle[:n]...)
		whole, rest := CutUTF8(b)
		if n == 0 || n == 4 {
			if len(rest) != 0 || len(whole) != len(b) {
				t.Errorf("%d bytes of a character: cut %q %q", n, whole, rest)
			}
			continue
		}
		if string(whole) != "4.blob," || len(rest) != n {
			t.Errorf("%d bytes of a character: cut %q %q", n, whole, rest)
		}
	}
	if whole, rest := CutUTF8(nil); len(whole)+len(rest) != 0 {
		t.Error("nil")
	}
}

// fakeGuacd is guacd for one session: it asks for args, reads what core
// sends up to connect, and answers ready (or ends with errorAt).
func fakeGuacd(t *testing.T, args []string, errorAt string) (string, <-chan []Instruction) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	got := make(chan []Instruction, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		r := bufio.NewReader(c)
		var seen []Instruction
		sel, _ := Read(r)
		seen = append(seen, sel)
		if errorAt == "select" {
			_, _ = c.Write([]byte(Instruction{Opcode: "error", Args: []string{"No such protocol", "512"}}.Encode()))
			got <- seen
			return
		}
		_, _ = c.Write([]byte(Instruction{Opcode: "args", Args: args}.Encode()))
		for {
			ins, err := Read(r)
			if err != nil {
				got <- seen
				return
			}
			seen = append(seen, ins)
			if ins.Opcode == "connect" {
				break
			}
		}
		if errorAt == "connect" {
			_, _ = c.Write([]byte(Instruction{Opcode: "error", Args: []string{"Aborted. See logs.", "519"}}.Encode()))
			got <- seen
			return
		}
		// Ready, and the session's first instructions with it.
		_, _ = c.Write([]byte(Instruction{Opcode: "ready", Args: []string{"$conn-1"}}.Encode() + "4.sync,1.0;"))
		// What core relays after: echoed back.
		buf := make([]byte, 64)
		n, _ := r.Read(buf)
		_, _ = c.Write(buf[:n])
		got <- seen
	}()
	return l.Addr().String(), got
}

func TestOpen(t *testing.T) {
	addr, got := fakeGuacd(t, []string{"VERSION_1_5_0", "hostname", "port", "username", "password", "color-depth"}, "")
	s, err := Open(context.Background(), addr, Config{Protocol: "rdp", Width: 1280, Height: 720, DPI: 96, Timezone: "Europe/Kyiv", Name: "alice",
		Params: map[string]string{"hostname": "core", "port": "24005", "username": "admin", "password": "p,a;s.s", "ignored": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if s.ID != "$conn-1" || s.Version != "VERSION_1_5_0" {
		t.Fatalf("session %+v", s)
	}
	// What was bought with ready comes first.
	buf := make([]byte, 64)
	n, _ := s.Read(buf)
	if string(buf[:n]) != "4.sync,1.0;" {
		t.Fatalf("first read %q", buf[:n])
	}
	if _, err := s.Write([]byte("5.mouse,1.1,1.2,1.0;")); err != nil {
		t.Fatal(err)
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ = s.Read(buf)
	if string(buf[:n]) != "5.mouse,1.1,1.2,1.0;" {
		t.Fatalf("echo %q", buf[:n])
	}
	seen := <-got
	var ops []string
	for _, i := range seen {
		ops = append(ops, i.Opcode)
	}
	if strings.Join(ops, " ") != "select size audio video image timezone name connect" {
		t.Fatalf("sent %v", ops)
	}
	if seen[0].Args[0] != "rdp" || !slices.Equal(seen[1].Args, []string{"1280", "720", "96"}) || seen[5].Args[0] != "Europe/Kyiv" {
		t.Errorf("select, size, timezone: %v", seen)
	}
	// connect: the version, then each arg guacd asked for, in its order.
	if !slices.Equal(seen[7].Args, []string{"VERSION_1_5_0", "core", "24005", "admin", "p,a;s.s", ""}) {
		t.Errorf("connect %v", seen[7].Args)
	}
}

func TestOpenRefused(t *testing.T) {
	for _, at := range []string{"select", "connect"} {
		addr, _ := fakeGuacd(t, []string{"VERSION_1_5_0", "hostname"}, at)
		_, err := Open(context.Background(), addr, Config{Protocol: "rdp", Width: 800, Height: 600, DPI: 96})
		var e *Error
		if !errors.As(err, &e) || e.Message == "" || (at == "connect" && e.Status != 519) || (at == "select" && e.Status != 512) {
			t.Errorf("%s: %v", at, err)
		}
	}
	// No guacd there.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	_ = l.Close()
	var e *Error
	if _, err := Open(context.Background(), addr, Config{Protocol: "vnc"}); !errors.As(err, &e) || e.Status != StatusUpstreamNotFound {
		t.Errorf("no guacd: %v", err)
	}
}

func TestVersionAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"VERSION_1_5_0": true, "VERSION_1_3_0": true, "VERSION_1_1_0": false, "VERSION_2_0_0": true, "": false, "VERSION_x": false} {
		if versionAtLeast(v, 1, 3) != want {
			t.Errorf("%q", v)
		}
	}
}
