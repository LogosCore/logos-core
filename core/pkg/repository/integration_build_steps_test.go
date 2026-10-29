package repository

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
)

// TestIntegrationBuildStepReports pins how a build's plan and step reports
// are stored: a step keeps the time of its first report whatever order the
// reports arrive in, its message is the latest (an empty one clears it), only
// the instance building it can report, and a build put back in the queue
// forgets them and has its queue deadline again.
func TestIntegrationBuildStepReports(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewBuildRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	b := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(), ModuleName: "bob",
		State: models.BuildStateQueued, Deadline: now.Add(30 * time.Minute), CreatedAt: now}
	if err := repo.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.MarkBuilding(ctx, b.BuildID, "bob-1", "dev", now.Add(time.Hour), 1<<20, now); !ok || err != nil {
		t.Fatalf("mark building: %v %v", ok, err)
	}

	// A report may come before the plan.
	second, first := now.Add(2*time.Second), now.Add(time.Second)
	for _, r := range []struct {
		step, message string
		at            time.Time
	}{
		{"render", "2 of 3", second},
		{"render", "1 of 3", first}, // arrived late: its time is the earlier
		{"upload", "uploading", second},
		{"upload", "", second.Add(time.Second)}, // clears the message
	} {
		if ok, err := repo.ReportStep(ctx, b.BuildID, "bob-1", r.step, r.message, r.at); !ok || err != nil {
			t.Fatalf("report %s: %v %v", r.step, ok, err)
		}
	}
	plan := []models.BuildPlanStep{{ID: "render", Label: "Render"}, {ID: "upload", Label: "Upload"}}
	if ok, err := repo.SetPlan(ctx, b.BuildID, "bob-1", plan); !ok || err != nil {
		t.Fatalf("set plan: %v %v", ok, err)
	}
	if ok, _ := repo.ReportStep(ctx, b.BuildID, "bob-2", "render", "not mine", now); ok {
		t.Error("another instance reported")
	}

	got, err := repo.FindByBuildID(ctx, b.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.StepReports["render"]; !r.StartedAt.Equal(first) || r.Message != "1 of 3" {
		t.Errorf("render: %+v, want the first time and the latest message", r)
	}
	if r := got.StepReports["upload"]; !r.StartedAt.Equal(second) || r.Message != "" {
		t.Errorf("upload: %+v", r)
	}
	if len(got.Plan) != 2 || got.Plan[1] != plan[1] {
		t.Errorf("plan %+v", got.Plan)
	}
	if !got.Deadline.Equal(now.Add(time.Hour)) || got.ArtifactMaxSize != 1<<20 {
		t.Errorf("building: deadline %s, artifact limit %d", got.Deadline, got.ArtifactMaxSize)
	}

	// Not taken after all: back in the queue, with nothing of that instance.
	if ok, err := repo.Unoffer(ctx, b.BuildID, "bob-1", b.Deadline, now.Add(time.Minute)); !ok || err != nil {
		t.Fatalf("unoffer: %v %v", ok, err)
	}
	if got, _ := repo.FindByBuildID(ctx, b.BuildID); got.Plan != nil || got.StepReports != nil || got.StartedAt != nil ||
		!got.Deadline.Equal(b.Deadline) || got.ArtifactMaxSize != 0 {
		t.Errorf("after unoffer: plan %+v, reports %+v, deadline %s (queued with %s), artifact limit %d",
			got.Plan, got.StepReports, got.Deadline, b.Deadline, got.ArtifactMaxSize)
	}
}

// TestIntegrationBuildsList pins the Builds page's query: newest first, a
// page at a time; the app's search over the build's fields, its plan, its
// connections' notes and its search text (build id, values, connection
// settings), but not the keys of those; state and factory filters; and the
// backfill that gives older builds their search text.
func TestIntegrationBuildsList(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewBuildRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	op, now := uuid.New(), time.Now().UTC().Truncate(time.Millisecond)
	conn := func(baseURL string) models.BuildConnections {
		return models.BuildConnections{Items: []protocol.BuildConnection{{
			Endpoint: protocol.ChannelEndpointExport{Format: protocol.ChannelEndpointExportV1, Name: "edge",
				Channel: protocol.ExportChannel{ModuleName: "http", SchemaVersion: 2}, Params: map[string]any{"base_url": baseURL}},
			Values: map[string]any{"path_prefix": "/cdn/"},
		}}}
	}
	eu := &models.Build{BuildID: uuid.New(), OperationID: op, ModuleName: "bob", Target: "dummy", Platform: "any/any",
		Description: "Dry run for the EU front", State: models.BuildStateFailed, Instance: "bob-local",
		Values: models.JSONMap{"greeting": "hello", "punches": []any{"jab", "hook"}}, Connections: conn("https://edge-eu.example.com"),
		ConnectionDescriptions: []string{"EU CDN front"}, Plan: []models.BuildPlanStep{{ID: "punches", Label: "Take punches"}},
		Error: &models.BuildError{Code: "knocked_out", Message: "BOB is set to fail"}, CreatedAt: now}
	us := &models.Build{BuildID: uuid.New(), OperationID: op, ModuleName: "bob", Target: "dummy2", Platform: "windows/amd64",
		Description: "Second wave", State: models.BuildStateSucceeded, Connections: conn("https://cdn.other.net"),
		Artifact: &models.BuildArtifact{Filename: "minion.exe"}, CreatedAt: now.Add(time.Minute)}
	tank := &models.Build{BuildID: uuid.New(), OperationID: op, ModuleName: "tank", Target: "dummy", Platform: "linux/amd64",
		Description: "Third", State: models.BuildStateQueued, Connections: conn("https://tank.example.org"), CreatedAt: now.Add(2 * time.Minute)}
	elsewhere := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(), ModuleName: "bob", Description: "Dry run elsewhere", CreatedAt: now}
	for _, b := range []*models.Build{eu, us, tank, elsewhere} {
		if err := repo.Insert(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	list := func(f BuildFilter) []string {
		t.Helper()
		bs, err := repo.FindByOperationIDWithCursor(ctx, op, f, nil, 10, true)
		if err != nil {
			t.Fatal(err)
		}
		n, err := repo.CountByOperationID(ctx, op, f)
		if err != nil || n != int64(len(bs)) {
			t.Fatalf("count %d %v, listed %d", n, err, len(bs))
		}
		out := make([]string, len(bs))
		for i, b := range bs {
			out[i] = b.Description
		}
		return out
	}
	for name, c := range map[string]struct {
		filter BuildFilter
		want   []string
	}{
		"all, newest first":        {BuildFilter{}, []string{"Third", "Second wave", "Dry run for the EU front"}},
		"description, case aside":  {BuildFilter{Search: "dry RUN"}, []string{"Dry run for the EU front"}},
		"a connection's setting":   {BuildFilter{Search: "edge-eu.example.com"}, []string{"Dry run for the EU front"}},
		"a value in a list":        {BuildFilter{Search: "hook"}, []string{"Dry run for the EU front"}},
		"a connection's note":      {BuildFilter{Search: "cdn front"}, []string{"Dry run for the EU front"}},
		"a plan step":              {BuildFilter{Search: "take punches"}, []string{"Dry run for the EU front"}},
		"the error":                {BuildFilter{Search: "knocked_out"}, []string{"Dry run for the EU front"}},
		"the artifact":             {BuildFilter{Search: "minion.exe"}, []string{"Second wave"}},
		"the factory instance":     {BuildFilter{Search: "bob-local"}, []string{"Dry run for the EU front"}},
		"the platform":             {BuildFilter{Search: "windows"}, []string{"Second wave"}},
		"the build id":             {BuildFilter{Search: us.BuildID.String()[:13]}, []string{"Second wave"}},
		"a setting's key: nothing": {BuildFilter{Search: "base_url"}, []string{}},
		"a quoted whole word":      {BuildFilter{Search: `"dummy"`}, []string{"Third", "Dry run for the EU front"}},
		"by state":                 {BuildFilter{States: []string{models.BuildStateFailed, models.BuildStateQueued}}, []string{"Third", "Dry run for the EU front"}},
		"by factory":               {BuildFilter{ModuleNames: []string{"bob"}}, []string{"Second wave", "Dry run for the EU front"}},
		"search and filters":       {BuildFilter{Search: "example", ModuleNames: []string{"bob"}}, []string{"Dry run for the EU front"}},
	} {
		if got := list(c.filter); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}

	// A page at a time: the next page starts past the last build's cursor.
	page, err := repo.FindByOperationIDWithCursor(ctx, op, BuildFilter{}, nil, 2, true)
	if err != nil || len(page) != 2 {
		t.Fatalf("first page %d: %v", len(page), err)
	}
	cur, err := pagination.DecodeCursor(BuildCursor(&page[1]))
	if err != nil {
		t.Fatal(err)
	}
	next, err := repo.FindByOperationIDWithCursor(ctx, op, BuildFilter{}, &cur, 2, true)
	if err != nil || len(next) != 1 || next[0].BuildID != eu.BuildID {
		t.Fatalf("next page %+v: %v", next, err)
	}

	// A build stored before builds had a search text is given one.
	raw := db.Collection(buildCollection)
	if err := raw.UpdateOne(ctx, bson.M{"build_id": tank.BuildID}, bson.M{"$unset": bson.M{"search_text": ""}}); err != nil {
		t.Fatal(err)
	}
	if got := list(BuildFilter{Search: "tank.example.org"}); len(got) != 0 {
		t.Fatalf("found without a search text: %q", got)
	}
	if n, err := repo.BackfillSearchText(ctx); n != 1 || err != nil {
		t.Fatalf("backfill: %d %v", n, err)
	}
	if got := list(BuildFilter{Search: "tank.example.org"}); !slices.Equal(got, []string{"Third"}) {
		t.Fatalf("after the backfill: %q", got)
	}
}
