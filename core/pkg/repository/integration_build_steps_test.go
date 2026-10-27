package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// TestIntegrationBuildStepReports pins how a build's plan and step reports
// are stored: a step keeps the time of its first report whatever order the
// reports arrive in, its message is the latest (an empty one clears it), only
// the instance building it can report, and a build put back in the queue
// forgets them.
func TestIntegrationBuildStepReports(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewBuildRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	b := &models.Build{MinionID: uuid.New(), OperationID: uuid.New(), ModuleName: "bob",
		State: models.BuildStateQueued, CreatedAt: now}
	if err := repo.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.MarkBuilding(ctx, b.MinionID, "bob-1", "dev", now.Add(time.Hour), now); !ok || err != nil {
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
		if ok, err := repo.ReportStep(ctx, b.MinionID, "bob-1", r.step, r.message, r.at); !ok || err != nil {
			t.Fatalf("report %s: %v %v", r.step, ok, err)
		}
	}
	plan := []models.BuildPlanStep{{ID: "render", Label: "Render"}, {ID: "upload", Label: "Upload"}}
	if ok, err := repo.SetPlan(ctx, b.MinionID, "bob-1", plan); !ok || err != nil {
		t.Fatalf("set plan: %v %v", ok, err)
	}
	if ok, _ := repo.ReportStep(ctx, b.MinionID, "bob-2", "render", "not mine", now); ok {
		t.Error("another instance reported")
	}

	got, err := repo.FindByMinionID(ctx, b.MinionID)
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

	// Not taken after all: back in the queue, with nothing of that instance.
	if ok, err := repo.Unoffer(ctx, b.MinionID, "bob-1", now.Add(time.Minute)); !ok || err != nil {
		t.Fatalf("unoffer: %v %v", ok, err)
	}
	if got, _ := repo.FindByMinionID(ctx, b.MinionID); got.Plan != nil || got.StepReports != nil || got.StartedAt != nil {
		t.Errorf("after unoffer: plan %+v, reports %+v", got.Plan, got.StepReports)
	}
}
