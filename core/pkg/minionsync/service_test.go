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
	"github.com/logoscore/logos-core/core/pkg/pagination"
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

type fakeSessions struct{ rows []models.MinionSession }

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

func (f *fakeSessions) CheckIn(_ context.Context, id uuid.UUID, at time.Time) error {
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			f.rows[i].LastSeenAt, f.rows[i].ExpiresAt = &at, nil
		}
	}
	return nil
}

func (f *fakeSessions) SetHello(_ context.Context, id uuid.UUID, h models.MinionHello) error {
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			f.rows[i].Hello = h
		}
	}
	return nil
}

func (f *fakeSessions) FindByOperationIDWithCursor(context.Context, uuid.UUID, string, *pagination.Cursor, int64, bool) ([]models.MinionSession, error) {
	return f.rows, nil
}

func (f *fakeSessions) CountByOperationID(context.Context, uuid.UUID, string) (int64, error) {
	return int64(len(f.rows)), nil
}

func (f *fakeSessions) DeleteByOperationID(context.Context, uuid.UUID) error { return nil }

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
	builds   fakeBuilds
	stagings *fakeStagings
	sessions *fakeSessions
	bus      *fakeBus
	build    *models.Build
	keys     protocol.AES256CBCHMACSHA256Material
	priv     *rsa.PrivateKey
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
	return &rig{
		svc:    NewService(bs, st, ss, &fakeCache{seen: map[string]bool{}}, bus, zap.NewNop()),
		builds: bs, stagings: st, sessions: ss, bus: bus, build: b, keys: keys, priv: priv,
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
	return r.svc.Handle(context.Background(), under.String(), frame)
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

	// The same message again is a replay: dropped, nothing answered.
	if reply, err := r.send(t, id, stage); err != nil || reply != nil {
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
		if _, err := r.svc.Handle(ctx, c.id, c.frame); !errors.Is(err, ErrUnopened) {
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
	if s.SessionID != sid || !s.Staged || s.BuildID != r.build.BuildID || s.OperationID != r.build.OperationID ||
		s.Target != "linux-elf" || s.Hello.Host != "web01" || s.Hello.PID != 4242 || s.ExpiresAt == nil ||
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
	if len(r.sessions.rows) != 1 || r.sessions.rows[0].SessionID != sid || r.sessions.rows[0].Staged {
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
	if s := r.sessions.rows[0]; s.LastSeenAt == nil || s.ExpiresAt != nil {
		t.Fatalf("after check-in %+v", s)
	}

	// A rehello: new details, the same session id; an item not built yet
	// gets unknown_item.
	reply, err := r.sendUnder(t, sid, keys, protocol.NewMessageID(), hello("web01-renamed"),
		map[string]any{"t": "get_tasks", "max": 1})
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
