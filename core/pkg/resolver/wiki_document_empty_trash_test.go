package resolver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// trashSweep records what EmptyWikiDocumentTrash asks of each repository.
// Every method it does not implement panics through the nil embeds — the old
// whole-document listing among them.
type trashSweep struct {
	trashed []uuid.UUID
	// Per-call id lists, in call order.
	backupDeletes, visitDeletes, hardDeletes, taskPulls [][]uuid.UUID
}

type sweepDocs struct {
	repository.IWikiDocumentRepository
	s *trashSweep
}

func (f sweepDocs) FindTrashedIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return f.s.trashed, nil
}

func (f sweepDocs) HardDeleteTrashedByIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	f.s.hardDeletes = append(f.s.hardDeletes, ids)
	return nil
}

type sweepBackups struct {
	repository.IWikiDocumentBackupRepository
	s *trashSweep
}

func (f sweepBackups) DeleteByDocumentIDs(_ context.Context, ids []uuid.UUID) error {
	f.s.backupDeletes = append(f.s.backupDeletes, ids)
	return nil
}

type sweepVisits struct {
	repository.IWikiDocumentVisitRepository
	s *trashSweep
}

func (f sweepVisits) DeleteByDocumentIDs(_ context.Context, ids []uuid.UUID) error {
	f.s.visitDeletes = append(f.s.visitDeletes, ids)
	return nil
}

type sweepTasks struct {
	repository.ITaskRepository
	s *trashSweep
}

func (f sweepTasks) PullWikiReferences(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	f.s.taskPulls = append(f.s.taskPulls, ids)
	return nil
}

// countingBus counts published events.
type countingBus struct {
	eventbus.IEventBus
	published int
}

func (b *countingBus) Publish(eventbus.Event) { b.published++ }

func newTrashSweep(t *testing.T, trashed int) (*wikiDocumentResolver, *trashSweep, *countingBus, context.Context, uuid.UUID) {
	t.Helper()
	caller, opID := uuid.New(), uuid.New()
	s := &trashSweep{}
	for range trashed {
		s.trashed = append(s.trashed, uuid.New())
	}
	bus := &countingBus{}
	r := &wikiDocumentResolver{
		docRepo:    sweepDocs{s: s},
		backupRepo: sweepBackups{s: s},
		visitRepo:  sweepVisits{s: s},
		taskRepo:   sweepTasks{s: s},
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) {
			return memberOp(opID, caller, models.OperationRoleAdmin), nil
		}},
		eventBus: bus,
	}
	return r, s, bus, newCallerCtx(caller), opID
}

// TestEmptyWikiDocumentTrash_SweepsInChunks empties a trash spanning several
// chunks: every trashed id reaches every cleanup, a chunk at a time, and the
// documents deleted are exactly the ones cleaned up after. The sweep used to
// cost two round trips per document.
func TestEmptyWikiDocumentTrash_SweepsInChunks(t *testing.T) {
	r, s, bus, ctx, opID := newTrashSweep(t, 2*emptyTrashChunk+500)

	ok, err := r.EmptyWikiDocumentTrash(ctx, opID.String())
	if err != nil || !ok {
		t.Fatalf("EmptyWikiDocumentTrash = %v, %v", ok, err)
	}

	for name, calls := range map[string][][]uuid.UUID{
		"backup deletes": s.backupDeletes,
		"visit deletes":  s.visitDeletes,
		"hard deletes":   s.hardDeletes,
		"task pulls":     s.taskPulls,
	} {
		if len(calls) != 3 {
			t.Errorf("%s: %d calls for %d documents, want 3 chunks", name, len(calls), len(s.trashed))
		}
		var all []uuid.UUID
		for _, chunk := range calls {
			if len(chunk) > emptyTrashChunk {
				t.Errorf("%s: a chunk of %d ids exceeds %d", name, len(chunk), emptyTrashChunk)
			}
			all = append(all, chunk...)
		}
		if !sameUUIDSet(all, s.trashed) {
			t.Errorf("%s covered %d ids, want every one of the %d trashed", name, len(all), len(s.trashed))
		}
	}
	if bus.published != 1 {
		t.Errorf("published %d events, want 1", bus.published)
	}
}

func TestEmptyWikiDocumentTrash_NothingToDo(t *testing.T) {
	r, s, bus, ctx, opID := newTrashSweep(t, 0)

	if ok, err := r.EmptyWikiDocumentTrash(ctx, opID.String()); err != nil || !ok {
		t.Fatalf("EmptyWikiDocumentTrash = %v, %v", ok, err)
	}
	if len(s.hardDeletes)+len(s.backupDeletes) != 0 || bus.published != 0 {
		t.Errorf("an empty trash wrote %d times and published %d events, want neither",
			len(s.hardDeletes)+len(s.backupDeletes), bus.published)
	}
}
