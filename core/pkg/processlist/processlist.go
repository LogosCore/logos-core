// Package processlist is a process_list task's processes as the web app
// reads them (docs: contracts/task-results-and-actions.md, process_list):
// from the task's parts, each pid once, as it first came; as a tree (each
// process under its parent, siblings in the order they started) or flat by
// a column; searched; a page at a time. Core orders the whole list for each
// page: a host's thousands of processes take milliseconds.
package processlist

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Order is how a list is ordered (GraphQL's MinionProcessOrder).
type Order string

const (
	OrderTree    Order = "TREE"
	OrderPID     Order = "PID"
	OrderName    Order = "NAME"
	OrderUser    Order = "USER"
	OrderMemory  Order = "MEMORY"
	OrderCPUTime Order = "CPU_TIME"
	OrderStarted Order = "STARTED"
	OrderThreads Order = "THREADS"
)

// Row is a process as a page shows it.
type Row struct {
	protocol.Process
	// Depth is its depth below its root, in the tree (0 flat); Children how
	// many processes the list has under it.
	Depth    int
	Children int
	// Minion: this session's minion's own process. Context: shown in the
	// tree for a match below it, not matching itself.
	Minion  bool
	Context bool
}

// Query is what a page asks: a search (by name, pid, user, path and command
// line), an order and its way, the cursor it follows (none: the first
// page), how many rows, and the pid of this session's minion (0: none).
type Query struct {
	Search    string
	Order     Order
	Desc      bool
	After     string
	First     int
	MinionPID uint64
}

// Page is a page of rows: Total is how many processes match, More whether
// rows follow the page.
type Page struct {
	Rows  []Row
	Total int
	More  bool
	// start is the first row's place in the order.
	start int
}

// Cursor is the cursor after the page's i-th row.
func (p Page) Cursor(i int) string { return strconv.Itoa(p.start + i + 1) }

// ErrBadCursor is a cursor that does not read.
var ErrBadCursor = errors.New("invalid cursor")

// Read is a process_list task's processes, from its parts in seq order: each
// pid once, as it first came; and the host's separator, the first part's.
func Read(parts []models.MinionTaskPart) (string, []protocol.Process, error) {
	var sep string
	var out []protocol.Process
	seen := map[uint64]bool{}
	for _, part := range parts {
		if len(part.DataCBOR) == 0 {
			continue
		}
		l, err := protocol.ReadProcessList(part.Seq, part.DataCBOR)
		if err != nil {
			return "", nil, fmt.Errorf("part %d: %w", part.Seq, err)
		}
		if sep == "" {
			sep = l.Sep
		}
		for _, p := range l.Processes {
			if !seen[p.PID] {
				seen[p.PID] = true
				out = append(out, p)
			}
		}
	}
	return sep, out, nil
}

// List orders procs as q says, keeps those its search matches (in the tree,
// with their ancestors), and gives the page after its cursor.
func List(procs []protocol.Process, q Query) (Page, error) {
	from := 0
	if q.After != "" {
		n, err := strconv.Atoi(q.After)
		if err != nil || n < 0 {
			return Page{}, ErrBadCursor
		}
		from = n
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	match := func(p protocol.Process) bool { return search == "" || matches(p, search) }
	var rows []Row
	if q.Order == OrderTree || q.Order == "" {
		rows = tree(procs, match, q.MinionPID)
	} else {
		rows = flat(procs, match, q)
	}
	page := Page{start: min(from, len(rows))}
	for _, r := range rows {
		if !r.Context {
			page.Total++
		}
	}
	end := min(page.start+max(q.First, 0), len(rows))
	page.Rows, page.More = rows[page.start:end], end < len(rows)
	return page, nil
}

// matches reports whether search (lower case) is in the process's name,
// pid, user, path or command line.
func matches(p protocol.Process, search string) bool {
	for _, s := range []string{p.Name, strconv.FormatUint(p.PID, 10), p.User, p.Path, p.Cmdline} {
		if strings.Contains(strings.ToLower(s), search) {
			return true
		}
	}
	return false
}

// parents are each process's parent's place in procs: -1 for a root (no
// parent, or one not in the list, or itself), and the lowest pid of each
// chain of parents that loops back, as reused pids can make one on Windows,
// so that every process leads to a root.
func parents(procs []protocol.Process) []int {
	byPID := make(map[uint64]int, len(procs))
	for i, p := range procs {
		byPID[p.PID] = i
	}
	parent := make([]int, len(procs))
	for i, p := range procs {
		parent[i] = -1
		if p.PPID != nil && *p.PPID != p.PID {
			if j, ok := byPID[*p.PPID]; ok {
				parent[i] = j
			}
		}
	}
	// 0: not seen; 1: on the walk under way; 2: leads to a root.
	state := make([]uint8, len(procs))
	for i := range procs {
		var walk []int
		j := i
		for j >= 0 && state[j] == 0 {
			state[j] = 1
			walk = append(walk, j)
			j = parent[j]
		}
		if j >= 0 && state[j] == 1 {
			lowest := j
			for k := parent[j]; k != j; k = parent[k] {
				if procs[k].PID < procs[lowest].PID {
					lowest = k
				}
			}
			parent[lowest] = -1
		}
		for _, k := range walk {
			state[k] = 2
		}
	}
	return parent
}

// tree is the processes matched, and their ancestors, depth first: each
// under its parent, siblings in the order they started.
func tree(procs []protocol.Process, match func(protocol.Process) bool, minion uint64) []Row {
	parent := parents(procs)
	kids := make([][]int, len(procs))
	var roots []int
	for i := range procs {
		if parent[i] < 0 {
			roots = append(roots, i)
		} else {
			kids[parent[i]] = append(kids[parent[i]], i)
		}
	}
	bySiblings := func(a, b int) int { return compareSiblings(procs[a], procs[b]) }
	slices.SortFunc(roots, bySiblings)
	for _, k := range kids {
		slices.SortFunc(k, bySiblings)
	}
	matched, shown := make([]bool, len(procs)), make([]bool, len(procs))
	for i, p := range procs {
		if match(p) {
			matched[i] = true
			for j := i; j >= 0 && !shown[j]; j = parent[j] {
				shown[j] = true
			}
		}
	}
	type frame struct{ i, depth int }
	stack := make([]frame, 0, len(roots))
	for k := len(roots) - 1; k >= 0; k-- {
		stack = append(stack, frame{roots[k], 0})
	}
	var rows []Row
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !shown[f.i] {
			continue
		}
		p := procs[f.i]
		rows = append(rows, Row{Process: p, Depth: f.depth, Children: len(kids[f.i]), Minion: minion != 0 && p.PID == minion,
			Context: !matched[f.i]})
		for k := len(kids[f.i]) - 1; k >= 0; k-- {
			stack = append(stack, frame{kids[f.i][k], f.depth + 1})
		}
	}
	return rows
}

// flat is the processes matched, ordered by a column: those without a value
// last either way, then by pid.
func flat(procs []protocol.Process, match func(protocol.Process) bool, q Query) []Row {
	parent := parents(procs)
	children := make([]int, len(procs))
	for _, j := range parent {
		if j >= 0 {
			children[j]++
		}
	}
	var rows []Row
	for i, p := range procs {
		if match(p) {
			rows = append(rows, Row{Process: p, Children: children[i], Minion: q.MinionPID != 0 && p.PID == q.MinionPID})
		}
	}
	slices.SortFunc(rows, func(a, b Row) int {
		if c := compareBy(q.Order, q.Desc, a.Process, b.Process); c != 0 {
			return c
		}
		return cmp.Compare(a.PID, b.PID)
	})
	return rows
}

// compareBy orders two processes by a column, the other way for desc;
// those without a value last either way.
func compareBy(order Order, desc bool, a, b protocol.Process) int {
	switch order {
	case OrderPID:
		pa, pb := a.PID, b.PID
		return compareOpt(&pa, &pb, desc)
	case OrderName:
		return compareText(a.Name, b.Name, desc)
	case OrderUser:
		return compareText(a.User, b.User, desc)
	case OrderMemory:
		return compareOpt(a.Memory, b.Memory, desc)
	case OrderCPUTime:
		return compareOpt(a.CPUTime, b.CPUTime, desc)
	case OrderStarted:
		return compareOpt(a.Started, b.Started, desc)
	case OrderThreads:
		return compareOpt(a.Threads, b.Threads, desc)
	}
	return 0
}

// compareSiblings orders processes under one parent: in the order they
// started (those that do not say, last), then by pid.
func compareSiblings(a, b protocol.Process) int {
	if c := compareOpt(a.Started, b.Started, false); c != 0 {
		return c
	}
	return cmp.Compare(a.PID, b.PID)
}

// compareOpt orders two values, nil last either way.
func compareOpt[T cmp.Ordered](a, b *T, desc bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	c := cmp.Compare(*a, *b)
	if desc {
		c = -c
	}
	return c
}

// compareText orders two names without regard to case, "" last either way.
func compareText(a, b string, desc bool) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	c := strings.Compare(strings.ToLower(a), strings.ToLower(b))
	if c == 0 {
		c = strings.Compare(a, b)
	}
	if desc {
		c = -c
	}
	return c
}
