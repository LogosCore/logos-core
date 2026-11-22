package resolver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/storage"
)

// IStorageResolver is the Storage page's (storage.graphql): the object store
// as core last read it, what core keeps in each bucket, and its maintenance.
// The schema's @hasPermission keeps it to admins (storage:read,
// storage:manage).
type IStorageResolver interface {
	StorageStatus(ctx context.Context) (*model.StorageStatus, error)
	StorageConsumers(ctx context.Context, bucket string, search *string, first *int, after *string) (*model.StorageConsumerConnection, error)
	StorageActions(ctx context.Context, first *int, after *string, last *int, before *string) (*model.StorageActionConnection, error)
	ReclaimStorage(ctx context.Context) (*model.StorageAction, error)
	GrowStorageBucket(ctx context.Context, bucket string) (*model.StorageAction, error)
}

// StorageService is pkg/storage's service, as the resolver uses it.
type StorageService interface {
	Status(ctx context.Context) storage.Snapshot
	Consumers(ctx context.Context, bucket, search, after string, first int) (storage.ConsumerPage, error)
	Actions(ctx context.Context, cursor *pagination.Cursor, limit int64, forward bool) ([]models.StorageAction, int64, error)
	Reclaim(ctx context.Context, actor models.ModuleActor) (*models.StorageAction, error)
	Grow(ctx context.Context, actor models.ModuleActor, bucket string) (*models.StorageAction, error)
}

type storageResolver struct{ svc StorageService }

func NewStorageResolver(svc StorageService) IStorageResolver { return &storageResolver{svc: svc} }

func (r *storageResolver) StorageStatus(ctx context.Context) (*model.StorageStatus, error) {
	snap := r.svc.Status(ctx)
	sw := snap.SeaweedFS
	out := &model.StorageStatus{At: snap.At.UTC().Format(time.RFC3339Nano), RecordsError: strPtr(snap.RecordsErr),
		Seaweedfs: &model.StorageSeaweedFs{Configured: sw.Configured, Reachable: sw.Reachable, Error: strPtr(sw.Err),
			Version: strPtr(sw.Version), Leader: strPtr(sw.Leader), Slots: sw.Slots, FreeSlots: sw.FreeSlots},
		Disks: []*model.StorageDisk{}, Buckets: []*model.StorageBucket{}, Warnings: []*model.StorageWarning{}}
	for _, d := range snap.Disks {
		out.Disks = append(out.Disks, &model.StorageDisk{Node: d.Node, Dir: strPtr(d.Dir), Error: strPtr(d.Err), Total: int(d.Total),
			Used: int(d.Used), Free: int(d.Free), Volumes: d.Volumes, MaxVolumes: d.MaxVolumes})
	}
	for _, b := range snap.Buckets {
		purposes := b.Purposes
		if purposes == nil {
			purposes = []string{}
		}
		out.Buckets = append(out.Buckets, &model.StorageBucket{Name: b.Name, Purposes: purposes, Volumes: b.Volumes,
			WritableVolumes: b.Writable, ReadOnlyVolumes: b.ReadOnly, Size: int(b.Size), DeletedBytes: int(b.Deleted),
			Objects: int(b.Objects), ObjectBytes: int(b.ObjectBytes)})
	}
	for _, w := range snap.Warnings {
		bucket := strPtr(w.Bucket)
		if w.Code == "reclaimable" || w.Code == "bucket_not_writable" {
			// SeaweedFS's own files are a bucket named "".
			bucket = &w.Bucket
		}
		out.Warnings = append(out.Warnings, &model.StorageWarning{Level: w.Level, Code: w.Code, Bucket: bucket, Message: w.Message})
	}
	return out, nil
}

func (r *storageResolver) StorageConsumers(ctx context.Context, bucket string, search *string, first *int, after *string) (*model.StorageConsumerConnection, error) {
	limit := 50
	if first != nil {
		if *first <= 0 {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "'first' must be positive")
		}
		limit = min(*first, 500)
	}
	q, from := "", ""
	if search != nil {
		q = *search
	}
	if after != nil {
		from = *after
	}
	page, err := r.svc.Consumers(ctx, bucket, q, from, limit)
	if errors.Is(err, storage.ErrBadCursor) {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
	}
	if err != nil {
		return nil, err
	}
	out := &model.StorageConsumerConnection{Edges: []*model.StorageConsumerEdge{}, PageInfo: &pagination.PageInfo{
		HasNextPage: page.More, HasPreviousPage: from != ""}, TotalCount: page.Total}
	for _, c := range page.Consumers {
		node := &model.StorageConsumer{Purpose: c.Purpose, Kind: c.Kind, Objects: int(c.Objects), Bytes: int(c.Bytes),
			OperationName: strPtr(c.OperationName)}
		if c.OperationID != uuid.Nil {
			id := c.OperationID.String()
			node.OperationID = &id
		}
		cursor := storage.ConsumerCursor(c)
		out.Edges = append(out.Edges, &model.StorageConsumerEdge{Node: node, Cursor: cursor})
	}
	if n := len(out.Edges); n > 0 {
		out.PageInfo.StartCursor, out.PageInfo.EndCursor = &out.Edges[0].Cursor, &out.Edges[n-1].Cursor
	}
	return out, nil
}

func (r *storageResolver) StorageActions(ctx context.Context, first *int, after *string, last *int, before *string) (*model.StorageActionConnection, error) {
	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("invalid pagination args: %v", err))
	}
	rows, total, err := r.svc.Actions(ctx, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, info := pagination.BuildEdges(rows, args,
		func(a *models.StorageAction) string { return repository.StorageActionCursor(a) },
		func(a *models.StorageAction, cursor string) *model.StorageActionEdge {
			return &model.StorageActionEdge{Node: storageActionModel(a), Cursor: cursor}
		})
	return &model.StorageActionConnection{Edges: edges, PageInfo: &info, TotalCount: int(total)}, nil
}

func (r *storageResolver) ReclaimStorage(ctx context.Context) (*model.StorageAction, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	a, err := r.svc.Reclaim(ctx, managementActor(auth))
	if err != nil {
		return nil, storageError(ctx, err)
	}
	return storageActionModel(a), nil
}

func (r *storageResolver) GrowStorageBucket(ctx context.Context, bucket string) (*model.StorageAction, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	a, err := r.svc.Grow(ctx, managementActor(auth), bucket)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	return storageActionModel(a), nil
}

// storageError is an action's refusal as GraphQL says it.
func storageError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, storage.ErrNotConfigured), errors.Is(err, storage.ErrUnknownBucket):
		return moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	case errors.Is(err, storage.ErrRunning):
		return moduleGQLError(ctx, errCodeConflict, err.Error())
	}
	return err
}

// storageActionModel is an action as GraphQL has it.
func storageActionModel(a *models.StorageAction) *model.StorageAction {
	return &model.StorageAction{ID: a.ActionID.String(), Action: a.Action, Bucket: strPtr(a.Bucket),
		Actor:  &model.ModuleActor{Type: a.Actor.Type, ID: a.Actor.ID, Name: strPtr(a.Actor.Name)},
		Status: a.Status, Error: strPtr(a.Err), StartedAt: a.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: taskTime(a.FinishedAt),
		Freed: int(a.Freed), Volumes: a.Volumes}
}
