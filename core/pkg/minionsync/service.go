// Package minionsync opens what a minion sends through a channel and answers
// it (docs: contracts/minion-messages.md). Built so far: staging and the
// session. A minion's first message, sealed with its build's keys and sent
// under its build id, carries a stage: its RSA public key and a correlation
// id. Core answers with a temporary id and new keys for the build's suite,
// encrypted to that key. The minion's hello, under the temporary id and
// sealed with those keys, opens a session with them (a hello straight under
// the build id opens one with the build's own keys), and every message after
// is sent under the session id. Under a session core records the check-in,
// takes a hello again (a rehello), and runs the tasks operators gave it
// (tasks.go): get_tasks, acks, results and cancels; files move in chunks
// (files.go, uploads.go, streams.go). What core sends a minion comes from
// one place whatever the connection's kind (outbox.go).
package minionsync

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// ErrReplayed means the message's id was seen before: a captured message
// sent again. The sync call answers 409, and the channel what any failure
// gets, so whoever sent it learns nothing.
var ErrReplayed = errors.New("the message was seen before")

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
	// replaced: the message opened with keys a claim replaced; keys are
	// those.
	replaced bool
	// place is the build connection a hello came through when a target's
	// call or connection brought it (0: the hello says): a minion a
	// channel acts as knows no build, and says none.
	place int
}

// seal is a message core sends under id, sealed with the sender's keys.
func (f *sender) seal(id uuid.UUID, plain []byte) ([]byte, error) {
	return protocol.SealFrame(f.keys, id, protocol.FrameCoreToMinion, plain)
}

// open is a message the minion sent under id, opened with the sender's
// keys.
func (f *sender) open(id uuid.UUID, frame []byte) ([]byte, error) {
	return protocol.OpenFrame(f.keys, id, protocol.FrameMinionToCore, frame)
}

// linkKeys are the sender's keys as a call or an open gives them to a
// channel that acts as the minion (protocol.LinkKeys).
func (f *sender) linkKeys() *protocol.LinkKeys {
	return &protocol.LinkKeys{Suite: f.crypto.Suite, Material: f.keys}
}

// Service handles minion messages.
type Service struct {
	builds   Builds
	stagings repository.IMinionStagingRepository
	sessions repository.IMinionSessionRepository
	// reservations are the session ids reserved with builds, a first claim
	// opens.
	reservations repository.IMinionSessionReservationRepository
	// tasks are what operators queue for sessions (tasks.go); nil takes none.
	tasks repository.IMinionTaskRepository
	// files takes the chunks of files minions send (files.go); nil takes
	// none.
	files Files
	// feedback keeps minions' warnings and errors (feedback.go); nil drops
	// them.
	feedback repository.IMinionFeedbackRepository
	// streams relays the files minions stream (streams.go); nil relays
	// none.
	streams Streams
	// fileBrowser is told of tasks' ends (streams.go); nil: nothing is.
	fileBrowser FileBrowser
	cache       cache.Cache
	bus         eventbus.IEventBus
	logger      *zap.Logger
	now         func() time.Time
	// conns are the persistent connections open, by session
	// (connections.go).
	conns *connections
	// targets are bind minions' targets, and link reaches their channels
	// (bind.go); nil calls none.
	targets repository.IMinionTargetRepository
	link    Linker
	binds   *binds
	// persist are bind-persistent targets' opens and connections
	// (bind_persistent.go).
	persist *persistent
	// reacks are the tasks core asks minions about (reacks.go).
	reacks *reacks
	// answers are the pushes core waits for the minions' answers to
	// (connections.go).
	answers *answers
	// channels are the channels' connection schemas: which kinds are
	// agentless.
	channels ChannelSchemas
	// targetChecker checks a target's values before each call, and
	// problemsText says its problems in its state detail; nil calls every
	// target.
	targetChecker TargetChecker
	problemsText  func(map[string]string) string
}

func NewService(builds Builds, stagings repository.IMinionStagingRepository, sessions repository.IMinionSessionRepository,
	reservations repository.IMinionSessionReservationRepository, c cache.Cache, bus eventbus.IEventBus, logger *zap.Logger) *Service {
	return &Service{builds: builds, stagings: stagings, sessions: sessions, reservations: reservations, cache: c, bus: bus,
		logger: logger.Named("minionsync"), now: func() time.Time { return time.Now().UTC() },
		conns: newConnections(), binds: newBinds(), persist: newPersistent(), reacks: newReacks(), answers: newAnswers()}
}

// Handle opens a minion's sealed frame sent under id from peer (the IP
// address its channel got it from, "" for unknown) and returns core's sealed
// answer: nil when there is nothing to say (the sync call's 204).
func (s *Service) Handle(ctx context.Context, id, peer string, frame []byte) ([]byte, error) {
	return s.Exchange(ctx, nil, id, peer, frame)
}

// Exchange is Handle for a message that came over via, a persistent
// connection (nil: the sync call). Over one, a request always gets a reply,
// empty when core has nothing to say, so the minion knows it arrived; and a
// message may also answer one core sent there (re): its items are taken,
// what core has to say back goes as a message of its own, and it gets no
// reply.
func (s *Service) Exchange(ctx context.Context, via *Conn, id, peer string, frame []byte) ([]byte, error) {
	if via != nil {
		via.touch(s.now())
	}
	sentUnder, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrUnopened
	}
	from, err := s.sender(ctx, sentUnder)
	if err != nil {
		return nil, err
	}
	if via != nil {
		from.place = via.placeOf()
	}
	if from.place == 0 && from.build != nil {
		// A hello that names no connection came through the one that
		// carried it, when only one of the build's fits.
		from.place = carrierPlace(from.build, carrierKind(via), ChannelOf(ctx))
	}
	plain, err := from.open(sentUnder, frame)
	if err != nil && from.session != nil {
		// Not the session's keys: maybe an earlier holder's, replaced by a
		// claim. It is told so.
		plain, err = s.openReplaced(from, sentUnder, frame)
	}
	if err != nil {
		s.logger.Info("minion message does not open", zap.String("id", id))
		return nil, ErrUnopened
	}
	var msg protocol.MinionMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		s.logger.Info("minion message is not CBOR", zap.String("id", id), zap.Error(err))
		return nil, ErrUnopened
	}
	if err := protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)); err != nil {
		s.logger.Info("minion message head", zap.String("id", id), zap.Error(err))
		return nil, ErrUnopened
	}
	// An answer is fresh when it answers a message core pushed to the
	// session and still waits for, over any of its sockets (the minion may
	// have connected again): a replayed one answers nothing.
	answer := msg.Re != nil
	if answer {
		if via == nil || from.session == nil || !s.answers.take(from.session.SessionID, msg.Re, s.now()) {
			s.logger.Info("minion answer to nothing core waits for", zap.String("id", id))
			return nil, ErrUnopened
		}
	} else {
		fresh, err := s.cache.SetNX(ctx, replayKeyPrefix+hex.EncodeToString(msg.ID), "1", replayWindow)
		if err != nil {
			s.logger.Warn("minion replay check failed, handling the message anyway", zap.Error(err))
		} else if !fresh {
			s.logger.Info("minion message replayed, refused", zap.String("id", id))
			return nil, ErrReplayed
		}
	}

	if via != nil && via.target != uuid.Nil {
		// Over a bind-persistent target's connection only its minion
		// speaks: one of another build reached at its address is not it,
		// and the connection closes.
		if !via.ofTarget(from) {
			s.logger.Info("targets: a minion of another build spoke over a target's connection", zap.String("target_id", via.target.String()),
				zap.String("id", id))
			via.refuse("a minion of another build spoke over the connection")
			return nil, ErrUnopened
		}
		via.hear(s.now())
		via.carry(msg.Items)
	}
	var items []cbor.RawMessage
	switch {
	case from.replaced:
		s.logger.Info("minion message from a replaced holder", zap.String("session_id", id))
		items, err = errMessage(protocol.ErrItemSessionReplaced, "another process claimed the session: start over without claiming")
		if via != nil {
			// Its socket is no longer the session's: the process starts
			// over, and its new session is the socket's.
			via.disown(from.session.SessionID)
		}
	case from.session != nil && from.session.Closed(s.now()):
		// A tombstone: the one thing to tell its minion.
		s.logger.Info("minion message under a closed session", zap.String("session_id", id))
		items, err = errMessage(protocol.ErrItemSessionClosed, "the session is closed: start over")
		if via != nil {
			s.conns.drop(from.session.SessionID, via)
			via.disown(from.session.SessionID)
		}
	case from.session != nil:
		p := pathSync
		if via != nil {
			s.seenOn(ctx, via, from.session)
			p = pathSocket
		}
		items, err = s.underSession(ctx, from.session, peer, msg, via, p)
	case from.staging != nil:
		items, err = s.underStaging(ctx, from, peer, msg)
	default:
		items, err = s.underBuild(ctx, from, peer, msg)
	}
	if err != nil {
		return nil, err
	}
	if via != nil && from.session == nil {
		// A hello over a persistent connection opened a session: the
		// connection serves it from now on (pushes can go at once), and a
		// bind-persistent target's is linked to it.
		s.seenOnHello(ctx, via, items)
	}
	if via != nil {
		via.carry(items)
	}
	// Through a call, nothing to say is the sync call's 204. Over a
	// connection socket a request always gets its reply, empty or not, so
	// the minion knows its message arrived.
	if len(items) == 0 && (via == nil || answer) {
		return nil, nil
	}
	if answer {
		// An answer gets no reply: what core has to say back goes as a
		// message of its own.
		return nil, s.push(ctx, via, from, items)
	}
	reply, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV3, Re: msg.ID, Items: items})
	if err != nil {
		return nil, err
	}
	return from.seal(sentUnder, reply)
}

// seenOnHello records that via serves the session a hello_response among
// items names.
func (s *Service) seenOnHello(ctx context.Context, via *Conn, items []cbor.RawMessage) {
	for _, raw := range items {
		if protocol.ItemType(raw) != protocol.ItemHelloResponse {
			continue
		}
		var r protocol.HelloResponseItem
		if protocol.UnmarshalCBOR(raw, &r) != nil {
			return
		}
		sid, err := uuid.FromBytes(r.SID)
		if err != nil {
			return
		}
		if session, err := s.sessions.FindBySessionID(ctx, sid); err == nil {
			s.seenOn(ctx, via, session)
		}
		return
	}
}

// underBuild answers a message sent under a build id: one stage, or one
// hello that opens a session with the build's own keys.
func (s *Service) underBuild(ctx context.Context, from *sender, peer string, msg protocol.MinionMessage) ([]cbor.RawMessage, error) {
	if err := protocol.ValidateBuildMessage(msg.Items); err != nil {
		// Said: a minion that sends more is told only in an err item, which
		// it may not read for what it is.
		s.logger.Info("minion message under its build id refused", zap.String("build_id", from.build.BuildID.String()),
			zap.Int("items", len(msg.Items)), zap.Error(err))
		return errItem(protocol.ErrItemBadMessage, err.Error(), 0)
	}
	if protocol.ItemType(msg.Items[0]) == protocol.ItemHello {
		return s.openSession(ctx, from, peer, msg.Items[0])
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
	if from.staging != nil {
		if err := s.stagings.Delete(ctx, from.staging.TempID); err != nil {
			if repository.IsNotFound(err) {
				return errItem(protocol.ErrItemBadMessage, "the temporary id served a hello already", 0)
			}
			return nil, fmt.Errorf("remove the staging: %w", err)
		}
	}
	if hello.SID != nil {
		items, claimed, err := s.claim(ctx, from, peer, hello)
		if err != nil || claimed {
			return items, err
		}
		// Not honoured: a new session, as without a claim.
	}
	return s.newSession(ctx, from, peer, hello, uuid.New(), false)
}

// newSession opens a session with id for a hello: a new one, or one its
// build reserved (persisted).
func (s *Service) newSession(ctx context.Context, from *sender, peer string, hello protocol.HelloItem, id uuid.UUID, persisted bool) ([]cbor.RawMessage, error) {
	b, now := from.build, s.now()
	// Until its first check-in a session is purged after the day's wait and
	// its tombstone's time.
	purge := now.Add(models.MinionSessionCheckInWindow + models.MinionSessionTombstone)
	session := &models.MinionSession{
		SessionID: id, OperationID: b.OperationID, BuildID: b.BuildID, BuildDescription: b.Description, Persisted: persisted,
		Description: b.Description, DescriptionSet: true,
		ModuleName: b.ModuleName, Target: b.Target, Platform: b.Platform, Staged: from.staging != nil,
		Crypto: from.crypto, Hello: models.MinionHelloFrom(hello), Address: models.DefaultAddress(hello.AddrStrings()),
		MaxTasks: models.MaxTasksOf(hello), MaxTasksSettable: b.MaxTasks != nil, CreatedAt: now, PurgeAt: &purge,
	}
	place := connPlace(hello.Conn)
	if place == 0 {
		place = from.place
	}
	conn := connOf(b, place)
	session.Conn, session.ConnKind, session.ConnName, session.ConnCount = conn.Place, conn.Kind, conn.Name, len(b.Connections.Items)
	session.ConnMaxMessage = conn.MaxMessage
	if peer != "" {
		session.PeerAddress = peer
		session.Peers = []models.MinionSessionPeer{{Address: peer, FirstSeen: now, LastSeen: now, Count: 1}}
	}
	if hello.Sleep != nil {
		session.Sleep = models.SleepFrom(*hello.Sleep)
		if models.LateBySchedule(session.ConnKind) {
			late, lost := session.Sleep.LateAtFrom(now), session.Sleep.LostAtFrom(now)
			session.LateAt, session.LostAt = &late, &lost
		}
	}
	var err error
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

// claim gives a persisted session to the process whose hello claims it,
// when its build names that session and the session is open: the session
// takes the claim's keys, build and host, the earlier holder's keys are kept
// to tell it session_replaced, and the restart is recorded. claimed is false
// for a claim not honoured, which opens a new session instead.
func (s *Service) claim(ctx context.Context, from *sender, peer string, hello protocol.HelloItem) (items []cbor.RawMessage, claimed bool, err error) {
	b, now := from.build, s.now()
	sid := uuid.UUID(hello.SID)
	refuse := func(why string) ([]cbor.RawMessage, bool, error) {
		s.logger.Info("minion claim not honoured, a new session instead", zap.String("session_id", sid.String()),
			zap.String("build_id", b.BuildID.String()), zap.String("why", why))
		return nil, false, nil
	}
	if b.PersistedSessionID == nil || *b.PersistedSessionID != sid {
		return refuse("the build does not persist this session")
	}
	sess, err := s.sessions.FindBySessionID(ctx, sid)
	switch {
	case repository.IsNotFound(err):
		// Not a session yet: maybe reserved with a build, for this first
		// claim to open.
		return s.claimReserved(ctx, from, peer, hello, sid, refuse)
	case err != nil:
		return nil, false, fmt.Errorf("load the claimed session: %w", err)
	case sess.OperationID != b.OperationID:
		return refuse("the session is another operation's")
	case sess.Closed(now):
		return refuse("the session is closed")
	}
	// An operator's pick of address stays while the host still has it.
	address, picked := models.DefaultAddress(hello.AddrStrings()), false
	if sess.AddressPicked && slices.Contains(hello.AddrStrings(), sess.Address) {
		address, picked = sess.Address, true
	}
	err = s.sessions.Claim(ctx, sid, repository.MinionSessionClaim{
		At: now, BuildID: b.BuildID, BuildDescription: b.Description, ModuleName: b.ModuleName, Target: b.Target,
		Platform: b.Platform, Staged: from.staging != nil, Crypto: from.crypto, Hello: models.MinionHelloFrom(hello),
		Address: address, AddressPicked: picked, Sleep: sleepOf(hello), MaxTasks: models.MaxTasksOf(hello),
		MaxTasksSettable: b.MaxTasks != nil,
	})
	if repository.IsNotFound(err) {
		return refuse("the session closed meanwhile")
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim the session: %w", err)
	}
	// The connection the claim came through: the one it names, else the
	// one that carried it when only one of the build's fits.
	c := connPlace(hello.Conn)
	if c == 0 {
		c = from.place
	}
	if c > 0 {
		conn := connOf(b, c)
		if err := s.sessions.SetConn(ctx, sid, conn, nil); err != nil {
			return nil, false, fmt.Errorf("claim the session: %w", err)
		}
		sess.ConnKind = conn.Kind
	}
	// The claim's own message is the session's check-in, from where it came.
	if err := s.sessions.CheckIn(ctx, sid, checkIn(now, peer, sleepOf(hello), sleepOf(hello), sess.ConnKind)); err != nil {
		return nil, false, fmt.Errorf("check in: %w", err)
	}
	s.logger.Info("minion claimed its persisted session", zap.Int64("number", sess.Number), zap.String("session_id", sid.String()),
		zap.String("build_id", b.BuildID.String()), zap.String("host", hello.Host), zap.Uint64("pid", hello.PID))
	s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(sess)))
	// A new process: the tasks the one before ran may be gone with it. Not
	// a bind-call minion's: core holds where its tasks stand, and its calls
	// carry them on (resume).
	if sess.ConnKind != protocol.KindBindCall {
		s.askTasks(ctx, sid, "a new process claimed its session")
	}
	items, err = helloResponse(sid)
	return items, err == nil, err
}

// claimReserved opens the session a build reserved (a new persisted
// session), with the reserved id, at its minion's first claim. The
// reservation goes first, so of two claims racing on it one opens it.
func (s *Service) claimReserved(ctx context.Context, from *sender, peer string, hello protocol.HelloItem, sid uuid.UUID,
	refuse func(string) ([]cbor.RawMessage, bool, error)) ([]cbor.RawMessage, bool, error) {
	if s.reservations == nil {
		return refuse("no such session")
	}
	res, err := s.reservations.FindBySessionID(ctx, sid)
	switch {
	case repository.IsNotFound(err):
		return refuse("no such session")
	case err != nil:
		return nil, false, fmt.Errorf("load the reservation: %w", err)
	case res.OperationID != from.build.OperationID:
		return refuse("the session is another operation's")
	}
	if err := s.reservations.Delete(ctx, sid); err != nil {
		if repository.IsNotFound(err) {
			return refuse("another claim opened the session first")
		}
		return nil, false, fmt.Errorf("take the reservation: %w", err)
	}
	items, err := s.newSession(ctx, from, peer, hello, sid, true)
	return items, err == nil, err
}

// underSession answers a message sent under a session id, which came by p
// (via, the connection socket it came over). Any message is a check-in; a
// hello replaces what the session recorded (a rehello) and gets the same
// session id back; acks and results are taken; and the reply carries, after
// the answers to the message's items, what the outbox has for the minion: a
// get_tasks gets the tasks waiting (outbox.go).
func (s *Service) underSession(ctx context.Context, session *models.MinionSession, peer string, msg protocol.MinionMessage, via *Conn, p path) ([]cbor.RawMessage, error) {
	// A schedule the message says (a sleep_update, or a rehello's) takes
	// effect with this check-in: the last one that reads.
	var said *models.MinionSessionSleep
	for _, raw := range msg.Items {
		switch protocol.ItemType(raw) {
		case protocol.ItemSleepUpdate:
			if u, err := readSleepUpdate(raw); err == nil {
				said = models.SleepFrom(u.Sleep())
			}
		case protocol.ItemHello:
			if h, err := readHello(raw); err == nil && h.Sleep != nil {
				said = models.SleepFrom(*h.Sleep)
			}
		}
	}
	schedule := said
	if schedule == nil {
		schedule = session.Sleep
	}
	if err := s.sessions.CheckIn(ctx, session.SessionID, checkIn(s.now(), peer, said, schedule, session.ConnKind)); err != nil {
		return nil, fmt.Errorf("check in: %w", err)
	}
	var items []cbor.RawMessage
	var wantTasks bool
	var wantMax *uint64
	var pulls []pull
	var feedback []protocol.FeedbackItem
	// The streams whose chunks this reply answers (streams.go).
	answered := map[string]bool{}
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
			address, picked := models.DefaultAddress(hello.AddrStrings()), false
			if session.AddressPicked && slices.Contains(hello.AddrStrings(), session.Address) {
				address, picked = session.Address, true
			}
			if err = s.sessions.SetHello(ctx, session.SessionID, models.MinionHelloFrom(hello), address, picked); err != nil {
				return nil, fmt.Errorf("rehello: %w", err)
			}
			// The limit of tasks it says now (the operator's goes again
			// while it says another: the outbox).
			session.MaxTasks, session.MaxTasksSends = models.MaxTasksOf(hello), 0
			if err = s.sessions.SetMaxTasks(ctx, session.SessionID, session.MaxTasks); err != nil {
				return nil, fmt.Errorf("rehello: %w", err)
			}
			s.endMaxTasksCommands(ctx, session)
			// The connection it says hello through: the one it names, else
			// the one that carried it when only one of the build's fits (a
			// call's answer is its target's).
			c := connPlace(hello.Conn)
			if c == 0 && p != pathCall {
				c = s.carrierPlaceOf(ctx, session, via)
			}
			if c > 0 && c != session.Conn {
				conn := s.sessionConn(ctx, session, c)
				if err = s.sessions.SetConn(ctx, session.SessionID, conn, nil); err != nil {
					return nil, fmt.Errorf("rehello: %w", err)
				}
				session.Conn, session.ConnKind, session.ConnMaxMessage = c, conn.Kind, conn.MaxMessage
			}
			s.logger.Info("minion rehello", zap.String("session_id", session.SessionID.String()))
			out, err = helloResponse(session.SessionID)
		case protocol.ItemSleepUpdate:
			// Taken with the check-in above; nothing to answer.
			if _, rErr := readSleepUpdate(raw); rErr != nil {
				out, err = errItem(protocol.ErrItemBadMessage, rErr.Error(), i)
			}
		case protocol.ItemConnUpdate:
			// Recorded; nothing to answer.
			u, rErr := readConnUpdate(raw)
			if rErr != nil {
				out, err = errItem(protocol.ErrItemBadMessage, rErr.Error(), i)
				break
			}
			to := connPlace(u.Conn)
			change := &models.MinionSessionConnChange{At: s.now(), From: session.Conn, To: to, Reason: u.Reason}
			conn := s.sessionConn(ctx, session, to)
			if err = s.sessions.SetConn(ctx, session.SessionID, conn, change); err != nil {
				return nil, fmt.Errorf("record a conn_update: %w", err)
			}
			s.logger.Info("minion moved to another connection", zap.String("session_id", session.SessionID.String()),
				zap.Int("from", session.Conn), zap.Int("to", to), zap.String("reason", u.Reason))
			session.Conn, session.ConnKind, session.ConnMaxMessage = to, conn.Kind, conn.MaxMessage
		case protocol.ItemFeedback:
			// Kept after the loop, all of the message's at once.
			f, rErr := readFeedback(raw)
			if rErr != nil {
				out, err = errItem(protocol.ErrItemBadMessage, rErr.Error(), i)
				break
			}
			feedback = append(feedback, f)
		case protocol.ItemMaxTasks:
			// The minion's limit of tasks at once, in answer to core's or
			// its own: recorded, nothing to answer. Its get_tasks follow it.
			m, rErr := readMaxTasks(raw)
			if rErr != nil {
				out, err = errItem(protocol.ErrItemBadMessage, rErr.Error(), i)
				break
			}
			n := int(m.Max)
			if session.MaxTasks == nil || *session.MaxTasks != n {
				s.logger.Info("minion's max tasks", zap.String("session_id", session.SessionID.String()), zap.Int("max", n))
			}
			if err = s.sessions.SetMaxTasks(ctx, session.SessionID, &n); err != nil {
				return nil, fmt.Errorf("record a max_tasks: %w", err)
			}
			session.MaxTasks, session.MaxTasksSends = &n, 0
			s.endMaxTasksCommands(ctx, session)
		case protocol.ItemGetTasks:
			// Answered after the other items (tasks.go): an ack or a result
			// in the same message first stops those tasks being sent again.
			get, gErr := readGetTasks(raw)
			switch {
			case gErr != nil:
				out, err = errItem(protocol.ErrItemBadMessage, gErr.Error(), i)
			case wantTasks:
				out, err = errItem(protocol.ErrItemBadMessage, "one get_tasks per message", i)
			default:
				wantTasks, wantMax = true, get.Max
			}
		case protocol.ItemAck:
			if s.tasks == nil {
				out, err = errItem(protocol.ErrItemUnknownItem, "no tasks are taken", i)
				break
			}
			var pl *pull
			out, pl, err = s.takeAck(ctx, session, raw, i)
			if pl != nil {
				pulls = append(pulls, *pl)
				// Its answer is a chunk, or a stream's ack: no other ack.
				answered[string(pl.xfer)] = true
			}
		case protocol.ItemChunk:
			out, err = s.takeChunk(ctx, session, raw, i, answered)
		case protocol.ItemResult:
			if s.tasks == nil {
				out, err = errItem(protocol.ErrItemUnknownItem, "no tasks are taken", i)
				break
			}
			// A call carries every resume the outbox has: the part's own
			// goes with it there.
			out, err = s.takeResult(ctx, session, raw, i, p != pathCall)
		case protocol.ItemErr:
			// The minion's word on an earlier item of core's (a cancel for a
			// task it no longer has): noted, not answered.
			var e protocol.ErrItem
			if protocol.UnmarshalCBOR(raw, &e) == nil {
				s.logger.Info("minion err", zap.String("session_id", session.SessionID.String()), zap.String("code", protocol.ErrCodeName(e.Code)), zap.String("msg", e.Msg))
				if e.Code == protocol.ErrItemUnknownTask && len(e.Task) == 16 {
					err = s.endUnknown(ctx, session, e)
				}
			}
		default:
			out, err = errItem(protocol.ErrItemUnknownItem, "not taken under a session yet", i)
		}
		if err != nil {
			return nil, err
		}
		// An err about an item that said a ctx carries it back: a minion
		// that keeps nothing finds there what to end or clean up.
		items = append(items, echoCtx(out, i, protocol.ItemCtx(raw))...)
	}
	// The message's feedback: kept, its counts on the session, which the
	// update published below carries to the Sessions table.
	if _, err := s.takeFeedback(ctx, session, feedback); err != nil {
		return nil, err
	}
	if s.tasks != nil {
		// After the answers, what the outbox has (cancels whenever core has
		// them, tasks only when asked), in the room the answers leave.
		d := delivery{streams: true, answered: answered, room: roomFor(session.MessageLimit(), items)}
		if wantTasks {
			// Through a request (the sync call, a call's answer) get_tasks's
			// max bounds this message; over a socket it is a standing
			// allowance (Notify).
			d.tasks = models.MaxMinionTasksPerReply
			if wantMax != nil && *wantMax < uint64(d.tasks) {
				d.tasks = int(*wantMax)
			}
			if via != nil {
				via.setAllowance(session.SessionID, wantMax)
				var err error
				if d.tasks, err = s.room(ctx, session, wantMax); err != nil {
					return nil, err
				}
			}
		}
		// A call carries the resumes of every task under way: the page
		// keeps nothing.
		d.resumes = p == pathCall
		o, err := s.outbox(ctx, session, d)
		if err != nil {
			return nil, err
		}
		items = append(items, o.items...)
		left := d.room - o.used
		if p == pathSync && o.soon {
			// A stream of its may go on any moment: a minion that polls
			// comes back soon rather than on its schedule.
			poll, err := pollItem()
			if err != nil {
				return nil, err
			}
			items = append(items, poll)
			left -= len(poll) + itemOverhead
		}
		// Chunks last: in the room the rest leaves.
		if len(pulls) > 0 {
			chunks, err := s.serveUploads(ctx, session, pulls, left)
			if err != nil {
				return nil, err
			}
			items = append(items, chunks...)
		}
	}
	s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
	return items, nil
}

// readConnUpdate reads and checks a conn_update.
func readMaxTasks(raw cbor.RawMessage) (protocol.MaxTasksItem, error) {
	var m protocol.MaxTasksItem
	if err := protocol.UnmarshalCBOR(raw, &m); err != nil {
		return m, errors.New("the max_tasks does not read")
	}
	return m, m.Validate()
}

func readConnUpdate(raw cbor.RawMessage) (protocol.ConnUpdateItem, error) {
	var u protocol.ConnUpdateItem
	if err := protocol.UnmarshalCBOR(raw, &u); err != nil {
		return u, errors.New("the conn_update does not read")
	}
	return u, u.Validate()
}

// connOf is the connection at place in b's connections: its kind and
// name, empty when b has no such one (nil: the build is gone).
func connOf(b *models.Build, place int) models.MinionSessionConn {
	c := models.MinionSessionConn{Place: place}
	if b != nil && place >= 1 && place <= len(b.Connections.Items) {
		bc := b.Connections.Items[place-1]
		c.Kind, c.Name, c.MaxMessage = bc.ConnectionKind(), bc.Endpoint.Name, bc.MaxMessage
	}
	return c
}

// sessionConn is the session's connection at place, from its build; a build
// that cannot be read leaves its kind and name empty.
func (s *Service) sessionConn(ctx context.Context, session *models.MinionSession, place int) models.MinionSessionConn {
	b, err := s.builds.FindByBuildID(ctx, session.BuildID)
	if err != nil {
		if !repository.IsNotFound(err) {
			s.logger.Warn("read the session's build for its connection", zap.String("session_id", session.SessionID.String()), zap.Error(err))
		}
		return connOf(nil, place)
	}
	return connOf(b, place)
}

// carrierKind is the kind of connection a message came through: a socket's,
// else the sync call's.
func carrierKind(via *Conn) string {
	if via != nil {
		return via.Kind
	}
	return protocol.KindReverseCall
}

// carrierPlace is the build connection a message of b's minion came
// through, as far as core can tell without the minion saying: b's only
// connection, else its only one of kind through the channel module ("":
// any); 0 when none or more fit, and the minion has to say.
func carrierPlace(b *models.Build, kind, module string) int {
	items := b.Connections.Items
	if len(items) == 1 {
		return 1
	}
	place := 0
	for i := range items {
		c := &items[i]
		if c.ConnectionKind() != kind || (module != "" && c.Endpoint.Channel.ModuleName != module) {
			continue
		}
		if place != 0 {
			return 0
		}
		place = i + 1
	}
	return place
}

// carrierPlaceOf is carrierPlace for a message of session's minion that
// came over via, or through the sync call.
func (s *Service) carrierPlaceOf(ctx context.Context, session *models.MinionSession, via *Conn) int {
	if via != nil && via.placeOf() > 0 {
		return via.placeOf()
	}
	b, err := s.builds.FindByBuildID(ctx, session.BuildID)
	if err != nil {
		return 0
	}
	return carrierPlace(b, carrierKind(via), ChannelOf(ctx))
}

// connPlace is a connection's place as core keeps it: 0 (not said) for one
// past any build's connections.
func connPlace(n uint64) int {
	if n > math.MaxInt32 {
		return 0
	}
	return int(n)
}

// readSleepUpdate reads and checks a sleep_update.
func readSleepUpdate(raw cbor.RawMessage) (protocol.SleepUpdateItem, error) {
	var u protocol.SleepUpdateItem
	if err := protocol.UnmarshalCBOR(raw, &u); err != nil {
		return u, errors.New("the sleep_update does not read")
	}
	return u, u.Sleep().Validate()
}

// sleepOf is the schedule a hello says; nil for none.
func sleepOf(h protocol.HelloItem) *models.MinionSessionSleep {
	if h.Sleep == nil {
		return nil
	}
	return models.SleepFrom(*h.Sleep)
}

// checkIn is a check-in at now from peer: said is a schedule the message
// says (nil for none), schedule the one in force, from which the session
// turns late, kind the kind of the connection the minion uses: on one that
// says whether the minion is there (bind-persistent) it is never late by a
// schedule (models.LateBySchedule).
func checkIn(now time.Time, peer string, said, schedule *models.MinionSessionSleep, kind string) repository.MinionSessionCheckIn {
	c := repository.MinionSessionCheckIn{At: now, Peer: peer, Sleep: said}
	switch {
	case !models.LateBySchedule(kind):
		c.Untimed = true
	case schedule != nil:
		late, lost := schedule.LateAtFrom(now), schedule.LostAtFrom(now)
		c.LateAt, c.LostAt = &late, &lost
	}
	return c
}

// echoCtx gives each err in out about item i the ctx that item said, when
// it said one and the err has none.
func echoCtx(out []cbor.RawMessage, i int, ctx []byte) []cbor.RawMessage {
	if ctx == nil {
		return out
	}
	for j, raw := range out {
		if protocol.ItemType(raw) != protocol.ItemErr {
			continue
		}
		var e protocol.ErrItem
		if protocol.UnmarshalCBOR(raw, &e) != nil || e.Item == nil || *e.Item != i || e.Ctx != nil {
			continue
		}
		e.Ctx = ctx
		if b, err := protocol.MarshalCBOR(e); err == nil {
			out[j] = b
		}
	}
	return out
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

// openReplaced opens a frame under a session with the keys earlier holders
// had (not older than a tombstone), and makes those the sender's keys.
func (s *Service) openReplaced(from *sender, id uuid.UUID, frame []byte) ([]byte, error) {
	for _, rk := range from.session.ReplacedKeys {
		if s.now().Sub(rk.At) > models.MinionSessionTombstone || rk.Crypto.Suite != protocol.CryptoAES256CBCHMACSHA256 {
			continue
		}
		var keys protocol.AES256CBCHMACSHA256Material
		if json.Unmarshal(rk.Crypto.Material, &keys) != nil {
			continue
		}
		if plain, err := protocol.OpenFrame(keys, id, protocol.FrameMinionToCore, frame); err == nil {
			from.keys, from.replaced = keys, true
			return plain, nil
		}
	}
	return nil, protocol.ErrFrame
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

// errMessage is a reply's one err item about the whole message.
func errMessage(code int, msg string) ([]cbor.RawMessage, error) {
	data, err := protocol.MarshalCBOR(protocol.ErrItem{T: protocol.ItemErr, Code: code, Msg: msg})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{data}, nil
}

// errItem is a reply's one err item.
func errItem(code int, msg string, item int) ([]cbor.RawMessage, error) {
	data, err := protocol.MarshalCBOR(protocol.ErrItem{T: protocol.ItemErr, Code: code, Msg: msg, Item: &item})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{data}, nil
}
