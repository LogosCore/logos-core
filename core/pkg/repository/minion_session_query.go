package repository

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The Sessions table's queries: its filters, its sort by any sortable column,
// and the keyset cursor for that sort.

// MinionSessionQuery is what an operator asks of an operation's sessions: the
// app's search, column filters (all of them hold) and the order.
type MinionSessionQuery struct {
	Search  string
	Filters []models.MinionSessionFilter
	Sort    models.MinionSessionSort
}

// Validate checks the filters and the sort.
func (q MinionSessionQuery) Validate() error {
	if len(q.Filters) > models.MaxSessionFilters {
		return fmt.Errorf("at most %d filters", models.MaxSessionFilters)
	}
	for _, f := range q.Filters {
		if err := f.Validate(); err != nil {
			return err
		}
	}
	return q.Sort.Validate()
}

// minionSessionFilter is the Mongo filter of q at now: the operation, the
// search and each column filter.
func minionSessionFilter(operationID uuid.UUID, q MinionSessionQuery, now time.Time) bson.M {
	and := bson.A{minionSessionListFilter(operationID, q.Search), openSessions(now)}
	for _, f := range q.Filters {
		and = append(and, columnFilter(f, now))
	}
	return bson.M{"$and": and}
}

// openSessions keeps out closed ones (models.MinionSession.Closed): the
// tombstones, and those that expired waiting for their first check-in.
func openSessions(now time.Time) bson.M {
	return bson.M{"closed_at": nil, "$or": bson.A{
		bson.M{"last_seen_at": bson.M{"$ne": nil}},
		bson.M{"created_at": bson.M{"$gt": now.Add(-models.MinionSessionCheckInWindow)}},
	}}
}

// columnFilter is one validated filter as a Mongo condition. A missing value
// (an unsent hello field, no check-in yet) is null to Mongo: it is empty, it
// contains nothing, and NOT_IN / NOT_CONTAINS keep it.
func columnFilter(f models.MinionSessionFilter, now time.Time) bson.M {
	col := models.MinionSessionColumns[f.Column]
	field := col.Field
	empty := bson.M{"$in": bson.A{nil, "", bson.A{}}}
	switch f.Op {
	case models.SessionFilterContains:
		rx := bson.M{"$regex": regexp.QuoteMeta(f.Text), "$options": "i"}
		if col.AlsoMatch != "" {
			return bson.M{"$or": bson.A{bson.M{field: rx}, bson.M{col.AlsoMatch: rx}}}
		}
		return bson.M{field: rx}
	case models.SessionFilterNotContains:
		not := bson.M{"$not": bson.M{"$regex": regexp.QuoteMeta(f.Text), "$options": "i"}}
		if col.AlsoMatch != "" {
			return bson.M{field: not, col.AlsoMatch: not}
		}
		return bson.M{field: not}
	case models.SessionFilterEmpty:
		return bson.M{field: empty}
	case models.SessionFilterNotEmpty:
		return bson.M{field: bson.M{"$nin": bson.A{nil, "", bson.A{}}}}
	case models.SessionFilterEquals:
		return bson.M{field: uuid.MustParse(f.Text)}
	case models.SessionFilterIn, models.SessionFilterNotIn:
		values := bson.A{}
		for _, v := range f.Values {
			if col.Kind == models.SessionColumnBool {
				values = append(values, v == "true")
				// A flag stored only when set is false when missing.
				if v == "false" {
					values = append(values, nil)
				}
			} else {
				values = append(values, v)
			}
		}
		if f.Op == models.SessionFilterIn {
			return bson.M{field: bson.M{"$in": values}}
		}
		return bson.M{field: bson.M{"$nin": values}}
	case models.SessionFilterRange:
		cond := bson.M{}
		if col.Kind == models.SessionColumnTime {
			if f.From != nil {
				cond["$gte"] = *f.From
			}
			if f.To != nil {
				cond["$lte"] = *f.To
			}
		} else {
			if f.Min != nil {
				cond["$gte"] = *f.Min
			}
			if f.Max != nil {
				cond["$lte"] = *f.Max
			}
			// A count stored only when set is 0 when missing.
			if f.Column == "restarts" && (f.Min == nil || *f.Min <= 0) && (f.Max == nil || *f.Max >= 0) {
				return bson.M{"$or": bson.A{bson.M{field: cond}, bson.M{field: nil}}}
			}
		}
		return bson.M{field: cond}
	case models.SessionFilterWithin:
		return bson.M{field: bson.M{"$gte": now.Add(-time.Duration(*f.Seconds) * time.Second)}}
	case models.SessionFilterOlderThan:
		return bson.M{field: bson.M{"$lt": now.Add(-time.Duration(*f.Seconds) * time.Second)}}
	case models.SessionFilterNever:
		return bson.M{field: nil}
	case models.SessionFilterLive:
		return bson.M{"late_at": bson.M{"$gt": now}}
	case models.SessionFilterLate:
		return bson.M{"late_at": bson.M{"$lte": now}}
	}
	// Validate refuses anything else.
	return bson.M{"_id": nil}
}

// sessionSortValue is a session's value in a sort column, as Mongo compares
// it: nil for a missing one, which sorts before every value.
func sessionSortValue(s *models.MinionSession, column string) any {
	str := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	h := s.Hello
	switch column {
	case "number":
		return s.Number
	case "addrs":
		return str(s.Address)
	case "peer":
		return str(s.PeerAddress)
	case "host":
		return str(h.Host)
	case "user":
		return str(h.User)
	case "domain":
		return str(h.Domain)
	case "proc":
		return str(h.Proc)
	case "pid":
		return int64(h.PID) //nolint:gosec // a pid fits
	case "os":
		return str(h.OS)
	case "arch":
		return str(h.Arch)
	case "version":
		return str(h.Version)
	case "lastSeen":
		if s.LastSeenAt == nil {
			return nil
		}
		return *s.LastSeenAt
	case "opened":
		return s.CreatedAt
	case "build":
		return s.ModuleName
	case "target":
		return s.Target
	case "platform":
		return s.Platform
	case "buildDescription":
		return str(s.BuildDescription)
	case "staged":
		return s.Staged
	// Stored only when set: missing (nil) otherwise, as Mongo sorts them.
	case "persisted":
		if !s.Persisted {
			return nil
		}
		return true
	case "restarts":
		if s.RestartCount == 0 {
			return nil
		}
		return s.RestartCount
	}
	return nil
}

// minionSessionCursor is a position in the list under one sort: the sort it
// was made for, the sort value and the _id. Value is typed by T: "s" string,
// "n" number, "t" time (RFC 3339 with nanoseconds), "b" bool, "z" none.
type minionSessionCursor struct {
	Column string             `json:"c"`
	Desc   bool               `json:"d"`
	T      string             `json:"t"`
	V      string             `json:"v,omitempty"`
	ID     primitive.ObjectID `json:"i"`
}

// MinionSessionCursor is a session's cursor in the list sorted by sort.
func MinionSessionCursor(s *models.MinionSession, sort models.MinionSessionSort) string {
	c := minionSessionCursor{Column: sort.Column, Desc: sort.Desc, ID: s.Id, T: "z"}
	switch v := sessionSortValue(s, sort.Column).(type) {
	case string:
		c.T, c.V = "s", v
	case int64:
		c.T, c.V = "n", strconv.FormatInt(v, 10)
	case time.Time:
		c.T, c.V = "t", v.UTC().Format(time.RFC3339Nano)
	case bool:
		c.T, c.V = "b", strconv.FormatBool(v)
	}
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// errSessionCursor is a cursor that does not read, or was made for another
// sort: the client restarts from the first page when the sort changes.
var errSessionCursor = errors.New("the cursor does not match the requested sort")

// MinionSessionKeyset is a decoded cursor: where a page starts.
type MinionSessionKeyset struct {
	value any
	id    primitive.ObjectID
}

// DecodeMinionSessionCursor reads a cursor for sort.
func DecodeMinionSessionCursor(s string, sort models.MinionSessionSort) (*MinionSessionKeyset, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errSessionCursor
	}
	var c minionSessionCursor
	if json.Unmarshal(data, &c) != nil || c.Column != sort.Column || c.Desc != sort.Desc {
		return nil, errSessionCursor
	}
	k := &MinionSessionKeyset{id: c.ID}
	switch c.T {
	case "s":
		k.value = c.V
	case "n":
		if k.value, err = strconv.ParseInt(c.V, 10, 64); err != nil {
			return nil, errSessionCursor
		}
	case "t":
		if k.value, err = time.Parse(time.RFC3339Nano, c.V); err != nil {
			return nil, errSessionCursor
		}
	case "b":
		k.value = c.V == "true"
	case "z":
	default:
		return nil, errSessionCursor
	}
	return k, nil
}

// keysetFilter selects what comes after k going ascending or not. Mongo puts
// null (a missing value) before every value, and a $gt / $lt on a value never
// matches null, so null is handled on its own:
//
//	ascending, after v:     > v, or = v with a later _id
//	ascending, after null:  any value, or null with a later _id
//	descending, after v:    < v, or null, or = v with an earlier _id
//	descending, after null: null with an earlier _id
func keysetFilter(field string, k *MinionSessionKeyset, ascending bool) bson.M {
	op := "$lt"
	if ascending {
		op = "$gt"
	}
	sameValueLaterID := bson.M{field: k.value, "_id": bson.M{op: k.id}}
	switch {
	case k.value == nil && ascending:
		return bson.M{"$or": bson.A{bson.M{field: bson.M{"$ne": nil}}, sameValueLaterID}}
	case k.value == nil:
		return sameValueLaterID
	case ascending:
		return bson.M{"$or": bson.A{bson.M{field: bson.M{"$gt": k.value}}, sameValueLaterID}}
	default:
		return bson.M{"$or": bson.A{bson.M{field: bson.M{"$lt": k.value}}, bson.M{field: nil}, sameValueLaterID}}
	}
}

// sortFields are the Mongo sort for sort, traversed forward or back.
func sortFields(sort models.MinionSessionSort, forward bool) []string {
	field := models.MinionSessionColumns[sort.Column].SortBy()
	ascending := !sort.Desc == forward
	if ascending {
		return []string{field, "_id"}
	}
	return []string{"-" + field, "-_id"}
}

// ParseSessionSearchNumber reads "12" or "#12" as a session number.
func ParseSessionSearchNumber(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	return n, err == nil && n > 0
}
