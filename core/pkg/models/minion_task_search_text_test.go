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
	// ConnectionParams are its params of the connection format, and
	// Connections the session's build's connections' names, in order.
	ConnectionParams []string `json:"connectionParams"`
	Connections      []string `json:"connections"`
	// JSON are its structured params.
	JSON []string `json:"json"`
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
		task := &MinionTask{Command: c.Command, LineRest: c.Rest, Args: c.Args, LineJSON: c.JSON}
		task.LineNames = MinionTaskLineNames(c.ConnectionParams, task.Args, c.Connections)
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

// A connection param's place as the resolver queues it (a Go int, or text
// of one), and one that is no whole number.
func TestMinionTaskLineNamesGoValues(t *testing.T) {
	names := []string{"ws", "https"}
	for _, c := range []struct {
		value any
		want  string
	}{{2, "https"}, {int64(1), "ws"}, {" 2 ", "https"}, {1.5, ""}, {"ws", ""}, {0, ""}, {true, ""}} {
		got := MinionTaskLineNames([]string{"conn"}, JSONMap{"conn": c.value}, names)["conn"]
		if got != c.want {
			t.Errorf("conn %#v: got %q, want %q", c.value, got, c.want)
		}
	}
	if got := MinionTaskLineNames([]string{"conn"}, JSONMap{"other": 1}, names); got != nil {
		t.Errorf("no conn: got %v, want nil", got)
	}
}
