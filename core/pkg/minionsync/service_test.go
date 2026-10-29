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
	"github.com/logoscore/logos-core/core/pkg/models"
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
	stagings *fakeStagings
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
	b := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(),
		Crypto: &protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: material}}
	priv, err := rsa.GenerateKey(rand.Reader, protocol.StageKeySize)
	if err != nil {
		t.Fatal(err)
	}
	st := &fakeStagings{}
	return &rig{
		svc:      NewService(fakeBuilds{b.BuildID: b}, st, &fakeCache{seen: map[string]bool{}}, zap.NewNop()),
		stagings: st, build: b, keys: keys, priv: priv,
	}
}

// send seals a minion message with items under the build id, as a minion does.
func (r *rig) send(t *testing.T, id []byte, items ...any) ([]byte, error) {
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
	frame, err := protocol.SealFrame(r.keys, r.build.BuildID, protocol.FrameMinionToCore, plain)
	if err != nil {
		t.Fatal(err)
	}
	return r.svc.Handle(context.Background(), r.build.BuildID.String(), frame)
}

func (r *rig) stage(t *testing.T) protocol.StageItem {
	der, _ := x509.MarshalPKIXPublicKey(&r.priv.PublicKey)
	return protocol.StageItem{T: protocol.ItemStage, Key: der, Corr: protocol.NewMessageID()}
}

// open reads core's answer as the minion does.
func (r *rig) open(t *testing.T, reply []byte) protocol.CoreMessage {
	t.Helper()
	plain, err := protocol.OpenFrame(r.keys, r.build.BuildID, protocol.FrameCoreToMinion, reply)
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
		"no items":   nil,
		"two stages": {r.stage(t), r.stage(t)},
		"short corr": {protocol.StageItem{T: protocol.ItemStage, Key: r.stage(t).Key, Corr: []byte{1}}},
		"a hello":    {map[string]any{"t": "hello", "pid": 1, "os": "linux", "arch": "amd64"}},
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
	if len(r.stagings.rows) != 0 {
		t.Fatal("a refused stage was stored")
	}
}
