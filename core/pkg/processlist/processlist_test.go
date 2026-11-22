package processlist

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func u(n uint64) *uint64 { return &n }
func i(n int64) *int64   { return &n }

// part is a process_list part as core keeps it: named CBOR.
func part(t *testing.T, seq uint64, procs ...protocol.Process) models.MinionTaskPart {
	t.Helper()
	data, err := protocol.ResultData(protocol.ProcessList{Sep: "/", Processes: procs})
	if err != nil {
		t.Fatal(err)
	}
	return models.MinionTaskPart{Seq: seq, DataCBOR: data}
}

// A host: init with sshd (and its shell under it) and cron, kthreadd with a
// worker, a process whose parent ended, two whose reused pids loop, and one
// that is its own parent (as Windows' idle process is).
func host() []protocol.Process {
	return []protocol.Process{
		{PID: 1, PPID: u(0), Name: "systemd", Started: i(100), User: "root", Memory: u(12 << 20), Threads: u(1)},
		{PID: 812, PPID: u(1), Name: "sshd", Started: i(200), User: "root", Path: "/usr/sbin/sshd", Cmdline: "sshd: /usr/sbin/sshd -D",
			Memory: u(6 << 20)},
		{PID: 900, PPID: u(812), Name: "bash", Started: i(300), User: "ann", Cmdline: "-bash", Memory: u(4 << 20), CPUTime: u(90)},
		{PID: 640, PPID: u(1), Name: "cron", Started: i(150), User: "root"},
		{PID: 2, PPID: u(0), Name: "kthreadd", Started: i(100)},
		{PID: 3, PPID: u(2), Name: "kworker/0:0"},
		{PID: 4000, PPID: u(3999), Name: "orphan", Started: i(500), User: "Bob"},
		{PID: 11, PPID: u(10), Name: "looped-b", Started: i(50)},
		{PID: 10, PPID: u(11), Name: "looped-a", Started: i(60)},
		{PID: 4, PPID: u(4), Name: "self-parent"},
	}
}

func names(rows []Row) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func all(t *testing.T, procs []protocol.Process, q Query) Page {
	t.Helper()
	q.First = 1000
	p, err := List(procs, q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The parts' processes in seq order, a pid that comes again kept as it first
// came; a part with no data passes; one that does not fit says which.
func TestRead(t *testing.T) {
	sep, procs, err := Read([]models.MinionTaskPart{
		part(t, 0, protocol.Process{PID: 1, Name: "init"}, protocol.Process{PID: 7, Name: "first"}),
		{Seq: 1},
		part(t, 2, protocol.Process{PID: 7, Name: "again"}, protocol.Process{PID: 8, Name: "late"}),
	})
	if err != nil || sep != "/" || !slices.Equal(names(rowsOf(procs)), []string{"init", "first", "late"}) {
		t.Fatalf("read %+v %v", procs, err)
	}
	bad, _ := protocol.ResultData(map[string]any{"sep": "?"})
	if _, _, err := Read([]models.MinionTaskPart{{Seq: 3, DataCBOR: bad}}); err == nil || !strings.Contains(err.Error(), "part 3") {
		t.Fatalf("a part that does not fit: %v", err)
	}
}

func rowsOf(procs []protocol.Process) []Row {
	out := make([]Row, len(procs))
	for k, p := range procs {
		out[k] = Row{Process: p}
	}
	return out
}

// The tree: roots (no parent in the list, their own parent, a loop's lowest
// pid) and each process under its parent, siblings in the order they
// started (those that do not say last), then by pid; depths, children, the
// minion's own.
func TestTree(t *testing.T) {
	p := all(t, host(), Query{Order: OrderTree, MinionPID: 900})
	want := []string{"looped-a", "looped-b", "systemd", "cron", "sshd", "bash", "kthreadd", "kworker/0:0", "orphan", "self-parent"}
	if got := names(p.Rows); !slices.Equal(got, want) {
		t.Fatalf("tree %v\nwant %v", got, want)
	}
	if p.Total != 10 || p.More {
		t.Fatalf("total %d, more %v", p.Total, p.More)
	}
	depth := map[string]int{}
	for _, r := range p.Rows {
		depth[r.Name] = r.Depth
		if r.Context {
			t.Errorf("%s shown as context with no search", r.Name)
		}
		if r.Minion != (r.PID == 900) {
			t.Errorf("%s: minion %v", r.Name, r.Minion)
		}
	}
	for name, d := range map[string]int{"systemd": 0, "sshd": 1, "bash": 2, "kworker/0:0": 1, "orphan": 0, "looped-a": 0, "looped-b": 1, "self-parent": 0} {
		if depth[name] != d {
			t.Errorf("%s: depth %d, want %d", name, depth[name], d)
		}
	}
	if r := p.Rows[slices.IndexFunc(p.Rows, func(r Row) bool { return r.Name == "systemd" })]; r.Children != 2 {
		t.Errorf("systemd's children: %d", r.Children)
	}
}

// A search keeps the matches, in the tree with their ancestors as context;
// it reads the name, the pid, the user, the path and the command line,
// whatever the case. The total counts the matches alone.
func TestSearch(t *testing.T) {
	p := all(t, host(), Query{Order: OrderTree, Search: "BASH"})
	if got := names(p.Rows); !slices.Equal(got, []string{"systemd", "sshd", "bash"}) || p.Total != 1 {
		t.Fatalf("tree search %v, total %d", got, p.Total)
	}
	if !p.Rows[0].Context || !p.Rows[1].Context || p.Rows[2].Context {
		t.Fatalf("context %+v", p.Rows)
	}
	for search, want := range map[string][]string{
		"812":            {"sshd"},
		"/usr/sbin":      {"sshd"},
		"sshd -d":        {"sshd"},
		"bob":            {"orphan"},
		"kworker/0":      {"kworker/0:0"},
		"no such thing!": {},
	} {
		if got := names(all(t, host(), Query{Order: OrderPID, Search: search}).Rows); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", search, got, want)
		}
	}
}

// Flat, by a column, either way: those without a value last either way,
// then by pid; names without regard to case.
func TestFlat(t *testing.T) {
	for _, c := range []struct {
		order Order
		desc  bool
		want  []string
	}{
		{OrderPID, false, []string{"systemd", "kthreadd", "kworker/0:0", "self-parent", "looped-a", "looped-b", "cron", "sshd", "bash", "orphan"}},
		{OrderPID, true, []string{"orphan", "bash", "sshd", "cron", "looped-b", "looped-a", "self-parent", "kworker/0:0", "kthreadd", "systemd"}},
		{OrderMemory, true, []string{"systemd", "sshd", "bash", "kthreadd", "kworker/0:0", "self-parent", "looped-a", "looped-b", "cron", "orphan"}},
		{OrderMemory, false, []string{"bash", "sshd", "systemd", "kthreadd", "kworker/0:0", "self-parent", "looped-a", "looped-b", "cron", "orphan"}},
		{OrderStarted, false, []string{"looped-b", "looped-a", "systemd", "kthreadd", "cron", "sshd", "bash", "orphan", "kworker/0:0", "self-parent"}},
		{OrderUser, false, []string{"bash", "orphan", "systemd", "cron", "sshd", "kthreadd", "kworker/0:0", "self-parent", "looped-a", "looped-b"}},
		{OrderName, false, []string{"bash", "cron", "kthreadd", "kworker/0:0", "looped-a", "looped-b", "orphan", "self-parent", "sshd", "systemd"}},
		{OrderCPUTime, true, []string{"bash", "systemd", "kthreadd", "kworker/0:0", "self-parent", "looped-a", "looped-b", "cron", "sshd", "orphan"}},
		{OrderThreads, false, []string{"systemd", "kthreadd", "kworker/0:0", "self-parent", "looped-a", "looped-b", "cron", "sshd", "bash", "orphan"}},
	} {
		p := all(t, host(), Query{Order: c.order, Desc: c.desc})
		if got := names(p.Rows); !slices.Equal(got, c.want) {
			t.Errorf("%s desc=%v: %v\nwant %v", c.order, c.desc, got, c.want)
		}
		for _, r := range p.Rows {
			if r.Depth != 0 || r.Context {
				t.Errorf("%s: flat row %+v", c.order, r)
			}
		}
	}
	if p := all(t, host(), Query{Order: OrderPID}); p.Rows[0].Name != "systemd" || p.Rows[0].Children != 2 {
		t.Fatalf("children in a flat list: %+v", p.Rows[0])
	}
}

// Pages: at most First rows, each with the cursor after it; the next page
// from a page's last cursor; More until the last; a cursor that does not
// read is refused.
func TestPages(t *testing.T) {
	var got []string
	after := ""
	for n := 0; ; n++ {
		p, err := List(host(), Query{Order: OrderPID, First: 4, After: after})
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 10 || len(p.Rows) > 4 {
			t.Fatalf("page %d: total %d, %d rows", n, p.Total, len(p.Rows))
		}
		got = append(got, names(p.Rows)...)
		if !p.More {
			break
		}
		after = p.Cursor(len(p.Rows) - 1)
	}
	if want := names(all(t, host(), Query{Order: OrderPID}).Rows); !slices.Equal(got, want) {
		t.Fatalf("paged %v\nwant %v", got, want)
	}
	if p, err := List(host(), Query{Order: OrderPID, First: 4, After: "99"}); err != nil || len(p.Rows) != 0 || p.More {
		t.Fatalf("past the end: %+v %v", p, err)
	}
	for _, bad := range []string{"x", "-1"} {
		if _, err := List(host(), Query{First: 4, After: bad}); !errors.Is(err, ErrBadCursor) {
			t.Errorf("cursor %q: %v", bad, err)
		}
	}
}
