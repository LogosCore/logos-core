package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	minionTaskCollection     = "minion_tasks"
	minionTaskPartCollection = "minion_task_parts"
)

// MinionTaskResult is what a result part brings to its task.
type MinionTaskResult struct {
	Seq  uint64
	Done bool
	OK   *bool
	Exit *int64
	Err  string
	Out  []byte
	Data map[string]any
	At   time.Time
}

// MinionTaskCursor is where a page of a session's tasks starts: before
// this one, newest first.
type MinionTaskCursor struct {
	QueuedAt time.Time
	TaskID   uuid.UUID
}

// IMinionTaskRepository keeps the tasks operators give sessions
// (models.MinionTask) and their output parts.
type IMinionTaskRepository interface {
	Insert(ctx context.Context, t *models.MinionTask) error
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
	// ForDelivery returns what a minion's get_tasks gets, at most limit:
	// tasks sent before and not yet acked first, then queued ones, oldest
	// first; none an operator asked to cancel.
	ForDelivery(ctx context.Context, sessionID uuid.UUID, limit int) ([]models.MinionTask, error)
	// MarkSent records that these tasks went out at at.
	MarkSent(ctx context.Context, taskIDs []uuid.UUID, at time.Time) error
	// Ack records that the minion got these tasks of the session.
	Ack(ctx context.Context, sessionID uuid.UUID, taskIDs []uuid.UUID, at time.Time) error
	// AddResult keeps a result part of a task of the session and brings the
	// task up to it. It returns the task after, and whether the part was
	// new: a part sent again changes nothing. IsNotFound for a task the
	// session does not have.
	AddResult(ctx context.Context, sessionID, taskID uuid.UUID, r MinionTaskResult) (*models.MinionTask, bool, error)
	// RequestCancel asks to stop a task: one never sent is cancelled at
	// once, one sent gets cancels until its last part comes. Nothing for a
	// finished one. It returns the task after.
	RequestCancel(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error)
	// CancelsToSend returns the session's tasks an operator asked to stop
	// that are not over, and counts a cancel sent for each; one never acked
	// that has had MinionCancelGiveUp is counted cancelled instead.
	CancelsToSend(ctx context.Context, sessionID uuid.UUID, at time.Time) (send, gaveUp []models.MinionTask, err error)
	// ListBySession returns a page of the session's tasks, newest first.
	ListBySession(ctx context.Context, sessionID uuid.UUID, before *MinionTaskCursor, limit int64) ([]models.MinionTask, error)
	// Parts returns a task's output parts in seq order.
	Parts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionTaskRepository struct {
	tasks database.Collection
	parts database.Collection
}

func NewMinionTaskRepository(db database.Database) IMinionTaskRepository {
	db.EnsureIndexes(context.Background(), minionTaskCollection, []opts.IndexModel{
		{Key: []string{"task_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		// A session's page, newest first; and what waits for its minion.
		{Key: []string{"session_id", "-queued_at", "-task_id"}},
		{Key: []string{"session_id", "state", "queued_at"}},
		{Key: []string{"operation_id"}},
	})
	db.EnsureIndexes(context.Background(), minionTaskPartCollection, []opts.IndexModel{
		{Key: []string{"task_id", "seq"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &minionTaskRepository{tasks: db.Collection(minionTaskCollection), parts: db.Collection(minionTaskPartCollection)}
}

func (r *minionTaskRepository) Insert(ctx context.Context, t *models.MinionTask) error {
	_, err := r.tasks.InsertOne(ctx, t)
	return err
}

func (r *minionTaskRepository) FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error) {
	var t models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"task_id": taskID}).One(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *minionTaskRepository) ForDelivery(ctx context.Context, sessionID uuid.UUID, limit int) ([]models.MinionTask, error) {
	var resend, queued []models.MinionTask
	notCancelled := bson.M{"$exists": false}
	if err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "state": models.MinionTaskSent,
		"acked_at": bson.M{"$exists": false}, "cancel_requested_at": notCancelled}).
		Sort("queued_at").Limit(int64(limit)).All(&resend); err != nil {
		return nil, err
	}
	if left := limit - len(resend); left > 0 {
		if err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "state": models.MinionTaskQueued, "cancel_requested_at": notCancelled}).
			Sort("queued_at").Limit(int64(left)).All(&queued); err != nil {
			return nil, err
		}
	}
	return append(resend, queued...), nil
}

func uuidsIn(ids []uuid.UUID) bson.A {
	in := make(bson.A, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	return in
}

func (r *minionTaskRepository) MarkSent(ctx context.Context, taskIDs []uuid.UUID, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	in := uuidsIn(taskIDs)
	if _, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": bson.M{"$in": in}, "state": models.MinionTaskQueued},
		bson.M{"$set": bson.M{"state": models.MinionTaskSent, "sent_at": at}}); err != nil {
		return err
	}
	_, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": bson.M{"$in": in}}, bson.M{"$inc": bson.M{"send_count": 1}})
	return err
}

func (r *minionTaskRepository) Ack(ctx context.Context, sessionID uuid.UUID, taskIDs []uuid.UUID, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	_, err := r.tasks.UpdateAll(ctx,
		bson.M{"session_id": sessionID, "task_id": bson.M{"$in": uuidsIn(taskIDs)}, "acked_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"acked_at": at}})
	return err
}

func (r *minionTaskRepository) AddResult(ctx context.Context, sessionID, taskID uuid.UUID, res MinionTaskResult) (*models.MinionTask, bool, error) {
	var t models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "session_id": sessionID}).One(&t); err != nil {
		return nil, false, err
	}
	// The output kept stops at MaxMinionTaskOutput.
	out := res.Out
	truncated := false
	if room := int64(models.MaxMinionTaskOutput) - t.OutSize; int64(len(out)) > room {
		out, truncated = out[:max(room, 0)], true
	}
	part := models.MinionTaskPart{TaskID: taskID, Seq: res.Seq, Out: out, Data: res.Data, At: res.At}
	if _, err := r.parts.InsertOne(ctx, &part); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return &t, false, nil
		}
		return nil, false, err
	}

	set := bson.M{}
	if t.AckedAt == nil {
		set["acked_at"] = res.At
	}
	if t.StartedAt == nil {
		set["started_at"] = res.At
	}
	if !t.Finished() {
		set["state"] = models.MinionTaskRunning
	}
	if truncated {
		set["out_truncated"] = true
	}
	if res.Done {
		set["done_at"] = res.At
		if res.OK != nil {
			set["ok"] = *res.OK
		}
		if res.Exit != nil {
			set["exit"] = *res.Exit
		}
		if res.Err != "" {
			set["err"] = res.Err
		}
		// What it ended as: stopped by an operator, or as it went.
		switch {
		case res.OK != nil && *res.OK:
			set["state"] = models.MinionTaskDone
		case t.CancelRequestedAt != nil:
			set["state"] = models.MinionTaskCancelled
		default:
			set["state"] = models.MinionTaskFailed
		}
	}
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID}).Apply(qmgo.Change{
		Update:    bson.M{"$set": set, "$inc": bson.M{"parts": 1, "out_size": len(out)}},
		ReturnNew: true,
	}, &after)
	if err != nil {
		return nil, false, err
	}
	return &after, true, nil
}

func (r *minionTaskRepository) RequestCancel(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	// Never sent: cancelled now.
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskQueued}).Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"state": models.MinionTaskCancelled, "done_at": at, "cancel_requested_at": at}},
		ReturnNew: true,
	}, &after)
	if err == nil {
		return &after, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}
	// Sent and not over: the minion is told.
	err = r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": bson.M{"$in": bson.A{models.MinionTaskSent, models.MinionTaskRunning}},
		"cancel_requested_at": bson.M{"$exists": false}}).Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"cancel_requested_at": at}},
		ReturnNew: true,
	}, &after)
	if err == nil {
		return &after, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}
	// Over, or asked already: as it is.
	return r.FindByTaskID(ctx, taskID)
}

func (r *minionTaskRepository) CancelsToSend(ctx context.Context, sessionID uuid.UUID, at time.Time) ([]models.MinionTask, []models.MinionTask, error) {
	var asked []models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "cancel_requested_at": bson.M{"$exists": true},
		"state": bson.M{"$in": bson.A{models.MinionTaskSent, models.MinionTaskRunning}}}).All(&asked); err != nil {
		return nil, nil, err
	}
	var send, gaveUp []models.MinionTask
	for _, t := range asked {
		if t.AckedAt == nil && t.CancelSends >= models.MinionCancelGiveUp {
			// Never acked, cancelled again and again: it most likely never
			// reached the minion.
			err := r.tasks.UpdateOne(ctx, bson.M{"task_id": t.TaskID, "state": models.MinionTaskSent, "acked_at": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"state": models.MinionTaskCancelled, "done_at": at}})
			if err != nil && !IsNotFound(err) {
				return nil, nil, err
			}
			if err == nil {
				t.State, t.DoneAt = models.MinionTaskCancelled, &at
				gaveUp = append(gaveUp, t)
			}
			continue
		}
		send = append(send, t)
	}
	if len(send) > 0 {
		ids := make([]uuid.UUID, len(send))
		for i, t := range send {
			ids[i] = t.TaskID
		}
		if _, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": bson.M{"$in": uuidsIn(ids)}}, bson.M{"$inc": bson.M{"cancel_sends": 1}}); err != nil {
			return nil, nil, err
		}
	}
	return send, gaveUp, nil
}

func (r *minionTaskRepository) ListBySession(ctx context.Context, sessionID uuid.UUID, before *MinionTaskCursor, limit int64) ([]models.MinionTask, error) {
	filter := bson.M{"session_id": sessionID}
	if before != nil {
		filter["$or"] = bson.A{
			bson.M{"queued_at": bson.M{"$lt": before.QueuedAt}},
			bson.M{"queued_at": before.QueuedAt, "task_id": bson.M{"$lt": before.TaskID}},
		}
	}
	var out []models.MinionTask
	err := r.tasks.Find(ctx, filter).Sort("-queued_at", "-task_id").Limit(limit).All(&out)
	return out, err
}

func (r *minionTaskRepository) Parts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error) {
	var out []models.MinionTaskPart
	err := r.parts.Find(ctx, bson.M{"task_id": taskID}).Sort("seq").All(&out)
	return out, err
}

func (r *minionTaskRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	var ids []struct {
		TaskID uuid.UUID `bson:"task_id"`
	}
	if err := r.tasks.Find(ctx, bson.M{"operation_id": operationID}).Select(bson.M{"task_id": 1}).All(&ids); err != nil {
		return err
	}
	if len(ids) > 0 {
		in := make(bson.A, len(ids))
		for i, t := range ids {
			in[i] = t.TaskID
		}
		if _, err := r.parts.RemoveAll(ctx, bson.M{"task_id": bson.M{"$in": in}}); err != nil {
			return err
		}
	}
	_, err := r.tasks.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
