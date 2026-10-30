package models

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// How operators look at minion sessions: the columns the Sessions table can
// show, filter and sort by, and the layout each operator keeps (one for all
// operations, on their user document).

// MinionSessionColumnKind is what a column holds, which decides its filters.
type MinionSessionColumnKind int

const (
	// SessionColumnText: free text. CONTAINS, NOT_CONTAINS, EMPTY, NOT_EMPTY.
	SessionColumnText MinionSessionColumnKind = iota
	// SessionColumnList: a list of text (addresses). The same filters as text,
	// against any element.
	SessionColumnList
	// SessionColumnEnum: a few distinct values. IN, NOT_IN.
	SessionColumnEnum
	// SessionColumnBool: IN, NOT_IN with "true" / "false".
	SessionColumnBool
	// SessionColumnNumber: RANGE (min, max, either open).
	SessionColumnNumber
	// SessionColumnTime: WITHIN / OLDER_THAN seconds of now, NEVER (no
	// value), RANGE (from, to).
	SessionColumnTime
	// SessionColumnID: a UUID. EQUALS.
	SessionColumnID
)

// MinionSessionColumn is one column: the stored field it reads, its kind,
// and whether the list sorts by it.
type MinionSessionColumn struct {
	Field    string
	Kind     MinionSessionColumnKind
	Sortable bool
}

// MinionSessionColumns are the Sessions table's columns by id: every property
// of a session an operator may want to see. The SPA keeps the same ids, with
// how each renders.
var MinionSessionColumns = map[string]MinionSessionColumn{
	"number":           {Field: "number", Kind: SessionColumnNumber, Sortable: true},
	"sessionId":        {Field: "session_id", Kind: SessionColumnID},
	"addrs":            {Field: "hello.addrs", Kind: SessionColumnList},
	"host":             {Field: "hello.host", Kind: SessionColumnText, Sortable: true},
	"user":             {Field: "hello.user", Kind: SessionColumnText, Sortable: true},
	"domain":           {Field: "hello.domain", Kind: SessionColumnText, Sortable: true},
	"integrity":        {Field: "hello.integrity", Kind: SessionColumnEnum, Sortable: true},
	"proc":             {Field: "hello.proc", Kind: SessionColumnText, Sortable: true},
	"pid":              {Field: "hello.pid", Kind: SessionColumnNumber, Sortable: true},
	"os":               {Field: "hello.os", Kind: SessionColumnEnum, Sortable: true},
	"arch":             {Field: "hello.arch", Kind: SessionColumnEnum, Sortable: true},
	"version":          {Field: "hello.version", Kind: SessionColumnEnum, Sortable: true},
	"lastSeen":         {Field: "last_seen_at", Kind: SessionColumnTime, Sortable: true},
	"opened":           {Field: "created_at", Kind: SessionColumnTime, Sortable: true},
	"build":            {Field: "module_name", Kind: SessionColumnEnum, Sortable: true},
	"target":           {Field: "target", Kind: SessionColumnEnum, Sortable: true},
	"platform":         {Field: "platform", Kind: SessionColumnEnum, Sortable: true},
	"buildDescription": {Field: "build_description", Kind: SessionColumnText, Sortable: true},
	"buildId":          {Field: "build_id", Kind: SessionColumnID},
	"staged":           {Field: "staged", Kind: SessionColumnBool, Sortable: true},
}

// Filter operations (GraphQL MinionSessionFilterOp).
const (
	SessionFilterContains    = "CONTAINS"
	SessionFilterNotContains = "NOT_CONTAINS"
	SessionFilterEmpty       = "EMPTY"
	SessionFilterNotEmpty    = "NOT_EMPTY"
	SessionFilterIn          = "IN"
	SessionFilterNotIn       = "NOT_IN"
	SessionFilterRange       = "RANGE"
	SessionFilterWithin      = "WITHIN"
	SessionFilterOlderThan   = "OLDER_THAN"
	SessionFilterNever       = "NEVER"
	SessionFilterEquals      = "EQUALS"
)

// sessionFilterOps are the operations each kind takes.
var sessionFilterOps = map[MinionSessionColumnKind][]string{
	SessionColumnText:   {SessionFilterContains, SessionFilterNotContains, SessionFilterEmpty, SessionFilterNotEmpty},
	SessionColumnList:   {SessionFilterContains, SessionFilterNotContains, SessionFilterEmpty, SessionFilterNotEmpty},
	SessionColumnEnum:   {SessionFilterIn, SessionFilterNotIn},
	SessionColumnBool:   {SessionFilterIn, SessionFilterNotIn},
	SessionColumnNumber: {SessionFilterRange},
	SessionColumnTime:   {SessionFilterWithin, SessionFilterOlderThan, SessionFilterNever, SessionFilterRange},
	SessionColumnID:     {SessionFilterEquals},
}

// Limits on what a list query or a saved layout may carry.
const (
	MaxSessionFilters      = 32
	MaxSessionFilterText   = 256
	MaxSessionFilterValues = 64
)

// MinionSessionFilter narrows the sessions list on one column. Which fields
// count depends on Op: Text for CONTAINS / NOT_CONTAINS / EQUALS, Values for
// IN / NOT_IN, Min and Max for a number RANGE, From and To for a time RANGE,
// Seconds for WITHIN / OLDER_THAN.
type MinionSessionFilter struct {
	Column  string     `bson:"column" json:"column"`
	Op      string     `bson:"op" json:"op"`
	Text    string     `bson:"text,omitempty" json:"text,omitempty"`
	Values  []string   `bson:"values,omitempty" json:"values,omitempty"`
	Min     *float64   `bson:"min,omitempty" json:"min,omitempty"`
	Max     *float64   `bson:"max,omitempty" json:"max,omitempty"`
	Seconds *int64     `bson:"seconds,omitempty" json:"seconds,omitempty"`
	From    *time.Time `bson:"from,omitempty" json:"from,omitempty"`
	To      *time.Time `bson:"to,omitempty" json:"to,omitempty"`
}

// Validate checks the column and that the operation fits it with what it
// needs.
func (f MinionSessionFilter) Validate() error {
	col, ok := MinionSessionColumns[f.Column]
	if !ok {
		return fmt.Errorf("filter: no column %q", f.Column)
	}
	if !slices.Contains(sessionFilterOps[col.Kind], f.Op) {
		return fmt.Errorf("filter on %s: %s does not apply (%v do)", f.Column, f.Op, sessionFilterOps[col.Kind])
	}
	if len(f.Text) > MaxSessionFilterText {
		return fmt.Errorf("filter on %s: text is at most %d bytes", f.Column, MaxSessionFilterText)
	}
	switch f.Op {
	case SessionFilterContains, SessionFilterNotContains:
		if f.Text == "" {
			return fmt.Errorf("filter on %s: %s needs text", f.Column, f.Op)
		}
	case SessionFilterEquals:
		if _, err := uuid.Parse(f.Text); err != nil {
			return fmt.Errorf("filter on %s: %q is not a UUID", f.Column, f.Text)
		}
	case SessionFilterIn, SessionFilterNotIn:
		if len(f.Values) == 0 || len(f.Values) > MaxSessionFilterValues {
			return fmt.Errorf("filter on %s: %s takes 1 to %d values", f.Column, f.Op, MaxSessionFilterValues)
		}
		if col.Kind == SessionColumnBool {
			for _, v := range f.Values {
				if v != "true" && v != "false" {
					return fmt.Errorf("filter on %s: %q is not true or false", f.Column, v)
				}
			}
		}
	case SessionFilterRange:
		if col.Kind == SessionColumnNumber && f.Min == nil && f.Max == nil {
			return fmt.Errorf("filter on %s: a range needs min or max", f.Column)
		}
		if col.Kind == SessionColumnTime && f.From == nil && f.To == nil {
			return fmt.Errorf("filter on %s: a range needs from or to", f.Column)
		}
	case SessionFilterWithin, SessionFilterOlderThan:
		if f.Seconds == nil || *f.Seconds <= 0 {
			return fmt.Errorf("filter on %s: %s needs seconds", f.Column, f.Op)
		}
	}
	return nil
}

// MinionSessionSort orders the sessions list by one sortable column; the
// session's _id breaks ties.
type MinionSessionSort struct {
	Column string `bson:"column" json:"column"`
	Desc   bool   `bson:"desc" json:"desc"`
}

// DefaultMinionSessionSort is newest first.
var DefaultMinionSessionSort = MinionSessionSort{Column: "opened", Desc: true}

// Validate checks the column sorts.
func (s MinionSessionSort) Validate() error {
	col, ok := MinionSessionColumns[s.Column]
	if !ok {
		return fmt.Errorf("sort: no column %q", s.Column)
	}
	if !col.Sortable {
		return fmt.Errorf("sort: %s does not sort", s.Column)
	}
	return nil
}

// MinionSessionsLayout is how an operator keeps the Sessions table: its
// columns in order, the sort and the filters. One for all operations.
type MinionSessionsLayout struct {
	Columns []string              `bson:"columns" json:"columns"`
	Sort    *MinionSessionSort    `bson:"sort,omitempty" json:"sort,omitempty"`
	Filters []MinionSessionFilter `bson:"filters,omitempty" json:"filters,omitempty"`
}

// Validate checks every column once, at least one, the sort and the filters.
func (l MinionSessionsLayout) Validate() error {
	if len(l.Columns) == 0 {
		return fmt.Errorf("layout: at least one column")
	}
	seen := map[string]bool{}
	for _, c := range l.Columns {
		if _, ok := MinionSessionColumns[c]; !ok {
			return fmt.Errorf("layout: no column %q", c)
		}
		if seen[c] {
			return fmt.Errorf("layout: column %q twice", c)
		}
		seen[c] = true
	}
	if l.Sort != nil {
		if err := l.Sort.Validate(); err != nil {
			return err
		}
	}
	if len(l.Filters) > MaxSessionFilters {
		return fmt.Errorf("layout: at most %d filters", MaxSessionFilters)
	}
	for _, f := range l.Filters {
		if err := f.Validate(); err != nil {
			return err
		}
	}
	return nil
}
