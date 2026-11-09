package minionsync

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/factorylink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Codecs (docs: contracts/minion-messages/codecs/). A message becomes bytes
// in two layers: its envelope writes it as plaintext (core's CBOR or JSON,
// or the factory's own), its suite seals the plaintext (core's
// aes-256-cbc-hmac-sha256, or the factory's own). Every message core reads
// or writes goes through readMessage and writeMessage, which run core's
// layers and call the build's factory, once each way, for the layers that
// are its own (factorylink). Core reads and writes the protocol's types
// whatever the codec.
//
// A factory that does not answer means a refusal: what the minion sends
// does not open (the sync call's 422), what core starts waits (its outbox
// keeps it), and the session says since when (CodecDownSince).

// FactoryLinks calls the factories that translate their minions' codec.
// Satisfied by *factorylink.Hub.
type FactoryLinks interface {
	Call(ctx context.Context, module string, c protocol.FactoryCall, timeout time.Duration) (protocol.FactoryAnswerFrame, error)
}

// WithFactoryLinks has the service call factories for the layers of their
// builds' codecs that are their own. Without it such a build's messages do
// not open.
func (s *Service) WithFactoryLinks(f FactoryLinks) *Service {
	s.factories = f
	return s
}

// ErrCodec means the build's codec could not write or read a message: its
// factory did not answer, answered a failure, or its message outgrew the
// connection. What the minion sent is refused as unopened; what core
// started waits.
var ErrCodec = errors.New("the build's codec")

// errOutgrew: the message, written, is past what the connection carries or
// the factory declared: core sends it again with fewer items.
var errOutgrew = errors.New("the message outgrew its room")

// setCodec gives the sender its build's codec: the suite of its keys, the
// envelope, and the factory that translates either, with its declaration.
func (f *sender) setCodec(envelope, module string, decl *protocol.BuildCodec, build uuid.UUID) {
	f.codec = protocol.Codec{Suite: f.crypto.Suite, Envelope: cmp.Or(envelope, protocol.EnvelopeCBOR)}
	f.buildID = build
	if f.codec.Translated() {
		f.factory, f.decl = module, decl
	}
}

// envelope is core's envelope of the sender's codec; false for a factory's.
func (f *sender) envelope() (protocol.MessageEnvelope, bool) {
	return protocol.CoreEnvelope(f.codec.Envelope)
}

// suiteIsFactorys reports whether the factory seals and opens the sender's
// messages.
func (f *sender) suiteIsFactorys() bool { return protocol.IsFactoryCodec(f.codec.Suite) }

// factoryMaterial reads a factory suite's key material: bytes, base64 in
// JSON.
func factoryMaterial(c protocol.BuildCrypto) ([]byte, error) {
	var m []byte
	if err := json.Unmarshal(c.Material, &m); err != nil || len(m) == 0 {
		return nil, fmt.Errorf("read the factory's keys: %v", err)
	}
	return m, nil
}

// candidates are the key materials a message under a session of a
// factory's suite may open with: the session's own, then the keys its
// earlier holders had (not older than a tombstone), newest first.
func (s *Service) candidates(from *sender) [][]byte {
	out := [][]byte{from.material}
	if from.session == nil {
		return out
	}
	for _, rk := range from.session.ReplacedKeys {
		if len(out) == protocol.MaxCodecCandidates {
			break
		}
		if s.now().Sub(rk.At) > models.MinionSessionTombstone || rk.Crypto.Suite != from.codec.Suite {
			continue
		}
		if m, err := factoryMaterial(rk.Crypto); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// readMessage is the message the minion sent under id, in CBOR: payload
// opened by the build's suite and read by its envelope. A message that
// opened with keys a claim replaced marks the sender replaced.
func (s *Service) readMessage(ctx context.Context, from *sender, id uuid.UUID, payload []byte) ([]byte, error) {
	env, coreEnv := from.envelope()
	if !from.suiteIsFactorys() {
		plain, err := from.open(id, payload)
		if err != nil && from.session != nil {
			// Not the session's keys: maybe an earlier holder's, replaced
			// by a claim. It is told so.
			plain, err = s.openReplaced(from, id, payload)
		}
		if err != nil {
			return nil, err
		}
		if coreEnv {
			return env.ReadMessage(plain)
		}
		a, err := s.codecCall(ctx, from, protocol.FactoryCall{Type: protocol.FactoryInbound, ID: id[:], Data: plain,
			Layers: []int{protocol.CodecLayerEnvelope}})
		if err != nil {
			return nil, err
		}
		return a.Data, nil
	}
	keys := s.candidates(from)
	a, err := s.codecCall(ctx, from, protocol.FactoryCall{Type: protocol.FactoryInbound, ID: id[:], Data: payload,
		Layers: from.codec.Layers(), Keys: keys})
	if err != nil {
		return nil, err
	}
	if i := *a.Opened; i > 0 {
		from.material, from.replaced = keys[i], true
	}
	if coreEnv {
		return env.ReadMessage(a.Data)
	}
	return a.Data, nil
}

// writeMessage is msg as the payload core sends under id: its items packed
// where the build packs, written by the build's envelope, sealed by its
// suite. Planned within the connection, it is checked as it is written: a
// message past the connection (or past what its factory declared) goes
// with half its items, the rest waiting for the next; sent is how many of
// msg's items went.
func (s *Service) writeMessage(ctx context.Context, from *sender, id uuid.UUID, msg protocol.CoreMessage) (payload []byte, sent int, err error) {
	items := msg.Items
	for {
		msg.Items = packMessage(from.session, items)
		payload, err = s.writeOnce(ctx, from, id, msg)
		if !errors.Is(err, errOutgrew) || len(items) <= 1 {
			if errors.Is(err, errOutgrew) {
				err = fmt.Errorf("%w: %v", ErrCodec, err)
			}
			return payload, len(items), err
		}
		s.logger.Warn("minion message: written past its room, sent again with half its items", zap.String("id", id.String()),
			zap.Int("items", len(items)), zap.Error(err))
		items = items[:len(items)/2]
	}
}

// writeOnce writes msg once (writeMessage).
func (s *Service) writeOnce(ctx context.Context, from *sender, id uuid.UUID, msg protocol.CoreMessage) ([]byte, error) {
	asCBOR, err := protocol.MarshalCBOR(msg)
	if err != nil {
		return nil, err
	}
	limit := protocol.MaxMinionMessageSize
	if from.session != nil {
		limit = from.session.MessageLimit()
	}
	env, coreEnv := from.envelope()
	form := asCBOR
	if coreEnv {
		if form, err = env.WriteMessage(asCBOR); err != nil {
			return nil, err
		}
		if len(form) > limit {
			return nil, fmt.Errorf("%w: %d bytes as %s, the connection carries %d", errOutgrew, len(form), env.Name(), limit)
		}
	}
	if !from.codec.Translated() {
		return from.seal(id, form)
	}
	c := protocol.FactoryCall{Type: protocol.FactoryOutbound, ID: id[:], Data: form, Layers: from.codec.Layers()}
	if from.suiteIsFactorys() {
		c.Keys = [][]byte{from.material}
	}
	a, err := s.codecCall(ctx, from, c)
	if err != nil {
		return nil, err
	}
	if bound := from.decl.Bound(len(form)); len(a.Data) > bound {
		return nil, fmt.Errorf("%w: the factory wrote %d bytes of %d, its declaration bounds them to %d", errOutgrew, len(a.Data),
			len(form), bound)
	}
	if from.suiteIsFactorys() {
		return a.Data, nil
	}
	return from.seal(id, a.Data)
}

// codecCall sends c to the sender's factory, for its build, within the
// declared timeout, and notes on the session whether the factory answers.
// Errors wrap ErrCodec.
func (s *Service) codecCall(ctx context.Context, from *sender, c protocol.FactoryCall) (protocol.FactoryAnswerFrame, error) {
	c.Build = from.buildID[:]
	var a protocol.FactoryAnswerFrame
	err := factorylink.ErrNoLink
	if s.factories != nil && from.factory != "" {
		timeout := protocol.DefaultCodecTimeout
		if from.decl != nil {
			timeout = from.decl.CallTimeout()
		}
		a, err = s.factories.Call(ctx, from.factory, c, timeout)
	}
	if from.session != nil && (err == nil || factorylink.Down(err) || errors.As(err, new(*factorylink.Failed))) {
		s.codecAnswers(ctx, from.session, !factorylink.Down(err))
	}
	if err != nil {
		s.logger.Info("minion codec: the factory's call", zap.String("module", from.factory), zap.Int("type", c.Type),
			zap.String("id", uuid.UUID(c.ID).String()), zap.Error(err))
		return a, fmt.Errorf("%w: %w", ErrCodec, err)
	}
	return a, nil
}

// codecAnswers records whether the factory that translates session's codec
// answers: since when it does not, cleared once it does.
func (s *Service) codecAnswers(ctx context.Context, session *models.MinionSession, answers bool) {
	if answers == (session.CodecDownSince == nil) {
		return
	}
	var since *time.Time
	if !answers {
		now := s.now()
		since = &now
	}
	if err := s.sessions.SetCodecDown(ctx, session.SessionID, since); err != nil {
		s.logger.Warn("minion codec: record whether the factory answers", zap.String("session_id", session.SessionID.String()),
			zap.Error(err))
		return
	}
	session.CodecDownSince = since
	if !answers {
		s.logger.Warn("minion codec: the session's factory does not answer", zap.String("session_id", session.SessionID.String()),
			zap.String("module", session.ModuleName))
	}
	s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
}

// stageKeys are the keys of a stage under a build of its factory's own
// suite: the factory makes them for the stage's key and wraps them for it
// (docs: Codecs, Keys). refused: the suite does not stage.
func (s *Service) stageKeys(ctx context.Context, from *sender, stageKey []byte) (crypto protocol.BuildCrypto, wrapped []byte, refused bool, err error) {
	a, err := s.codecCall(ctx, from, protocol.FactoryCall{Type: protocol.FactoryKeys, For: protocol.FactoryKeysForStage,
		StageKey: stageKey})
	var failed *factorylink.Failed
	if errors.As(err, &failed) && failed.Kind == protocol.CodecFailRefused {
		return crypto, nil, true, nil
	}
	if err != nil {
		return crypto, nil, false, err
	}
	raw, err := json.Marshal(a.Material)
	if err != nil {
		return crypto, nil, false, err
	}
	return protocol.BuildCrypto{Suite: from.codec.Suite, Material: raw}, a.Wrapped, false, nil
}

// planLimit is the largest message core plans for session's minion, in
// the CBOR it counts items in: the connection's limit, less what the
// session's factory declares it may grow (n × growth / 100 + overhead),
// and three quarters of that under core's JSON (bytes are base64 there).
// writeMessage checks the message as written, and sends fewer items when
// it is past the connection's.
func planLimit(session *models.MinionSession) int {
	limit := session.MessageLimit()
	if c := session.Codec; c != nil && c.Growth > 0 {
		limit = (limit - c.Overhead) * 100 / c.Growth
	}
	if session.Envelope == protocol.EnvelopeJSON {
		limit = limit * 3 / 4
	}
	return max(limit, 0)
}

// sessionEnvelope is core's envelope of session's build; false for a
// factory's.
func sessionEnvelope(session *models.MinionSession) (protocol.MessageEnvelope, bool) {
	if session == nil {
		return protocol.CBOREnvelope, true
	}
	return protocol.CoreEnvelope(session.Envelope)
}

// unpackItems takes the packed items of a message the minion sent in their
// places, as its envelope wrote them: core's, else CBOR (a factory that
// translates its envelope hands core items unpacked, or packed in CBOR).
func unpackItems(from *sender, items []cbor.RawMessage) {
	env, ok := from.envelope()
	if !ok {
		env = protocol.CBOREnvelope
	}
	protocol.UnpackItemsIn(env, items)
}

// unpackEnvelope is the envelope a session's packed items were written in
// (unpackItems).
func unpackEnvelope(session *models.MinionSession) protocol.MessageEnvelope {
	if env, ok := sessionEnvelope(session); ok {
		return env
	}
	return protocol.CBOREnvelope
}
