package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Composite commands' tasks (docs: contracts/task-outsourcing): what core's
// composites service keeps of one (models.MinionComposite), and its subtasks,
// ordinary tasks of the session that name it (parent_id). The minion's end
// of a composite's own op is AddResult's (and the other writers' that end a
// task): the op ends, not the task, and its factory hears of it.

// IMinionCompositeRepository is the task repository's part for composite
// commands' tasks.
type IMinionCompositeRepository interface {
	// UpdateComposite changes a composite task's record as u says: IsNotFound
	// when it is not one, has ended, or u's condition does not hold. It
	// returns the task after.
	UpdateComposite(ctx context.Context, taskID uuid.UUID, u ComposeUpdate) (*models.MinionTask, error)
	// ClaimComposite makes by the core process that makes the task's calls
	// until until, unless another holds it at now: IsNotFound then (or for a
	// task that is not an open composite one). ReleaseComposite lets go.
	ClaimComposite(ctx context.Context, taskID uuid.UUID, by string, until, now time.Time) (*models.MinionTask, error)
	ReleaseComposite(ctx context.Context, taskID uuid.UUID, by string) error
	// SendOwnOp queues a composite task's own op, its task item's op and
	// args (wire), with the upload and credentials they name, and the args a
	// decision gave (own, nil for the task's): the task goes to its minion as
	// any queued task. IsNotFound unless its own op had not run.
	SendOwnOp(ctx context.Context, taskID uuid.UUID, op int, wire []byte, upload *models.MinionTaskUpload, credentialIDs []uuid.UUID,
		own models.JSONMap, at time.Time) (*models.MinionTask, error)
	// EndOwnOp ends a composite task's own op that its minion never had
	// (queued, not sent): end says how; the task waits in core again. A sent
	// one is stopped as any task (RequestCancel), and ends with its part.
	// IsNotFound when it is not queued.
	EndOwnOp(ctx context.Context, taskID uuid.UUID, end models.MinionComposeOwnEnd) (*models.MinionTask, error)
	// CancelOwnOp asks the minion to stop a composite task's own op that went
	// out: its cancel goes with the next replies, as an operator's does.
	// IsNotFound when it is not out.
	CancelOwnOp(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error)
	// EndComposite ends a composite task: state, its ok and err (nil and ""
	// keep what is there), done at at. IsNotFound when it ended already.
	EndComposite(ctx context.Context, taskID uuid.UUID, state string, ok *bool, err string, at time.Time) (*models.MinionTask, error)
	// FinishComposite ends a composite task with no op of its own with a
	// result of its own (a finish's out and data, the data as its result's
	// CBOR, with the problem its check found) or, with from, the result of
	// one of its subtasks: its parts, sizes and flags.
	FinishComposite(ctx context.Context, taskID uuid.UUID, f CompositeResult) (*models.MinionTask, error)
	// Subtask is one of a composite task's subtasks: IsNotFound for another
	// task.
	Subtask(ctx context.Context, parentID, taskID uuid.UUID) (*models.MinionTask, error)
	// ListSubtasks are a composite task's subtasks, in the order they were
	// queued.
	ListSubtasks(ctx context.Context, parentID uuid.UUID) ([]models.MinionTask, error)
	// OpenSubtasks are a composite task's subtasks that have not ended, in
	// the order they were queued; with keys, only those with one of them.
	OpenSubtasks(ctx context.Context, parentID uuid.UUID, keys []string) ([]models.MinionTask, error)
	// CountOpenSubtasks counts a composite task's subtasks that have not
	// ended (cleanup ones too).
	CountOpenSubtasks(ctx context.Context, parentID uuid.UUID) (int, error)
	// UnheardSubtasks are a composite task's subtasks that ended and that it
	// has not taken the end of (Heard), in the order they ended, limit at
	// most. MarkHeard records that it took one's.
	UnheardSubtasks(ctx context.Context, parentID uuid.UUID, limit int64) ([]models.MinionTask, error)
	MarkHeard(ctx context.Context, taskID uuid.UUID) error
	// ComposeDue are the composite tasks core looks at next by now
	// (WakeAt), limit at most; UnheardParents the composite tasks with a
	// subtask that ended unheard: what a restart of core, or a lost event,
	// left.
	ComposeDue(ctx context.Context, now time.Time, limit int64) ([]uuid.UUID, error)
	UnheardParents(ctx context.Context, limit int64) ([]uuid.UUID, error)
}

// ComposeUpdate is a change of a composite task's record (models.MinionComposite).
type ComposeUpdate struct {
	// Set are the record's fields by their bson names (seq, ctx, status,
	// timer_at, ...), and Unset those cleared.
	Set   map[string]any
	Unset []string
	// PopEnd: its factory heard the first pending end (the call that told
	// it was answered); DropEnds: it heard all of them (the stopped call).
	PopEnd, DropEnds bool
	// Ends are ends its factory has not heard of, added after the others.
	Ends []models.MinionComposeEnd
	// Waiting, when not nil, replaces the subtasks waiting in the task.
	Waiting *[]models.MinionSubtask
	// Subtasks adds to the count of subtasks its decisions asked for.
	Subtasks int
	// IfSeq, when not nil, makes the change only while the latest call is
	// this one; IfPhase, while the task is in this phase (a stop that came
	// meanwhile makes an answer count for its ctx alone).
	IfSeq   *int
	IfPhase *string
}

// CompositeResult is the result a composite task with no op of its own ends
// with.
type CompositeResult struct {
	State string
	OK    bool
	Err   string
	At    time.Time
	// Out and Data are its own (Data its result's CBOR), Problem why the
	// data does not fit its result; OutCut and DataCut: they were cut to the
	// task's budget.
	Out, Data       []byte
	Problem         string
	OutCut, DataCut bool
	// From is the subtask whose result it takes instead.
	From *models.MinionTask
}

// compositeField is a composite record's field (by its bson name) in the
// task's document.
func compositeField(name string) string { return "composite." + name }

func (r *minionTaskRepository) UpdateComposite(ctx context.Context, taskID uuid.UUID, u ComposeUpdate) (*models.MinionTask, error) {
	filter := bson.M{"task_id": taskID, "composite": bson.M{"$exists": true}, "state": bson.M{"$in": openStatesA()}}
	if u.IfSeq != nil {
		filter["composite.seq"] = *u.IfSeq
	}
	if u.IfPhase != nil {
		if *u.IfPhase == "" {
			filter["composite.phase"] = bson.M{"$in": bson.A{nil, ""}}
		} else {
			filter["composite.phase"] = *u.IfPhase
		}
	}
	set, unset := bson.M{}, bson.M{}
	for k, v := range u.Set {
		set[compositeField(k)] = v
	}
	for _, k := range u.Unset {
		unset[compositeField(k)] = ""
	}
	if u.DropEnds {
		set[compositeField("pending")] = []models.MinionComposeEnd{}
	}
	if u.Waiting != nil {
		set[compositeField("waiting")] = *u.Waiting
	}
	update := bson.M{"$inc": bson.M{"rev": 1}}
	if u.Subtasks != 0 {
		update["$inc"] = bson.M{"rev": 1, compositeField("subtasks"): u.Subtasks}
	}
	if len(set) > 0 {
		update["$set"] = set
	}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	if u.PopEnd {
		update["$pop"] = bson.M{compositeField("pending"): -1}
	}
	var after models.MinionTask
	if err := r.tasks.Find(ctx, filter).Apply(qmgo.Change{Update: update, ReturnNew: true}, &after); err != nil {
		return nil, err
	}
	if len(u.Ends) == 0 {
		return &after, nil
	}
	// The ends go after: a field takes one change of its array at a time.
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID}).Apply(qmgo.Change{
		Update:    bson.M{"$push": bson.M{compositeField("pending"): bson.M{"$each": u.Ends}}},
		ReturnNew: true,
	}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) ClaimComposite(ctx context.Context, taskID uuid.UUID, by string, until, now time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "composite": bson.M{"$exists": true}, "state": bson.M{"$in": openStatesA()},
		"$or": bson.A{
			bson.M{"composite.lease_until": bson.M{"$exists": false}},
			bson.M{"composite.lease_until": bson.M{"$lt": now}},
			bson.M{"composite.lease_by": by},
		}}).Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"composite.lease_by": by, "composite.lease_until": until}},
		ReturnNew: true,
	}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) ReleaseComposite(ctx context.Context, taskID uuid.UUID, by string) error {
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID, "composite.lease_by": by},
		bson.M{"$unset": bson.M{"composite.lease_by": "", "composite.lease_until": ""}})
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (r *minionTaskRepository) SendOwnOp(ctx context.Context, taskID uuid.UUID, op int, wire []byte, upload *models.MinionTaskUpload,
	credentialIDs []uuid.UUID, own models.JSONMap, at time.Time) (*models.MinionTask, error) {
	set := bson.M{"state": models.MinionTaskQueued, "op": op, "composite.own_op": string(protocol.OwnOpOut)}
	unset := bson.M{}
	if len(wire) > 0 {
		set["wire_args"] = wire
	} else {
		unset["wire_args"] = ""
	}
	if own != nil {
		set["composite.own_args"] = own
	}
	// A file or a credential the op's args name: the task's upload and
	// credentials. One of the task's own args stays named otherwise, kept
	// for the subtasks that may send it.
	if upload != nil {
		set["upload"] = upload
	}
	if len(credentialIDs) > 0 {
		set["credential_ids"] = credentialIDs
	}
	update := bson.M{"$set": set, "$inc": bumpRev}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskComposing,
		"composite.own_op": string(protocol.OwnOpNotRun)}).Apply(qmgo.Change{Update: update, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

// ownOpEnded is the update that ends a composite task's own op as end says,
// not the task: it waits in core again, and its factory hears of the end.
func ownOpEnded(end models.MinionComposeOwnEnd, at time.Time, id uuid.UUID) bson.M {
	return bson.M{
		"$set": bson.M{"state": models.MinionTaskComposing, "composite.own_op": string(protocol.OwnOpEnded),
			"composite.own_end": end, "composite.wake_at": at},
		"$unset": bson.M{"cancel_requested_at": "", "cancel_reason": ""},
		"$push":  bson.M{"composite.pending": models.MinionComposeEnd{TaskID: &id, OwnOp: true, At: at}},
		"$inc":   bumpRev,
	}
}

func (r *minionTaskRepository) EndOwnOp(ctx context.Context, taskID uuid.UUID, end models.MinionComposeOwnEnd) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskQueued,
		"composite.own_op": string(protocol.OwnOpOut)}).Apply(qmgo.Change{Update: ownOpEnded(end, end.At, taskID), ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) CancelOwnOp(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "composite.own_op": string(protocol.OwnOpOut),
		"state": bson.M{"$in": bson.A{models.MinionTaskSent, models.MinionTaskRunning}}, "cancel_requested_at": bson.M{"$exists": false}}).
		Apply(qmgo.Change{Update: bson.M{"$set": bson.M{"cancel_requested_at": at}, "$inc": bumpRev}, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) EndComposite(ctx context.Context, taskID uuid.UUID, state string, ok *bool, err string, at time.Time) (*models.MinionTask, error) {
	set := bson.M{"state": state, "done_at": at}
	if ok != nil {
		set["ok"] = *ok
	}
	if err != "" {
		set["err"] = err
	}
	var after models.MinionTask
	e := r.tasks.Find(ctx, bson.M{"task_id": taskID, "composite": bson.M{"$exists": true}, "state": bson.M{"$in": openStatesA()}}).
		Apply(qmgo.Change{Update: bson.M{"$set": set, "$inc": bumpRev,
			"$unset": bson.M{"composite.wake_at": "", "composite.lease_by": "", "composite.lease_until": "", "composite.call_at": "",
				"composite.call_deadline": "", "composite.timer_at": ""}}, ReturnNew: true}, &after)
	if e != nil {
		return nil, e
	}
	return &after, nil
}

func (r *minionTaskRepository) FinishComposite(ctx context.Context, taskID uuid.UUID, f CompositeResult) (*models.MinionTask, error) {
	set := bson.M{"state": f.State, "done_at": f.At, "ok": f.OK}
	if f.Err != "" {
		set["err"] = f.Err
	}
	inc := bson.M{"rev": 1}
	switch {
	case f.From != nil:
		parts, err := r.Parts(ctx, f.From.TaskID)
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			copied := models.MinionTaskPart{TaskID: taskID, Seq: p.Seq, Out: p.Out, DataCBOR: p.DataCBOR, Data: p.Data, At: p.At}
			if _, err := r.parts.InsertOne(ctx, &copied); err != nil && !mongo.IsDuplicateKeyError(err) {
				return nil, err
			}
		}
		from := f.From
		resultOf := from.TaskID
		set["result_of"] = resultOf
		set["parts"], set["out_size"], set["data_size"], set["data_parts"] = from.Parts, from.OutSize, from.DataSize, from.DataParts
		for k, v := range map[string]any{"out_truncated": from.OutTruncated, "data_truncated": from.DataTruncated} {
			if v.(bool) {
				set[k] = true
			}
		}
		if from.DataProblem != "" {
			set["data_problem"] = from.DataProblem
		}
		if from.DoneSeq != nil {
			set["done_seq"] = *from.DoneSeq
		}
		if len(from.LatestData) > 0 {
			set["latest_data"], set["latest_data_seq"] = from.LatestData, from.LatestDataSeq
		}
	case len(f.Out) > 0 || len(f.Data) > 0:
		part := models.MinionTaskPart{TaskID: taskID, Seq: 0, Out: f.Out, DataCBOR: f.Data, At: f.At}
		if _, err := r.parts.InsertOne(ctx, &part); err != nil && !mongo.IsDuplicateKeyError(err) {
			return nil, err
		}
		set["done_seq"] = uint64(0)
		inc["parts"], inc["out_size"], inc["data_size"] = 1, len(f.Out), len(f.Data)
		if len(f.Data) > 0 {
			inc["data_parts"] = 1
		}
		if f.OutCut {
			set["out_truncated"] = true
		}
		if f.DataCut {
			set["data_truncated"] = true
		}
		if f.Problem != "" {
			set["data_problem"] = f.Problem
		}
	}
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "composite": bson.M{"$exists": true}, "state": bson.M{"$in": openStatesA()}}).
		Apply(qmgo.Change{Update: bson.M{"$set": set, "$inc": inc,
			"$unset": bson.M{"composite.wake_at": "", "composite.lease_by": "", "composite.lease_until": "", "composite.call_at": "",
				"composite.call_deadline": "", "composite.timer_at": ""}}, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) Subtask(ctx context.Context, parentID, taskID uuid.UUID) (*models.MinionTask, error) {
	var t models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "parent_id": parentID}).One(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *minionTaskRepository) ListSubtasks(ctx context.Context, parentID uuid.UUID) ([]models.MinionTask, error) {
	var out []models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"parent_id": parentID}).Sort("queued_at").Limit(protocol.MaxSubtasks).All(&out)
	return out, err
}

func (r *minionTaskRepository) OpenSubtasks(ctx context.Context, parentID uuid.UUID, keys []string) ([]models.MinionTask, error) {
	filter := bson.M{"parent_id": parentID, "state": bson.M{"$in": openStatesA()}}
	if keys != nil {
		filter["subtask_key"] = bson.M{"$in": keys}
	}
	var out []models.MinionTask
	err := r.tasks.Find(ctx, filter).Sort("queued_at").All(&out)
	return out, err
}

func (r *minionTaskRepository) CountOpenSubtasks(ctx context.Context, parentID uuid.UUID) (int, error) {
	n, err := r.tasks.Find(ctx, bson.M{"parent_id": parentID, "state": bson.M{"$in": openStatesA()}}).Count()
	return int(n), err
}

// finishedStates are a task's states once it ended.
var finishedStates = bson.A{models.MinionTaskDone, models.MinionTaskFailed, models.MinionTaskCancelled}

func (r *minionTaskRepository) UnheardSubtasks(ctx context.Context, parentID uuid.UUID, limit int64) ([]models.MinionTask, error) {
	var out []models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"parent_id": parentID, "state": bson.M{"$in": finishedStates}, "heard": bson.M{"$ne": true}}).
		Sort("done_at", "queued_at").Limit(limit).All(&out)
	return out, err
}

func (r *minionTaskRepository) MarkHeard(ctx context.Context, taskID uuid.UUID) error {
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"heard": true}})
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (r *minionTaskRepository) ComposeDue(ctx context.Context, now time.Time, limit int64) ([]uuid.UUID, error) {
	var rows []struct {
		TaskID uuid.UUID `bson:"task_id"`
	}
	err := r.tasks.Find(ctx, bson.M{"composite.wake_at": bson.M{"$lte": now}}).Select(bson.M{"task_id": 1}).
		Sort("composite.wake_at").Limit(limit).All(&rows)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		out[i] = row.TaskID
	}
	return out, nil
}

func (r *minionTaskRepository) UnheardParents(ctx context.Context, limit int64) ([]uuid.UUID, error) {
	var rows []struct {
		ParentID uuid.UUID `bson:"parent_id"`
	}
	err := r.tasks.Find(ctx, bson.M{"parent_id": bson.M{"$exists": true}, "state": bson.M{"$in": finishedStates}, "heard": bson.M{"$ne": true}}).
		Select(bson.M{"parent_id": 1}).Limit(limit).All(&rows)
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, row := range rows {
		if !seen[row.ParentID] {
			seen[row.ParentID] = true
			out = append(out, row.ParentID)
		}
	}
	return out, nil
}

// openStatesA are the states of a task that has not ended, for a query.
func openStatesA() bson.A {
	out := make(bson.A, len(models.MinionTaskOpenStates))
	for i, s := range models.MinionTaskOpenStates {
		out[i] = s
	}
	return out
}
