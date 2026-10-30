package minionsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.uber.org/zap"
)

type fakeBuilds map[uuid.UUID]*models.Build

func (f fakeBuilds) FindByBuildID(_ context.Context, id uuid.UUID) (*models.Build, error) {
	if b, ok := f[id]; ok {
		return b, nil
	}
	return nil, qmgo.ErrNoSuchDocuments
}

type fakeStagings struct{ rows []models.MinionStaging }

func (f *fakeStagings) Insert(_ context.Context, s *models.MinionStaging) error {
	f.rows = append(f.rows, *s)
	return nil
}

func (f *fakeStagings) FindByTempID(_ context.Context, id uuid.UUID) (*models.MinionStaging, error) {
	for i := range f.rows {
		if f.rows[i].TempID == id {
			return &f.rows[i], nil
		}
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeStagings) Delete(_ context.Context, id uuid.UUID) error {
	for i := range f.rows {
		if f.rows[i].TempID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return qmgo.ErrNoSuchDocuments
}

// fakeSessions keeps sessions in memory; the list methods are not used here
// (the embedded nil interface panics if they are).
type fakeSessions struct {
	repository.IMinionSessionRepository
	rows []models.MinionSession
	seq  int64
}

func (f *fakeSessions) NextNumber(context.Context, uuid.UUID) (int64, error) {
	f.seq++
	return f.seq, nil
}

func (f *fakeSessions) Insert(_ context.Context, s *models.MinionSession) error {
	f.rows = append(f.rows, *s)
	return nil
}

func (f *fakeSessions) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionSession, error) {
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			row := f.rows[i]
			return &row, nil
		}
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeSessions) CheckIn(_ context.Context, id uuid.UUID, c repository.MinionSessionCheckIn) error {
	for i := range f.rows {
		r := &f.rows[i]
		if r.SessionID != id {
			continue
		}
		at := c.At
		r.LastSeenAt, r.PurgeAt = &at, nil
		if c.Peer != "" {
			r.PeerAddress = c.Peer
		}
		if c.Sleep != nil {
			r.Sleep = c.Sleep
		}
		if c.LateAt != nil {
			r.LateAt = c.LateAt
		}
	}
	return nil
}

func (f *fakeSessions) SetHello(_ context.Context, id uuid.UUID, h models.MinionHello, address string, picked bool) error {
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			f.rows[i].Hello, f.rows[i].Address, f.rows[i].AddressPicked = h, address, picked
		}
	}
	return nil
}

func (f *fakeSessions) SetPersisted(_ context.Context, id uuid.UUID) error {
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			f.rows[i].Persisted = true
		}
	}
	return nil
}

func (f *fakeSessions) Claim(_ context.Context, id uuid.UUID, c repository.MinionSessionClaim) error {
	for i := range f.rows {
		r := &f.rows[i]
		if r.SessionID != id || r.ClosedAt != nil {
			continue
		}
		r.ReplacedKeys = append([]models.MinionSessionReplacedKeys{{Crypto: r.Crypto, At: c.At}}, r.ReplacedKeys...)
		r.Restarts = append([]models.MinionSessionRestart{{At: c.At, PID: c.Hello.PID, Host: c.Hello.Host, BuildID: c.BuildID}}, r.Restarts...)
		r.RestartCount++
		r.Crypto, r.Hello, r.BuildID, r.Address, r.AddressPicked, r.Persisted = c.Crypto, c.Hello, c.BuildID, c.Address, c.AddressPicked, true
		r.Sleep, r.LateAt = c.Sleep, nil
		return nil
	}
	return qmgo.ErrNoSuchDocuments
}

// fakeReservations keeps reserved session ids; the rest is not used here.
type fakeReservations struct {
	repository.IMinionSessionReservationRepository
	rows map[uuid.UUID]models.MinionSessionReservation
}

func (f *fakeReservations) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionSessionReservation, error) {
	if r, ok := f.rows[id]; ok {
		return &r, nil
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeReservations) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := f.rows[id]; !ok {
		return qmgo.ErrNoSuchDocuments
	}
	delete(f.rows, id)
	return nil
}

// fakeBus keeps what is published.
type fakeBus struct {
	eventbus.NopEventBus
	topics []eventbus.Topic
}

func (f *fakeBus) Publish(e eventbus.Event) { f.topics = append(f.topics, e.Topic) }

// fakeCache keeps SetNX's keys; the rest of cache.Cache is unused here.
type fakeCache struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (f *fakeCache) SetNX(_ context.Context, key string, _ interface{}, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}
func (f *fakeCache) Get(context.Context, string) (string, error)                   { return "", nil }
func (f *fakeCache) Set(context.Context, string, interface{}, time.Duration) error { return nil }
func (f *fakeCache) AppendAt(context.Context, string, int64, string, int64, time.Duration) (int64, error) {
	return 0, nil
}
func (f *fakeCache) IncrWithTTL(context.Context, string, time.Duration) (int64, error) { return 0, nil }
func (f *fakeCache) SetWithTags(context.Context, string, interface{}, []string, time.Duration) error {
	return nil
}
func (f *fakeCache) Del(context.Context, ...string) error                  { return nil }
func (f *fakeCache) InvalidateCache(context.Context, string, string) error { return nil }
func (f *fakeCache) Close() error                                          { return nil }
func (f *fakeCache) IsEnabled() bool                                       { return true }

type rig struct {
	svc      *Service
	reserved *fakeReservations
	builds   fakeBuilds
	stagings *fakeStagings
	sessions *fakeSessions
	bus      *fakeBus
	build    *models.Build
	keys     protocol.AES256CBCHMACSHA256Material
	// peer is the address the next messages come from.
	peer string
	priv *rsa.PrivateKey
}

func newRig(t *testing.T) *rig {
	t.Helper()
	keys := protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}
	_, _ = rand.Read(keys.EncKey)
	_, _ = rand.Read(keys.MACKey)
	material, _ := json.Marshal(keys)
	b := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(), ModuleName: "bob", Target: "linux-elf", Platform: "linux/amd64",
		Crypto: &protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: material}}
	priv, err := rsa.GenerateKey(rand.Reader, protocol.StageKeySize)
	if err != nil {
		t.Fatal(err)
	}
	st, ss, bus, bs := &fakeStagings{}, &fakeSessions{}, &fakeBus{}, fakeBuilds{b.BuildID: b}
	rs := &fakeReservations{rows: map[uuid.UUID]models.MinionSessionReservation{}}
	return &rig{
		svc:      NewService(bs, st, ss, rs, &fakeCache{seen: map[string]bool{}}, bus, zap.NewNop()),
		reserved: rs, builds: bs, stagings: st, sessions: ss, bus: bus, build: b, keys: keys, priv: priv,
	}
}

// send seals a minion message with items under the build id, as a minion does.
func (r *rig) send(t *testing.T, id []byte, items ...any) ([]byte, error) {
	t.Helper()
	return r.sendUnder(t, r.build.BuildID, r.keys, id, items...)
}

// sendUnder seals a minion message with items under another id and keys.
func (r *rig) sendUnder(t *testing.T, under uuid.UUID, keys protocol.AES256CBCHMACSHA256Material, id []byte, items ...any) ([]byte, error) {
	t.Helper()
	msg := protocol.MinionMessage{V: protocol.MinionMessageV1, ID: id}
	for _, it := range items {
		raw, err := protocol.MarshalCBOR(it)
		if err != nil {
			t.Fatal(err)
		}
		msg.Items = append(msg.Items, raw)
	}
	plain, _ := protocol.MarshalCBOR(msg)
	frame, err := protocol.SealFrame(keys, under, protocol.FrameMinionToCore, plain)
	if err != nil {
		t.Fatal(err)
	}
	return r.svc.Handle(context.Background(), under.String(), r.peer, frame)
}

func (r *rig) stage(t *testing.T) protocol.StageItem {
	der, _ := x509.MarshalPKIXPublicKey(&r.priv.PublicKey)
	return protocol.StageItem{T: protocol.ItemStage, Key: der, Corr: protocol.NewMessageID()}
}

// open reads core's answer as the minion does.
func (r *rig) open(t *testing.T, reply []byte) protocol.CoreMessage {
	t.Helper()
	return r.openUnder(t, r.build.BuildID, r.keys, reply)
}

func (r *rig) openUnder(t *testing.T, under uuid.UUID, keys protocol.AES256CBCHMACSHA256Material, reply []byte) protocol.CoreMessage {
	t.Helper()
	plain, err := protocol.OpenFrame(keys, under, protocol.FrameCoreToMinion, reply)
	if err != nil {
		t.Fatalf("open the reply: %v", err)
	}
	var msg protocol.CoreMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestStage(t *testing.T) {
	r := newRig(t)
	stage, id := r.stage(t), protocol.NewMessageID()
	reply, err := r.send(t, id, stage)
	if err != nil {
		t.Fatal(err)
	}
	msg := r.open(t, reply)
	if err := protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)); err != nil || !bytes.Equal(msg.Re, id) ||
		len(msg.Items) != 1 || protocol.ItemType(msg.Items[0]) != protocol.ItemStageResponse {
		t.Fatalf("reply %+v: %v", msg, err)
	}
	var resp protocol.StageResponseItem
	if err := protocol.UnmarshalCBOR(msg.Items[0], &resp); err != nil {
		t.Fatal(err)
	}
	tempID, err := uuid.FromBytes(resp.TID)
	if err != nil || tempID.Version() != 4 || !bytes.Equal(resp.Corr, stage.Corr) {
		t.Fatalf("response %+v: %v", resp, err)
	}
	material, err := protocol.DecryptStageKeys(r.priv, resp.Keys)
	if err != nil {
		t.Fatal(err)
	}
	var keys protocol.AES256CBCHMACSHA256Material
	if err := protocol.UnmarshalCBOR(material, &keys); err != nil || len(keys.EncKey) != 32 || bytes.Equal(keys.EncKey, r.keys.EncKey) {
		t.Fatalf("new keys %+v: %v", keys, err)
	}

	// Core keeps the temporary id and the same new keys for the next message.
	if len(r.stagings.rows) != 1 {
		t.Fatalf("stagings %d", len(r.stagings.rows))
	}
	st := r.stagings.rows[0]
	var stored protocol.AES256CBCHMACSHA256Material
	_ = json.Unmarshal(st.Crypto.Material, &stored)
	if st.TempID != tempID || st.BuildID != r.build.BuildID || st.OperationID != r.build.OperationID ||
		!bytes.Equal(stored.EncKey, keys.EncKey) || !bytes.Equal(stored.MACKey, keys.MACKey) {
		t.Fatalf("staging %+v", st)
	}

	// The same message again is a replay: refused, like any failure.
	if reply, err := r.send(t, id, stage); !errors.Is(err, ErrReplayed) || reply != nil {
		t.Fatalf("replay: %x %v", reply, err)
	}
}

func TestStageRefusals(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	frame, _ := protocol.SealFrame(r.keys, r.build.BuildID, protocol.FrameMinionToCore, []byte{0xa0})
	for name, c := range map[string]struct {
		id    string
		frame []byte
	}{
		"unknown build": {uuid.NewString(), frame},
		"not a UUID":    {"x", frame},
		"bad frame":     {r.build.BuildID.String(), append(bytes.Clone(frame[:len(frame)-1]), frame[len(frame)-1]^1)},
		"no head":       {r.build.BuildID.String(), frame}, // {}: neither id nor re
	} {
		if _, err := r.svc.Handle(ctx, c.id, "", c.frame); !errors.Is(err, ErrUnopened) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Opened, but not a single stage: an err item, and no staging.
	for name, items := range map[string][]any{
		"no items":            nil,
		"two stages":          {r.stage(t), r.stage(t)},
		"short corr":          {protocol.StageItem{T: protocol.ItemStage, Key: r.stage(t).Key, Corr: []byte{1}}},
		"a hello and a stage": {hello("web01"), r.stage(t)},
		"a bad hello":         {protocol.HelloItem{T: protocol.ItemHello, OS: "linux", Arch: "amd64"}},
	} {
		reply, err := r.send(t, protocol.NewMessageID(), items...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		msg := r.open(t, reply)
		var e protocol.ErrItem
		if len(msg.Items) != 1 || protocol.UnmarshalCBOR(msg.Items[0], &e) != nil || e.Code != protocol.ErrItemBadMessage {
			t.Errorf("%s: %+v", name, msg)
		}
	}
	if len(r.stagings.rows) != 0 || len(r.sessions.rows) != 0 {
		t.Fatal("a refused stage or hello was stored")
	}
}

func hello(host string) protocol.HelloItem {
	return protocol.HelloItem{T: protocol.ItemHello, PID: 4242, OS: "linux", Arch: "amd64", Host: host}
}

// staged stages and returns the temporary id and the new keys.
func (r *rig) staged(t *testing.T) (uuid.UUID, protocol.AES256CBCHMACSHA256Material) {
	t.Helper()
	reply, err := r.send(t, protocol.NewMessageID(), r.stage(t))
	if err != nil {
		t.Fatal(err)
	}
	var resp protocol.StageResponseItem
	if err := protocol.UnmarshalCBOR(r.open(t, reply).Items[0], &resp); err != nil {
		t.Fatal(err)
	}
	material, err := protocol.DecryptStageKeys(r.priv, resp.Keys)
	if err != nil {
		t.Fatal(err)
	}
	var keys protocol.AES256CBCHMACSHA256Material
	if err := protocol.UnmarshalCBOR(material, &keys); err != nil {
		t.Fatal(err)
	}
	return uuid.UUID(resp.TID), keys
}

// sessionID reads a reply's one hello_response.
func sessionID(t *testing.T, msg protocol.CoreMessage) uuid.UUID {
	t.Helper()
	var resp protocol.HelloResponseItem
	if len(msg.Items) != 1 || protocol.ItemType(msg.Items[0]) != protocol.ItemHelloResponse ||
		protocol.UnmarshalCBOR(msg.Items[0], &resp) != nil || len(resp.SID) != 16 {
		t.Fatalf("not a hello_response: %+v", msg)
	}
	return uuid.UUID(resp.SID)
}

func TestHelloUnderTemporaryID(t *testing.T) {
	r := newRig(t)
	tempID, keys := r.staged(t)
	reply, err := r.sendUnder(t, tempID, keys, protocol.NewMessageID(), hello("web01"))
	if err != nil {
		t.Fatal(err)
	}
	sid := sessionID(t, r.openUnder(t, tempID, keys, reply))

	if len(r.sessions.rows) != 1 || len(r.stagings.rows) != 0 {
		t.Fatalf("sessions %d, stagings %d", len(r.sessions.rows), len(r.stagings.rows))
	}
	s := r.sessions.rows[0]
	var stored protocol.AES256CBCHMACSHA256Material
	_ = json.Unmarshal(s.Crypto.Material, &stored)
	if s.SessionID != sid || s.Number != 1 || !s.Staged || s.BuildID != r.build.BuildID || s.OperationID != r.build.OperationID ||
		s.Target != "linux-elf" || s.Hello.Host != "web01" || s.Hello.PID != 4242 || s.PurgeAt == nil ||
		!bytes.Equal(stored.EncKey, keys.EncKey) {
		t.Fatalf("session %+v", s)
	}
	if len(r.bus.topics) != 1 || r.bus.topics[0] != eventbus.TopicMinionSessionCreated {
		t.Fatalf("events %v", r.bus.topics)
	}

	// The temporary id served its hello: another one under it does not open.
	if _, err := r.sendUnder(t, tempID, keys, protocol.NewMessageID(), hello("web01")); !errors.Is(err, ErrUnopened) {
		t.Fatalf("second hello: %v", err)
	}
}

func TestHelloUnderBuildID(t *testing.T) {
	r := newRig(t)
	reply, err := r.send(t, protocol.NewMessageID(), hello("db02"))
	if err != nil {
		t.Fatal(err)
	}
	sid := sessionID(t, r.open(t, reply))
	if len(r.sessions.rows) != 1 || r.sessions.rows[0].SessionID != sid || r.sessions.rows[0].Number != 1 || r.sessions.rows[0].Staged {
		t.Fatalf("sessions %+v", r.sessions.rows)
	}
	// The session has the build's own keys.
	if reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID()); err != nil || reply != nil {
		t.Fatalf("check-in: %x %v", reply, err)
	}
}

func TestUnderSession(t *testing.T) {
	r := newRig(t)
	tempID, keys := r.staged(t)
	reply, _ := r.sendUnder(t, tempID, keys, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.openUnder(t, tempID, keys, reply))

	// A check-in: nothing to say, and the session stays.
	if reply, err := r.sendUnder(t, sid, keys, protocol.NewMessageID()); err != nil || reply != nil {
		t.Fatalf("check-in: %x %v", reply, err)
	}
	if s := r.sessions.rows[0]; s.LastSeenAt == nil || s.PurgeAt != nil {
		t.Fatalf("after check-in %+v", s)
	}

	// get_tasks: taken, and no task waits, so nothing to say; max 0 is
	// refused.
	if reply, err := r.sendUnder(t, sid, keys, protocol.NewMessageID(), map[string]any{"t": "get_tasks", "max": 1}); err != nil || reply != nil {
		t.Fatalf("get_tasks: %x %v", reply, err)
	}
	reply, err := r.sendUnder(t, sid, keys, protocol.NewMessageID(), map[string]any{"t": "get_tasks", "max": 0})
	if err != nil {
		t.Fatal(err)
	}
	var bad protocol.ErrItem
	if msg := r.openUnder(t, sid, keys, reply); len(msg.Items) != 1 || protocol.UnmarshalCBOR(msg.Items[0], &bad) != nil || bad.Code != protocol.ErrItemBadMessage {
		t.Fatalf("get_tasks max 0: %+v", msg)
	}

	// A rehello: new details, the same session id; an item not built yet
	// gets unknown_item.
	reply, err = r.sendUnder(t, sid, keys, protocol.NewMessageID(), hello("web01-renamed"),
		map[string]any{"t": "result", "task": 1, "seq": 0})
	if err != nil {
		t.Fatal(err)
	}
	msg := r.openUnder(t, sid, keys, reply)
	if len(msg.Items) != 2 {
		t.Fatalf("reply %+v", msg)
	}
	if sessionID(t, protocol.CoreMessage{Items: msg.Items[:1]}) != sid || r.sessions.rows[0].Hello.Host != "web01-renamed" {
		t.Fatalf("rehello %+v", r.sessions.rows[0])
	}
	var e protocol.ErrItem
	if protocol.UnmarshalCBOR(msg.Items[1], &e) != nil || e.Code != protocol.ErrItemUnknownItem || e.Item == nil || *e.Item != 1 {
		t.Fatalf("err %+v", e)
	}

	// The build's keys do not open a session's messages.
	if _, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID()); !errors.Is(err, ErrUnopened) {
		t.Fatalf("build keys under a session: %v", err)
	}
}

// A session shows core's guess at its address; a rehello keeps an
// operator's pick while the host still has it.
func TestSessionAddress(t *testing.T) {
	r := newRig(t)
	h := hello("web01")
	h.Addrs = []string{"172.17.0.1", "10.0.0.5", "10.0.0.6"}
	reply, _ := r.send(t, protocol.NewMessageID(), h)
	sid := sessionID(t, r.open(t, reply))
	if s := r.sessions.rows[0]; s.Address != "10.0.0.5" || s.AddressPicked {
		t.Fatalf("guess %q picked=%v", s.Address, s.AddressPicked)
	}
	r.sessions.rows[0].Address, r.sessions.rows[0].AddressPicked = "10.0.0.6", true

	h.Addrs = []string{"10.0.0.6", "10.0.0.7"}
	if _, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), h); err != nil {
		t.Fatal(err)
	}
	if s := r.sessions.rows[0]; s.Address != "10.0.0.6" || !s.AddressPicked {
		t.Fatalf("kept pick %q picked=%v", s.Address, s.AddressPicked)
	}
	h.Addrs = []string{"10.0.0.7"}
	if _, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), h); err != nil {
		t.Fatal(err)
	}
	if s := r.sessions.rows[0]; s.Address != "10.0.0.7" || s.AddressPicked {
		t.Fatalf("pick gone: %q picked=%v", s.Address, s.AddressPicked)
	}
}

// A session keeps the external address its messages came from.
func TestSessionPeerAddress(t *testing.T) {
	r := newRig(t)
	r.peer = "203.0.113.7"
	reply, _ := r.send(t, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.open(t, reply))
	s := r.sessions.rows[0]
	if s.PeerAddress != "203.0.113.7" || len(s.Peers) != 1 || s.Peers[0].Address != "203.0.113.7" || s.Peers[0].Count != 1 {
		t.Fatalf("at hello: %q %+v", s.PeerAddress, s.Peers)
	}
	r.peer = "198.51.100.2"
	if _, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID()); err != nil {
		t.Fatal(err)
	}
	if got := r.sessions.rows[0].PeerAddress; got != "198.51.100.2" {
		t.Fatalf("after a check-in from elsewhere: %q", got)
	}
}

// A closed session, and one that expired waiting for its first check-in,
// still open: their minion is told, sealed, to start over.
func TestClosedSession(t *testing.T) {
	r := newRig(t)
	reply, _ := r.send(t, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.open(t, reply))
	closedErr := func(name string) {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), map[string]any{"t": "get_tasks", "max": 1})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var e protocol.ErrItem
		msg := r.openUnder(t, sid, r.keys, reply)
		if len(msg.Items) != 1 || protocol.UnmarshalCBOR(msg.Items[0], &e) != nil || e.Code != protocol.ErrItemSessionClosed || e.Item != nil {
			t.Fatalf("%s: %+v", name, msg)
		}
		if r.sessions.rows[0].LastSeenAt != nil {
			t.Fatalf("%s: a closed session checked in", name)
		}
	}

	// Never checked in, a day and more ago.
	r.sessions.rows[0].CreatedAt = time.Now().Add(-25 * time.Hour)
	closedErr("expired")

	// Closed by core.
	closed := time.Now()
	r.sessions.rows[0].CreatedAt, r.sessions.rows[0].ClosedAt = time.Now(), &closed
	closedErr("closed")
}

// persistedBuild is a second build of the rig's operation, built to persist
// session sid, with keys of its own.
func (r *rig) persistedBuild(t *testing.T, sid uuid.UUID) (*models.Build, protocol.AES256CBCHMACSHA256Material) {
	t.Helper()
	keys := protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}
	_, _ = rand.Read(keys.EncKey)
	_, _ = rand.Read(keys.MACKey)
	material, _ := json.Marshal(keys)
	b := &models.Build{BuildID: uuid.New(), OperationID: r.build.OperationID, ModuleName: "bob", Target: "linux-elf",
		Platform: "linux/amd64", PersistedSessionID: &sid,
		Crypto: &protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: material}}
	r.builds[b.BuildID] = b
	return b, keys
}

func claimHello(host string, sid uuid.UUID) protocol.HelloItem {
	h := hello(host)
	h.PID, h.SID = 777, sid[:]
	return h
}

// A build that persists a session gets it back when its minion claims it;
// the process that held it is told session_replaced, with its own keys.
func TestClaimPersistedSession(t *testing.T) {
	r := newRig(t)
	reply, _ := r.send(t, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.open(t, reply))
	b2, keys2 := r.persistedBuild(t, sid)

	reply, err := r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("web01-rebooted", sid))
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionID(t, r.openUnder(t, b2.BuildID, keys2, reply)); got != sid {
		t.Fatalf("claimed %s, got %s", sid, got)
	}
	s := r.sessions.rows[0]
	if len(r.sessions.rows) != 1 || s.BuildID != b2.BuildID || s.Hello.Host != "web01-rebooted" || s.RestartCount != 1 ||
		len(s.Restarts) != 1 || s.Restarts[0].PID != 777 || !s.Persisted || len(s.ReplacedKeys) != 1 {
		t.Fatalf("claimed session %+v", s)
	}

	// The claimer goes on under the session with its keys.
	if reply, err := r.sendUnder(t, sid, keys2, protocol.NewMessageID()); err != nil || reply != nil {
		t.Fatalf("claimer's check-in: %x %v", reply, err)
	}
	// The process that held it, with the session's old keys: told, sealed
	// with those, and not checked in.
	seen := r.sessions.rows[0].LastSeenAt
	reply, err = r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), map[string]any{"t": "get_tasks", "max": 1})
	if err != nil {
		t.Fatal(err)
	}
	var e protocol.ErrItem
	msg := r.openUnder(t, sid, r.keys, reply)
	if len(msg.Items) != 1 || protocol.UnmarshalCBOR(msg.Items[0], &e) != nil || e.Code != protocol.ErrItemSessionReplaced {
		t.Fatalf("old holder: %+v", msg)
	}
	if r.sessions.rows[0].LastSeenAt != seen {
		t.Fatal("a replaced holder checked in")
	}
}

// A build made with a new persisted session: its minion's first claim opens
// the reserved session, with the reserved id; a later one takes it over.
func TestClaimReservedSession(t *testing.T) {
	r := newRig(t)
	rid := uuid.New()
	b2, keys2 := r.persistedBuild(t, rid)
	r.reserved.rows[rid] = models.MinionSessionReservation{SessionID: rid, OperationID: b2.OperationID, BuildID: b2.BuildID}

	reply, err := r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("web01", rid))
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionID(t, r.openUnder(t, b2.BuildID, keys2, reply)); got != rid {
		t.Fatalf("claimed %s, got %s", rid, got)
	}
	s := r.sessions.rows[0]
	if len(r.sessions.rows) != 1 || s.SessionID != rid || !s.Persisted || s.Number != 1 || s.RestartCount != 0 || len(r.reserved.rows) != 0 {
		t.Fatalf("opened %+v, reservations %d", s, len(r.reserved.rows))
	}

	// A restart of the minion: an ordinary claim of the open session.
	if reply, err = r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("web01", rid)); err != nil {
		t.Fatal(err)
	}
	if got := sessionID(t, r.openUnder(t, b2.BuildID, keys2, reply)); got != rid || r.sessions.rows[0].RestartCount != 1 || len(r.sessions.rows) != 1 {
		t.Fatalf("restart: %s, %d restarts, %d sessions", got, r.sessions.rows[0].RestartCount, len(r.sessions.rows))
	}
}

// A claim not honoured opens a new session, as a hello without one does.
func TestClaimNotHonoured(t *testing.T) {
	r := newRig(t)
	reply, _ := r.send(t, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.open(t, reply))

	// The rig's own build does not persist the session.
	reply, err := r.send(t, protocol.NewMessageID(), claimHello("web02", sid))
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionID(t, r.open(t, reply)); got == sid || len(r.sessions.rows) != 2 {
		t.Fatalf("a build that does not persist it got %s (%d sessions)", got, len(r.sessions.rows))
	}

	// A closed session is not given back.
	b2, keys2 := r.persistedBuild(t, sid)
	closed := time.Now()
	r.sessions.rows[0].ClosedAt = &closed
	reply, err = r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("web01", sid))
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionID(t, r.openUnder(t, b2.BuildID, keys2, reply)); got == sid || len(r.sessions.rows) != 3 {
		t.Fatalf("a closed session was claimed: %s", got)
	}
}

// A session turns late by the schedule its minion says: in its hello, then
// in a sleep_update; core recomputes when the session is due on each
// check-in. Without a schedule there is none.
func TestSessionSchedule(t *testing.T) {
	r := newRig(t)
	h := hello("web01")
	h.Sleep = &protocol.Sleep{Interval: 60_000, Jitter: 20}
	reply, _ := r.send(t, protocol.NewMessageID(), h)
	sid := sessionID(t, r.open(t, reply))
	s := r.sessions.rows[0]
	// 60 s, plus 20 %, plus the grace.
	if s.Sleep == nil || s.LateAt == nil || s.LateAt.Sub(s.CreatedAt) != 72*time.Second+models.SessionLateGrace {
		t.Fatalf("at hello: %+v late at %v", s.Sleep, s.LateAt)
	}

	// A plain check-in pushes it on by the same schedule.
	before := *r.sessions.rows[0].LateAt
	if _, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID()); err != nil {
		t.Fatal(err)
	}
	if !r.sessions.rows[0].LateAt.After(before) && !r.sessions.rows[0].LateAt.Equal(before) {
		t.Fatal("a check-in did not move late_at")
	}

	// A sleep_update: an hour now.
	if _, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), protocol.SleepUpdateItem{T: protocol.ItemSleepUpdate, Interval: 3_600_000}); err != nil {
		t.Fatal(err)
	}
	s = r.sessions.rows[0]
	if s.Sleep.Interval != 3_600_000 || s.LateAt.Sub(*s.LastSeenAt) != time.Hour+models.SessionLateGrace {
		t.Fatalf("after the update: %+v, late %v after the check-in", s.Sleep, s.LateAt.Sub(*s.LastSeenAt))
	}

	// A bad one is refused, and changes nothing.
	reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), protocol.SleepUpdateItem{T: protocol.ItemSleepUpdate, Interval: 0})
	if err != nil {
		t.Fatal(err)
	}
	var e protocol.ErrItem
	if msg := r.openUnder(t, sid, r.keys, reply); len(msg.Items) != 1 || protocol.UnmarshalCBOR(msg.Items[0], &e) != nil || e.Code != protocol.ErrItemBadMessage {
		t.Fatalf("bad update: %+v", msg)
	}
	if r.sessions.rows[0].Sleep.Interval != 3_600_000 {
		t.Fatal("a bad update was taken")
	}

	// No schedule, no late_at.
	reply, _ = r.send(t, protocol.NewMessageID(), hello("web02"))
	sessionID(t, r.open(t, reply))
	if s := r.sessions.rows[1]; s.Sleep != nil || s.LateAt != nil {
		t.Fatalf("without a schedule: %+v %v", s.Sleep, s.LateAt)
	}
}

// A deleted build's minions open no new sessions, staged or not.
func TestHelloAfterBuildDeleted(t *testing.T) {
	r := newRig(t)
	tempID, keys := r.staged(t)
	delete(r.builds, r.build.BuildID)
	if _, err := r.sendUnder(t, tempID, keys, protocol.NewMessageID(), hello("web01")); !errors.Is(err, ErrUnopened) {
		t.Fatalf("hello: %v", err)
	}
	if len(r.sessions.rows) != 0 {
		t.Fatal("a session was opened")
	}
}
