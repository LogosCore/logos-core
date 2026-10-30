package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/logoscore/logos-core/core/pkg/cache"
	"github.com/logoscore/logos-core/core/pkg/minionsync"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// fakeCache is an in-memory Cache used to exercise the SetNX dedup path without
// a live Redis. Only SetNX is meaningful here; the rest satisfy the interface.
type fakeCache struct {
	seen map[string]struct{}
}

func newFakeCache() *fakeCache { return &fakeCache{seen: map[string]struct{}{}} }

func (f *fakeCache) Get(ctx context.Context, key string) (string, error) { return "", nil }
func (f *fakeCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return nil
}
func (f *fakeCache) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	if _, ok := f.seen[key]; ok {
		return false, nil
	}
	f.seen[key] = struct{}{}
	return true, nil
}

// AppendAt is unused by this package; present to satisfy cache.Cache.
func (f *fakeCache) AppendAt(_ context.Context, _ string, _ int64, _ string, _ int64, _ time.Duration) (int64, error) {
	return 0, nil
}

// IncrWithTTL is unused by this package; present to satisfy cache.Cache.
func (f *fakeCache) IncrWithTTL(_ context.Context, _ string, _ time.Duration) (int64, error) {
	return 0, nil
}
func (f *fakeCache) SetWithTags(ctx context.Context, key string, value interface{}, tags []string, ttl time.Duration) error {
	return nil
}
func (f *fakeCache) Del(ctx context.Context, keys ...string) error                  { return nil }
func (f *fakeCache) InvalidateCache(ctx context.Context, e string, id string) error { return nil }
func (f *fakeCache) Close() error                                                   { return nil }
func (f *fakeCache) IsEnabled() bool                                                { return true }

// fakeGate is a stub RegistrationGate. registeredGate() admits everything; the
// rejection/error variants are constructed inline per test.
type fakeGate struct {
	registered bool
	err        error
}

func (g fakeGate) IsRegistered(_ context.Context, _ string) (bool, error) {
	return g.registered, g.err
}

func registeredGate() fakeGate { return fakeGate{registered: true} }

const (
	testMessageID = "c4ca4238-a0b9-4382-8dcc-509a6f75849b"
	testBuildID   = "5b0c4f6e-8a1d-4c2b-9e7f-3d2a1b0c9e8f"
	testTrace     = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	testMaxSize   = 64
)

// ciphertext is not UTF-8: the body travels as raw bytes.
var ciphertext = []byte{0x00, 0xff, 0xfe, 0x80, 0x0a}

func validSyncHeaders() http.Header {
	h := http.Header{}
	protocol.InboundMinionMessage{
		MessageID: testMessageID, BuildID: testBuildID, ChannelInstance: "http-1", TraceParent: testTrace,
	}.SetHeaders(h)
	return h
}

func newSyncRequest(h http.Header, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, protocol.SyncPath, bytes.NewReader(body))
	c.Request.Header = h
	return w, c
}

func doSync(ctrl IChannelController, h http.Header, body []byte) *httptest.ResponseRecorder {
	w, c := newSyncRequest(h, body)
	ctrl.Sync(c)
	c.Writer.WriteHeaderNow()
	return w
}

// fakeMinions answers every message with reply and err, and counts them.
type fakeMinions struct {
	reply []byte
	err   error
	calls int
	id    string
	frame []byte
}

func (f *fakeMinions) Handle(_ context.Context, id, _ string, frame []byte) ([]byte, error) {
	f.calls++
	f.id, f.frame = id, frame
	return f.reply, f.err
}

func newCtrl(gate RegistrationGate) IChannelController {
	return newCtrlWith(gate, &fakeMinions{})
}

func newCtrlWith(gate RegistrationGate, minions MinionHandler) IChannelController {
	gin.SetMode(gin.TestMode)
	return NewChannelController(newFakeCache(), gate, minions, zap.NewNop(), testMaxSize)
}

// What the minion handler answers decides the reply: its sealed answer as a
// 200 body, nothing as a 204, a message that does not open as a 422.
func TestChannelSync_MinionAnswers(t *testing.T) {
	m := &fakeMinions{reply: []byte{0x01, 0xff}}
	w := doSync(newCtrlWith(registeredGate(), m), validSyncHeaders(), ciphertext)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), m.reply) || w.Header().Get("Content-Type") != protocol.SyncContentType {
		t.Fatalf("answer: %d %x %q", w.Code, w.Body.Bytes(), w.Header().Get("Content-Type"))
	}
	if m.id != testBuildID || !bytes.Equal(m.frame, ciphertext) {
		t.Fatalf("handler got %s %x", m.id, m.frame)
	}
	if out := protocol.OutboundFromHeaders(w.Header()); out.BuildID != testBuildID {
		t.Fatalf("reply headers %+v", out)
	}

	m = &fakeMinions{err: fmt.Errorf("wrapped: %w", minionsync.ErrUnopened)}
	if w := doSync(newCtrlWith(registeredGate(), m), validSyncHeaders(), ciphertext); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unopened: %d %s", w.Code, w.Body.String())
	}
	m = &fakeMinions{err: minionsync.ErrReplayed}
	if w := doSync(newCtrlWith(registeredGate(), m), validSyncHeaders(), ciphertext); w.Code != http.StatusConflict {
		t.Fatalf("replayed: %d %s", w.Code, w.Body.String())
	}
	m = &fakeMinions{err: errors.New("mongo down")}
	if w := doSync(newCtrlWith(registeredGate(), m), validSyncHeaders(), ciphertext); w.Code != http.StatusInternalServerError {
		t.Fatalf("failure: %d", w.Code)
	}

	// A replay of the channel's message id is not handed on again.
	m = &fakeMinions{reply: []byte{1}}
	ctrl := newCtrlWith(registeredGate(), m)
	doSync(ctrl, validSyncHeaders(), ciphertext)
	if w := doSync(ctrl, validSyncHeaders(), ciphertext); w.Code != http.StatusNoContent || m.calls != 1 {
		t.Fatalf("replay: %d after %d calls", w.Code, m.calls)
	}
}

func TestChannelSync_Valid(t *testing.T) {
	w := doSync(newCtrl(registeredGate()), validSyncHeaders(), ciphertext)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Errorf("a 204 carries no body: %q, content type %q", w.Body.Bytes(), w.Header().Get("Content-Type"))
	}
	out := protocol.OutboundFromHeaders(w.Header())
	if err := protocol.ValidateOutbound(out); err != nil {
		t.Fatalf("reply headers: %v", err)
	}
	if out.BuildID != testBuildID || out.MessageID == testMessageID || out.TraceParent != testTrace {
		t.Errorf("reply %+v: want the build id echoed, core's own message id, the traceparent returned", out)
	}
}

func TestChannelSync_ValidationErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(h http.Header)
		body   []byte
		code   int
	}{
		{"json body", func(h http.Header) { h.Set("Content-Type", "application/json") }, ciphertext, http.StatusUnsupportedMediaType},
		{"no content type", func(h http.Header) { h.Del("Content-Type") }, ciphertext, http.StatusUnsupportedMediaType},
		{"missing build id", func(h http.Header) { h.Del(protocol.HeaderBuildID) }, ciphertext, http.StatusBadRequest},
		{"build id not a UUID", func(h http.Header) { h.Set(protocol.HeaderBuildID, "s-2b77df") }, ciphertext, http.StatusBadRequest},
		{"missing message id", func(h http.Header) { h.Del(protocol.HeaderMessageID) }, ciphertext, http.StatusBadRequest},
		{"missing instance", func(h http.Header) { h.Del(protocol.HeaderChannelInstance) }, ciphertext, http.StatusBadRequest},
		{"empty body", nil, nil, http.StatusBadRequest},
		{"over the limit", nil, make([]byte, testMaxSize+1), http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := validSyncHeaders()
			if tt.mutate != nil {
				tt.mutate(h)
			}
			w := doSync(newCtrl(registeredGate()), h, tt.body)
			if w.Code != tt.code {
				t.Fatalf("expected %d, got %d (%s)", tt.code, w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("error content type %q, want JSON", ct)
			}
		})
	}
}

// A body without Content-Length (chunked) is cut off at the limit.
func TestChannelSync_ChunkedOverTheLimit(t *testing.T) {
	w, c := newSyncRequest(validSyncHeaders(), make([]byte, testMaxSize+1))
	c.Request.ContentLength = -1
	newCtrl(registeredGate()).Sync(c)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", w.Code, w.Body.String())
	}
}

// An invalid traceparent is ignored, not returned.
func TestChannelSync_InvalidTraceParent(t *testing.T) {
	h := validSyncHeaders()
	h.Set(protocol.HeaderTraceParent, "not-a-trace")
	w := doSync(newCtrl(registeredGate()), h, ciphertext)
	if w.Code != http.StatusNoContent || w.Header().Get(protocol.HeaderTraceParent) != "" {
		t.Fatalf("got %d, traceparent %q", w.Code, w.Header().Get(protocol.HeaderTraceParent))
	}
}

func TestChannelSync_UnregisteredInstanceRejected(t *testing.T) {
	w := doSync(newCtrl(fakeGate{registered: false}), validSyncHeaders(), ciphertext)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unregistered instance, got %d (%s)", w.Code, w.Body.String())
	}
}

// The gate runs before the body is read: an unregistered sender is refused
// even with a body over the limit.
func TestChannelSync_GateBeforeBody(t *testing.T) {
	w := doSync(newCtrl(fakeGate{registered: false}), validSyncHeaders(), make([]byte, testMaxSize+1))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestChannelSync_RegistryErrorFailsClosed(t *testing.T) {
	w := doSync(newCtrl(fakeGate{err: errors.New("mongo down")}), validSyncHeaders(), ciphertext)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on registry error, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestChannelSync_DuplicateMessageIDReturnsNoOp(t *testing.T) {
	// Shared cache across both calls so the second SetNX reports a duplicate.
	ctrl := newCtrl(registeredGate())
	if w := doSync(ctrl, validSyncHeaders(), ciphertext); w.Code != http.StatusNoContent {
		t.Fatalf("first call: expected 204, got %d", w.Code)
	}
	w := doSync(ctrl, validSyncHeaders(), ciphertext)
	if w.Code != http.StatusNoContent {
		t.Fatalf("duplicate call: expected 204 no-op, got %d (%s)", w.Code, w.Body.String())
	}
	if out := protocol.OutboundFromHeaders(w.Header()); out.BuildID != testBuildID {
		t.Errorf("duplicate response not a valid no-op: %+v", out)
	}
}

// Compile-time assurance the fake satisfies the interface.
var _ cache.Cache = (*fakeCache)(nil)
