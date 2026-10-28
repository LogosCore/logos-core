package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func TestConnectionInputs(t *testing.T) {
	name, note := "edge", "EU front"
	got, err := connectionInputs(context.Background(), []*model.BuildConnectionInput{
		{ModuleName: "http", Name: &name, Description: &note, Settings: map[string]any{"base_url": "https://a.example.com"}, Values: map[string]any{"method": "GET"}},
		{ModuleName: "http"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := got[0]; c.ModuleName != "http" || c.Name != "edge" || c.Description != "EU front" ||
		c.Settings["base_url"] != "https://a.example.com" || c.Values["method"] != "GET" {
		t.Errorf("connection = %+v", c)
	}
	if c := got[1]; c.ModuleName != "http" || c.Name != "" || c.Description != "" || c.Settings != nil || c.Values != nil {
		t.Errorf("bare connection = %+v", c)
	}
	for _, in := range []*model.BuildConnectionInput{{ModuleName: "http", Settings: "x"}, {ModuleName: "http", Values: []any{}}} {
		if _, err := connectionInputs(context.Background(), []*model.BuildConnectionInput{in}); err == nil {
			t.Errorf("%+v was taken", in)
		}
	}
}

func TestToBuildModelConnectionDescriptions(t *testing.T) {
	b := &models.Build{Connections: models.BuildConnections{Items: make([]protocol.BuildConnection, 3)}}
	if d := toBuildModel(b).ConnectionDescriptions; len(d) != 3 || d[0] != "" {
		t.Errorf("without notes: %q", d)
	}
	b.Description = "a dry run"
	if d := toBuildModel(b).Description; d != "a dry run" {
		t.Errorf("description %q", d)
	}
	b.ConnectionDescriptions = []string{"EU front", "", "fallback"}
	if d := toBuildModel(b).ConnectionDescriptions; len(d) != 3 || d[0] != "EU front" || d[2] != "fallback" {
		t.Errorf("notes: %q", d)
	}
}

func TestToBuildModelStages(t *testing.T) {
	started := time.Date(2026, 10, 27, 9, 0, 10, 250_000_000, time.UTC)
	b := &models.Build{
		State: models.BuildStateBuilding, CreatedAt: started.Add(-10 * time.Second), StartedAt: &started,
		Plan:        []models.BuildPlanStep{{ID: "render", Label: "Render"}, {ID: "upload", Label: "Upload"}},
		StepReports: map[string]models.BuildStepReport{"render": {StartedAt: started, Message: "1 of 3"}},
	}
	st := toBuildModel(b).Stages
	if len(st) != 4 || st[0].ID != "core.queue" || !st[0].Core || st[1].ID != "render" || st[1].Core ||
		st[1].State != "RUNNING" || *st[1].Message != "1 of 3" || *st[1].StartedAt != "2026-10-27T09:00:10.25Z" ||
		st[1].FinishedAt != nil || st[2].Message != nil || st[3].State != "PENDING" {
		t.Errorf("stages %+v %+v", st[1], st[3])
	}
}

func TestToBuildSchemaModelCommands(t *testing.T) {
	s := &protocol.BuildSchema{
		Targets: []protocol.BuildTarget{{ID: "exe"}, {ID: "shellcode"}},
		Commands: []protocol.BuildCommand{
			{ID: "sleep", Required: true, Params: []protocol.FieldSpec{{Key: "interval", Type: protocol.FieldDuration, Label: "Interval"}}},
			{ID: "screenshot", Label: "Screenshot", Default: true, Targets: []string{"exe"}},
		},
	}
	cs := toBuildSchemaModel(s).Commands
	if len(cs) != 2 {
		t.Fatalf("commands %+v", cs)
	}
	// A command without targets is every target's; the list names them.
	if c := cs[0]; c.ID != "sleep" || c.Label != "sleep" || !c.Required || c.Default ||
		len(c.Targets) != 2 || c.Targets[1] != "shellcode" || len(c.Params) != 1 || c.Params[0].Key != "interval" {
		t.Errorf("sleep = %+v", c)
	}
	if c := cs[1]; c.Label != "Screenshot" || !c.Default || len(c.Targets) != 1 || c.Targets[0] != "exe" ||
		c.Params == nil {
		t.Errorf("screenshot = %+v", c)
	}
	if got := toBuildSchemaModel(&protocol.BuildSchema{}).Commands; got == nil {
		t.Error("no commands: nil, want an empty list")
	}
}

func TestToBuildModelCommands(t *testing.T) {
	if c := toBuildModel(&models.Build{}).Commands; c == nil || len(c) != 0 {
		t.Errorf("a build without commands: %#v, want an empty list", c)
	}
	if c := toBuildModel(&models.Build{Commands: []string{"sleep", "exit"}}).Commands; len(c) != 2 || c[1] != "exit" {
		t.Errorf("commands %v", c)
	}
}
