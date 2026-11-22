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
	StorageRetention(ctx context.Context) (*model.StorageRetention, error)
	StorageCleanupPreview(ctx context.Context, olderThanDays int, first *int, after *string) (*model.StorageCleanupPreview, error)
	SetStorageRetention(ctx context.Context, enabled bool, olderThanDays int) (*model.StorageRetention, error)
	CleanUpStorage(ctx context.Context, olderThanDays int, operationID *string) (*model.StorageAction, error)
}

// StorageService is pkg/storage's service, as the resolver uses it.
type StorageService interface {
	Status(ctx context.Context) storage.Snapshot
	Consumers(ctx context.Context, bucket, search, after string, first int) (storage.ConsumerPage, error)
	Actions(ctx context.Context, cursor *pagination.Cursor, limit int64, forward bool) ([]models.StorageAction, int64, error)
	Reclaim(ctx context.Context, actor models.ModuleActor) (*models.StorageAction, error)
	Grow(ctx context.Context, actor models.ModuleActor, bucket string) (*models.StorageAction, error)
	Retention(ctx context.Context) (storage.Retention, error)
	SetRetention(ctx context.Context, actor models.ModuleActor, enabled bool, days int) (storage.Retention, error)
	CleanupPreview(ctx context.Context, days int, after string, first int) (storage.CleanupPreview, error)
	CleanUp(ctx context.Context, actor models.ModuleActor, days int, operationID *uuid.UUID) (*models.StorageAction, error)
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

func (r *storageResolver) StorageRetention(ctx context.Context) (*model.StorageRetention, error) {
	ret, err := r.svc.Retention(ctx)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	return storageRetentionModel(ret), nil
}

func (r *storageResolver) StorageCleanupPreview(ctx context.Context, olderThanDays int, first *int, after *string) (*model.StorageCleanupPreview, error) {
	limit := 50
	if first != nil {
		if *first <= 0 {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "'first' must be positive")
		}
		limit = min(*first, 500)
	}
	from := ""
	if after != nil {
		from = *after
	}
	p, err := r.svc.CleanupPreview(ctx, olderThanDays, from, limit)
	if errors.Is(err, storage.ErrBadCursor) {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
	}
	if err != nil {
		return nil, storageError(ctx, err)
	}
	ops := &model.StorageCleanupOperationConnection{Edges: []*model.StorageCleanupOperationEdge{},
		PageInfo: &pagination.PageInfo{HasNextPage: p.More, HasPreviousPage: from != ""}, TotalCount: p.Total}
	for _, o := range p.Operations {
		node := &model.StorageCleanupOperation{OperationID: o.OperationID.String(), OperationName: strPtr(o.OperationName),
			Recordings: int(o.Recordings), Bytes: int(o.Bytes)}
		ops.Edges = append(ops.Edges, &model.StorageCleanupOperationEdge{Node: node, Cursor: storage.CleanupCursor(o)})
	}
	if n := len(ops.Edges); n > 0 {
		ops.PageInfo.StartCursor, ops.PageInfo.EndCursor = &ops.Edges[0].Cursor, &ops.Edges[n-1].Cursor
	}
	return &model.StorageCleanupPreview{OlderThanDays: p.OlderThanDays, Before: p.Before.UTC().Format(time.RFC3339Nano),
		Recordings: int(p.Recordings), Bytes: int(p.Bytes), Operations: ops}, nil
}

func (r *storageResolver) SetStorageRetention(ctx context.Context, enabled bool, olderThanDays int) (*model.StorageRetention, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	ret, err := r.svc.SetRetention(ctx, managementActor(auth), enabled, olderThanDays)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	return storageRetentionModel(ret), nil
}

func (r *storageResolver) CleanUpStorage(ctx context.Context, olderThanDays int, operationID *string) (*model.StorageAction, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	var op *uuid.UUID
	if operationID != nil {
		id, err := uuid.Parse(*operationID)
		if err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
		}
		op = &id
	}
	a, err := r.svc.CleanUp(ctx, managementActor(auth), olderThanDays, op)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	return storageActionModel(a), nil
}

// storageError is an action's refusal as GraphQL says it.
func storageError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, storage.ErrNotConfigured), errors.Is(err, storage.ErrUnknownBucket), errors.Is(err, storage.ErrBadAge),
		errors.Is(err, storage.ErrUnknownOperation):
		return moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	case errors.Is(err, storage.ErrRunning):
		return moduleGQLError(ctx, errCodeConflict, err.Error())
	}
	return err
}

// storageActionModel is an action as GraphQL has it.
func storageActionModel(a *models.StorageAction) *model.StorageAction {
	m := &model.StorageAction{ID: a.ActionID.String(), Action: a.Action, Bucket: strPtr(a.Bucket),
		Actor:  &model.ModuleActor{Type: a.Actor.Type, ID: a.Actor.ID, Name: strPtr(a.Actor.Name)},
		Status: a.Status, Error: strPtr(a.Err), StartedAt: a.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: taskTime(a.FinishedAt),
		Freed: int(a.Freed), Volumes: a.Volumes, Before: taskTime(a.Before), OperationName: strPtr(a.OperationName),
		Recordings: int(a.Recordings), Bytes: int(a.Bytes)}
	if a.OlderThanDays > 0 {
		days := a.OlderThanDays
		m.OlderThanDays = &days
	}
	if a.OperationID != nil {
		id := a.OperationID.String()
		m.OperationID = &id
	}
	return m
}

// storageRetentionModel is the retention policy as GraphQL has it.
func storageRetentionModel(r storage.Retention) *model.StorageRetention {
	m := &model.StorageRetention{Enabled: r.Policy.Enabled, OlderThanDays: r.Policy.OlderThanDays, UpdatedAt: taskTime(r.Policy.UpdatedAt),
		NextRunAt: taskTime(r.NextRun)}
	if by := r.Policy.UpdatedBy; by != nil {
		m.UpdatedBy = &model.ModuleActor{Type: by.Type, ID: by.ID, Name: strPtr(by.Name)}
	}
	if r.LastRun != nil {
		m.LastRun = storageActionModel(r.LastRun)
	}
	return m
}
