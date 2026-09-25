package resolver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/auth"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// ISessionResolver defines the business logic methods for the Session entity.
//
// Mongo holds the immutable creation log; Redis holds the live active set.
// The resolver loads rows from Mongo and decorates each one with `Status`
// and `LastActivityAt` from Redis at query time.
type ISessionResolver interface {
	// Queries
	MySessions(ctx context.Context, activeOnly *bool, first *int, after *string, last *int, before *string) (*model.SessionConnection, error)
	Sessions(ctx context.Context, userID *string, search *string, activeOnly *bool, first *int, after *string, last *int, before *string) (*model.SessionConnection, error)
	Session(ctx context.Context, id string) (*models.Session, error)

	// Mutations
	RevokeSession(ctx context.Context, id string) (bool, error)
	RevokeAllMySessions(ctx context.Context) (int, error)
	AdminRevokeSession(ctx context.Context, id string) (bool, error)
	AdminRevokeAllUserSessions(ctx context.Context, userID string) (int, error)

	// Field resolvers
	ID(ctx context.Context, obj *models.Session) (string, error)
	UserID(ctx context.Context, obj *models.Session) (string, error)
	User(ctx context.Context, obj *models.Session) (*models.User, error)
	Status(ctx context.Context, obj *models.Session) (models.SessionStatus, error)
	LastActivityAt(ctx context.Context, obj *models.Session) (*string, error)
	IsCurrent(ctx context.Context, obj *models.Session) (bool, error)
	CreatedAt(ctx context.Context, obj *models.Session) (string, error)
	UpdatedAt(ctx context.Context, obj *models.Session) (string, error)
}

type sessionResolver struct {
	sessionRepo repository.ISessionRepository // Mongo creation log
	userRepo    repository.IUserRepository
	tokenStore  auth.TokenStore // Redis active set
	bus         eventbus.IEventBus
}

func NewSessionResolver(
	sessionRepo repository.ISessionRepository,
	userRepo repository.IUserRepository,
	tokenStore auth.TokenStore,
	bus eventbus.IEventBus,
) ISessionResolver {
	return &sessionResolver{
		sessionRepo: sessionRepo,
		userRepo:    userRepo,
		tokenStore:  tokenStore,
		bus:         bus,
	}
}

// --- Queries ---

func (r *sessionResolver) MySessions(ctx context.Context, activeOnly *bool, first *int, after *string, last *int, before *string) (*model.SessionConnection, error) {
	authInfo := gqlctx.AuthFromContext(ctx)
	userUUID, err := uuid.Parse(authInfo.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID in token: %w", err)
	}
	return r.listSessions(ctx, []uuid.UUID{userUUID}, boolOr(activeOnly, false), first, after, last, before)
}

func (r *sessionResolver) Sessions(ctx context.Context, userID *string, search *string, activeOnly *bool, first *int, after *string, last *int, before *string) (*model.SessionConnection, error) {
	if userID != nil && *userID != "" && search != nil && *search != "" {
		return nil, fmt.Errorf("cannot specify both userID and search")
	}

	var userIDs []uuid.UUID

	if userID != nil && *userID != "" {
		uid, err := uuid.Parse(*userID)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID: %w", err)
		}
		userIDs = []uuid.UUID{uid}
	}

	if search != nil && *search != "" {
		users, err := r.userRepo.FindAll(ctx, *search, 0, 100)
		if err != nil {
			return nil, fmt.Errorf("failed to search users: %w", err)
		}
		if len(users) == 0 {
			return &model.SessionConnection{
				Edges:      []*model.SessionEdge{},
				PageInfo:   &pagination.PageInfo{},
				TotalCount: 0,
			}, nil
		}
		userIDs = make([]uuid.UUID, len(users))
		for i, u := range users {
			userIDs[i] = u.UserID
		}
	}

	return r.listSessions(ctx, userIDs, boolOr(activeOnly, false), first, after, last, before)
}

func (r *sessionResolver) Session(ctx context.Context, id string) (*models.Session, error) {
	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	sess, err := r.sessionRepo.FindByID(ctx, sid)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	// Decorate with active state. We need to know the owning user_id —
	// luckily it's right there on the row.
	r.decorate(ctx, []*models.Session{&sess})
	return &sess, nil
}

// listSessions returns one page of session rows, each decorated with
// `status` and `last_activity_at` from Redis.
//
//   - activeOnly=false (default): every row scoped to userIDs; empty
//     userIDs is the admin global view.
//   - activeOnly=true: the live session ids come from Redis first — per
//     user, or a SCAN of every user's index for the global view — and then
//     scope the same paginated Mongo query. Both modes share cursors,
//     totalCount and hasNextPage; the active mode used to return the first
//     page only, with totalCount set to that page's length.
func (r *sessionResolver) listSessions(ctx context.Context, userIDs []uuid.UUID, activeOnly bool,
	first *int, after *string, last *int, before *string) (*model.SessionConnection, error) {

	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, fmt.Errorf("invalid pagination args: %w", err)
	}

	filter := repository.SessionFilter{UserIDs: userIDs}
	var live map[uuid.UUID]time.Time
	if activeOnly {
		live, err = r.liveSessions(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		// Non-nil even when empty: no live sessions must match no rows.
		filter.SessionIDs = make([]uuid.UUID, 0, len(live))
		for id := range live {
			filter.SessionIDs = append(filter.SessionIDs, id)
		}
	}

	total, err := r.sessionRepo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}
	rows, err := r.sessionRepo.FindWithCursor(ctx, filter, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, fmt.Errorf("find: %w", err)
	}

	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(s *models.Session) string { return pagination.EncodeCursor(s.CreateAt, s.Id) },
		func(s *models.Session, cursor string) *model.SessionEdge {
			return &model.SessionEdge{Node: s, Cursor: cursor}
		})
	page := make([]*models.Session, len(edges))
	for i, e := range edges {
		page[i] = e.Node
	}
	if live != nil {
		// Every row here came out of this snapshot; asking Redis again per
		// user would only add round trips.
		markLive(page, live)
	} else {
		r.decorate(ctx, page)
	}

	return &model.SessionConnection{
		Edges:      edges,
		PageInfo:   &pageInfo,
		TotalCount: int(total),
	}, nil
}

// liveSessions returns the live session ids, with their last activity, for
// userIDs — or for every user when userIDs is empty. Redis is the only
// source of truth for what is active.
func (r *sessionResolver) liveSessions(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]time.Time, error) {
	var actives []auth.ActiveSession
	if len(userIDs) == 0 {
		// Global active view: SCAN all session_index:* keys.
		// O(total Redis keys) — acceptable for admin-only queries.
		all, err := r.tokenStore.ListAllActive(ctx)
		if err != nil {
			return nil, fmt.Errorf("list all active: %w", err)
		}
		actives = all
	} else {
		for _, uid := range userIDs {
			own, err := r.tokenStore.ListByUser(ctx, uid)
			if err != nil {
				return nil, fmt.Errorf("list active: %w", err)
			}
			actives = append(actives, own...)
		}
	}
	live := make(map[uuid.UUID]time.Time, len(actives))
	for _, a := range actives {
		live[a.SessionID] = a.LastActivityAt
	}
	return live, nil
}

// decorate populates Status + LastActivityAt on each row from Redis.
//
// Strategy: group rows by user_id, then for each user fetch their full
// active set in one ListByUser call (one Redis SMEMBERS+MGET round-trip
// per user). Match each row's session_id against the active set.
//
// For typical pages (one or a few users) this is O(pages) Redis calls
// regardless of page size.
func (r *sessionResolver) decorate(ctx context.Context, rows []*models.Session) {
	live := make(map[uuid.UUID]time.Time)
	fetched := make(map[uuid.UUID]struct{})
	for _, row := range rows {
		if _, done := fetched[row.UserID]; done {
			continue
		}
		fetched[row.UserID] = struct{}{}
		actives, err := r.tokenStore.ListByUser(ctx, row.UserID)
		if err != nil {
			continue // best-effort: that user's rows render inactive
		}
		for _, a := range actives {
			live[a.SessionID] = a.LastActivityAt
		}
	}
	markLive(rows, live)
}

// markLive sets each row's status and last activity from a snapshot of the
// live set, keyed by session id. Rows absent from it are inactive.
func markLive(rows []*models.Session, live map[uuid.UUID]time.Time) {
	for _, row := range rows {
		last, ok := live[row.SessionID]
		if !ok {
			row.Status = models.SessionStatusInactive
			continue
		}
		row.Status = models.SessionStatusActive
		unix := last.Unix()
		row.LastActivityAt = &unix
	}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// --- Mutations ---

func (r *sessionResolver) RevokeSession(ctx context.Context, id string) (bool, error) {
	authInfo := gqlctx.AuthFromContext(ctx)
	userUUID, err := uuid.Parse(authInfo.UserID)
	if err != nil {
		return false, fmt.Errorf("invalid user ID in token: %w", err)
	}

	sid, err := uuid.Parse(id)
	if err != nil {
		return false, fmt.Errorf("invalid session ID: %w", err)
	}

	if authInfo.CurrentSessionID != "" && sid.String() == authInfo.CurrentSessionID {
		return false, fmt.Errorf("cannot revoke current session — use logout instead")
	}

	if _, err := r.tokenStore.DeleteBySessionID(ctx, userUUID, sid); err != nil {
		if errors.Is(err, auth.ErrTokenInvalid) {
			return false, fmt.Errorf("session not found or already inactive")
		}
		return false, fmt.Errorf("failed to revoke: %w", err)
	}

	r.bus.Publish(eventbus.NewSessionTerminatedEvent(eventbus.UserActor(authInfo.UserID), eventbus.SessionEventPayload{
		SessionID: id, UserID: authInfo.UserID, Reason: "user_revoked",
	}))
	return true, nil
}

func (r *sessionResolver) RevokeAllMySessions(ctx context.Context) (int, error) {
	authInfo := gqlctx.AuthFromContext(ctx)
	userUUID, err := uuid.Parse(authInfo.UserID)
	if err != nil {
		return 0, fmt.Errorf("invalid user ID in token: %w", err)
	}
	return r.bulkRevoke(ctx, userUUID, authInfo.CurrentSessionID, "user_revoked", authInfo.UserID)
}

func (r *sessionResolver) AdminRevokeSession(ctx context.Context, id string) (bool, error) {
	authInfo := gqlctx.AuthFromContext(ctx)

	sid, err := uuid.Parse(id)
	if err != nil {
		return false, fmt.Errorf("invalid session ID: %w", err)
	}
	if authInfo.CurrentSessionID != "" && sid.String() == authInfo.CurrentSessionID {
		return false, fmt.Errorf("cannot revoke your own current session — use logout instead")
	}

	// Look up the session in the Mongo creation log to learn the owning
	// user_id, then delete the Redis entry by session_id.
	row, err := r.sessionRepo.FindByID(ctx, sid)
	if err != nil {
		return false, fmt.Errorf("session not found: %w", err)
	}

	if _, err := r.tokenStore.DeleteBySessionID(ctx, row.UserID, sid); err != nil {
		if errors.Is(err, auth.ErrTokenInvalid) {
			return false, fmt.Errorf("session is already inactive")
		}
		return false, fmt.Errorf("failed to revoke: %w", err)
	}

	r.bus.Publish(eventbus.NewSessionTerminatedEvent(eventbus.UserActor(authInfo.UserID), eventbus.SessionEventPayload{
		SessionID: id, UserID: row.UserID.String(), Reason: "admin_revoked",
	}))
	return true, nil
}

func (r *sessionResolver) AdminRevokeAllUserSessions(ctx context.Context, userID string) (int, error) {
	authInfo := gqlctx.AuthFromContext(ctx)
	targetUUID, err := uuid.Parse(userID)
	if err != nil {
		return 0, fmt.Errorf("invalid user ID: %w", err)
	}
	return r.bulkRevoke(ctx, targetUUID, "", "admin_revoked", authInfo.UserID)
}

// bulkRevoke removes all live sessions for a user (skipping the optional
// excludeSessionID), then publishes one terminated event per session.
func (r *sessionResolver) bulkRevoke(ctx context.Context, userID uuid.UUID, excludeSessionID string,
	reason string, actorUserID string) (int, error) {

	actives, err := r.tokenStore.ListByUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("list active: %w", err)
	}
	var revoked int
	for _, a := range actives {
		if excludeSessionID != "" && a.SessionID.String() == excludeSessionID {
			continue
		}
		if _, err := r.tokenStore.DeleteBySessionID(ctx, userID, a.SessionID); err != nil {
			continue
		}
		r.bus.Publish(eventbus.NewSessionTerminatedEvent(eventbus.UserActor(actorUserID), eventbus.SessionEventPayload{
			SessionID: a.SessionID.String(), UserID: userID.String(), Reason: reason,
		}))
		revoked++
	}
	return revoked, nil
}

// --- Field Resolvers ---

func (r *sessionResolver) ID(_ context.Context, obj *models.Session) (string, error) {
	return obj.SessionID.String(), nil
}

func (r *sessionResolver) UserID(_ context.Context, obj *models.Session) (string, error) {
	return obj.UserID.String(), nil
}

func (r *sessionResolver) User(ctx context.Context, obj *models.Session) (*models.User, error) {
	user, err := gqlctx.LoadUser(ctx, r.userRepo, obj.UserID)
	if err != nil {
		return nil, fmt.Errorf("session user not found: %w", err)
	}
	return &user, nil
}

func (r *sessionResolver) Status(_ context.Context, obj *models.Session) (models.SessionStatus, error) {
	if obj.Status == "" {
		return models.SessionStatusInactive, nil
	}
	return obj.Status, nil
}

func (r *sessionResolver) LastActivityAt(_ context.Context, obj *models.Session) (*string, error) {
	if obj.LastActivityAt == nil {
		return nil, nil
	}
	s := time.Unix(*obj.LastActivityAt, 0).UTC().Format(time.RFC3339)
	return &s, nil
}

func (r *sessionResolver) IsCurrent(ctx context.Context, obj *models.Session) (bool, error) {
	authInfo := gqlctx.AuthFromContext(ctx)
	if authInfo.CurrentSessionID == "" {
		return false, nil
	}
	return obj.SessionID.String() == authInfo.CurrentSessionID, nil
}

func (r *sessionResolver) CreatedAt(_ context.Context, obj *models.Session) (string, error) {
	return obj.CreateAt.Format(time.RFC3339), nil
}

func (r *sessionResolver) UpdatedAt(_ context.Context, obj *models.Session) (string, error) {
	return obj.UpdateAt.Format(time.RFC3339), nil
}
