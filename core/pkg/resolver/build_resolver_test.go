package resolver

import (
	"context"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func TestConnectionInputs(t *testing.T) {
	name := "edge"
	got, err := connectionInputs(context.Background(), []*model.BuildConnectionInput{
		{ModuleName: "http", Name: &name, Settings: map[string]any{"base_url": "https://a.example.com"}, Values: map[string]any{"method": "GET"}},
		{ModuleName: "http"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := got[0]; c.ModuleName != "http" || c.Name != "edge" || c.Settings["base_url"] != "https://a.example.com" || c.Values["method"] != "GET" {
		t.Errorf("connection = %+v", c)
	}
	if c := got[1]; c.ModuleName != "http" || c.Name != "" || c.Settings != nil || c.Values != nil {
		t.Errorf("bare connection = %+v", c)
	}
	for _, in := range []*model.BuildConnectionInput{{ModuleName: "http", Settings: "x"}, {ModuleName: "http", Values: []any{}}} {
		if _, err := connectionInputs(context.Background(), []*model.BuildConnectionInput{in}); err == nil {
			t.Errorf("%+v was taken", in)
		}
	}
}

func TestToPresetModelConnections(t *testing.T) {
	v := builds.PresetView{
		BuildPreset: models.BuildPreset{Connections: []models.BuildPresetConnection{
			{ModuleName: "http", Name: "edge", SchemaVersion: 2, Settings: models.JSONMap{"token": "s3cret"}, Values: models.JSONMap{"method": "GET"}},
			{ModuleName: "http", SchemaVersion: 2},
		}},
		Channels: []*protocol.ConnectionSchema{{Version: 3}, {Version: 2}},
	}
	m := toPresetModel(v)
	c := m.Connections[0]
	// Operation data: secret settings are shown like the rest.
	if c.ModuleName != "http" || *c.Name != "edge" || c.Settings.(map[string]any)["token"] != "s3cret" ||
		c.Values.(map[string]any)["method"] != "GET" || c.SchemaVersion != 2 || !c.SchemaChanged {
		t.Errorf("connection = %+v", c)
	}
	if c := m.Connections[1]; c.Name != nil || c.SchemaChanged || c.Settings == nil || c.Values == nil {
		t.Errorf("bare connection = %+v", c)
	}
}
