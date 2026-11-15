// Package recordingstest holds in-memory fakes of what pkg/recordings
// keeps recordings with: the bucket, the repository, the task's counts.
package recordingstest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/bson"
)

// Store keeps objects in memory; Fail makes the next Fail puts fail.
type Store struct {
	mu   sync.Mutex
	objs map[string][]byte
	fail int
	puts int
}

func NewStore() *Store { return &Store{objs: map[string][]byte{}} }

// Fail makes the next n puts fail.
func (f *Store) Fail(n int) {
	f.mu.Lock()
	f.fail = n
	f.mu.Unlock()
}

// Puts counts the puts tried.
func (f *Store) Puts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts
}

func (f *Store) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	b, _ := io.ReadAll(body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	if f.fail > 0 {
		f.fail--
		return errors.New("store down")
	}
	f.objs[key] = b
	return nil
}

func (f *Store) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objs[key]
	if !ok {
		return nil, blob.ObjectInfo{}, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(b)), blob.ObjectInfo{ContentLength: int64(len(b))}, nil
}

func (f *Store) Head(_ context.Context, key string) (blob.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objs[key]
	if !ok {
		return blob.ObjectInfo{}, errors.New("no such key")
	}
	return blob.ObjectInfo{ContentLength: int64(len(b))}, nil
}

func (f *Store) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objs, key)
	return nil
}

// Keys are the objects kept, in order.
func (f *Store) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.objs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Repo keeps recordings in memory, the fields Save sets as the
// repository sets them.
type Repo struct {
	mu   sync.Mutex
	recs map[uuid.UUID]*models.DesktopRecording
}

func NewRepo() *Repo { return &Repo{recs: map[uuid.UUID]*models.DesktopRecording{}} }

// Len is how many it keeps.
func (f *Repo) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recs)
}

func (f *Repo) Insert(_ context.Context, r *models.DesktopRecording) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *r
	f.recs[r.RecordingID] = &c
	return nil
}

func (f *Repo) FindByID(_ context.Context, id uuid.UUID) (*models.DesktopRecording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	c := *r
	return &c, nil
}

func (f *Repo) ListByTask(_ context.Context, taskID uuid.UUID, before, limit int64) ([]models.DesktopRecording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.DesktopRecording
	for _, r := range f.recs {
		if r.TaskID == taskID && (before == 0 || r.Number < before) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	if int64(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func apply(r *models.DesktopRecording, set bson.M) {
	for k, v := range set {
		switch k {
		case "parts":
			r.Parts = v.(int)
		case "size":
			r.Size = v.(int64)
		case "duration_ms":
			r.DurationMs = v.(int64)
		case "lost":
			r.Lost = v.(int64)
		case "seen_at":
			r.SeenAt = v.(time.Time)
		case "state":
			r.State = v.(string)
		case "end":
			r.End = v.(string)
		case "msg":
			r.Msg = v.(string)
		case "ended_at":
			at := v.(time.Time)
			r.EndedAt = &at
		default:
			panic("a field the fake does not set: " + k)
		}
	}
}

func (f *Repo) Save(_ context.Context, id uuid.UUID, set bson.M) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok {
		return qmgo.ErrNoSuchDocuments
	}
	apply(r, set)
	return nil
}

func (f *Repo) Stale(_ context.Context, before time.Time) ([]models.DesktopRecording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.DesktopRecording
	for _, r := range f.recs {
		if r.State == models.DesktopRecordingRecording && r.SeenAt.Before(before) {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *Repo) EndStale(_ context.Context, id uuid.UUID, before time.Time, set bson.M) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok || r.State != models.DesktopRecordingRecording || !r.SeenAt.Before(before) {
		return false, nil
	}
	apply(r, set)
	return true, nil
}

func (f *Repo) ListByOperation(_ context.Context, op uuid.UUID) ([]models.DesktopRecording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.DesktopRecording
	for _, r := range f.recs {
		if r.OperationID == op {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *Repo) DeleteByOperationID(_ context.Context, op uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, r := range f.recs {
		if r.OperationID == op {
			delete(f.recs, id)
		}
	}
	return nil
}

func (f *Repo) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs, id)
	return nil
}

// Tasks counts one task's recordings.
type Tasks struct {
	mu   sync.Mutex
	task models.MinionTask
}

func NewTasks(task models.MinionTask) *Tasks { return &Tasks{task: task} }

func (f *Tasks) AddRecording(_ context.Context, taskID uuid.UUID, kept, made int) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if taskID != f.task.TaskID {
		return nil, qmgo.ErrNoSuchDocuments
	}
	f.task.Recordings += kept
	f.task.RecordingsMade += int64(made)
	t := f.task
	return &t, nil
}

// Counts are the task's recordings kept, and made.
func (f *Tasks) Counts() (int, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.task.Recordings, f.task.RecordingsMade
}
