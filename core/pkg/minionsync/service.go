// Package minionsync opens what a minion sends through a channel and answers
// it (docs: contracts/minion-messages.md). Built so far: staging and the
// session. A minion's first message, sealed with its build's keys and sent
// under its build id, carries a stage: its RSA public key and a correlation
// id. Core answers with a temporary id and new keys for the build's suite,
// encrypted to that key. The minion's hello, under the temporary id and
// sealed with those keys, opens a session with them (a hello straight under
// the build id opens one with the build's own keys), and every message after
// is sent under the session id. Under a session core records the check-in,
// takes a hello again (a rehello) and get_tasks (no tasks wait yet); the
// other task items are not built yet.
package minionsync

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/cache"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/payloadcrypto"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// ErrUnopened means the message did not open: no keys for its id, or a
// frame that fails its checks. The sync call answers 422, and nothing more:
// core cannot tell who sent it.
var ErrUnopened = errors.New("the message does not open")

// replayWindow is how long core remembers a minion message's id.
const replayWindow = 24 * time.Hour

const replayKeyPrefix = "minion:msg:"

// Builds is what the service reads of builds.
type Builds interface {
	FindByBuildID(ctx context.Context, buildID uuid.UUID) (*models.Build, error)
}

// sender is who a message's id names, with the keys it is sealed with: a
// session, a staging (a temporary id) or a build. A staging comes with its
// build.
type sender struct {
	build   *models.Build
	staging *models.MinionStaging
	session *models.MinionSession
	crypto  protocol.BuildCrypto
	keys    protocol.AES256CBCHMACSHA256Material
}

// Service handles minion messages.
type Service struct {
	builds   Builds
	stagings repository.IMinionStagingRepository
	sessions repository.IMinionSessionRepository
	cache    cache.Cache
	bus      eventbus.IEventBus
	logger   *zap.Logger
	now      func() time.Time
}

func NewService(builds Builds, stagings repository.IMinionStagingRepository, sessions repository.IMinionSessionRepository,
	c cache.Cache, bus eventbus.IEventBus, logger *zap.Logger) *Service {
	return &Service{builds: builds, stagings: stagings, sessions: sessions, cache: c, bus: bus,
		logger: logger.Named("minionsync"), now: func() time.Time { return time.Now().UTC() }}
}

// Handle opens a minion's sealed frame sent under id from peer (the IP
// address its channel got it from, "" for unknown) and returns core's sealed
// answer: nil when there is nothing to say (the sync call's 204).
func (s *Service) Handle(ctx context.Context, id, peer string, frame []byte) ([]byte, error) {
	sentUnder, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrUnopened
	}
	from, err := s.sender(ctx, sentUnder)
	if err != nil {
		return nil, err
	}
	plain, err := protocol.OpenFrame(from.keys, sentUnder, protocol.FrameMinionToCore, frame)
	if err != nil {
		s.logger.Info("minion message does not open", zap.String("id", id))
		return nil, ErrUnopened
	}
	var msg protocol.MinionMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		s.logger.Info("minion message is not CBOR", zap.String("id", id), zap.Error(err))
		return nil, ErrUnopened
	}
	if err := protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)); err != nil || msg.ID == nil {
		s.logger.Info("minion message head", zap.String("id", id), zap.Error(err))
		return nil, ErrUnopened
	}

	fresh, err := s.cache.SetNX(ctx, replayKeyPrefix+hex.EncodeToString(msg.ID), "1", replayWindow)
	if err != nil {
		s.logger.Warn("minion replay check failed, handling the message anyway", zap.Error(err))
	} else if !fresh {
		s.logger.Info("minion message replayed, dropped", zap.String("id", id))
		return nil, nil
	}

	var items []cbor.RawMessage
	switch {
	case from.session != nil:
		items, err = s.underSession(ctx, from.session, peer, msg)
	case from.staging != nil:
		items, err = s.underStaging(ctx, from, peer, msg)
	default:
		items, err = s.underBuild(ctx, from, peer, msg)
	}
	if err != nil || len(items) == 0 {
		return nil, err
	}
	reply, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV1, Re: msg.ID, Items: items})
	if err != nil {
		return nil, err
	}
	return protocol.SealFrame(from.keys, sentUnder, protocol.FrameCoreToMinion, reply)
}

// underBuild answers a message sent under a build id: one stage, or one
// hello that opens a session with the build's own keys.
func (s *Service) underBuild(ctx context.Context, from *sender, peer string, msg protocol.MinionMessage) ([]cbor.RawMessage, error) {
	if len(msg.Items) == 1 && protocol.ItemType(msg.Items[0]) == protocol.ItemHello {
		return s.openSession(ctx, from, peer, msg.Items[0])
	}
	if len(msg.Items) != 1 || protocol.ItemType(msg.Items[0]) != protocol.ItemStage {
		return errItem(protocol.ErrItemBadMessage, "under a build id the only item taken is one stage or one hello", 0)
	}
	b := from.build
	var stage protocol.StageItem
	if err := protocol.UnmarshalCBOR(msg.Items[0], &stage); err != nil {
		return errItem(protocol.ErrItemBadMessage, "the stage does not read", 0)
	}
	pub, err := protocol.ParseStageKey(stage.Key)
	if err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), 0)
	}
	if len(stage.Corr) != 16 {
		return errItem(protocol.ErrItemBadMessage, "corr is 16 bytes", 0)
	}

	fresh, err := payloadcrypto.NewKeys(b.Crypto.Suite)
	if err != nil {
		return nil, err
	}
	var material protocol.AES256CBCHMACSHA256Material
	if err := json.Unmarshal(fresh.Material, &material); err != nil {
		return nil, err
	}
	asCBOR, err := protocol.MarshalCBOR(material)
	if err != nil {
		return nil, err
	}
	sealed, err := protocol.EncryptStageKeys(pub, asCBOR)
	if err != nil {
		return nil, fmt.Errorf("encrypt the stage keys: %w", err)
	}
	staging := &models.MinionStaging{
		TempID: uuid.New(), BuildID: b.BuildID, OperationID: b.OperationID, Crypto: *fresh, CreatedAt: s.now(),
	}
	if err := s.stagings.Insert(ctx, staging); err != nil {
		return nil, fmt.Errorf("store the staging: %w", err)
	}
	s.logger.Info("minion staged", zap.String("build_id", b.BuildID.String()),
		zap.String("temp_id", staging.TempID.String()), zap.String("operation_id", b.OperationID.String()))

	item, err := protocol.MarshalCBOR(protocol.StageResponseItem{
		T: protocol.ItemStageResponse, TID: staging.TempID[:], Keys: sealed, Corr: stage.Corr,
	})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

// underStaging answers a message sent under a temporary id: one hello,
// which opens a session with the keys the stage got.
func (s *Service) underStaging(ctx context.Context, from *sender, peer string, msg protocol.MinionMessage) ([]cbor.RawMessage, error) {
	if len(msg.Items) != 1 || protocol.ItemType(msg.Items[0]) != protocol.ItemHello {
		return errItem(protocol.ErrItemBadMessage, "under a temporary id the only item taken is one hello", 0)
	}
	return s.openSession(ctx, from, peer, msg.Items[0])
}

// openSession reads a hello and opens a session for it with the keys of the
// id it came under. A temporary id serves one hello: its entry goes first,
// so of two hellos racing on it one opens a session.
func (s *Service) openSession(ctx context.Context, from *sender, peer string, raw cbor.RawMessage) ([]cbor.RawMessage, error) {
	hello, err := readHello(raw)
	if err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), 0)
	}
	b, now := from.build, s.now()
	expires := now.Add(repository.MinionSessionCheckInWindow)
	session := &models.MinionSession{
		SessionID: uuid.New(), OperationID: b.OperationID, BuildID: b.BuildID, BuildDescription: b.Description,
		ModuleName: b.ModuleName, Target: b.Target, Platform: b.Platform, Staged: from.staging != nil,
		Crypto: from.crypto, Hello: models.MinionHelloFrom(hello), Address: models.DefaultAddress(hello.Addrs),
		CreatedAt: now, ExpiresAt: &expires,
	}
	if peer != "" {
		session.PeerAddress = peer
		session.Peers = []models.MinionSessionPeer{{Address: peer, FirstSeen: now, LastSeen: now, Count: 1}}
	}
	if from.staging != nil {
		if err := s.stagings.Delete(ctx, from.staging.TempID); err != nil {
			if repository.IsNotFound(err) {
				return errItem(protocol.ErrItemBadMessage, "the temporary id served a hello already", 0)
			}
			return nil, fmt.Errorf("remove the staging: %w", err)
		}
	}
	if session.Number, err = s.sessions.NextNumber(ctx, b.OperationID); err != nil {
		return nil, fmt.Errorf("number the session: %w", err)
	}
	if err := s.sessions.Insert(ctx, session); err != nil {
		return nil, fmt.Errorf("store the session: %w", err)
	}
	s.logger.Info("minion session opened", zap.Int64("number", session.Number), zap.String("session_id", session.SessionID.String()),
		zap.String("build_id", b.BuildID.String()), zap.Bool("staged", session.Staged),
		zap.String("host", hello.Host), zap.Uint64("pid", hello.PID))
	s.bus.Publish(eventbus.NewMinionSessionCreatedEvent(sessionPayload(session)))
	return helloResponse(session.SessionID)
}

// underSession answers a message sent under a session id. Any message is a
// check-in; a hello replaces what the session recorded (a rehello) and gets
// the same session id back; a get_tasks is taken (no tasks wait yet). Other
// items are not built yet.
func (s *Service) underSession(ctx context.Context, session *models.MinionSession, peer string, msg protocol.MinionMessage) ([]cbor.RawMessage, error) {
	if err := s.sessions.CheckIn(ctx, session.SessionID, s.now(), peer); err != nil {
		return nil, fmt.Errorf("check in: %w", err)
	}
	var items []cbor.RawMessage
	for i, raw := range msg.Items {
		var out []cbor.RawMessage
		var err error
		switch protocol.ItemType(raw) {
		case protocol.ItemHello:
			var hello protocol.HelloItem
			if hello, err = readHello(raw); err != nil {
				out, err = errItem(protocol.ErrItemBadMessage, err.Error(), i)
				break
			}
			// An operator's pick stays while the host still has it.
			address, picked := models.DefaultAddress(hello.Addrs), false
			if session.AddressPicked && slices.Contains(hello.Addrs, session.Address) {
				address, picked = session.Address, true
			}
			if err = s.sessions.SetHello(ctx, session.SessionID, models.MinionHelloFrom(hello), address, picked); err != nil {
				return nil, fmt.Errorf("rehello: %w", err)
			}
			s.logger.Info("minion rehello", zap.String("session_id", session.SessionID.String()))
			out, err = helloResponse(session.SessionID)
		case protocol.ItemGetTasks:
			// The minion takes tasks. None are built yet, so none wait:
			// nothing to answer.
			var get protocol.GetTasksItem
			if protocol.UnmarshalCBOR(raw, &get) != nil {
				out, err = errItem(protocol.ErrItemBadMessage, "the get_tasks does not read", i)
			} else if vErr := get.Validate(); vErr != nil {
				out, err = errItem(protocol.ErrItemBadMessage, vErr.Error(), i)
			}
		default:
			out, err = errItem(protocol.ErrItemUnknownItem, "not taken under a session yet", i)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, out...)
	}
	s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
	return items, nil
}

// readHello reads and checks a hello.
func readHello(raw cbor.RawMessage) (protocol.HelloItem, error) {
	var hello protocol.HelloItem
	if err := protocol.UnmarshalCBOR(raw, &hello); err != nil {
		return hello, errors.New("the hello does not read")
	}
	return hello, hello.Validate()
}

func helloResponse(sid uuid.UUID) ([]cbor.RawMessage, error) {
	item, err := protocol.MarshalCBOR(protocol.HelloResponseItem{T: protocol.ItemHelloResponse, SID: sid[:]})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

func sessionPayload(s *models.MinionSession) eventbus.MinionSessionEventPayload {
	return eventbus.MinionSessionEventPayload{SessionID: s.SessionID.String(), OperationID: s.OperationID.String()}
}

// sender finds who id names: a session, a staging or a build, with a suite
// core implements, or ErrUnopened. A staging whose build was deleted does
// not open: a deleted build's minions open no new sessions.
func (s *Service) sender(ctx context.Context, id uuid.UUID) (*sender, error) {
	from := &sender{}
	session, err := s.sessions.FindBySessionID(ctx, id)
	switch {
	case err == nil:
		from.session, from.crypto = session, session.Crypto
		return from, from.readKeys()
	case !repository.IsNotFound(err):
		return nil, fmt.Errorf("load session: %w", err)
	}
	staging, err := s.stagings.FindByTempID(ctx, id)
	switch {
	case err == nil:
		from.staging, from.crypto = staging, staging.Crypto
		id = staging.BuildID
	case !repository.IsNotFound(err):
		return nil, fmt.Errorf("load staging: %w", err)
	}
	b, err := s.builds.FindByBuildID(ctx, id)
	if err != nil {
		if repository.IsNotFound(err) {
			s.logger.Info("minion message under an unknown id", zap.String("id", id.String()))
			return nil, ErrUnopened
		}
		return nil, fmt.Errorf("load build: %w", err)
	}
	from.build = b
	if from.staging == nil {
		if b.Crypto == nil {
			return nil, ErrUnopened
		}
		from.crypto = *b.Crypto
	}
	return from, from.readKeys()
}

// readKeys reads the sender's key material; a suite core does not implement
// does not open.
func (f *sender) readKeys() error {
	if f.crypto.Suite != protocol.CryptoAES256CBCHMACSHA256 {
		return ErrUnopened
	}
	if err := json.Unmarshal(f.crypto.Material, &f.keys); err != nil {
		return fmt.Errorf("read the keys: %w", err)
	}
	return nil
}

// errItem is a reply's one err item.
func errItem(code, msg string, item int) ([]cbor.RawMessage, error) {
	data, err := protocol.MarshalCBOR(protocol.ErrItem{T: protocol.ItemErr, Code: code, Msg: msg, Item: &item})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{data}, nil
}
