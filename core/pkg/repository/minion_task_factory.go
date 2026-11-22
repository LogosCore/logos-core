package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// IMinionFactoryRepository keeps factory commands' tasks (docs:
// contracts/factory-commands): offered to their factory's instances until one
// takes them, then running there, with their output and their result as the
// instance reports them.
type IMinionFactoryRepository interface {
	// ClaimFactoryOffer takes a task waiting for an instance whose offer is
	// due by now, for by until until: nil, nil when none is.
	ClaimFactoryOffer(ctx context.Context, by string, until, now time.Time) (*models.MinionTask, error)
	// OfferFactory marks a claimed task offered to instance, before the offer
	// goes (an instance reports as soon as it accepts, which can be before
	// its reply comes): false when the task no longer waits (stopped).
	OfferFactory(ctx context.Context, taskID uuid.UUID, by, instance string, at time.Time) (bool, error)
	// UnofferFactory puts back a task instance did not take: false when the
	// instance reported meanwhile (it took the task, and its reply was lost),
	// or the task ended.
	UnofferFactory(ctx context.Context, taskID uuid.UUID, instance string) (bool, error)
	// AcceptFactory records that instance took the task, at at. IsNotFound
	// when the task ended, or is another instance's.
	AcceptFactory(ctx context.Context, taskID uuid.UUID, instance string, at time.Time) (*models.MinionTask, error)
	// WaitFactory leaves a claimed task no instance took to wait, offered
	// again at next; with note, why it waits (news: the task changes, and
	// waits since at unless it waited already).
	WaitFactory(ctx context.Context, taskID uuid.UUID, by string, next time.Time, why string, note bool, at time.Time) (*models.MinionTask, error)
	// FactoryOutput keeps an output report of the instance that runs the
	// task: seq's part, its out as the task's budget takes it (an empty out
	// keeps no part: the command still runs). kept is false for a seq core
	// has (a report sent again). IsNotFound when the task ended, or is
	// another instance's.
	FactoryOutput(ctx context.Context, taskID uuid.UUID, instance string, seq int64, out []byte, at time.Time) (*models.MinionTask, bool, error)
	// FinishFactory ends the task with its factory's result. IsNotFound when
	// it ended, or is another instance's.
	FinishFactory(ctx context.Context, taskID uuid.UUID, instance string, res FactoryResult) (*models.MinionTask, error)
	// EndFactory ends the task on core's word (a stop, a give-up): state,
	// and err. IsNotFound when it ended already.
	EndFactory(ctx context.Context, taskID uuid.UUID, state, err string, at time.Time) (*models.MinionTask, error)
	// FactoryUnderWay are the tasks offered to an instance or running on one,
	// limit at most.
	FactoryUnderWay(ctx context.Context, limit int64) ([]models.MinionTask, error)
}

// FactoryResult is the result a factory command's task ends with.
type FactoryResult struct {
	// State is DONE or FAILED; OK, Err and Exit as its factory said them.
	State string
	OK    bool
	Err   string
	Exit  *int64
	// Data is its data as its result's CBOR; Problem why it does not fit its
	// result; DataCut: it was over the task's budget, and dropped.
	Data    []byte
	Problem string
	DataCut bool
	At      time.Time
}

// factoryUnset are a factory task's fields that only matter while it waits
// or is offered.
var factoryUnset = bson.M{"factory.waiting": "", "factory.waiting_since": "", "factory.offer_at": "", "factory.lease_by": "",
	"factory.lease_until": ""}

func (r *minionTaskRepository) ClaimFactoryOffer(ctx context.Context, by string, until, now time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"state": models.MinionTaskFactory, "factory.instance": bson.M{"$exists": false},
		"factory.offer_at": bson.M{"$lte": now},
		"$or": bson.A{
			bson.M{"factory.lease_until": bson.M{"$exists": false}},
			bson.M{"factory.lease_until": bson.M{"$lt": now}},
		}}).Sort("factory.offer_at").Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"factory.lease_by": by, "factory.lease_until": until}},
		ReturnNew: true,
	}, &after)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) OfferFactory(ctx context.Context, taskID uuid.UUID, by, instance string, at time.Time) (bool, error) {
	res, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskFactory,
		"factory.instance": bson.M{"$exists": false}, "factory.lease_by": by},
		bson.M{"$set": bson.M{"factory.instance": instance, "factory.offered_at": at}, "$inc": bumpRev})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

func (r *minionTaskRepository) UnofferFactory(ctx context.Context, taskID uuid.UUID, instance string) (bool, error) {
	res, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskFactory, "factory.instance": instance,
		"factory.accepted_at": bson.M{"$exists": false}, "factory.report_at": bson.M{"$exists": false}},
		bson.M{"$unset": bson.M{"factory.instance": "", "factory.offered_at": ""}, "$inc": bumpRev})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

func (r *minionTaskRepository) AcceptFactory(ctx context.Context, taskID uuid.UUID, instance string, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskFactory, "factory.instance": instance}).
		Apply(qmgo.Change{Update: bson.M{
			"$min":   bson.M{"factory.accepted_at": at, "started_at": at},
			"$max":   bson.M{"factory.report_at": at},
			"$unset": factoryUnset,
			"$inc":   bumpRev,
		}, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) WaitFactory(ctx context.Context, taskID uuid.UUID, by string, next time.Time, why string, note bool,
	at time.Time) (*models.MinionTask, error) {
	update := bson.M{"$set": bson.M{"factory.offer_at": next}, "$unset": bson.M{"factory.lease_by": "", "factory.lease_until": ""}}
	if note {
		update["$set"] = bson.M{"factory.offer_at": next, "factory.waiting": why}
		update["$min"] = bson.M{"factory.waiting_since": at}
		update["$inc"] = bumpRev
	}
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskFactory, "factory.instance": bson.M{"$exists": false},
		"factory.lease_by": by}).Apply(qmgo.Change{Update: update, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) FactoryOutput(ctx context.Context, taskID uuid.UUID, instance string, seq int64, out []byte,
	at time.Time) (*models.MinionTask, bool, error) {
	running := bson.M{"task_id": taskID, "state": models.MinionTaskFactory, "factory.instance": instance}
	var t models.MinionTask
	if err := r.tasks.Find(ctx, running).One(&t); err != nil {
		return nil, false, err
	}
	if seq <= t.Factory.Seq {
		// Sent again (its reply was lost): kept once. Still a sign of life.
		if err := r.tasks.UpdateOne(ctx, running, bson.M{"$max": bson.M{"factory.report_at": at}}); err != nil && !IsNotFound(err) {
			return nil, false, err
		}
		return &t, false, nil
	}
	// Output and data share the task's budget, as a minion's do (AddResult).
	room := max(int64(models.MaxMinionTaskOutput)-t.OutSize-t.DataSize, 0)
	cut := false
	if int64(len(out)) > room {
		out, cut = out[:room], true
	}
	update := bson.M{
		"$set": bson.M{"factory.seq": seq},
		"$max": bson.M{"factory.report_at": at},
		// A report that came before the instance's reply: it took the task.
		"$min": bson.M{"factory.accepted_at": at, "started_at": at},
	}
	if cut {
		update["$set"] = bson.M{"factory.seq": seq, "out_truncated": true}
	}
	if len(out) > 0 {
		// Parts count from 0, reports from 1: a seq that skips one is a part
		// lost for good (MissingParts), as a minion's is.
		part := models.MinionTaskPart{TaskID: taskID, Seq: uint64(seq - 1), Out: out, At: at} //nolint:gosec // seq is 1 or more here
		if _, err := r.parts.InsertOne(ctx, &part); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return &t, false, nil
			}
			return nil, false, err
		}
		// What the console shows changed; a report with nothing printed
		// (the command still runs) changes nothing it shows.
		update["$inc"] = bson.M{"rev": 1, "parts": 1, "out_size": len(out)}
	}
	filter := bson.M{"task_id": taskID, "state": models.MinionTaskFactory, "factory.instance": instance,
		"factory.seq": bson.M{"$not": bson.M{"$gte": seq}}}
	var after models.MinionTask
	err := r.tasks.Find(ctx, filter).Apply(qmgo.Change{Update: update, ReturnNew: true}, &after)
	if IsNotFound(err) {
		// A later report was kept meanwhile, or the task ended.
		return &t, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &after, true, nil
}

func (r *minionTaskRepository) FinishFactory(ctx context.Context, taskID uuid.UUID, instance string, res FactoryResult) (*models.MinionTask, error) {
	running := bson.M{"task_id": taskID, "state": models.MinionTaskFactory, "factory.instance": instance}
	var t models.MinionTask
	if err := r.tasks.Find(ctx, running).One(&t); err != nil {
		return nil, err
	}
	set := bson.M{"state": res.State, "done_at": res.At, "ok": res.OK, "factory.result_at": res.At}
	if res.Err != "" {
		set["err"] = res.Err
	}
	if res.Exit != nil {
		set["exit"] = *res.Exit
	}
	if res.Problem != "" {
		set["data_problem"] = res.Problem
	}
	if res.DataCut {
		set["data_truncated"] = true
	}
	inc := bson.M{"rev": 1}
	// Its data is the part after its output's: the done part.
	next := uint64(max(t.Factory.Seq, 0)) //nolint:gosec // not negative
	switch {
	case len(res.Data) > 0:
		part := models.MinionTaskPart{TaskID: taskID, Seq: next, DataCBOR: res.Data, At: res.At}
		if _, err := r.parts.InsertOne(ctx, &part); err != nil && !mongo.IsDuplicateKeyError(err) {
			return nil, err
		}
		inc["parts"], inc["data_size"], inc["data_parts"] = 1, len(res.Data), 1
		set["done_seq"] = next
	case next > 0:
		set["done_seq"] = next - 1
	}
	var after models.MinionTask
	err := r.tasks.Find(ctx, running).Apply(qmgo.Change{Update: bson.M{
		"$set":   set,
		"$min":   bson.M{"started_at": res.At},
		"$unset": factoryUnset,
		"$inc":   inc,
	}, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) EndFactory(ctx context.Context, taskID uuid.UUID, state, err string, at time.Time) (*models.MinionTask, error) {
	set := bson.M{"state": state, "done_at": at}
	if state == models.MinionTaskFailed {
		set["ok"] = false
	}
	if err != "" {
		set["err"] = err
	}
	var after models.MinionTask
	e := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskFactory}).Apply(qmgo.Change{Update: bson.M{
		"$set": set, "$unset": factoryUnset, "$inc": bumpRev,
	}, ReturnNew: true}, &after)
	if e != nil {
		return nil, e
	}
	return &after, nil
}

func (r *minionTaskRepository) FactoryUnderWay(ctx context.Context, limit int64) ([]models.MinionTask, error) {
	var out []models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"state": models.MinionTaskFactory, "factory.instance": bson.M{"$exists": true}}).
		Sort("queued_at").Limit(limit).All(&out)
	return out, err
}
