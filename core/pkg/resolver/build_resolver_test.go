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
