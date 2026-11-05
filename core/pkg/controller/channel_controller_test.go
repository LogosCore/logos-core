package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/logoscore/logos-core/core/pkg/minionsync"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// fakeGate is a stub RegistrationGate. registeredGate() admits everything; the
// rejection/error variants are constructed inline per test.
type fakeGate struct {
	registered bool
	module     string
	err        error
}

func (g fakeGate) IsRegistered(_ context.Context, _ string) (bool, error) {
	return g.registered, g.err
}

func (g fakeGate) Registration(_ context.Context, _ string) (string, bool, error) {
	return g.module, g.registered, g.err
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
	reply  []byte
	err    error
	calls  int
	id     string
	frame  []byte
	module string
	// ctxErr and deadline are what the last message's context said.
	ctxErr   error
	deadline bool
}

func (f *fakeMinions) OpenedFor(string, uint64) (uuid.UUID, string, bool) { return uuid.Nil, "", false }

func (f *fakeMinions) ForTarget(*minionsync.Conn, string, uint64) bool { return false }

func (f *fakeMinions) Handle(ctx context.Context, id, _ string, frame []byte) ([]byte, error) {
	f.calls++
	f.id, f.frame, f.module = id, frame, minionsync.ChannelOf(ctx)
	f.ctxErr = ctx.Err()
	_, f.deadline = ctx.Deadline()
	return f.reply, f.err
}

func (f *fakeMinions) Exchange(ctx context.Context, _ *minionsync.Conn, id, peer string, frame []byte) ([]byte, error) {
	return f.Handle(ctx, id, peer, frame)
}

func (f *fakeMinions) KeepAlive(context.Context, *minionsync.Conn) {}

func (f *fakeMinions) Closed(context.Context, *minionsync.Conn) {}

func newCtrl(gate RegistrationGate) IChannelController {
	return newCtrlWith(gate, &fakeMinions{})
}

func newCtrlWith(gate RegistrationGate, minions MinionHandler) IChannelController {
	gin.SetMode(gin.TestMode)
	return NewChannelController(gate, minions, zap.NewNop(), testMaxSize)
}

// What the minion handler answers decides the reply: its sealed answer as a
// 200 body, nothing as a 204, a message that does not open as a 422.
func TestChannelSync_MinionAnswers(t *testing.T) {
	m := &fakeMinions{reply: []byte{0x01, 0xff}}
	w := doSync(newCtrlWith(fakeGate{registered: true, module: "http"}, m), validSyncHeaders(), ciphertext)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), m.reply) || w.Header().Get("Content-Type") != protocol.SyncContentType {
		t.Fatalf("answer: %d %x %q", w.Code, w.Body.Bytes(), w.Header().Get("Content-Type"))
	}
	// With the channel that carried it: its instance's module.
	if m.id != testBuildID || !bytes.Equal(m.frame, ciphertext) || m.module != "http" {
		t.Fatalf("handler got %s %x through %q", m.id, m.frame, m.module)
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

	// A request sent again (the channel retrying it) is handed on: core
	// answers it by the minion's own message id, with the reply it got.
	m = &fakeMinions{reply: []byte{1}}
	ctrl := newCtrlWith(registeredGate(), m)
	doSync(ctrl, validSyncHeaders(), ciphertext)
	if w := doSync(ctrl, validSyncHeaders(), ciphertext); w.Code != http.StatusOK || m.calls != 2 || !bytes.Equal(w.Body.Bytes(), m.reply) {
		t.Fatalf("sent again: %d after %d calls", w.Code, m.calls)
	}
}

// The handling is core's own: a channel whose request ends (it timed out,
// it went away) does not cut it short, and it has a deadline of its own.
func TestChannelSync_HandlingOutlivesTheRequest(t *testing.T) {
	m := &fakeMinions{reply: []byte{1}}
	w, c := newSyncRequest(validSyncHeaders(), ciphertext)
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	newCtrlWith(registeredGate(), m).Sync(c)
	c.Writer.WriteHeaderNow()
	if m.calls != 1 || m.ctxErr != nil || !m.deadline {
		t.Fatalf("handled %d: ctx %v, deadline %v (%d)", m.calls, m.ctxErr, m.deadline, w.Code)
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
