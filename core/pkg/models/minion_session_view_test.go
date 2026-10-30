package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestMinionSessionFilterValidate(t *testing.T) {
	secs, zero, one := int64(60), int64(0), 1.0
	for name, c := range map[string]struct {
		f  MinionSessionFilter
		ok bool
	}{
		"text contains":       {MinionSessionFilter{Column: "host", Op: "CONTAINS", Text: "web"}, true},
		"contains needs text": {MinionSessionFilter{Column: "host", Op: "CONTAINS"}, false},
		"text takes no IN":    {MinionSessionFilter{Column: "host", Op: "IN", Values: []string{"a"}}, false},
		"enum in":             {MinionSessionFilter{Column: "os", Op: "IN", Values: []string{"linux"}}, true},
		"in needs values":     {MinionSessionFilter{Column: "os", Op: "IN"}, false},
		"bool true":           {MinionSessionFilter{Column: "staged", Op: "IN", Values: []string{"true"}}, true},
		"bool yes":            {MinionSessionFilter{Column: "staged", Op: "IN", Values: []string{"yes"}}, false},
		"number range":        {MinionSessionFilter{Column: "pid", Op: "RANGE", Min: &one}, true},
		"empty range":         {MinionSessionFilter{Column: "pid", Op: "RANGE"}, false},
		"within":              {MinionSessionFilter{Column: "lastSeen", Op: "WITHIN", Seconds: &secs}, true},
		"within 0":            {MinionSessionFilter{Column: "lastSeen", Op: "WITHIN", Seconds: &zero}, false},
		"never":               {MinionSessionFilter{Column: "lastSeen", Op: "NEVER"}, true},
		"id equals":           {MinionSessionFilter{Column: "buildId", Op: "EQUALS", Text: uuid.NewString()}, true},
		"id equals not an id": {MinionSessionFilter{Column: "buildId", Op: "EQUALS", Text: "abc"}, false},
		"unknown column":      {MinionSessionFilter{Column: "nope", Op: "EMPTY"}, false},
	} {
		if err := c.f.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMinionSessionsLayoutValidate(t *testing.T) {
	ok := MinionSessionsLayout{Columns: []string{"number", "host"}, Sort: &MinionSessionSort{Column: "lastSeen", Desc: true}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, l := range map[string]MinionSessionsLayout{
		"no columns":      {},
		"twice":           {Columns: []string{"host", "host"}},
		"unknown":         {Columns: []string{"nope"}},
		"unsortable sort": {Columns: []string{"host"}, Sort: &MinionSessionSort{Column: "addrs"}},
		"bad filter":      {Columns: []string{"host"}, Filters: []MinionSessionFilter{{Column: "host", Op: "NEVER"}}},
	} {
		if l.Validate() == nil {
			t.Errorf("%s passed", name)
		}
	}
}
