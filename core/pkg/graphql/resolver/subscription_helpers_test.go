package resolver

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/vektah/gqlparser/v2/ast"
	"go.uber.org/zap"
)

var errOperationNotFound = errors.New("operation not found")

// fakeOperationRepo is a minimal stub of repository.IOperationRepository for
// subscription_helpers tests. Only FindByID and FindByMemberID are exercised
// — every other method panics so a misrouted call shows up immediately
// instead of returning a misleading zero value.
type fakeOperationRepo struct {
	// findByID is keyed on the stringified UUID. Public (00000000-0000-0000-
	// -0000-000000000001) is short-circuited like the real repo, so callers
	// don't need to register it.
	findByID       map[string]models.Operation
	findByMemberID func(userID uuid.UUID) []models.Operation
}

func (f *fakeOperationRepo) FindByID(_ context.Context, id uuid.UUID) (models.Operation, error) {
	// Mirror the real repository's Public short-circuit so tests don't
	// have to register the synthetic operation explicitly.
	if models.IsPublicOperation(id) {
		return models.SynthesizePublicOperation(), nil
	}
	op, ok := f.findByID[id.String()]
	if !ok {
		return models.Operation{}, errOperationNotFound
	}
	return op, nil
}

func (f *fakeOperationRepo) FindByMemberID(_ context.Context, userID uuid.UUID) ([]models.Operation, error) {
	if f.findByMemberID == nil {
		return nil, nil
	}
	return f.findByMemberID(userID), nil
}

func (f *fakeOperationRepo) Create(context.Context, *models.Operation) error { panic("unused") }
func (f *fakeOperationRepo) FindAll(context.Context, string, int64, int64, *uuid.UUID) ([]models.Operation, error) {
	panic("unused")
}
func (f *fakeOperationRepo) FindWithCursor(context.Context, string, repository.OperationSort, *pagination.Cursor, int64, bool, *uuid.UUID) ([]models.Operation, error) {
	panic("unused")
}
func (f *fakeOperationRepo) Count(context.Context, string, *uuid.UUID) (int64, error) {
	panic("unused")
}
func (f *fakeOperationRepo) Update(context.Context, *models.Operation, map[string]interface{}) error {
	panic("unused")
}
func (f *fakeOperationRepo) Delete(context.Context, *models.Operation) error { panic("unused") }
func (f *fakeOperationRepo) AddMember(context.Context, uuid.UUID, uuid.UUID, models.OperationRole) error {
	panic("unused")
}
func (f *fakeOperationRepo) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error {
	panic("unused")
}
func (f *fakeOperationRepo) UpdateMemberRole(context.Context, uuid.UUID, uuid.UUID, models.OperationRole) error {
	panic("unused")
}
func (f *fakeOperationRepo) RemoveMemberSafe(context.Context, uuid.UUID, uuid.UUID) error {
	panic("unused")
}
func (f *fakeOperationRepo) UpdateMemberRoleSafe(context.Context, uuid.UUID, uuid.UUID, models.OperationRole) error {
	panic("unused")
}

func newResolverForTest(repo repository.IOperationRepository) *subscriptionResolver {
	return &subscriptionResolver{Resolver: &Resolver{OperationRepo: repo}}
}

// authedCtx returns a context carrying the AuthInfo for userID with the given
// roles, plus the AuthInfo itself. Helpers in subscription_helpers.go take
// auth as a parameter but AuthorizeOperationRole reads it from the context —
// both must point at the same caller, so we build them together.
func authedCtx(userID string, roles ...string) (context.Context, gqlctx.AuthInfo) {
	info := gqlctx.AuthInfo{UserID: userID, Roles: roles}
	return gqlctx.WithAuthInfo(context.Background(), info), info
}

// publicOpEvent returns an event scoped to the Public operation. Used to
// exercise the filter's per-event acceptance decision.
func publicOpEvent(topic eventbus.Topic) eventbus.Event {
	return eventbus.Event{
		Topic: topic,
		Payload: eventbus.WikiDocumentEventPayload{
			OperationID: models.PublicOperationID.String(),
			DocumentID:  uuid.New().String(),
		},
	}
}

// TestBuildOperationFilter_PublicAllowsNonAdmin guards the original bug:
// before the fix, a non-admin caller subscribing to the synthetic Public
// operation by id was rejected with "forbidden: not a member of this
// operation", because Public has no explicit Mongo membership. Admins
// short-circuited the check and worked. The fix routes through
// authorization.AuthorizeOperationRole which honors Public's implicit-
// operator rule for every authenticated caller.
func TestBuildOperationFilter_PublicAllowsNonAdmin(t *testing.T) {
	r := newResolverForTest(&fakeOperationRepo{})
	ctx, auth := authedCtx(uuid.NewString(), "user")

	pub := models.PublicOperationID.String()
	filter, err := r.buildOperationFilter(ctx, auth, &pub)
	if err != nil {
		t.Fatalf("non-admin subscribing to Public should be allowed, got error: %v", err)
	}
	if filter == nil {
		t.Fatal("expected non-nil filter")
	}
	if !filter(publicOpEvent(eventbus.TopicWikiDocumentUpdated)) {
		t.Fatal("filter should accept events scoped to the Public operation")
	}
}

// TestBuildOperationFilter_PublicAllowsAdmin keeps the admin path covered
// — admins were already working, and they should keep working after the
// rewrite.
func TestBuildOperationFilter_PublicAllowsAdmin(t *testing.T) {
	r := newResolverForTest(&fakeOperationRepo{})
	ctx, auth := authedCtx(uuid.NewString(), "admin")

	pub := models.PublicOperationID.String()
	filter, err := r.buildOperationFilter(ctx, auth, &pub)
	if err != nil {
		t.Fatalf("admin subscribing to Public should be allowed, got error: %v", err)
	}
	if !filter(publicOpEvent(eventbus.TopicWikiDocumentUpdated)) {
		t.Fatal("admin filter should accept events scoped to the Public operation")
	}
}

// TestBuildOperationFilter_NonMemberRejected ensures we did not over-rotate
// the auth path — a regular non-admin asking for an operation they don't
// belong to should still be rejected.
func TestBuildOperationFilter_NonMemberRejected(t *testing.T) {
	opID := uuid.New()
	otherUser := uuid.New()
	r := newResolverForTest(&fakeOperationRepo{
		findByID: map[string]models.Operation{
			opID.String(): {
				OperationID: opID,
				Name:        "private",
				Members: []models.OperationMember{
					{UserID: otherUser, Role: models.OperationRoleOperator},
				},
			},
		},
	})

	ctx, auth := authedCtx(uuid.NewString(), "user")
	target := opID.String()

	if _, err := r.buildOperationFilter(ctx, auth, &target); err == nil {
		t.Fatal("non-member non-admin should be rejected on a private operation")
	} else if !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden error, got: %v", err)
	}
}

// TestBuildOperationFilter_NilIncludesPublic guards the second half of the
// fix: when the caller omits operationID, the membership snapshot built
// from FindByMemberID has to include the synthetic Public operation,
// otherwise Public-scoped events are silently dropped for non-admins.
func TestBuildOperationFilter_NilIncludesPublic(t *testing.T) {
	callerID := uuid.New()
	r := newResolverForTest(&fakeOperationRepo{
		// caller has no explicit memberships
		findByMemberID: func(uuid.UUID) []models.Operation { return nil },
	})

	ctx, auth := authedCtx(callerID.String(), "user")
	filter, err := r.buildOperationFilter(ctx, auth, nil)
	if err != nil {
		t.Fatalf("nil operationID branch should succeed: %v", err)
	}
	if !filter(publicOpEvent(eventbus.TopicWikiDocumentUpdated)) {
		t.Fatal("nil-operationID filter must deliver Public-scoped events")
	}
}

// TestBuildOperationsFilter_ExplicitPublicAllowsNonAdmin mirrors the
// single-op test for the multi-op helper used by myCredentialChanged.
func TestBuildOperationsFilter_ExplicitPublicAllowsNonAdmin(t *testing.T) {
	r := newResolverForTest(&fakeOperationRepo{})
	ctx, auth := authedCtx(uuid.NewString(), "user")

	pub := models.PublicOperationID.String()
	filter, err := r.buildOperationsFilter(ctx, auth, []string{pub})
	if err != nil {
		t.Fatalf("non-admin with Public in opIDs should be allowed, got error: %v", err)
	}
	if !filter(publicOpEvent(eventbus.TopicWikiDocumentUpdated)) {
		t.Fatal("filter should accept events scoped to Public when Public is in opIDs")
	}
}

// TestBuildOperationsFilter_NilIncludesPublic mirrors the nil-branch fix
// for the multi-op helper.
func TestBuildOperationsFilter_NilIncludesPublic(t *testing.T) {
	r := newResolverForTest(&fakeOperationRepo{
		findByMemberID: func(uuid.UUID) []models.Operation { return nil },
	})

	ctx, auth := authedCtx(uuid.NewString(), "user")
	filter, err := r.buildOperationsFilter(ctx, auth, nil)
	if err != nil {
		t.Fatalf("nil opIDs branch should succeed: %v", err)
	}
	if !filter(publicOpEvent(eventbus.TopicWikiDocumentUpdated)) {
		t.Fatal("nil-opIDs filter must deliver Public-scoped events")
	}
}

// countingDocRepo stands in for the wiki repository, counting the document
// reads a subscription makes. Every other method panics via the nil embed.
type countingDocRepo struct {
	repository.IWikiDocumentRepository
	reads atomic.Int32
}

func (f *countingDocRepo) FindByIDs(_ context.Context, ids []uuid.UUID) ([]models.WikiDocument, error) {
	f.reads.Add(1)
	docs := make([]models.WikiDocument, len(ids))
	for i, id := range ids {
		docs[i] = models.WikiDocument{DocumentID: id, OperationID: models.PublicOperationID}
	}
	return docs, nil
}

// withSelection attaches the operation and field context gqlgen would, with
// the given fields selected on the subscription's event type.
func withSelection(ctx context.Context, fields ...string) context.Context {
	sel := make(ast.SelectionSet, len(fields))
	for i, name := range fields {
		sel[i] = &ast.Field{Name: name, Alias: name}
	}
	ctx = graphql.WithOperationContext(ctx, &graphql.OperationContext{})
	return graphql.WithFieldContext(ctx, &graphql.FieldContext{
		Field: graphql.CollectedField{Selections: sel},
	})
}

// TestWikiDocumentChanged_ReadsDocumentOnlyWhenSelected pins the per-event
// cost of the subscription. Every connected client runs its own read on every
// save — and Hocuspocus saves every couple of seconds while someone types —
// so a client that does not select `document` must not trigger one.
func TestWikiDocumentChanged_ReadsDocumentOnlyWhenSelected(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection []string
		wantReads int32
	}{
		{"ids only", []string{"action", "documentId", "parentDocumentId"}, 0},
		{"with document", []string{"action", "documentId", "document"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.NewEventBus(zap.NewNop())
			bus.Start()
			docs := &countingDocRepo{}
			r := newResolverForTest(&fakeOperationRepo{})
			r.EventBus = bus
			r.WikiDocumentRepo = docs

			authed, _ := authedCtx(uuid.NewString(), "user")
			ctx, cancel := context.WithCancel(withSelection(authed, tc.selection...))
			ch, err := r.wikiDocumentChanged(ctx, models.PublicOperationID.String())
			if err != nil {
				cancel()
				t.Fatalf("subscribe: %v", err)
			}
			t.Cleanup(func() {
				cancel()
				bus.Stop(context.Background())
			})

			bus.Publish(publicOpEvent(eventbus.TopicWikiDocumentUpdated))
			select {
			case evt := <-ch:
				if got := docs.reads.Load(); got != tc.wantReads {
					t.Errorf("document reads = %d, want %d", got, tc.wantReads)
				}
				if got, want := evt.Document != nil, tc.wantReads > 0; got != want {
					t.Errorf("event carries a document = %v, want %v", got, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no event delivered")
			}
		})
	}
}

// capturingBus hands the test the handler a subscription registers, and
// signals when the subscription lets go of the bus.
type capturingBus struct {
	eventbus.IEventBus
	handler      eventbus.Handler
	unsubscribed chan struct{}
}

func (b *capturingBus) Subscribe(_ []eventbus.Topic, h eventbus.Handler, _ ...eventbus.Filter) func() {
	b.handler = h
	return func() { close(b.unsubscribed) }
}

// TestStream_SurvivesAnEventInFlightAtDisconnect pins the race every
// subscription resolver used to have: they closed the client channel right
// after unsubscribing, while the bus could still be running the handler for
// an event it had already dispatched — and a send on the closed channel
// panicked. The bus recovered it, as an error in the log.
func TestStream_SurvivesAnEventInFlightAtDisconnect(t *testing.T) {
	bus := &capturingBus{unsubscribed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	ch := stream(ctx, bus, nil, nil, func(_ context.Context, e eventbus.Event) *string {
		if e.Topic == "skip" {
			return nil
		}
		topic := string(e.Topic)
		return &topic
	})

	bus.handler(context.Background(), eventbus.Event{Topic: "skip"})
	bus.handler(context.Background(), eventbus.Event{Topic: "first"})
	if got := <-ch; *got != "first" {
		t.Fatalf("first payload = %q, want the event after the skipped one", *got)
	}

	cancel()
	<-bus.unsubscribed
	time.Sleep(20 * time.Millisecond) // anything the stream does after unsubscribing, it has done

	// Late deliveries must neither panic nor find the channel closed.
	for range 20 {
		bus.handler(context.Background(), eventbus.Event{Topic: "late"})
	}
	for {
		select {
		case v, open := <-ch:
			if !open {
				t.Fatal("the stream closed the client channel; a send racing that close panics")
			}
			if *v != "late" {
				t.Fatalf("unexpected payload %q", *v)
			}
			continue
		default:
		}
		break
	}
}
