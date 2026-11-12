package models

import (
	"encoding/json"
	"os"
	"testing"
)

// taskLine is a case of testdata/minion-task-lines.json, which the
// console's lineOf is tested against too (frontend
// minion-sessions/task-line.test.ts): the search text is the line it shows.
type taskLine struct {
	Why     string         `json:"why"`
	Command string         `json:"command"`
	Rest    string         `json:"rest"`
	Args    map[string]any `json:"args"`
	Line    string         `json:"line"`
}

func TestMinionTaskSearchText(t *testing.T) {
	raw, err := os.ReadFile("testdata/minion-task-lines.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []taskLine
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		task := &MinionTask{Command: c.Command, LineRest: c.Rest, Args: c.Args}
		if got := MinionTaskSearchText(task); got != c.Line {
			t.Errorf("%s:\n got %s\nwant %s", c.Why, got, c.Line)
		}
	}
}

// A task's args as the resolver queues them (Go ints, []string) read as
// they do once stored (JSON's numbers and lists).
func TestMinionTaskSearchTextGoValues(t *testing.T) {
	task := &MinionTask{Command: "tunnel", Args: JSONMap{"port": 8080, "routes": []string{"10.0.0.0/8"}, "max": int64(3)}}
	if got, want := MinionTaskSearchText(task), "tunnel --max=3 --port=8080 --routes=10.0.0.0/8"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
