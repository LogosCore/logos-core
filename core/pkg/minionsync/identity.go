package minionsync

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Identity (docs: contracts/minion-messages/identity): who a
// minion's work runs as when not its process's own. A session's identity
// (session.Acting) is what the tasks it acks from then on run as; a task's
// (task.As) is what it said of one task. Each change goes in the session's
// history (models.MinionIdentityEvent), as do its process's (a hello, a
// rehello that changes it). Nothing answers an identity but an err on a bad
// one; one sent again changes nothing.

// WithIdentities keeps sessions' identity histories; without it the
// session's and tasks' identities are kept, their history not.
func (s *Service) WithIdentities(identities repository.IMinionIdentityRepository) *Service {
	s.identities = identities
	return s
}

// readIdentity reads and checks an identity item.
func readIdentity(raw cbor.RawMessage) (protocol.IdentityItem, error) {
	var it protocol.IdentityItem
	if err := protocol.UnmarshalCBOR(raw, &it); err != nil {
		return it, errors.New("the identity does not read")
	}
	return it, it.Validate()
}

// takeIdentity keeps an identity: the session's, or one task's.
func (s *Service) takeIdentity(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	it, err := readIdentity(raw)
	if err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), i)
	}
	if s.tasks == nil {
		return errItem(protocol.ErrItemUnknownItem, "no tasks are taken", i)
	}
	var task *models.MinionTask
	if it.Task != nil {
		id, _ := uuid.FromBytes(it.Task)
		task, err = s.tasks.FindByTaskID(ctx, id)
		if err != nil || task.SessionID != session.SessionID {
			e, err := errItem(protocol.ErrItemUnknownTask, "no task of this session", i)
			if err == nil && len(e) > 0 {
				e = withTask(e, it.Task)
			}
			return e, err
		}
	}
	var credential *uuid.UUID
	if it.Credential != "" {
		id, err := uuid.Parse(it.Credential)
		if err != nil || task == nil || !slices.Contains(task.CredentialIDs, id) {
			return errItem(protocol.ErrItemBadMessage, "identity: the credential is not one its task gave", i)
		}
		credential = &id
	}
	now := s.now()
	ident := models.IdentityFrom(it, credential, now)
	if it.Scope == protocol.IdentityTask {
		if err := s.taskIdentity(ctx, session, task, ident); err != nil {
			return nil, err
		}
	} else if err := s.sessionIdentity(ctx, session, ident); err != nil {
		return nil, err
	}
	if credential != nil && s.creds != nil {
		// Used and accepted: as a login that worked.
		if err := s.creds.LoginWorked(ctx, session.OperationID, *credential); err != nil {
			s.logger.Warn("mark a credential an identity took valid", zap.String("credential_id", credential.String()), zap.Error(err))
		}
	}
	return nil, nil
}

// withTask adds the task an unknown_task names to an err item.
func withTask(items []cbor.RawMessage, task []byte) []cbor.RawMessage {
	var e protocol.ErrItem
	if protocol.UnmarshalCBOR(items[0], &e) != nil {
		return items
	}
	e.Task = task
	if b, err := protocol.MarshalCBOR(e); err == nil {
		items[0] = b
	}
	return items
}

// sessionIdentity keeps who the session's tasks run as from now on: a
// change goes in its history.
func (s *Service) sessionIdentity(ctx context.Context, session *models.MinionSession, ident models.MinionIdentity) error {
	var acting *models.MinionIdentity
	if !ident.Own() {
		acting = &ident
	}
	if (acting == nil && session.Acting == nil) || (acting != nil && session.Acting != nil && acting.SameAs(*session.Acting)) {
		return nil
	}
	if err := s.sessions.SetActing(ctx, session.SessionID, acting); err != nil {
		return fmt.Errorf("keep the session's identity: %w", err)
	}
	session.Acting = acting
	e := s.identityEvent(session, models.IdentityScopeSession, ident)
	if acting == nil {
		// Back to the process's own: who that is.
		e.User, e.Domain, e.Integrity = session.Hello.User, session.Hello.Domain, session.Hello.Integrity
	}
	s.addIdentityEvent(ctx, e)
	s.logger.Info("minion identity", zap.String("session_id", session.SessionID.String()), zap.String("how", ident.How))
	s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
	return nil
}

// taskIdentity keeps who a task ran as: in the history when another than
// the process's own.
func (s *Service) taskIdentity(ctx context.Context, session *models.MinionSession, task *models.MinionTask, ident models.MinionIdentity) error {
	ident.TaskID = nil
	if task.As != nil && task.As.SameAs(ident) {
		return nil
	}
	if _, err := s.tasks.SetAs(ctx, session.SessionID, task.TaskID, ident); err != nil {
		return fmt.Errorf("keep the task's identity: %w", err)
	}
	task.As = &ident
	if s.identities != nil {
		if ident.Own() {
			if err := s.identities.DropTask(ctx, session.SessionID, task.TaskID); err != nil {
				s.logger.Warn("identity history", zap.Error(err))
			}
		} else {
			e := s.identityEvent(session, models.IdentityScopeTask, ident)
			id := task.TaskID
			e.TaskID = &id
			if err := s.identities.PutTask(ctx, &e); err != nil {
				s.logger.Warn("identity history", zap.Error(err))
			}
		}
	}
	s.publishTask(task)
	return nil
}

func (s *Service) identityEvent(session *models.MinionSession, scope string, ident models.MinionIdentity) models.MinionIdentityEvent {
	return models.MinionIdentityEvent{EventID: uuid.New(), OperationID: session.OperationID, SessionID: session.SessionID, At: s.now(),
		Scope: scope, User: ident.User, Domain: ident.Domain, Integrity: ident.Integrity, How: ident.How,
		CredentialID: ident.CredentialID, TaskID: ident.TaskID}
}

func (s *Service) addIdentityEvent(ctx context.Context, e models.MinionIdentityEvent) {
	if s.identities == nil {
		return
	}
	if err := s.identities.Add(ctx, &e); err != nil {
		s.logger.Warn("identity history", zap.String("session_id", e.SessionID.String()), zap.Error(err))
	}
}

// processIdentity notes the process's own identity in the history: as the
// session opens, and when a rehello changes it.
func (s *Service) processIdentity(ctx context.Context, session *models.MinionSession, was *models.MinionHello) {
	h := session.Hello
	if was != nil && was.User == h.User && was.Domain == h.Domain && was.Integrity == h.Integrity {
		return
	}
	s.addIdentityEvent(ctx, models.MinionIdentityEvent{EventID: uuid.New(), OperationID: session.OperationID, SessionID: session.SessionID,
		At: s.now(), Scope: models.IdentityScopeProcess, User: h.User, Domain: h.Domain, Integrity: h.Integrity})
}

// actingOf is the task whose identity is in effect for session (its acting
// identity's), as a call or an open to an agentless kind carries it: a
// channel acting as the minion keeps nothing between calls, so it takes it
// up again from there. Its args as the task carried them. nil for none.
func (s *Service) actingOf(ctx context.Context, session *models.MinionSession) *protocol.LinkActing {
	if session == nil || session.Acting == nil || session.Acting.TaskID == nil || s.tasks == nil {
		return nil
	}
	t, err := s.tasks.FindByTaskID(ctx, *session.Acting.TaskID)
	if err != nil || t.SessionID != session.SessionID {
		return nil
	}
	var args protocol.Args
	if len(t.WireArgs) > 0 {
		if err := protocol.UnmarshalCBOR(t.WireArgs, &args); err != nil {
			s.logger.Warn("the acting task's args do not read", zap.String("task_id", t.TaskID.String()), zap.Error(err))
			return nil
		}
	}
	return &protocol.LinkActing{Task: t.TaskID[:], Cmd: uint64(t.Op), Args: args}
}
