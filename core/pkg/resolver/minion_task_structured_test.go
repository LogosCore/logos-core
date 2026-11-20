package resolver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A structured param's value (docs: contracts/minion-build.md, Structured
// values), queued as the API sends it: checked, sent to the minion as its
// CBOR form, kept as JSON, and its task's line shows it as JSON.
func TestQueueStructured(t *testing.T) {
	g := newCompositeRig(t)
	cmd := protocol.BuildCommand{ID: "scan", Op: 5, Params: []protocol.FieldSpec{
		{Key: "targets", Num: 1, Type: protocol.FieldStructured, Required: true},
		{Key: "timeout", Num: 2, Type: protocol.FieldDuration},
	}}
	// The JSON scalar's numbers, as gqlgen decodes them.
	d := json.NewDecoder(strings.NewReader(`{"targets": {"hosts": [{"name": "a", "port": 22}], "weight": 0.5}}`))
	d.UseNumber()
	var values map[string]any
	if err := d.Decode(&values); err != nil {
		t.Fatal(err)
	}
	wire, problems := protocol.TaskArgs(cmd.Params, values)
	if problems != nil {
		t.Fatal(problems)
	}
	task, err := g.r.queue(context.Background(), g.session, cmd, values, wire, queueing{by: uuid.New(), files: g.r.taskFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.LineJSON) != 1 || task.LineJSON[0] != "targets" {
		t.Errorf("line json: %v", task.LineJSON)
	}
	if got, want := models.MinionTaskSearchText(task), `scan --targets='{"hosts":[{"name":"a","port":22}],"weight":0.5}'`; got != want {
		t.Errorf("line:\n got %s\nwant %s", got, want)
	}
	// What the minion reads.
	var args protocol.Args
	if err := protocol.UnmarshalCBOR(task.WireArgs, &args); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hosts []struct {
			Name string `cbor:"name"`
			Port int    `cbor:"port"`
		} `cbor:"hosts"`
		Weight float64 `cbor:"weight"`
	}
	if ok, err := protocol.ArgStructured(args, uint64(1), &got); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if len(got.Hosts) != 1 || got.Hosts[0].Name != "a" || got.Hosts[0].Port != 22 || got.Weight != 0.5 {
		t.Errorf("the minion reads %+v", got)
	}

	// A null inside is refused, where it is.
	_, problems = protocol.TaskArgs(cmd.Params, map[string]any{"targets": map[string]any{"hosts": []any{nil}}})
	if !strings.Contains(problems["targets"], "at .hosts[0]: null is not taken") {
		t.Errorf("problems: %v", problems)
	}
}
