package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/logoscore/logos-golang-protocol/protocol"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// fakeTransport records publishes and lets the test answer them. onPublish,
// when set, runs synchronously inside publish (after bind), like a module that
// answers before Publish returns.
type fakeTransport struct {
	mu        sync.Mutex
	published []published
	gen       uint64
	err       error
	onPublish func(p published)
}

type published struct {
	key string
	msg amqp.Publishing
}

func (f *fakeTransport) publish(_ context.Context, key string, msg amqp.Publishing, bind func(uint64)) error {
	f.mu.Lock()
	if f.err != nil {
		f.mu.Unlock()
		return f.err
	}
	bind(f.gen)
	p := published{key: key, msg: msg}
	f.published = append(f.published, p)
	hook := f.onPublish
	f.mu.Unlock()
	if hook != nil {
		hook(p)
	}
	return nil
}

func (f *fakeTransport) close() error { return nil }

func replyBody(t *testing.T, correlationID string, payload any) []byte {
	t.Helper()
	req := protocol.Envelope{CorrelationID: correlationID, Type: "mgmt.config.apply", Version: protocol.EnvelopeVersionV1}
	reply, err := protocol.NewReply(req, protocol.Source{Service: "channel", Instance: "http-1"}, payload)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(reply)
	return b
}

func TestRPCClient_PublishesEnvelopeWithContractProperties(t *testing.T) {
	ft := &fakeTransport{gen: 1}
	c := newRPCClient(ft, zap.NewNop())
	ft.onPublish = func(p published) {
		go c.deliver(p.msg.CorrelationId, replyBody(t, p.msg.CorrelationId, map[string]any{"applied_revision": 3}))
	}

	reply, err := c.Call(context.Background(), "logos.channel.rpc.http-1", protocol.TypeMgmtConfigApply,
		protocol.ConfigApplyRequest{ManagementConfig: protocol.ManagementConfig{Revision: 3, Values: map[string]any{}}},
		1500*time.Millisecond)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if reply.Status != protocol.StatusOK {
		t.Errorf("status = %q", reply.Status)
	}

	p := ft.published[0]
	if p.key != "logos.channel.rpc.http-1" {
		t.Errorf("routing key = %q", p.key)
	}
	m := p.msg
	if m.ReplyTo != "amq.rabbitmq.reply-to" || m.Type != protocol.TypeMgmtConfigApply || m.Expiration != "1500" {
		t.Errorf("properties = reply_to %q type %q expiration %q", m.ReplyTo, m.Type, m.Expiration)
	}
	if m.CorrelationId == "" || m.CorrelationId != m.MessageId {
		t.Errorf("correlation_id %q must equal message_id %q", m.CorrelationId, m.MessageId)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(m.Body, &env); err != nil {
		t.Fatal(err)
	}
	if env.MessageID != m.MessageId || env.CorrelationID != m.CorrelationId || env.Type != protocol.TypeMgmtConfigApply ||
		env.Version != protocol.EnvelopeVersionV1 || env.Source.Service != protocol.ServiceCore {
		t.Errorf("envelope = %+v", env)
	}
	var payload protocol.ConfigApplyRequest
	if err := json.Unmarshal(env.Payload, &payload); err != nil || payload.Revision != 3 {
		t.Errorf("payload = %s (%v)", env.Payload, err)
	}
}

func TestRPCClient_ConcurrentCallsGetTheirOwnReplies(t *testing.T) {
	ft := &fakeTransport{gen: 1}
	c := newRPCClient(ft, zap.NewNop())

	// Collect every request, then answer them in reverse order.
	const n = 50
	ready := make(chan published, n)
	ft.onPublish = func(p published) { ready <- p }

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reply, err := c.Call(context.Background(), "q", "mgmt.action.invoke", map[string]any{"i": i}, 5*time.Second)
			if err != nil {
				errs <- err
				return
			}
			var got struct{ I int }
			_ = json.Unmarshal(reply.Payload, &got)
			if got.I != i {
				errs <- fmt.Errorf("call %d got reply for %d", i, got.I)
			}
		}(i)
	}
	var reqs []published
	for i := 0; i < n; i++ {
		reqs = append(reqs, <-ready)
	}
	for i := len(reqs) - 1; i >= 0; i-- {
		var env protocol.Envelope
		_ = json.Unmarshal(reqs[i].msg.Body, &env)
		var in struct{ I int }
		_ = json.Unmarshal(env.Payload, &in)
		c.deliver(reqs[i].msg.CorrelationId, replyBody(t, reqs[i].msg.CorrelationId, map[string]any{"i": in.I}))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if len(c.pending) != 0 {
		t.Errorf("%d calls left in the correlation map", len(c.pending))
	}
}

func TestRPCClient_TimeoutAndLateReply(t *testing.T) {
	ft := &fakeTransport{gen: 1}
	c := newRPCClient(ft, zap.NewNop())

	start := time.Now()
	_, err := c.Call(context.Background(), "q", "mgmt.action.invoke", nil, 50*time.Millisecond)
	if !errors.Is(err, ErrRPCTimeout) {
		t.Fatalf("err = %v, want ErrRPCTimeout", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("timeout took far longer than requested")
	}
	if len(c.pending) != 0 {
		t.Error("timed-out call must leave the correlation map")
	}
	// A reply arriving after the caller gave up is dropped, not a panic or block.
	c.deliver(ft.published[0].msg.CorrelationId, replyBody(t, ft.published[0].msg.CorrelationId, nil))
}

func TestRPCClient_EnvelopeCorrelationIDFallback(t *testing.T) {
	ft := &fakeTransport{gen: 1}
	c := newRPCClient(ft, zap.NewNop())
	ft.onPublish = func(p published) {
		go c.deliver("", replyBody(t, p.msg.CorrelationId, nil)) // module forgot the AMQP property
	}
	if _, err := c.Call(context.Background(), "q", "x", nil, time.Second); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestRPCClient_ErrorReplyIsAReplyNotAnError(t *testing.T) {
	ft := &fakeTransport{gen: 1}
	c := newRPCClient(ft, zap.NewNop())
	ft.onPublish = func(p published) {
		req := protocol.Envelope{CorrelationID: p.msg.CorrelationId, Version: protocol.EnvelopeVersionV1}
		b, _ := json.Marshal(protocol.NewErrorReply(req, protocol.Source{}, protocol.CodeActionFailed, "boom"))
		go c.deliver(p.msg.CorrelationId, b)
	}
	reply, err := c.Call(context.Background(), "q", "mgmt.action.invoke", nil, time.Second)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if reply.Status != protocol.StatusError || reply.Error == nil || reply.Error.Code != protocol.CodeActionFailed {
		t.Errorf("reply = %+v", reply)
	}
}

func TestRPCClient_UnroutableAndChannelClose(t *testing.T) {
	ft := &fakeTransport{gen: 7}
	c := newRPCClient(ft, zap.NewNop())

	ft.onPublish = func(p published) {
		go c.resolve(p.msg.CorrelationId, rpcResult{err: ErrRPCUnroutable})
	}
	if _, err := c.Call(context.Background(), "missing", "x", nil, 5*time.Second); !errors.Is(err, ErrRPCUnroutable) {
		t.Errorf("err = %v, want ErrRPCUnroutable", err)
	}

	ft.onPublish = func(published) { go c.failGen(7, ErrRPCChannelClosed) }
	if _, err := c.Call(context.Background(), "q", "x", nil, 5*time.Second); !errors.Is(err, ErrRPCChannelClosed) {
		t.Errorf("err = %v, want ErrRPCChannelClosed", err)
	}

	// Closing an older generation does not touch calls on the current one.
	ft.onPublish = func(p published) {
		c.failGen(6, ErrRPCChannelClosed)
		go c.deliver(p.msg.CorrelationId, replyBody(t, p.msg.CorrelationId, nil))
	}
	if _, err := c.Call(context.Background(), "q", "x", nil, 5*time.Second); err != nil {
		t.Errorf("err = %v, want reply", err)
	}
}

func TestRPCClient_PublishErrorAndEmptyQueue(t *testing.T) {
	ft := &fakeTransport{err: errors.New("broker down")}
	c := newRPCClient(ft, zap.NewNop())
	if _, err := c.Call(context.Background(), "q", "x", nil, time.Second); err == nil {
		t.Error("publish failure must surface")
	}
	if len(c.pending) != 0 {
		t.Error("failed publish must leave the correlation map")
	}
	if _, err := c.Call(context.Background(), "", "x", nil, time.Second); err == nil {
		t.Error("empty routing key must be refused")
	}
}
