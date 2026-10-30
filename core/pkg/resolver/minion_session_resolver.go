package resolver

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// Minion sessions over GraphQL (minion_sessions.graphql): read-only, opened
// by pkg/minionsync. Viewers of an operation read them.

// IMinionSessionResolver is the business logic behind minion_sessions.graphql.
type IMinionSessionResolver interface {
	MinionSessions(ctx context.Context, operationID string, search *string, filters []*model.MinionSessionFilterInput, sort *model.MinionSessionSortInput, first *int, after *string, last *int, before *string) (*model.MinionSessionConnection, error)
	MinionSessionValues(ctx context.Context, operationID string, column string) ([]string, error)
	MinionSession(ctx context.Context, id string) (*model.MinionSession, error)
	// MinionSessionsLayout and SetMinionSessionsLayout are the caller's own
	// Sessions table layout.
	MinionSessionsLayout(ctx context.Context) (*model.MinionSessionsLayout, error)
	SetMinionSessionsLayout(ctx context.Context, layout *model.MinionSessionsLayoutInput) (*model.MinionSessionsLayout, error)
	// SetMinionSessionAddress picks the address a session shows; nil goes
	// back to core's guess.
	SetMinionSessionAddress(ctx context.Context, id string, address *string) (*model.MinionSession, error)
	// Load reads one session for a subscription event, already authorized
	// by the subscription's filter; nil when it cannot be read.
	Load(ctx context.Context, sessionID string) *model.MinionSession
}

type minionSessionResolver struct {
	sessions      repository.IMinionSessionRepository
	operationRepo repository.IOperationRepository
	userRepo      repository.IUserRepository
	preferences   repository.IUserPreferencesRepository
	reservations  repository.IMinionSessionReservationRepository
	bus           eventbus.IEventBus
}

func NewMinionSessionResolver(sessions repository.IMinionSessionRepository, operationRepo repository.IOperationRepository,
	userRepo repository.IUserRepository, preferences repository.IUserPreferencesRepository,
	reservations repository.IMinionSessionReservationRepository, bus eventbus.IEventBus) IMinionSessionResolver {
	return &minionSessionResolver{sessions: sessions, operationRepo: operationRepo, userRepo: userRepo, preferences: preferences,
		reservations: reservations, bus: bus}
}

// viewer authorizes a viewer of the operation.
func (r *minionSessionResolver) viewer(ctx context.Context, operationID string) (uuid.UUID, error) {
	op, err := uuid.Parse(operationID)
	if err != nil {
		return op, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	operation, err := gqlctx.LoadOperation(ctx, r.operationRepo, op)
	if err != nil {
		return op, fmt.Errorf("operation not found: %w", err)
	}
	return op, authorization.AuthorizeOperationRole(ctx, &operation, models.OperationRoleViewer)
}

func (r *minionSessionResolver) MinionSessions(ctx context.Context, operationID string, search *string, filters []*model.MinionSessionFilterInput, sort *model.MinionSessionSortInput, first *int, after *string, last *int, before *string) (*model.MinionSessionConnection, error) {
	op, err := r.viewer(ctx, operationID)
	if err != nil {
		return nil, err
	}
	q := repository.MinionSessionQuery{Search: strings.TrimSpace(deref(search)), Sort: models.DefaultMinionSessionSort}
	if sort != nil {
		q.Sort = fromSessionSortInput(sort)
	}
	if q.Filters, err = fromSessionFilterInputs(filters); err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	if err := q.Validate(); err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	// The cursors are the sort's own (repository.MinionSessionCursor), not
	// pagination's: parse the page size and direction, then the cursor.
	args, err := pagination.ParseArgs(first, nil, last, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid pagination args: %w", err)
	}
	cursor := after
	if !args.Forward {
		cursor = before
	}
	var keyset *repository.MinionSessionKeyset
	if cursor != nil {
		if keyset, err = repository.DecodeMinionSessionCursor(*cursor, q.Sort); err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
		// Only its presence counts: BuildEdges reads a previous page from it.
		args.Cursor = &pagination.Cursor{}
	}
	total, err := r.sessions.CountByOperationID(ctx, op, q)
	if err != nil {
		return nil, fmt.Errorf("count sessions: %w", err)
	}
	rows, err := r.sessions.FindByOperationIDWithCursor(ctx, op, q, keyset, args.Limit+1, args.Forward)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(s *models.MinionSession) string { return repository.MinionSessionCursor(s, q.Sort) },
		func(s *models.MinionSession, cursor string) *model.MinionSessionEdge {
			return &model.MinionSessionEdge{Node: toMinionSessionModel(s), Cursor: cursor}
		})
	return &model.MinionSessionConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

func (r *minionSessionResolver) SetMinionSessionAddress(ctx context.Context, id string, address *string) (*model.MinionSession, error) {
	if gqlctx.AuthFromContext(ctx).Agent != nil {
		return nil, moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid session ID")
	}
	s, err := r.sessions.FindBySessionID(ctx, sid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "session not found")
		}
		return nil, err
	}
	operation, err := gqlctx.LoadOperation(ctx, r.operationRepo, s.OperationID)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}
	if err := authorization.AuthorizeOperationRole(ctx, &operation, models.OperationRoleOperator); err != nil {
		return nil, err
	}
	addr, picked := models.DefaultAddress(s.Hello.Addrs), false
	if address != nil {
		if !slices.Contains(s.Hello.Addrs, *address) {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("%q is not one of the session's addresses", *address))
		}
		addr, picked = *address, true
	}
	if err := r.sessions.SetAddress(ctx, sid, addr, picked); err != nil {
		if repository.IsNotFound(err) {
			// The host's addresses changed under us (a rehello).
			return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("%q is not one of the session's addresses", addr))
		}
		return nil, fmt.Errorf("set the address: %w", err)
	}
	s.Address, s.AddressPicked = addr, picked
	r.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(eventbus.MinionSessionEventPayload{
		SessionID: s.SessionID.String(), OperationID: s.OperationID.String(),
	}))
	return toMinionSessionModel(s), nil
}

func (r *minionSessionResolver) MinionSession(ctx context.Context, id string) (*model.MinionSession, error) {
	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid session ID")
	}
	s, err := r.sessions.FindBySessionID(ctx, sid)
	if repository.IsNotFound(err) {
		return r.reservation(ctx, sid)
	}
	if err != nil {
		return nil, err
	}
	if _, err := r.viewer(ctx, s.OperationID.String()); err != nil {
		return nil, err
	}
	return toMinionSessionModel(s), nil
}

// reservation is a session reserved with a build, not claimed yet, as a
// RESERVED session: its ids and nothing else.
func (r *minionSessionResolver) reservation(ctx context.Context, sid uuid.UUID) (*model.MinionSession, error) {
	if r.reservations == nil {
		return nil, nil
	}
	res, err := r.reservations.FindBySessionID(ctx, sid)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := r.viewer(ctx, res.OperationID.String()); err != nil {
		return nil, err
	}
	return &model.MinionSession{ID: res.SessionID.String(), State: "RESERVED", OperationID: res.OperationID.String(),
		BuildID: res.BuildID.String(), Persisted: true, Addrs: []string{}, Peers: []*model.MinionSessionPeer{},
		Restarts: []*model.MinionSessionRestart{}, CreatedAt: res.CreatedAt.Format(time.RFC3339)}, nil
}

func (r *minionSessionResolver) MinionSessionValues(ctx context.Context, operationID string, column string) ([]string, error) {
	op, err := r.viewer(ctx, operationID)
	if err != nil {
		return nil, err
	}
	values, err := r.sessions.Values(ctx, op, column)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	return values, nil
}

func (r *minionSessionResolver) MinionSessionsLayout(ctx context.Context) (*model.MinionSessionsLayout, error) {
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}
	u, err := r.userRepo.FindByID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	return toSessionsLayoutModel(u.MinionSessionsLayout), nil
}

// SetMinionSessionsLayout keeps the caller's layout. No event: only the
// caller reads it, and their own client already has it.
func (r *minionSessionResolver) SetMinionSessionsLayout(ctx context.Context, in *model.MinionSessionsLayoutInput) (*model.MinionSessionsLayout, error) {
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}
	var layout *models.MinionSessionsLayout
	if in != nil {
		layout = &models.MinionSessionsLayout{Columns: in.Columns, Split: in.Split}
		if in.Sort != nil {
			s := fromSessionSortInput(in.Sort)
			layout.Sort = &s
		}
		if layout.Filters, err = fromSessionFilterInputs(in.Filters); err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
		if err := layout.Validate(); err != nil {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
	}
	if err := r.preferences.SetMinionSessionsLayout(ctx, uid, layout); err != nil {
		return nil, fmt.Errorf("store the layout: %w", err)
	}
	return toSessionsLayoutModel(layout), nil
}

func fromSessionSortInput(in *model.MinionSessionSortInput) models.MinionSessionSort {
	return models.MinionSessionSort{Column: in.Column, Desc: in.Direction == model.SortDirectionDesc}
}

// fromSessionFilterInputs reads filters; Validate checks what they mean.
func fromSessionFilterInputs(in []*model.MinionSessionFilterInput) ([]models.MinionSessionFilter, error) {
	out := make([]models.MinionSessionFilter, 0, len(in))
	for _, f := range in {
		m := models.MinionSessionFilter{Column: f.Column, Op: string(f.Op), Text: deref(f.Text), Values: f.Values, Min: f.Min, Max: f.Max}
		if f.Seconds != nil {
			s := int64(*f.Seconds)
			m.Seconds = &s
		}
		for _, t := range []struct {
			in  *string
			out **time.Time
		}{{f.From, &m.From}, {f.To, &m.To}} {
			if t.in == nil || *t.in == "" {
				continue
			}
			v, err := time.Parse(time.RFC3339, *t.in)
			if err != nil {
				return nil, fmt.Errorf("filter on %s: %q is not an RFC 3339 time", f.Column, *t.in)
			}
			*t.out = &v
		}
		out = append(out, m)
	}
	return out, nil
}

// toSessionsLayoutModel is a stored layout for GraphQL; nil stays nil (the
// default).
func toSessionsLayoutModel(l *models.MinionSessionsLayout) *model.MinionSessionsLayout {
	if l == nil {
		return nil
	}
	out := &model.MinionSessionsLayout{Columns: l.Columns, Filters: []*model.MinionSessionFilter{}, Split: l.Split}
	if l.Sort != nil {
		dir := model.SortDirectionAsc
		if l.Sort.Desc {
			dir = model.SortDirectionDesc
		}
		out.Sort = &model.MinionSessionSort{Column: l.Sort.Column, Direction: dir}
	}
	for _, f := range l.Filters {
		m := &model.MinionSessionFilter{Column: f.Column, Op: model.MinionSessionFilterOp(f.Op), Text: strPtr(f.Text),
			Values: f.Values, Min: f.Min, Max: f.Max, From: timePtrNano(f.From), To: timePtrNano(f.To)}
		if f.Seconds != nil {
			s := int(*f.Seconds)
			m.Seconds = &s
		}
		out.Filters = append(out.Filters, m)
	}
	return out
}

func timePtrNano(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func (r *minionSessionResolver) Load(ctx context.Context, sessionID string) *model.MinionSession {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return nil
	}
	s, err := r.sessions.FindBySessionID(ctx, id)
	if err != nil {
		return nil
	}
	return toMinionSessionModel(s)
}

func toMinionSessionModel(s *models.MinionSession) *model.MinionSession {
	h := s.Hello
	out := &model.MinionSession{
		ID: s.SessionID.String(), State: sessionState(s), Number: int(s.Number), OperationID: s.OperationID.String(), BuildID: s.BuildID.String(),
		BuildDescription: s.BuildDescription, ModuleName: s.ModuleName, Target: s.Target, Platform: s.Platform,
		Staged: s.Staged, Pid: int(h.PID), Os: h.OS, Arch: h.Arch, Host: strPtr(h.Host), User: strPtr(h.User),
		Domain: strPtr(h.Domain), Proc: strPtr(h.Proc), Integrity: strPtr(h.Integrity), Addrs: orEmpty(h.Addrs),
		Version: strPtr(h.Version), Address: strPtr(s.Address), AddressPicked: s.AddressPicked,
		PeerAddress: strPtr(s.PeerAddress), Peers: toSessionPeerModels(s.Peers),
		Persisted: s.Persisted, RestartCount: int(s.RestartCount), Restarts: toSessionRestartModels(s.Restarts),
		LateAt: timePtr(s.LateAt), CreatedAt: s.CreatedAt.Format(time.RFC3339), LastSeenAt: timePtr(s.LastSeenAt),
	}
	if len(h.X) > 0 {
		out.X = h.X
	}
	if s.Sleep != nil {
		interval, jitter := int(s.Sleep.Interval), int(s.Sleep.Jitter)
		out.SleepInterval, out.SleepJitter = &interval, &jitter
	}
	return out
}

func toSessionPeerModels(peers []models.MinionSessionPeer) []*model.MinionSessionPeer {
	out := make([]*model.MinionSessionPeer, len(peers))
	for i, p := range peers {
		out[i] = &model.MinionSessionPeer{Address: p.Address, FirstSeen: p.FirstSeen.UTC().Format(time.RFC3339),
			LastSeen: p.LastSeen.UTC().Format(time.RFC3339), Count: int(p.Count)}
	}
	return out
}

func toSessionRestartModels(restarts []models.MinionSessionRestart) []*model.MinionSessionRestart {
	out := make([]*model.MinionSessionRestart, len(restarts))
	for i, r := range restarts {
		out[i] = &model.MinionSessionRestart{At: r.At.UTC().Format(time.RFC3339), Pid: int(r.PID), //nolint:gosec // a pid fits
			Host: strPtr(r.Host), BuildID: r.BuildID.String()}
	}
	return out
}

func sessionState(s *models.MinionSession) string {
	if s.Closed(time.Now().UTC()) {
		return "CLOSED"
	}
	return "OPEN"
}
