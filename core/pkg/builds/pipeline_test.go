package builds

import (
	"strings"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
)

var (
	t0 = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	at = func(s int) *time.Time { t := t0.Add(time.Duration(s) * time.Second); return &t }
)

// plannedBuild is a build bob-1 took at 10s with the badge, render, upload
// plan; mutate takes it further.
func plannedBuild(mutate func(b *models.Build)) *models.Build {
	b := &models.Build{
		State: models.BuildStateBuilding, CreatedAt: t0, StartedAt: at(10),
		Plan: []models.BuildPlanStep{{ID: "badge", Label: "Check the badge"}, {ID: "render", Label: "Describe"}, {ID: "upload", Label: "Upload"}},
	}
	if mutate != nil {
		mutate(b)
	}
	return b
}

func reports(r map[string]int) map[string]models.BuildStepReport {
	out := map[string]models.BuildStepReport{}
	for id, s := range r {
		out[id] = models.BuildStepReport{StartedAt: *at(s), Message: "on " + id}
	}
	return out
}

// states renders a pipeline as "id:STATE …", core's stages by short name.
func states(stages []Stage) string {
	parts := make([]string, len(stages))
	for i, st := range stages {
		parts[i] = strings.TrimPrefix(st.ID, "core.") + ":" + st.State
	}
	return strings.Join(parts, " ")
}

func TestPipelineStates(t *testing.T) {
	for name, c := range map[string]struct {
		build *models.Build
		want  string
	}{
		"queued": {&models.Build{State: models.BuildStateQueued, CreatedAt: t0},
			"queue:RUNNING build:PENDING check:PENDING"},
		"taken, nothing reported": {plannedBuild(nil),
			"queue:SUCCEEDED badge:RUNNING render:PENDING upload:PENDING check:PENDING"},
		"a later step reported": {plannedBuild(func(b *models.Build) { b.StepReports = reports(map[string]int{"render": 12}) }),
			"queue:SUCCEEDED badge:SUCCEEDED render:RUNNING upload:PENDING check:PENDING"},
		"reports out of order": {plannedBuild(func(b *models.Build) { b.StepReports = reports(map[string]int{"upload": 13, "badge": 11}) }),
			"queue:SUCCEEDED badge:SUCCEEDED render:SUCCEEDED upload:RUNNING check:PENDING"},
		"a step outside the plan": {plannedBuild(func(b *models.Build) { b.StepReports = reports(map[string]int{"sign": 12}) }),
			"queue:SUCCEEDED badge:RUNNING render:PENDING upload:PENDING check:PENDING"},
		"checking the artifact": {plannedBuild(func(b *models.Build) {
			b.StepReports, b.ResultReceived, b.ResultAt = reports(map[string]int{"render": 12}), true, at(14)
		}), "queue:SUCCEEDED badge:SUCCEEDED render:SUCCEEDED upload:SUCCEEDED check:RUNNING"},
		"succeeded": {plannedBuild(func(b *models.Build) {
			b.State, b.ResultReceived, b.ResultAt, b.FinishedAt = models.BuildStateSucceeded, true, at(14), at(15)
		}), "queue:SUCCEEDED badge:SUCCEEDED render:SUCCEEDED upload:SUCCEEDED check:SUCCEEDED"},
		"the factory failed": {plannedBuild(func(b *models.Build) {
			b.State, b.FinishedAt, b.StepReports = models.BuildStateFailed, at(14), reports(map[string]int{"render": 12})
		}), "queue:SUCCEEDED badge:SUCCEEDED render:FAILED upload:SKIPPED check:SKIPPED"},
		"the artifact did not check out": {plannedBuild(func(b *models.Build) {
			b.State, b.ResultReceived, b.ResultAt, b.FinishedAt = models.BuildStateFailed, true, at(14), at(15)
		}), "queue:SUCCEEDED badge:SUCCEEDED render:SUCCEEDED upload:SUCCEEDED check:FAILED"},
		"cancelled while building": {plannedBuild(func(b *models.Build) {
			b.State, b.FinishedAt = models.BuildStateCancelled, at(11)
		}), "queue:SUCCEEDED badge:CANCELLED render:SKIPPED upload:SKIPPED check:SKIPPED"},
		"cancelled in the queue": {&models.Build{State: models.BuildStateCancelled, CreatedAt: t0, FinishedAt: at(5)},
			"queue:CANCELLED build:SKIPPED check:SKIPPED"},
		"timed out in the queue": {&models.Build{State: models.BuildStateFailed, CreatedAt: t0, FinishedAt: at(5)},
			"queue:FAILED build:SKIPPED check:SKIPPED"},
		"refused": {&models.Build{State: models.BuildStateFailed, CreatedAt: t0, StartedAt: at(10), FinishedAt: at(10)},
			"queue:SUCCEEDED build:FAILED check:SKIPPED"},
	} {
		if got := states(Pipeline(c.build)); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, c.want)
		}
	}
}

func TestPipelineTimes(t *testing.T) {
	b := plannedBuild(func(b *models.Build) {
		b.State, b.ResultReceived, b.ResultAt, b.FinishedAt = models.BuildStateSucceeded, true, at(14), at(15)
		b.StepReports = reports(map[string]int{"badge": 11, "upload": 13}) // render's report was lost
	})
	st := Pipeline(b)
	span := func(i int) string {
		var from, to string
		if s := st[i].StartedAt; s != nil {
			from = s.Sub(t0).String()
		}
		if f := st[i].FinishedAt; f != nil {
			to = f.Sub(t0).String()
		}
		return from + "-" + to
	}
	for i, want := range []string{
		"0s-10s",  // queued until taken
		"11s-13s", // the badge until the next step reported
		"-",       // render: done, never reported
		"13s-14s", // the upload until the result
		"14s-15s", // the check until the build finished
	} {
		if got := span(i); got != want {
			t.Errorf("%s: %s, want %s", st[i].ID, got, want)
		}
	}
	if st[1].Message != "on badge" || st[0].Message != "" || !st[0].Core || st[1].Core || !st[4].Core {
		t.Errorf("stages %+v", st)
	}

	// Every step reported: each ends when the next one starts.
	st = Pipeline(plannedBuild(func(b *models.Build) {
		b.State, b.FinishedAt, b.StepReports = models.BuildStateFailed, at(14), reports(map[string]int{"badge": 11, "render": 12, "upload": 13})
	}))
	if f := st[1].FinishedAt; f == nil || !f.Equal(*at(12)) {
		t.Errorf("badge ends %v, want when render starts", f)
	}

	// A running step has no end; taken with nothing reported, the first step
	// started when the build was taken.
	st = Pipeline(plannedBuild(nil))
	if st[1].StartedAt == nil || !st[1].StartedAt.Equal(*at(10)) || st[1].FinishedAt != nil {
		t.Errorf("running first step %+v", st[1])
	}
	// The step a failed build was on ends with the build.
	st = Pipeline(plannedBuild(func(b *models.Build) {
		b.State, b.FinishedAt, b.StepReports = models.BuildStateFailed, at(14), reports(map[string]int{"render": 12})
	}))
	if f := st[2].FinishedAt; f == nil || !f.Equal(*at(14)) {
		t.Errorf("failed step %+v", st[2])
	}
}
