package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
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
	// Data is the part's data, the CBOR map as it came.
	Data []byte
	At   time.Time
	// Named turns Data, as it came, into its result's named shape
	// (protocol.NamedResultData), before anything else reads it; nil: kept
	// as it came.
	Named func(result string, data []byte) ([]byte, error)
	// Check checks Data against the task's result (taskdata.Check); nil:
	// no check.
	Check func(result string, seq uint64, data []byte) error
	// Compose is the part's data for a part without, made from what else
	// it carries for the task's result (a dir_listing from its listing:
	// taskdata.ListingData); nil, or nil data: none.
	Compose func(result string) []byte
	// Latest: the task's result has every part say its data whole
	// (taskdata.Latest): the newest is kept on the task, not on its part.
	Latest func(result string) bool
}

// ErrMinionTaskEnded is AddResult's answer to a part for a task that
// ended, other than one it lacks before its done part: ignored.
var ErrMinionTaskEnded = errors.New("the task ended")

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
	// Document is a task's document as stored, in its key order: for
	// debugging. IsNotFound for an unknown task.
	Document(ctx context.Context, taskID uuid.UUID) (bson.D, error)
	// ForDelivery returns what a minion's get_tasks gets, at most limit:
	// tasks sent before and not yet acked first, then queued ones, oldest
	// first; none an operator asked to cancel.
	ForDelivery(ctx context.Context, sessionID uuid.UUID, limit int) ([]models.MinionTask, error)
	// CountRunning counts a session's tasks its minion has and has not
	// ended: acknowledged, or with a result, and not finished. Tasks sent
	// and not acknowledged are not counted: ForDelivery sends them again,
	// within its limit.
	CountRunning(ctx context.Context, sessionID uuid.UUID) (int, error)
	// MarkSent records that these tasks went out at at.
	MarkSent(ctx context.Context, taskIDs []uuid.UUID, at time.Time) error
	// Ack records that the minion got these tasks of the session.
	Ack(ctx context.Context, sessionID uuid.UUID, taskIDs []uuid.UUID, at time.Time) error
	// AddResult keeps a result part of a task of the session and brings the
	// task up to it. It returns the task after, and whether the part was
	// new: a part sent again changes nothing. IsNotFound for a task the
	// session does not have; ErrMinionTaskEnded for a part after the task
	// ended (a part it lacks before its done part is still kept, and
	// changes nothing but the output and data).
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
	// OutParts are the parts with output, in seq order, their output only.
	OutParts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	// FindByUploadXfer is the session's task whose file goes in transfer
	// xfer.
	FindByUploadXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionTask, error)
	// UploadAcked records how far the minion has its task's file.
	UploadAcked(ctx context.Context, taskID uuid.UUID, next int64, at time.Time) (*models.MinionTask, error)
	// UploadFileUse says whether a task file is named by a task that has not
	// ended (open), and by any task at all (named).
	UploadFileUse(ctx context.Context, fileID uuid.UUID) (open, named bool, err error)
	// SetDataProblem records why the task's data does not fit, unless one is
	// recorded.
	SetDataProblem(ctx context.Context, taskID uuid.UUID, problem string) (*models.MinionTask, error)

	// Streams (minionstreams). StreamAnnounce records a stream_file
	// task's announcement, once: IsNotFound for a task that is not the
	// session's, not a file_stream one, or over; the task as it is when
	// announced already.
	StreamAnnounce(ctx context.Context, sessionID, taskID uuid.UUID, s models.MinionTaskStream) (*models.MinionTask, error)
	// FindByStreamXfer is the session's task streaming in transfer xfer.
	FindByStreamXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionTask, error)
	// StreamAttach gives the stream to a downloader from offset from: a new
	// generation. It returns the task after.
	StreamAttach(ctx context.Context, taskID uuid.UUID, reader string, from int64, at time.Time) (*models.MinionTask, error)
	// StreamDelivered records how far generation gen's downloader has the
	// file; false when another took the stream.
	StreamDelivered(ctx context.Context, taskID uuid.UUID, gen, delivered int64, at time.Time) (bool, error)
	// StreamDetach lets go of generation gen's downloader.
	StreamDetach(ctx context.Context, taskID uuid.UUID, gen int64, at time.Time) error
	// StreamTook moves want past a chunk kept for generation gen, when want
	// is its offset; false when it was not (a chunk from elsewhere, or
	// another downloader's). sha256, when given, is the whole's.
	StreamTook(ctx context.Context, taskID uuid.UUID, gen, off, n int64, sha256 []byte) (bool, error)
	// ActiveStreams are a session's streams whose tasks have not ended.
	ActiveStreams(ctx context.Context, sessionID uuid.UUID) ([]models.MinionTask, error)
	// IdleStreams are streams whose tasks have not ended with no
	// downloader (or uploader) since before, or one that stopped saying it
	// is there.
	IdleStreams(ctx context.Context, before time.Time) ([]models.MinionTask, error)

	// Streams core sends a minion (minionstreams, upstream.go).
	// UploadAttach gives the stream to an uploader: a new generation.
	UploadAttach(ctx context.Context, taskID uuid.UUID, uploader string, at time.Time) (*models.MinionTask, error)
	// UploadSize records the size the uploader says, once.
	UploadSize(ctx context.Context, taskID uuid.UUID, size int64) (*models.MinionTask, error)
	// UploadReceived moves received past n bytes taken from generation
	// gen's uploader at off, with the hash's state after them (and the
	// whole's SHA-256 when they are the last); false when off is not where
	// the stream is, or another uploader took it.
	UploadReceived(ctx context.Context, taskID uuid.UUID, gen, off, n int64, hashState, sha256 []byte, at time.Time) (bool, error)
	// UploadDetach lets go of generation gen's uploader.
	UploadDetach(ctx context.Context, taskID uuid.UUID, gen int64, at time.Time) error
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
		// A transfer's task, and the tasks of a task file.
		{Key: []string{"session_id", "upload.xfer"}, IndexOptions: new(options.IndexOptions).SetSparse(true)},
		{Key: []string{"upload.file_id"}, IndexOptions: new(options.IndexOptions).SetSparse(true)},
		{Key: []string{"session_id", "stream.xfer"}, IndexOptions: new(options.IndexOptions).SetSparse(true)},
	})
	db.EnsureIndexes(context.Background(), minionTaskPartCollection, []opts.IndexModel{
		{Key: []string{"task_id", "seq"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		// The parts with output, for reading it (OutParts).
		{Key: []string{"task_id", "seq"}, IndexOptions: new(options.IndexOptions).SetName("task_id_1_seq_1_out").
			SetPartialFilterExpression(bson.M{"out": bson.M{"$exists": true}})},
	})
	return &minionTaskRepository{tasks: db.Collection(minionTaskCollection), parts: db.Collection(minionTaskPartCollection)}
}

// bumpRev is in every write of a task (models.MinionTask.Rev).
var bumpRev = bson.M{"rev": 1}

func (r *minionTaskRepository) Insert(ctx context.Context, t *models.MinionTask) error {
	t.Rev = 1
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

func (r *minionTaskRepository) Document(ctx context.Context, taskID uuid.UUID) (bson.D, error) {
	var d bson.D
	if err := r.tasks.Find(ctx, bson.M{"task_id": taskID}).One(&d); err != nil {
		return nil, err
	}
	return d, nil
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

func (r *minionTaskRepository) CountRunning(ctx context.Context, sessionID uuid.UUID) (int, error) {
	n, err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "$or": bson.A{
		bson.M{"state": models.MinionTaskRunning},
		bson.M{"state": models.MinionTaskSent, "acked_at": bson.M{"$exists": true}},
	}}).Count()
	return int(n), err
}

func (r *minionTaskRepository) MarkSent(ctx context.Context, taskIDs []uuid.UUID, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	in := uuidsIn(taskIDs)
	if _, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": bson.M{"$in": in}, "state": models.MinionTaskQueued},
		bson.M{"$set": bson.M{"state": models.MinionTaskSent, "sent_at": at}, "$inc": bumpRev}); err != nil {
		return err
	}
	_, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": bson.M{"$in": in}}, bson.M{"$inc": bson.M{"send_count": 1, "rev": 1}})
	return err
}

func (r *minionTaskRepository) Ack(ctx context.Context, sessionID uuid.UUID, taskIDs []uuid.UUID, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	_, err := r.tasks.UpdateAll(ctx,
		bson.M{"session_id": sessionID, "task_id": bson.M{"$in": uuidsIn(taskIDs)}, "acked_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"acked_at": at}, "$inc": bumpRev})
	return err
}

func (r *minionTaskRepository) AddResult(ctx context.Context, sessionID, taskID uuid.UUID, res MinionTaskResult) (*models.MinionTask, bool, error) {
	var t models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "session_id": sessionID}).One(&t); err != nil {
		return nil, false, err
	}
	// After its end a task takes only the parts it lacks before its done
	// part, late; the done part is the last.
	late := t.Finished()
	if late && (t.DoneSeq == nil || res.Seq >= *t.DoneSeq) {
		return &t, false, ErrMinionTaskEnded
	}

	// Output and data share one budget: past it, output is cut and data
	// (whole or none: a CBOR map is not cut) dropped.
	room := max(int64(models.MaxMinionTaskOutput)-t.OutSize-t.DataSize, 0)
	out, data := res.Out, res.Data
	var namedErr error
	if len(data) > 0 && res.Named != nil {
		if named, err := res.Named(t.Result, data); err != nil {
			namedErr = err
		} else {
			data = named
		}
	}
	if len(data) == 0 && res.Compose != nil {
		data = res.Compose(t.Result)
	}
	// A result whose parts each say it whole keeps only the newest, on the
	// task: it takes the budget once, its size, not each part's.
	latest := len(data) > 0 && res.Latest != nil && res.Latest(t.Result)
	if latest {
		room += t.DataSize
	}
	outCut, dataCut := false, false
	if int64(len(out)) > room {
		out, outCut = out[:room], true
	}
	room -= int64(len(out))
	if int64(len(data)) > room {
		data, dataCut = nil, true
	}
	problem := ""
	if len(data) > 0 && t.DataProblem == "" {
		switch {
		case namedErr != nil:
			problem = fmt.Sprintf("part %d: %v", res.Seq, namedErr)
		case res.Check != nil:
			if err := res.Check(t.Result, res.Seq, data); err != nil {
				problem = fmt.Sprintf("part %d: %v", res.Seq, err)
			}
		}
	}

	part := models.MinionTaskPart{TaskID: taskID, Seq: res.Seq, Out: out, DataCBOR: data, At: res.At}
	if latest {
		part.DataCBOR = nil
	}
	if _, err := r.parts.InsertOne(ctx, &part); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return &t, false, nil
		}
		return nil, false, err
	}

	set := bson.M{}
	if outCut {
		set["out_truncated"] = true
	}
	if dataCut {
		set["data_truncated"] = true
	}
	if problem != "" {
		set["data_problem"] = problem
	}
	if !late {
		if t.AckedAt == nil {
			set["acked_at"] = res.At
		}
		if t.StartedAt == nil {
			set["started_at"] = res.At
		}
		set["state"] = models.MinionTaskRunning
	}
	if res.Done && !late {
		set["done_at"] = res.At
		set["done_seq"] = res.Seq
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
	inc := bson.M{"parts": 1, "out_size": len(out), "data_size": len(data), "rev": 1}
	if len(data) > 0 {
		inc["data_parts"] = 1
	}
	if latest {
		delete(inc, "data_size")
		// The newest by seq: a late part does not take an earlier one back.
		if t.LatestDataSeq == nil || res.Seq > *t.LatestDataSeq {
			set["latest_data"], set["latest_data_seq"], set["data_size"] = data, res.Seq, len(data)
		}
	}
	update := bson.M{"$inc": inc}
	if len(set) > 0 {
		update["$set"] = set
	}
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID}).Apply(qmgo.Change{Update: update, ReturnNew: true}, &after)
	if err != nil {
		return nil, false, err
	}
	return &after, true, nil
}

func (r *minionTaskRepository) RequestCancel(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	// Never sent: cancelled now.
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "state": models.MinionTaskQueued}).Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"state": models.MinionTaskCancelled, "done_at": at, "cancel_requested_at": at}, "$inc": bumpRev},
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
		Update:    bson.M{"$set": bson.M{"cancel_requested_at": at}, "$inc": bumpRev},
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
				bson.M{"$set": bson.M{"state": models.MinionTaskCancelled, "done_at": at}, "$inc": bumpRev})
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
		if _, err := r.tasks.UpdateAll(ctx, bson.M{"task_id": bson.M{"$in": uuidsIn(ids)}}, bson.M{"$inc": bson.M{"cancel_sends": 1, "rev": 1}}); err != nil {
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

func (r *minionTaskRepository) OutParts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error) {
	var out []models.MinionTaskPart
	// The partial index of the parts with output: a walk's thousands of
	// parts without are not read.
	err := r.parts.Find(ctx, bson.M{"task_id": taskID, "out": bson.M{"$exists": true}}).Sort("seq").
		Select(bson.M{"seq": 1, "out": 1}).All(&out)
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

func (r *minionTaskRepository) FindByUploadXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionTask, error) {
	var t models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "upload.xfer": xfer}).One(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *minionTaskRepository) UploadAcked(ctx context.Context, taskID uuid.UUID, next int64, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "upload": bson.M{"$exists": true}}).Apply(qmgo.Change{
		Update: bson.M{
			"$set": bson.M{"upload.acked": next, "upload.last_ack_at": at},
			// The first ask sets it; later ones are later.
			"$min": bson.M{"upload.first_ack_at": at},
			"$inc": bumpRev,
		},
		ReturnNew: true,
	}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) UploadFileUse(ctx context.Context, fileID uuid.UUID) (bool, bool, error) {
	named, err := r.tasks.Find(ctx, bson.M{"upload.file_id": fileID}).Count()
	if err != nil || named == 0 {
		return false, false, err
	}
	open, err := r.tasks.Find(ctx, bson.M{"upload.file_id": fileID, "state": bson.M{"$in": bson.A{
		models.MinionTaskQueued, models.MinionTaskSent, models.MinionTaskRunning}}}).Count()
	return open > 0, true, err
}

func (r *minionTaskRepository) SetDataProblem(ctx context.Context, taskID uuid.UUID, problem string) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "data_problem": bson.M{"$exists": false}}).Apply(qmgo.Change{
		Update: bson.M{"$set": bson.M{"data_problem": problem}, "$inc": bumpRev}, ReturnNew: true,
	}, &after)
	if IsNotFound(err) {
		return r.FindByTaskID(ctx, taskID)
	}
	if err != nil {
		return nil, err
	}
	return &after, nil
}

// openStates are the states of a task that has not ended and was sent.
var openStates = bson.A{models.MinionTaskSent, models.MinionTaskRunning}

func (r *minionTaskRepository) StreamAnnounce(ctx context.Context, sessionID, taskID uuid.UUID, st models.MinionTaskStream) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "session_id": sessionID, "result": protocol.ResultFileStream,
		"state":  bson.M{"$in": bson.A{models.MinionTaskQueued, models.MinionTaskSent, models.MinionTaskRunning}},
		"stream": bson.M{"$exists": false}}).Apply(qmgo.Change{
		Update: bson.M{"$set": bson.M{"stream": st}, "$inc": bumpRev}, ReturnNew: true,
	}, &after)
	if IsNotFound(err) {
		t, ferr := r.FindByTaskID(ctx, taskID)
		if ferr != nil || t.SessionID != sessionID || t.Stream == nil || t.Finished() {
			return nil, qmgo.ErrNoSuchDocuments
		}
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) FindByStreamXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionTask, error) {
	var t models.MinionTask
	if err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "stream.xfer": xfer}).One(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *minionTaskRepository) StreamAttach(ctx context.Context, taskID uuid.UUID, reader string, from int64, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "stream": bson.M{"$exists": true}}).Apply(qmgo.Change{
		Update: bson.M{
			"$set": bson.M{"stream.reader": reader, "stream.reader_at": at, "stream.want": from, "stream.delivered": from, "stream.ran": true,
				"stream.from": from, "stream.attached_at": at},
			"$unset": bson.M{"stream.idle_since": ""},
			"$inc":   bson.M{"stream.gen": 1, "rev": 1},
		},
		ReturnNew: true,
	}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) StreamDelivered(ctx context.Context, taskID uuid.UUID, gen, delivered int64, at time.Time) (bool, error) {
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID, "stream.gen": gen},
		bson.M{"$set": bson.M{"stream.delivered": delivered, "stream.reader_at": at}, "$inc": bumpRev})
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *minionTaskRepository) StreamDetach(ctx context.Context, taskID uuid.UUID, gen int64, at time.Time) error {
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID, "stream.gen": gen},
		bson.M{"$set": bson.M{"stream.reader": "", "stream.idle_since": at}, "$inc": bumpRev})
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (r *minionTaskRepository) StreamTook(ctx context.Context, taskID uuid.UUID, gen, off, n int64, sha256 []byte) (bool, error) {
	update := bson.M{"$inc": bson.M{"stream.want": n, "rev": 1}}
	if sha256 != nil {
		update["$set"] = bson.M{"stream.sha256": sha256}
	}
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID, "stream.gen": gen, "stream.want": off}, update)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *minionTaskRepository) ActiveStreams(ctx context.Context, sessionID uuid.UUID) ([]models.MinionTask, error) {
	var out []models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"session_id": sessionID, "state": bson.M{"$in": openStates},
		"$or": bson.A{bson.M{"stream": bson.M{"$exists": true}}, bson.M{"upload.stream": true}}}).All(&out)
	return out, err
}

func (r *minionTaskRepository) IdleStreams(ctx context.Context, before time.Time) ([]models.MinionTask, error) {
	var out []models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"state": bson.M{"$in": openStates},
		"$or": bson.A{
			bson.M{"stream": bson.M{"$exists": true}, "stream.idle_since": bson.M{"$lt": before}},
			bson.M{"stream": bson.M{"$exists": true}, "stream.reader": bson.M{"$ne": ""}, "stream.reader_at": bson.M{"$lt": before}},
			bson.M{"upload.stream": true, "upload.idle_since": bson.M{"$lt": before}},
			bson.M{"upload.stream": true, "upload.uploader": bson.M{"$nin": bson.A{"", nil}}, "upload.uploader_at": bson.M{"$lt": before}},
		}}).All(&out)
	return out, err
}

func (r *minionTaskRepository) UploadAttach(ctx context.Context, taskID uuid.UUID, uploader string, at time.Time) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "upload.stream": true}).Apply(qmgo.Change{
		Update: []bson.M{{"$set": bson.M{
			"upload.uploader": uploader, "upload.uploader_at": at, "upload.attached_at": at,
			"upload.from":       bson.M{"$ifNull": bson.A{"$upload.received", 0}},
			"upload.gen":        bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$upload.gen", 0}}, 1}},
			"upload.idle_since": "$$REMOVE",
			"rev":               bson.M{"$add": bson.A{"$rev", 1}},
		}}},
		ReturnNew: true,
	}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) UploadSize(ctx context.Context, taskID uuid.UUID, size int64) (*models.MinionTask, error) {
	var after models.MinionTask
	err := r.tasks.Find(ctx, bson.M{"task_id": taskID, "upload.stream": true, "upload.sized": bson.M{"$ne": true}}).Apply(qmgo.Change{
		Update: bson.M{"$set": bson.M{"upload.size": size, "upload.sized": true}, "$inc": bumpRev}, ReturnNew: true,
	}, &after)
	if IsNotFound(err) {
		return r.FindByTaskID(ctx, taskID)
	}
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionTaskRepository) UploadReceived(ctx context.Context, taskID uuid.UUID, gen, off, n int64, hashState, sum []byte, at time.Time) (bool, error) {
	set := bson.M{"upload.hash_state": hashState, "upload.uploader_at": at}
	if sum != nil {
		set["upload.sha256"] = sum
	}
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID, "upload.gen": gen, "upload.received": off},
		bson.M{"$set": set, "$inc": bson.M{"upload.received": n, "rev": 1}})
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *minionTaskRepository) UploadDetach(ctx context.Context, taskID uuid.UUID, gen int64, at time.Time) error {
	err := r.tasks.UpdateOne(ctx, bson.M{"task_id": taskID, "upload.gen": gen},
		bson.M{"$set": bson.M{"upload.uploader": "", "upload.idle_since": at}, "$inc": bumpRev})
	if IsNotFound(err) {
		return nil
	}
	return err
}
