package resolver

// Helper functions for subscription resolvers.
// These live in a separate file because gqlgen manages *.resolvers.go files
// and moves non-resolver functions to a "deleted code" comment block on regen.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// buildOperationFilter creates an event bus filter for operation-scoped subscriptions.
// If operationID is provided, filters to that single operation.
// Otherwise, fetches the caller's operations and filters to that set.
//
// Authorization goes through authorization.AuthorizeOperationRole so the
// synthetic Public operation (which has no Mongo row and no explicit members)
// is correctly treated as implicit-operator for any authenticated caller.
// Re-implementing the membership check inline here previously caused Public
// subscriptions to fail for non-admins — the helper short-circuits Public,
// the inline loop did not.
func (r *subscriptionResolver) buildOperationFilter(ctx context.Context, auth gqlctx.AuthInfo, operationID *string) (eventbus.Filter, error) {
	if operationID != nil && *operationID != "" {
		target := *operationID

		opID, err := uuid.Parse(target)
		if err != nil {
			return nil, fmt.Errorf("invalid operation ID")
		}
		op, err := r.OperationRepo.FindByID(ctx, opID)
		if err != nil {
			return nil, fmt.Errorf("operation not found")
		}
		if err := authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleViewer); err != nil {
			return nil, err
		}

		// Track whether the subscriber's membership was revoked mid-subscription.
		// Safe to mutate — the filter is only called from a single dispatch goroutine.
		revoked := false
		return func(event eventbus.Event) bool {
			if extractOperationID(event) != target {
				return false
			}
			// If this user was removed from the operation, deliver the removal
			// event (so the client can react) then stop all future events.
			if p, ok := event.Payload.(eventbus.OperationMemberPayload); ok &&
				p.MemberID == auth.UserID &&
				event.Topic == eventbus.TopicOperationMemberRemoved {
				revoked = true
				return true
			}
			return !revoked
		}, nil
	}

	// Fetch the caller's operations to build a membership set.
	// This set is captured once at subscribe time — if the caller joins
	// a new operation, they need to reconnect to receive events for it.
	userID, err := uuid.Parse(auth.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}

	ops, err := r.OperationRepo.FindByMemberID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch operations: %w", err)
	}

	// +1 for the synthetic Public tree, which has no Mongo row and is never
	// returned by FindByMemberID but is readable by every authenticated user
	// (implicit operator). It must deliver wiki document-changed events for
	// live collab.
	opSet := make(map[string]struct{}, len(ops)+1)
	for _, op := range ops {
		opSet[op.OperationID.String()] = struct{}{}
	}
	opSet[models.PublicOperationID.String()] = struct{}{}

	return func(event eventbus.Event) bool {
		// Always deliver events triggered by the subscriber themselves
		// (e.g., they just created an operation that isn't in the snapshot yet).
		if event.Actor.Type == eventbus.ActorUser && event.Actor.ID == auth.UserID {
			return true
		}

		// If this is a member event targeting the subscriber (someone added/removed them),
		// pass it through and update the membership snapshot so future events for that
		// operation are also delivered (or stopped). Safe to mutate opSet here — the filter
		// is only called from the single dispatch() goroutine.
		if p, ok := event.Payload.(eventbus.OperationMemberPayload); ok && p.MemberID == auth.UserID {
			if event.Topic == eventbus.TopicOperationMemberAdded {
				opSet[p.OperationID] = struct{}{}
			} else if event.Topic == eventbus.TopicOperationMemberRemoved {
				delete(opSet, p.OperationID)
			}
			return true
		}

		_, ok := opSet[extractOperationID(event)]
		return ok
	}, nil
}

// myCredentialChangedOpCap mirrors the limit in pkg/resolver.myCredentialsOpCap.
// The two values intentionally agree — a client that can fetch myCredentials
// for N operations should also be able to subscribe to that same N.
// Bumping one without the other will silently desync the two surfaces.
const myCredentialChangedOpCap = 100

// buildOperationsFilter is the multi-op sibling of buildOperationFilter, built
// for subscriptions that span several operations at once (myCredentialChanged).
// Same nil/empty/explicit semantics as resolveAccessibleOperationIDs in the
// myCredentials query resolver:
//
//   - opIDs == nil  → caller's full membership set, captured once at subscribe
//     time (same trade-off as buildOperationFilter's nil branch — a new
//     membership won't be picked up without a reconnect). The synthetic
//     Public operation is always included because it is implicit-operator
//     for every authenticated caller.
//   - opIDs == []   → returns a filter that never matches. The subscription
//     stays open but emits nothing. The frontend uses an `enabled` flag to
//     avoid opening the SSE stream in this case, so this branch is mostly
//     defensive.
//   - opIDs has ids → every id is authorized at viewer minimum via
//     authorization.AuthorizeOperationRole. Returns a filter that delivers
//     events only when the event's operation_id is in the set.
func (r *subscriptionResolver) buildOperationsFilter(ctx context.Context, auth gqlctx.AuthInfo, opIDs []string) (eventbus.Filter, error) {
	if opIDs == nil {
		userID, err := uuid.Parse(auth.UserID)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID")
		}
		ops, err := r.OperationRepo.FindByMemberID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch operations: %w", err)
		}
		// +1 for the synthetic Public tree, which has no Mongo row and is never
		// returned by FindByMemberID but is readable by every authenticated user
		// (implicit operator). It must deliver wiki document-changed events for
		// live collab.
		opSet := make(map[string]struct{}, len(ops)+1)
		for _, op := range ops {
			opSet[op.OperationID.String()] = struct{}{}
		}
		opSet[models.PublicOperationID.String()] = struct{}{}
		return filterFromOpSet(opSet, auth), nil
	}

	if len(opIDs) == 0 {
		return func(_ eventbus.Event) bool { return false }, nil
	}

	if len(opIDs) > myCredentialChangedOpCap {
		return nil, fmt.Errorf("too many operations selected (max %d)", myCredentialChangedOpCap)
	}

	// Explicit ids: authorize each one via the shared helper, which honors
	// the Public operation's implicit-operator rule and the app-admin bypass.
	opSet := make(map[string]struct{}, len(opIDs))
	for _, raw := range opIDs {
		opUID, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid operation ID %q: %w", raw, err)
		}
		op, err := r.OperationRepo.FindByID(ctx, opUID)
		if err != nil {
			return nil, fmt.Errorf("operation not found: %w", err)
		}
		if err := authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleViewer); err != nil {
			return nil, err
		}
		opSet[raw] = struct{}{}
	}

	return filterFromOpSet(opSet, auth), nil
}

// filterFromOpSet returns an event-bus filter that delivers an event only if
// its operation_id is in the set. Self-authored events always pass — same
// affordance as buildOperationFilter so the subscriber sees their own writes
// without a refetch.
func filterFromOpSet(opSet map[string]struct{}, auth gqlctx.AuthInfo) eventbus.Filter {
	return func(event eventbus.Event) bool {
		if event.Actor.Type == eventbus.ActorUser && event.Actor.ID == auth.UserID {
			return true
		}
		_, ok := opSet[extractOperationID(event)]
		return ok
	}
}

// selectsField reports whether the client's selection on the current field
// includes name, fragments included. Without an executing operation — a
// resolver called directly from a test — it reports true, so the caller
// falls back to doing the work.
func selectsField(ctx context.Context, name string) bool {
	if !graphql.HasOperationContext(ctx) || graphql.GetFieldContext(ctx) == nil {
		return true
	}
	return slices.Contains(graphql.CollectAllFields(ctx), name)
}

// extractOperationID pulls the operation ID from any operation-related event payload.
func extractOperationID(event eventbus.Event) string {
	switch p := event.Payload.(type) {
	case eventbus.OperationEventPayload:
		return p.OperationID
	case eventbus.OperationDeletedPayload:
		return p.OperationID
	case eventbus.OperationMemberPayload:
		return p.OperationID
	case eventbus.WikiDocumentEventPayload:
		return p.OperationID
	case eventbus.WikiPresencePayload:
		return p.OperationID
	case eventbus.CredentialEventPayload:
		return p.OperationID
	case eventbus.HashEventPayload:
		return p.OperationID
	case eventbus.HashCrackedPayload:
		return p.OperationID
	case eventbus.HashBulkImportPayload:
		return p.OperationID
	case eventbus.HostEventPayload:
		return p.OperationID
	case eventbus.TaskEventPayload:
		return p.OperationID
	case eventbus.OperationEventLoggedPayload:
		return p.OperationID
	case eventbus.AgentActionPayload:
		// Empty for calls that are not operation-scoped (list_operations, or a
		// refusal that never resolved one). Those correctly match no
		// operation stream.
		return p.OperationID
	}
	return ""
}

// topicToAction maps an event bus topic suffix to a GraphQL EventAction.
func topicToAction(topic eventbus.Topic) model.EventAction {
	s := string(topic)
	switch {
	case strings.HasSuffix(s, ".created"), strings.HasSuffix(s, ".added"):
		return model.EventActionCreated
	case strings.HasSuffix(s, ".updated"):
		return model.EventActionUpdated
	case strings.HasSuffix(s, ".deleted"), strings.HasSuffix(s, ".removed"):
		return model.EventActionDeleted
	}
	return model.EventActionUpdated
}

// toUserEvent converts an event bus Event to a GraphQL UserEvent.
func toUserEvent(event eventbus.Event) *model.UserEvent {
	evt := &model.UserEvent{Action: topicToAction(event.Topic)}
	switch p := event.Payload.(type) {
	case eventbus.UserEventPayload:
		evt.UserID = p.UserID
		evt.Username = &p.Username
	case eventbus.UserDeletedPayload:
		evt.UserID = p.UserID
	}
	return evt
}

// toOperationEvent converts an event bus Event to a GraphQL OperationEvent.
func toOperationEvent(event eventbus.Event) *model.OperationEvent {
	evt := &model.OperationEvent{Action: topicToAction(event.Topic)}
	switch p := event.Payload.(type) {
	case eventbus.OperationEventPayload:
		evt.OperationID = p.OperationID
		evt.Name = &p.Name
	case eventbus.OperationDeletedPayload:
		evt.OperationID = p.OperationID
	}
	return evt
}

// toOperationMemberEvent converts an event bus Event to a GraphQL OperationMemberEvent.
func toOperationMemberEvent(event eventbus.Event) *model.OperationMemberEvent {
	evt := &model.OperationMemberEvent{Action: topicToAction(event.Topic)}
	if p, ok := event.Payload.(eventbus.OperationMemberPayload); ok {
		evt.OperationID = p.OperationID
		evt.UserID = p.MemberID
	}
	return evt
}

// toWikiDocumentEvent converts an event bus Event to a GraphQL WikiDocumentEvent.
func toWikiDocumentEvent(event eventbus.Event) *model.WikiDocumentEvent {
	evt := &model.WikiDocumentEvent{Action: topicToAction(event.Topic)}

	// Map delete/restore/move topics to the right action. topicToAction can't
	// be trusted for the hard-delete topic: it matches the suffix ".deleted",
	// but the topic is "wiki.document.hard_deleted" (ends "_deleted"), so it
	// falls through to UPDATED. Both soft- and hard-delete must surface as
	// DELETED so the client drops per-doc caches and refreshes the trash list
	// (hard-delete + empty-trash arrive with documentId="").
	switch event.Topic {
	case eventbus.TopicWikiDocumentSoftDeleted, eventbus.TopicWikiDocumentHardDeleted:
		evt.Action = model.EventActionDeleted
	case eventbus.TopicWikiDocumentRestored:
		evt.Action = model.EventActionCreated
	case eventbus.TopicWikiDocumentMoved:
		evt.Action = model.EventActionUpdated
	}

	if p, ok := event.Payload.(eventbus.WikiDocumentEventPayload); ok {
		evt.DocumentID = p.DocumentID
		evt.OperationID = p.OperationID
		if p.ParentDocumentID != "" {
			evt.ParentDocumentID = &p.ParentDocumentID
		}
		if p.PreviousParentDocumentID != "" {
			evt.PreviousParentDocumentID = &p.PreviousParentDocumentID
		}
	}
	return evt
}

// toWikiDocumentPresenceEvent converts an event bus Event to a GraphQL WikiDocumentPresenceEvent.
func toWikiDocumentPresenceEvent(event eventbus.Event) *model.WikiDocumentPresenceEvent {
	action := model.PresenceActionJoined
	if event.Topic == eventbus.TopicWikiPresenceLeft {
		action = model.PresenceActionLeft
	}

	evt := &model.WikiDocumentPresenceEvent{Action: action}
	if p, ok := event.Payload.(eventbus.WikiPresencePayload); ok {
		evt.DocumentID = p.DocumentID
		evt.OperationID = p.OperationID
		evt.UserID = p.UserID
		evt.Username = p.Username
	}
	return evt
}

// credentialTopics is the list of credential event bus topics for subscriptions.
var credentialTopics = []eventbus.Topic{
	eventbus.TopicCredentialCreated,
	eventbus.TopicCredentialUpdated,
	eventbus.TopicCredentialDeleted,
	eventbus.TopicCredentialCommentAdded,
	eventbus.TopicCredentialCommentUpdated,
	eventbus.TopicCredentialCommentRemoved,
}

// toCredentialEvent converts an event bus Event to a GraphQL CredentialEvent.
// Comment.* topics surface as UPDATED so the client refetches the full credential.
func toCredentialEvent(event eventbus.Event) *model.CredentialEvent {
	var action model.EventAction
	switch event.Topic {
	case eventbus.TopicCredentialCreated:
		action = model.EventActionCreated
	case eventbus.TopicCredentialDeleted:
		action = model.EventActionDeleted
	default:
		action = model.EventActionUpdated
	}

	evt := &model.CredentialEvent{Action: action}
	if p, ok := event.Payload.(eventbus.CredentialEventPayload); ok {
		evt.CredentialID = p.CredentialID
		evt.OperationID = p.OperationID
	}
	return evt
}

// hashTopics is the list of hash event bus topics for subscriptions. Includes
// every hash topic so the subscriber refetches once per mutation regardless of
// which axis (status, link, comment, bulk import) changed.
var hashTopics = []eventbus.Topic{
	eventbus.TopicHashCreated,
	eventbus.TopicHashUpdated,
	eventbus.TopicHashDeleted,
	eventbus.TopicHashCracked,
	eventbus.TopicHashBulkImported,
}

// toHashEvent converts an event bus Event to a GraphQL HashEvent. Cracked and
// bulk_imported both surface as UPDATED — the client refetches and the table
// re-renders the new state. Comment.* topics surface as UPDATED for the same
// reason.
func toHashEvent(event eventbus.Event) *model.HashEvent {
	var action model.EventAction
	switch event.Topic {
	case eventbus.TopicHashCreated:
		action = model.EventActionCreated
	case eventbus.TopicHashDeleted:
		action = model.EventActionDeleted
	default:
		action = model.EventActionUpdated
	}
	evt := &model.HashEvent{Action: action}
	switch p := event.Payload.(type) {
	case eventbus.HashEventPayload:
		evt.HashID = p.HashID
		evt.OperationID = p.OperationID
	case eventbus.HashCrackedPayload:
		evt.HashID = p.HashID
		evt.OperationID = p.OperationID
	case eventbus.HashBulkImportPayload:
		// Bulk imports have no single subject id — leave HashID empty; the
		// client treats an empty id as "invalidate the list" rather than "fetch
		// this row".
		evt.OperationID = p.OperationID
	}
	return evt
}

// hostTopics is the list of host event bus topics for subscriptions.
var hostTopics = []eventbus.Topic{
	eventbus.TopicHostCreated,
	eventbus.TopicHostUpdated,
	eventbus.TopicHostDeleted,
}

// toHostEvent converts an event bus Event to a GraphQL HostEvent.
func toHostEvent(event eventbus.Event) *model.HostEvent {
	var action model.EventAction
	switch event.Topic {
	case eventbus.TopicHostCreated:
		action = model.EventActionCreated
	case eventbus.TopicHostDeleted:
		action = model.EventActionDeleted
	default:
		action = model.EventActionUpdated
	}
	evt := &model.HostEvent{Action: action}
	if p, ok := event.Payload.(eventbus.HostEventPayload); ok {
		evt.HostID = p.HostID
		evt.OperationID = p.OperationID
	}
	return evt
}

// skillTopics is the list of community skill registry topics for the
// skillChanged subscription.
var skillTopics = []eventbus.Topic{
	eventbus.TopicSkillPublished,
	eventbus.TopicSkillRemoved,
}

// toSkillEvent converts an event bus Event to a GraphQL SkillEvent. A new
// version of an existing skill is an update; a first publish is a creation.
// The client refetches either way, so action is advisory.
func toSkillEvent(event eventbus.Event) *model.SkillEvent {
	action := model.EventActionCreated
	if event.Topic == eventbus.TopicSkillRemoved {
		action = model.EventActionDeleted
	}
	evt := &model.SkillEvent{Action: action}
	if p, ok := event.Payload.(eventbus.SkillEventPayload); ok {
		evt.SkillID = p.SkillID
		evt.Name = p.Name
		if event.Topic == eventbus.TopicSkillPublished && p.Version > 1 {
			evt.Action = model.EventActionUpdated
		}
	}
	return evt
}

// moduleTopics is the list of module lifecycle event bus topics for the admin
// moduleChanged subscription.
var moduleTopics = []eventbus.Topic{
	eventbus.TopicModuleRegistered,
	eventbus.TopicModuleDeregistered,
	eventbus.TopicModuleDead,
}

// toModuleEvent converts an event bus Event to a GraphQL ModuleEvent. A module
// row is never hard-deleted: registered (incl. revival) maps to CREATED, while
// deregistered and dead are status updates on a surviving row → UPDATED. The
// subscription resolver refetches the full row for every event, so action is
// purely advisory for the client.
func toModuleEvent(event eventbus.Event) *model.ModuleEvent {
	action := model.EventActionUpdated
	if event.Topic == eventbus.TopicModuleRegistered {
		action = model.EventActionCreated
	}
	evt := &model.ModuleEvent{Action: action}
	if p, ok := event.Payload.(eventbus.ModuleEventPayload); ok {
		evt.Instance = p.Instance
	}
	return evt
}

// wikiDocumentTopics is the list of wiki document event bus topics for subscriptions.
var wikiDocumentTopics = []eventbus.Topic{
	eventbus.TopicWikiDocumentCreated,
	eventbus.TopicWikiDocumentUpdated,
	eventbus.TopicWikiDocumentSoftDeleted,
	eventbus.TopicWikiDocumentRestored,
	eventbus.TopicWikiDocumentMoved,
	eventbus.TopicWikiDocumentHardDeleted,
}

// wikiPresenceTopics is the list of wiki presence event bus topics for subscriptions.
var wikiPresenceTopics = []eventbus.Topic{
	eventbus.TopicWikiPresenceJoined,
	eventbus.TopicWikiPresenceLeft,
}

// --- Streaming ---

// subscriberAuth returns the caller's identity, refusing anonymous callers —
// the first check of every subscription.
func subscriberAuth(ctx context.Context) (gqlctx.AuthInfo, error) {
	auth := gqlctx.AuthFromContext(ctx)
	if auth.UserID == "" {
		return auth, fmt.Errorf("unauthorized")
	}
	return auth, nil
}

// stream feeds a subscription: every event on topics that passes filter (nil
// for all) is turned into a payload by build — which may return nil to skip
// it — and sent to the client until the subscription's context ends.
//
// The channel is never closed. gqlgen stops reading it when ctx ends, and at
// that moment the bus may still be running build for an event in flight.
// Every resolver used to close the channel right after unsubscribing, and a
// send racing that close panicked. Unsubscribing alone stops the bus calling
// build, and the channel is collected with the subscription.
func stream[T any](
	ctx context.Context,
	bus eventbus.IEventBus,
	topics []eventbus.Topic,
	filter eventbus.Filter,
	build func(context.Context, eventbus.Event) *T,
) <-chan *T {
	ch := make(chan *T, 1)
	unsubscribe := bus.Subscribe(topics, func(_ context.Context, event eventbus.Event) {
		payload := build(ctx, event)
		if payload == nil {
			return
		}
		select {
		case ch <- payload:
		case <-ctx.Done():
		}
	}, filter)
	go func() {
		<-ctx.Done()
		unsubscribe()
	}()
	return ch
}

// pure adapts a conversion that reads nothing to stream's builder shape.
func pure[T any](convert func(eventbus.Event) *T) func(context.Context, eventbus.Event) *T {
	return func(_ context.Context, event eventbus.Event) *T { return convert(event) }
}

// load reads the row an event names, or returns nil when the id does not
// parse or the row cannot be read — the event still goes out, with its ids.
func load[T any](ctx context.Context, id string, find func(context.Context, uuid.UUID) (T, error)) *T {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	row, err := find(ctx, uid)
	if err != nil {
		return nil
	}
	return &row
}

// Event builders: each converts a bus event to its GraphQL payload and, for
// anything but a delete, loads the row it names so the client can update
// without a follow-up query.

func (r *subscriptionResolver) userEvent(ctx context.Context, event eventbus.Event) *model.UserEvent {
	evt := toUserEvent(event)
	if evt.Action != model.EventActionDeleted {
		evt.User = load(ctx, evt.UserID, r.UserRepo.FindByID)
	}
	return evt
}

func (r *subscriptionResolver) operationEvent(ctx context.Context, event eventbus.Event) *model.OperationEvent {
	evt := toOperationEvent(event)
	if evt.Action != model.EventActionDeleted {
		evt.Operation = load(ctx, evt.OperationID, r.OperationRepo.FindByID)
	}
	return evt
}

func (r *subscriptionResolver) credentialEvent(ctx context.Context, event eventbus.Event) *model.CredentialEvent {
	evt := toCredentialEvent(event)
	if evt.Action != model.EventActionDeleted && r.CredentialRepo != nil {
		evt.Credential = load(ctx, evt.CredentialID, r.CredentialRepo.FindByID)
	}
	return evt
}

// hashEvent leaves a bulk import's summary event — which names no single
// hash — without a row.
func (r *subscriptionResolver) hashEvent(ctx context.Context, event eventbus.Event) *model.HashEvent {
	evt := toHashEvent(event)
	if evt.Action != model.EventActionDeleted && evt.HashID != "" && r.HashRepo != nil {
		evt.Hash = load(ctx, evt.HashID, r.HashRepo.FindByID)
	}
	return evt
}

func (r *subscriptionResolver) hostEvent(ctx context.Context, event eventbus.Event) *model.HostEvent {
	evt := toHostEvent(event)
	if evt.Action != model.EventActionDeleted && r.HostRepo != nil {
		evt.Host = load(ctx, evt.HostID, r.HostRepo.FindByID)
	}
	return evt
}

func (r *subscriptionResolver) taskEvent(ctx context.Context, event eventbus.Event) *model.TaskEvent {
	evt := toTaskEvent(event)
	if evt.Action != model.EventActionDeleted && r.TaskRepo != nil {
		evt.Task = load(ctx, evt.TaskID, r.TaskRepo.FindByID)
	}
	return evt
}

// sessionEvent's row carries the status the event's topic implies; see
// applySessionStatusFromTopic.
func (r *subscriptionResolver) sessionEvent(ctx context.Context, event eventbus.Event) *model.SessionEvent {
	evt := toSessionEvent(event)
	if evt.SessionID != "" {
		if sess := load(ctx, evt.SessionID, r.SessionRepo.FindByID); sess != nil {
			applySessionStatusFromTopic(sess, event.Topic)
			evt.Session = sess
		}
	}
	return evt
}

func (r *subscriptionResolver) moduleEvent(ctx context.Context, event eventbus.Event) *model.ModuleEvent {
	evt := toModuleEvent(event)
	if r.ModuleRepo != nil && evt.Instance != "" {
		if mod, err := r.ModuleRepo.FindByInstance(ctx, evt.Instance); err == nil {
			evt.Module = &mod
		}
	}
	return evt
}

// timelineEvent streams the logged row itself, so an event whose row cannot
// be read is skipped rather than sent empty.
func (r *subscriptionResolver) timelineEvent(ctx context.Context, event eventbus.Event) *models.OperationEvent {
	p, ok := event.Payload.(eventbus.OperationEventLoggedPayload)
	if !ok {
		return nil
	}
	eventUID, err := uuid.Parse(p.EventID)
	if err != nil {
		return nil
	}
	row, err := r.TimelineResolver.FindByEventID(ctx, eventUID)
	if err != nil {
		return nil
	}
	return row
}

// wikiDocumentEvents builds wiki document events, reading the document only
// when the client selected it — see wikiDocumentChanged.
func (r *subscriptionResolver) wikiDocumentEvents(fetchDocument bool) func(context.Context, eventbus.Event) *model.WikiDocumentEvent {
	return func(ctx context.Context, event eventbus.Event) *model.WikiDocumentEvent {
		evt := toWikiDocumentEvent(event)
		// Without its CRDT state, which no field of the event exposes.
		if fetchDocument && evt.Action != model.EventActionDeleted {
			if docID, err := uuid.Parse(evt.DocumentID); err == nil {
				if docs, err := r.WikiDocumentRepo.FindByIDs(ctx, []uuid.UUID{docID}); err == nil && len(docs) == 1 {
					evt.Document = &docs[0]
				}
			}
		}
		return evt
	}
}

// agentActivityEvent converts an agent action to its feed row.
func agentActivityEvent(event eventbus.Event) *model.AgentActivityEvent {
	p, ok := event.Payload.(eventbus.AgentActionPayload)
	if !ok {
		return nil
	}
	return &model.AgentActivityEvent{
		OperationID: p.OperationID,
		AgentKeyID:  p.AgentKeyID,
		AgentName:   p.AgentName,
		AgentLabel:  p.AgentLabel,
		OwnerUserID: p.OwnerUserID,
		Tool:        p.Tool,
		Write:       p.Write,
		Outcome:     p.Outcome,
		Summary:     p.Summary,
	}
}

// wikiDocumentChanged implements the wikiDocumentChanged subscription.
// Same operation-scoping pattern as OperationChanged.
func (r *subscriptionResolver) wikiDocumentChanged(ctx context.Context, operationID string) (<-chan *model.WikiDocumentEvent, error) {
	auth, err := subscriberAuth(ctx)
	if err != nil {
		return nil, err
	}
	filter, err := r.buildOperationFilter(ctx, auth, &operationID)
	if err != nil {
		return nil, err
	}
	// The document read runs once per subscriber per event, and Hocuspocus
	// persists every couple of seconds while someone types — so a client
	// that only wants the event's ids should not pay for it. The selection
	// cannot change for the life of the subscription, so decide once.
	fetchDocument := selectsField(ctx, "document")
	return stream(ctx, r.EventBus, wikiDocumentTopics, filter, r.wikiDocumentEvents(fetchDocument)), nil
}

// wikiDocumentPresenceChanged implements the wikiDocumentPresenceChanged subscription.
func (r *subscriptionResolver) wikiDocumentPresenceChanged(ctx context.Context, operationID string) (<-chan *model.WikiDocumentPresenceEvent, error) {
	auth, err := subscriberAuth(ctx)
	if err != nil {
		return nil, err
	}
	filter, err := r.buildOperationFilter(ctx, auth, &operationID)
	if err != nil {
		return nil, err
	}
	return stream(ctx, r.EventBus, wikiPresenceTopics, filter, pure(toWikiDocumentPresenceEvent)), nil
}

// taskTopics is the list of task event bus topics for subscriptions.
// Includes every task topic so the subscriber refetches once per mutation
// regardless of which axis (stage, status, assignees, references, etc.)
// changed.
var taskTopics = []eventbus.Topic{
	eventbus.TopicTaskCreated,
	eventbus.TopicTaskUpdated,
	eventbus.TopicTaskStageChanged,
	eventbus.TopicTaskStatusSet,
	eventbus.TopicTaskAssigneesChanged,
	eventbus.TopicTaskReferencesChanged,
	eventbus.TopicTaskSoftDeleted,
	eventbus.TopicTaskRestored,
	eventbus.TopicTaskHardDeleted,
}

// toTaskEvent converts an event bus Event to a GraphQL TaskEvent. Soft and
// hard delete map to DELETED so the subscriber prunes the row from its
// cache; restore maps to CREATED so the kanban view re-inserts the card;
// everything else (including stage / status changes) maps to UPDATED.
func toTaskEvent(event eventbus.Event) *model.TaskEvent {
	var action model.EventAction
	switch event.Topic {
	case eventbus.TopicTaskCreated, eventbus.TopicTaskRestored:
		action = model.EventActionCreated
	case eventbus.TopicTaskSoftDeleted, eventbus.TopicTaskHardDeleted:
		action = model.EventActionDeleted
	default:
		action = model.EventActionUpdated
	}

	evt := &model.TaskEvent{Action: action}
	if p, ok := event.Payload.(eventbus.TaskEventPayload); ok {
		evt.TaskID = p.TaskID
		evt.OperationID = p.OperationID
	}
	return evt
}

// taskChanged implements the taskChanged subscription. Same shape as
// wikiDocumentChanged — auth via the operation filter, then the full task for
// non-DELETED events so clients can update without an additional query.
func (r *subscriptionResolver) taskChanged(ctx context.Context, operationID string) (<-chan *model.TaskEvent, error) {
	auth, err := subscriberAuth(ctx)
	if err != nil {
		return nil, err
	}
	filter, err := r.buildOperationFilter(ctx, auth, &operationID)
	if err != nil {
		return nil, err
	}
	return stream(ctx, r.EventBus, taskTopics, filter, r.taskEvent), nil
}

// sessionTopics is the list of session event bus topics for subscriptions.
var sessionTopics = []eventbus.Topic{
	eventbus.TopicSessionCreated,
	eventbus.TopicSessionRefreshed,
	eventbus.TopicSessionTerminated,
}

// toSessionEvent converts an event bus Event to a GraphQL SessionEvent.
//
// Action mapping is deliberate — the frontend session guard uses `action` as
// the authoritative "what happened" signal:
//
//	session.created    → CREATED
//	session.refreshed  → UPDATED (the session stays active; activity advanced)
//	session.terminated → DELETED (the session's auth record is gone; the
//	                     historical Mongo row survives, but semantically the
//	                     *session* ended, so DELETED is the right action for
//	                     GraphQL consumers that care about liveness)
//
// Do not "fix" the terminated → DELETED mapping back to UPDATED without first
// updating frontend/src/hooks/use-session-guard.ts, which relies on it to
// decide whether to force a local logout.
func toSessionEvent(event eventbus.Event) *model.SessionEvent {
	var action model.EventAction
	switch event.Topic {
	case eventbus.TopicSessionCreated:
		action = model.EventActionCreated
	case eventbus.TopicSessionRefreshed:
		action = model.EventActionUpdated
	case eventbus.TopicSessionTerminated:
		action = model.EventActionDeleted
	default:
		action = topicToAction(event.Topic)
	}

	evt := &model.SessionEvent{Action: action}
	if p, ok := event.Payload.(eventbus.SessionEventPayload); ok {
		evt.SessionID = p.SessionID
		evt.UserID = p.UserID
	}
	return evt
}

// applySessionStatusFromTopic sets the derived Status field on a session
// loaded directly from Mongo. Status is not persisted (bson:"-"), so a raw
// FindByID always produces an empty string, which the Session.status field
// resolver converts to INACTIVE — wrong for created/refreshed events and
// the cause of spurious client-side logouts after token refresh.
//
// The topic is authoritative here: session.created and session.refreshed
// both mean the session is currently active; session.terminated means it
// is not. We do not consult Redis — the topic is published by the code
// path that just finished mutating the active-session state, so it is
// strictly more up to date than any subsequent Redis read would be.
func applySessionStatusFromTopic(sess *models.Session, topic eventbus.Topic) {
	switch topic {
	case eventbus.TopicSessionCreated, eventbus.TopicSessionRefreshed:
		sess.Status = models.SessionStatusActive
	case eventbus.TopicSessionTerminated:
		sess.Status = models.SessionStatusInactive
	}
}
