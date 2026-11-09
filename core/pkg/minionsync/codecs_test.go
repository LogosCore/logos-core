package minionsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/factorylink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func (f *fakeSessions) SetCodecDown(_ context.Context, id uuid.UUID, since *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			f.rows[i].CodecDownSince = since
		}
	}
	return nil
}

// A toy factory for the tests: its suite "acme.xor" seals a plaintext as
// tag ‖ dir ‖ plaintext XOR the key, the tag the first 4 bytes of
// SHA-256(key ‖ id ‖ dir), so the key that opens it is known; its envelope
// "acme.hex" is the message's CBOR in hex.
const (
	toySuite    = "acme.xor"
	toyEnvelope = "acme.hex"
)

func toyTag(key, id []byte, dir byte) []byte {
	sum := sha256.Sum256(append(append(append([]byte{}, key...), id...), dir))
	return sum[:4]
}

func toyXOR(key, b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[i] ^ key[i%len(key)]
	}
	return out
}

func toySeal(key, id []byte, dir byte, plain []byte) []byte {
	return append(append(toyTag(key, id, dir), dir), toyXOR(key, plain)...)
}

// toyOpen opens payload with the first of keys whose tag fits: its index,
// or -1.
func toyOpen(keys [][]byte, id []byte, dir byte, payload []byte) ([]byte, int) {
	if len(payload) < 5 || payload[4] != dir {
		return nil, -1
	}
	for i, k := range keys {
		if bytes.Equal(payload[:4], toyTag(k, id, dir)) {
			return toyXOR(k, payload[5:]), i
		}
	}
	return nil, -1
}

// toyFactory answers factory calls as the toy factory would, or not at all
// while down.
type toyFactory struct {
	mu    sync.Mutex
	down  bool
	calls []protocol.FactoryCall
	// grow pads what it writes outbound by this many bytes.
	grow int
}

func (f *toyFactory) Call(_ context.Context, module string, c protocol.FactoryCall, timeout time.Duration) (protocol.FactoryAnswerFrame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if module != "acme" || timeout != 2*time.Second || len(c.Build) != 16 {
		return protocol.FactoryAnswerFrame{}, errors.New("a call for another factory, timeout or build")
	}
	if f.down {
		return protocol.FactoryAnswerFrame{}, factorylink.ErrNoLink
	}
	fail := func(kind int) (protocol.FactoryAnswerFrame, error) {
		return protocol.FactoryAnswerFrame{}, &factorylink.Failed{CodecFailure: protocol.CodecFailure{Kind: kind}}
	}
	switch c.Type {
	case protocol.FactoryKeys:
		m := protocol.NewMessageID()
		if c.For == protocol.FactoryKeysForStage {
			return protocol.FactoryAnswerFrame{Material: m, Wrapped: append([]byte("wrapped:"), m...)}, nil
		}
		return protocol.FactoryAnswerFrame{Material: m}, nil
	case protocol.FactoryInbound:
		data := c.Data
		var opened *uint64
		if c.Suite() {
			plain, i := toyOpen(c.Keys, c.ID, protocol.FrameMinionToCore, data)
			if i < 0 {
				return fail(protocol.CodecFailNotOpened)
			}
			n := uint64(i)
			data, opened = plain, &n
		}
		if c.Envelope() {
			b, err := hex.DecodeString(string(data))
			if err != nil {
				return fail(protocol.CodecFailNotMessage)
			}
			data = b
		}
		return protocol.FactoryAnswerFrame{Data: data, Opened: opened}, nil
	case protocol.FactoryOutbound:
		data := c.Data
		if c.Envelope() {
			data = []byte(hex.EncodeToString(data) + strings.Repeat(" ", f.grow))
		}
		if c.Suite() {
			data = toySeal(c.Keys[0], c.ID, protocol.FrameCoreToMinion, data)
		}
		return protocol.FactoryAnswerFrame{Data: data}, nil
	}
	return fail(protocol.CodecFailOther)
}

func (f *toyFactory) count(typ int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Type == typ {
			n++
		}
	}
	return n
}

// codecRig is a rig whose build has suite and envelope, translated by a
// toy factory where they are its own; key is a toy suite's material.
type codecRig struct {
	*rig
	factory *toyFactory
	key     []byte
}

func newCodecRig(t *testing.T, suite, envelope string) *codecRig {
	t.Helper()
	r := &codecRig{rig: newRig(t), factory: &toyFactory{}}
	r.build.ModuleName = "acme"
	if envelope != protocol.EnvelopeCBOR {
		r.build.Envelope = envelope
	}
	if suite == toySuite {
		r.key = protocol.NewMessageID()
		material, _ := json.Marshal(r.key)
		r.build.Crypto = &protocol.BuildCrypto{Suite: toySuite, Material: material}
	}
	if protocol.IsFactoryCodec(suite) || protocol.IsFactoryCodec(envelope) {
		r.build.Codec = &protocol.BuildCodec{Growth: 250, Overhead: 64, Timeout: "2s"}
	}
	r.svc.WithFactoryLinks(r.factory)
	return r
}

// write is a minion message with items, written and sealed as the minion
// of the rig's build does, under id with key (a toy key, or the rig's AES
// keys when the suite is core's).
func (r *codecRig) write(t *testing.T, under uuid.UUID, key []byte, items ...any) []byte {
	t.Helper()
	msg := protocol.MinionMessage{V: protocol.MinionMessageV3, ID: protocol.NewMessageID()}
	for _, it := range items {
		raw, _ := protocol.MarshalCBOR(it)
		msg.Items = append(msg.Items, raw)
	}
	plain, _ := protocol.MarshalCBOR(msg)
	switch r.build.Envelope {
	case protocol.EnvelopeJSON:
		plain, _ = protocol.JSONEnvelope.WriteMessage(plain)
	case toyEnvelope:
		plain = []byte(hex.EncodeToString(plain))
	}
	if key != nil {
		return toySeal(key, under[:], protocol.FrameMinionToCore, plain)
	}
	frame, err := protocol.SealFrame(r.keys, under, protocol.FrameMinionToCore, plain)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

// read is core's reply as the minion reads it, and its plaintext.
func (r *codecRig) read(t *testing.T, under uuid.UUID, key, reply []byte) (protocol.CoreMessage, []byte) {
	t.Helper()
	var plain []byte
	if key != nil {
		var i int
		if plain, i = toyOpen([][]byte{key}, under[:], protocol.FrameCoreToMinion, reply); i < 0 {
			t.Fatal("the reply does not open with the toy suite")
		}
	} else {
		var err error
		if plain, err = protocol.OpenFrame(r.keys, under, protocol.FrameCoreToMinion, reply); err != nil {
			t.Fatal(err)
		}
	}
	text := plain
	switch r.build.Envelope {
	case protocol.EnvelopeJSON:
		var err error
		if plain, err = protocol.JSONEnvelope.ReadMessage(plain); err != nil {
			t.Fatalf("the reply is not the JSON envelope: %v\n%s", err, text)
		}
	case toyEnvelope:
		var err error
		if plain, err = hex.DecodeString(strings.TrimSpace(string(plain))); err != nil {
			t.Fatal(err)
		}
	}
	var msg protocol.CoreMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		t.Fatal(err)
	}
	return msg, text
}

func (r *codecRig) hello(t *testing.T) uuid.UUID {
	t.Helper()
	reply, err := r.svc.Handle(context.Background(), r.build.BuildID.String(), "", r.write(t, r.build.BuildID, r.key, hello("web01")))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := r.read(t, r.build.BuildID, r.key, reply)
	var resp protocol.HelloResponseItem
	if err := protocol.UnmarshalCBOR(msg.Items[0], &resp); err != nil {
		t.Fatalf("hello: %+v: %v", msg, err)
	}
	return uuid.UUID(resp.SID)
}

// Core's JSON envelope with core's suite: the minion writes JSON, core
// answers in JSON, and no factory is called.
func TestJSONEnvelope(t *testing.T) {
	r := newCodecRig(t, protocol.CryptoAES256CBCHMACSHA256, protocol.EnvelopeJSON)
	sid := r.hello(t)
	row := r.sessionRow(t, sid)
	if row.Envelope != protocol.EnvelopeJSON || row.Codec != nil {
		t.Fatalf("session envelope %q, codec %+v", row.Envelope, row.Codec)
	}
	reply, err := r.svc.Handle(context.Background(), sid.String(), "", r.write(t, sid, nil, protocol.GetTasksItem{T: protocol.ItemGetTasks}))
	if err != nil {
		t.Fatal(err)
	}
	if reply != nil {
		if _, text := r.read(t, sid, nil, reply); text[0] != '{' {
			t.Fatalf("not JSON: %q", text)
		}
	}
	if len(r.factory.calls) != 0 {
		t.Fatal("a factory called for core's codec")
	}
	// A CBOR message under a JSON build does not read.
	plain, _ := protocol.MarshalCBOR(protocol.MinionMessage{V: protocol.MinionMessageV3, ID: protocol.NewMessageID()})
	frame, _ := protocol.SealFrame(r.keys, sid, protocol.FrameMinionToCore, plain)
	if _, err := r.svc.Handle(context.Background(), sid.String(), "", frame); !errors.Is(err, ErrUnopened) {
		t.Fatalf("CBOR under JSON: %v", err)
	}
}

// Tasks planned for a JSON session fit its connection as JSON, and go
// packed where the build packs: as JSON, deflated.
func TestJSONEnvelopePlansAndPacks(t *testing.T) {
	for _, packs := range []bool{false, true} {
		r := newCodecRig(t, protocol.CryptoAES256CBCHMACSHA256, protocol.EnvelopeJSON)
		tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
		r.svc.WithTasks(tasks)
		sid := r.hello(t)
		row := r.sessionRow(t, sid)
		row.ConnMaxMessage = 2048
		if packs {
			row.Compression = protocol.CompressionDeflate
		}
		for range 6 {
			wire, _ := protocol.MarshalCBOR(protocol.Args{1: strings.Repeat("ls -la /usr/lib; ", 20)})
			tasks.rows = append(tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, OperationID: r.build.OperationID,
				Command: "shell", Op: 11, WireArgs: wire, State: models.MinionTaskQueued})
		}
		reply, err := r.svc.Handle(context.Background(), sid.String(), "", r.write(t, sid, nil, protocol.GetTasksItem{T: protocol.ItemGetTasks}))
		if err != nil {
			t.Fatal(err)
		}
		msg, text := r.read(t, sid, nil, reply)
		if len(text) > 2048 {
			t.Fatalf("packs %v: %d bytes of JSON over a connection of 2048", packs, len(text))
		}
		n, packed := 0, 0
		for _, it := range msg.Items {
			switch protocol.ItemType(it) {
			case protocol.ItemTask:
				n++
			case protocol.ItemPacked:
				inner, err := protocol.UnpackIn(protocol.JSONEnvelope, it, protocol.MaxMinionMessageSize)
				if err != nil || protocol.ItemType(inner) != protocol.ItemTask {
					t.Fatalf("a packed item that is not a task in JSON: %v", err)
				}
				n++
				packed++
			}
		}
		if n == 0 || (packs != (packed > 0)) {
			t.Fatalf("packs %v: %d tasks, %d packed", packs, n, packed)
		}
	}
}

// A factory's suite and envelope: one call each way per message, the
// factory's bytes on the wire, the session keeping the build's codec.
func TestFactoryCodec(t *testing.T) {
	r := newCodecRig(t, toySuite, toyEnvelope)
	sid := r.hello(t)
	if in, out := r.factory.count(protocol.FactoryInbound), r.factory.count(protocol.FactoryOutbound); in != 1 || out != 1 {
		t.Fatalf("hello: %d inbound, %d outbound calls", in, out)
	}
	row := r.sessionRow(t, sid)
	if row.Envelope != toyEnvelope || row.Codec == nil || row.Crypto.Suite != toySuite || row.CodecDownSince != nil {
		t.Fatalf("session %+v", row)
	}
	for _, c := range r.factory.calls {
		if !slicesEqual(c.Layers, []int{protocol.CodecLayerSuite, protocol.CodecLayerEnvelope}) || len(c.Keys) != 1 ||
			!bytes.Equal(c.Keys[0], r.key) || uuid.UUID(c.Build) != r.build.BuildID {
			t.Fatalf("call %+v", c)
		}
	}
	reply, err := r.svc.Handle(context.Background(), sid.String(), "", r.write(t, sid, r.key, protocol.GetTasksItem{T: protocol.ItemGetTasks}))
	if err != nil {
		t.Fatal(err)
	}
	if reply != nil {
		r.read(t, sid, r.key, reply)
	}
	// A frame of core's suite does not open.
	if _, err := r.svc.Handle(context.Background(), sid.String(), "", []byte("garbage!")); !errors.Is(err, ErrUnopened) {
		t.Fatal(err)
	}
}

// The factory's envelope over core's suite: core seals, the factory writes.
func TestFactoryEnvelopeOnly(t *testing.T) {
	r := newCodecRig(t, protocol.CryptoAES256CBCHMACSHA256, toyEnvelope)
	r.hello(t)
	for _, c := range r.factory.calls {
		if !slicesEqual(c.Layers, []int{protocol.CodecLayerEnvelope}) || c.Keys != nil {
			t.Fatalf("call %+v", c)
		}
	}
	if len(r.factory.calls) != 2 {
		t.Fatalf("%d calls", len(r.factory.calls))
	}
}

// A factory that does not answer: what the minion sends is refused, the
// session says since when, and once the factory answers again it clears.
func TestFactoryDown(t *testing.T) {
	r := newCodecRig(t, toySuite, protocol.EnvelopeJSON)
	sid := r.hello(t)
	r.factory.down = true
	frame := r.write(t, sid, r.key, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if _, err := r.svc.Handle(context.Background(), sid.String(), "", frame); !errors.Is(err, ErrUnopened) {
		t.Fatalf("down: %v", err)
	}
	if r.sessionRow(t, sid).CodecDownSince == nil {
		t.Fatal("the session does not say its factory is down")
	}
	r.factory.down = false
	if _, err := r.svc.Handle(context.Background(), sid.String(), "", r.write(t, sid, r.key, protocol.GetTasksItem{T: protocol.ItemGetTasks})); err != nil {
		t.Fatal(err)
	}
	if r.sessionRow(t, sid).CodecDownSince != nil {
		t.Fatal("the flag stayed once the factory answered")
	}
	// A hello under the build id while it is down: refused, nothing opened.
	r.factory.down = true
	if _, err := r.svc.Handle(context.Background(), r.build.BuildID.String(), "", r.write(t, r.build.BuildID, r.key, hello("web02"))); !errors.Is(err, ErrUnopened) {
		t.Fatalf("hello while down: %v", err)
	}
}

// A stage under a factory's suite: the factory makes the keys and wraps
// them for the stage's key, which core does not read.
func TestFactoryStage(t *testing.T) {
	r := newCodecRig(t, toySuite, protocol.EnvelopeCBOR)
	stage := protocol.StageItem{T: protocol.ItemStage, Key: []byte("the suite's own key"), Corr: protocol.NewMessageID()}
	reply, err := r.svc.Handle(context.Background(), r.build.BuildID.String(), "", r.write(t, r.build.BuildID, r.key, stage))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := r.read(t, r.build.BuildID, r.key, reply)
	var resp protocol.StageResponseItem
	if err := protocol.UnmarshalCBOR(msg.Items[0], &resp); err != nil {
		t.Fatal(err)
	}
	material, ok := bytes.CutPrefix(resp.Keys, []byte("wrapped:"))
	if !ok || len(r.stagings.rows) != 1 || r.stagings.rows[0].Crypto.Suite != toySuite {
		t.Fatalf("stage response %+v, stagings %+v", resp, r.stagings.rows)
	}
	var keys protocol.FactoryCall
	for _, c := range r.factory.calls {
		if c.Type == protocol.FactoryKeys {
			keys = c
		}
	}
	if keys.For != protocol.FactoryKeysForStage || string(keys.StageKey) != "the suite's own key" {
		t.Fatalf("keys call %+v", keys)
	}
	// The hello under the temporary id opens with the stage's keys.
	tid := uuid.UUID(resp.TID)
	reply, err = r.svc.Handle(context.Background(), tid.String(), "", r.write(t, tid, material, hello("web01")))
	if err != nil {
		t.Fatal(err)
	}
	if msg, _ := r.read(t, tid, material, reply); protocol.ItemType(msg.Items[0]) != protocol.ItemHelloResponse {
		t.Fatalf("hello under the temporary id: %+v", msg)
	}
}

// A message under a claimed session sealed with a replaced holder's keys:
// the factory says which candidate opened it, and the holder is told
// session_replaced.
func TestFactorySuiteReplacedKeys(t *testing.T) {
	r := newCodecRig(t, toySuite, protocol.EnvelopeCBOR)
	sid := r.hello(t)
	old := protocol.NewMessageID()
	material, _ := json.Marshal(old)
	row := r.sessionRow(t, sid)
	row.ReplacedKeys = []models.MinionSessionReplacedKeys{{Crypto: protocol.BuildCrypto{Suite: toySuite, Material: material}, At: time.Now()}}
	reply, err := r.svc.Handle(context.Background(), sid.String(), "", r.write(t, sid, old, protocol.GetTasksItem{T: protocol.ItemGetTasks}))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := r.read(t, sid, old, reply)
	var e protocol.ErrItem
	if len(msg.Items) != 1 || protocol.UnmarshalCBOR(msg.Items[0], &e) != nil || e.Code != protocol.ErrItemSessionReplaced {
		t.Fatalf("reply %+v", msg)
	}
}

// What a factory writes past its declaration is not sent.
func TestFactoryOutgrew(t *testing.T) {
	r := newCodecRig(t, protocol.CryptoAES256CBCHMACSHA256, toyEnvelope)
	r.factory.grow = 100_000
	if _, err := r.svc.Handle(context.Background(), r.build.BuildID.String(), "", r.write(t, r.build.BuildID, nil, hello("web01"))); !errors.Is(err, ErrUnopened) {
		t.Fatalf("an answer past its bound: %v", err)
	}
}

func TestPlanLimit(t *testing.T) {
	s := &models.MinionSession{ConnMaxMessage: 10_000}
	if planLimit(s) != 10_000 {
		t.Fatal(planLimit(s))
	}
	s.Envelope = protocol.EnvelopeJSON
	if planLimit(s) != 7_500 {
		t.Fatal(planLimit(s))
	}
	s.Codec = &protocol.BuildCodec{Growth: 200, Overhead: 1000}
	if planLimit(s) != 3_375 {
		t.Fatal(planLimit(s))
	}
}

func slicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
