package builds

import (
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// A build's pipeline is how the app shows its progress: the factory's plan
// for the build, framed by core's own stages, the queue before an instance
// takes the build and the artifact check after its result. Core works the
// stages out from the build record (the plan, the factory's step reports, the
// build's times and outcome) whenever it is read. A progress report that
// never arrived costs a step its times, not its place: the steps before the
// one reported are done.

// Stage states.
const (
	StagePending   = "PENDING"
	StageRunning   = "RUNNING"
	StageSucceeded = "SUCCEEDED"
	StageFailed    = "FAILED"
	StageCancelled = "CANCELLED"
	StageSkipped   = "SKIPPED" // not run: the build ended before it
)

// Core's own stages. Their ids hold a dot, which a factory's step id cannot.
const (
	StageQueue = "core.queue"
	StageCheck = "core.check"
)

// Stage is one stage of a build's pipeline.
type Stage struct {
	ID    string
	Label string
	// Core marks core's own stages: the queue and the artifact check.
	Core  bool
	State string
	// Message is the factory's latest message for its step.
	Message    string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// defaultPlan stands in for a plan core has not got: the build was not taken
// yet, its factory refused it, or accepted it without a valid plan.
var defaultPlan = []models.BuildPlanStep{{ID: "build", Label: "Build"}}

// Pipeline is b's pipeline: the queue, the factory's steps, the artifact
// check.
func Pipeline(b *models.Build) []Stage {
	taken, ended := b.StartedAt != nil, b.Finished()
	created := b.CreatedAt

	queue := Stage{ID: StageQueue, Label: "Queued", Core: true, StartedAt: &created}
	switch {
	case taken:
		queue.State, queue.FinishedAt = StageSucceeded, b.StartedAt
	case !ended:
		queue.State = StageRunning
	default: // it ended in the queue
		queue.State, queue.FinishedAt = endState(b), b.FinishedAt
	}

	check := Stage{ID: StageCheck, Label: "Check artifact", Core: true}
	switch {
	case b.ResultReceived:
		check.StartedAt = b.ResultAt
		switch b.State {
		case models.BuildStateBuilding:
			check.State = StageRunning
		case models.BuildStateSucceeded:
			check.State, check.FinishedAt = StageSucceeded, b.FinishedAt
		default:
			check.State, check.FinishedAt = endState(b), b.FinishedAt
		}
	case ended:
		check.State = StageSkipped
	default:
		check.State = StagePending
	}

	out := []Stage{queue}
	out = append(out, factoryStages(b, taken, ended)...)
	return append(out, check)
}

// factoryStages are the stages of b's plan.
func factoryStages(b *models.Build, taken, ended bool) []Stage {
	plan := b.Plan
	if len(plan) == 0 {
		plan = defaultPlan
	}
	out := make([]Stage, len(plan))
	current := -1 // the last step in the plan the factory reported
	for i, st := range plan {
		out[i] = Stage{ID: st.ID, Label: st.Label}
		if r, ok := b.StepReports[st.ID]; ok {
			at := r.StartedAt
			out[i].StartedAt, out[i].Message = &at, r.Message
			current = i
		}
	}
	if taken && current < 0 {
		// Taken, and nothing reported yet: the factory is on its first step.
		current = 0
		out[0].StartedAt = b.StartedAt
	}

	// The factory's part is done once its result arrived; every step of a
	// build whose artifact is being checked, or checked out, is done.
	done := b.ResultReceived || b.State == models.BuildStateSucceeded
	for i := range out {
		switch {
		case !taken:
			out[i].State = waiting(ended)
		case done || i < current:
			out[i].State = StageSucceeded
		case i == current && !ended:
			out[i].State = StageRunning
		case i == current:
			out[i].State = endState(b)
		default:
			out[i].State = waiting(ended)
		}
	}

	// A step ends when the next reported one starts, or when the factory's
	// part ends. The running step has neither yet.
	end := b.ResultAt
	if end == nil && ended {
		end = b.FinishedAt
	}
	for i := range out {
		if out[i].StartedAt == nil {
			continue
		}
		out[i].FinishedAt = end
		for j := i + 1; j < len(out); j++ {
			if out[j].StartedAt != nil {
				out[i].FinishedAt = out[j].StartedAt
				break
			}
		}
	}
	return out
}

// endState is how a build that ended without succeeding leaves the stage it
// was in.
func endState(b *models.Build) string {
	if b.State == models.BuildStateCancelled {
		return StageCancelled
	}
	return StageFailed
}

// waiting is the state of a stage the build has not reached: pending while
// it runs, skipped once it ended.
func waiting(ended bool) string {
	if ended {
		return StageSkipped
	}
	return StagePending
}
