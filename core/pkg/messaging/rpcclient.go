package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/logoscore/logos-golang-protocol/protocol"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Core → module RPC. The mirror of rpcserver.go: core publishes a request
// envelope to a module's own rpc_queue (the one it declared at module.register)
// and waits for the ReplyEnvelope.
//
// Wire shape (the module SDK is built against exactly this):
//   - published to the DEFAULT exchange, routing key = the instance's rpc_queue
//   - reply_to = "amq.rabbitmq.reply-to" (RabbitMQ direct reply-to)
//   - correlation_id = message_id = the envelope's message_id (a ULID)
//   - AMQP `type` = envelope type (e.g. "mgmt.config.apply")
//   - expiration = the call timeout in ms, so a request to a module that is
//     down does not sit in its queue and run long after the caller gave up
//   - mandatory, so a request to a queue that does not exist is returned
//     immediately (ErrRPCUnroutable) instead of being dropped and timing out
//
// Why a dedicated amqp091 connection rather than messaging.Client's wagslane
// Conn: direct reply-to requires the request to be published on the SAME
// channel that consumes amq.rabbitmq.reply-to (in no-ack mode). wagslane's
// Publisher and Consumer each own a private channel, and its Conn does not
// expose the underlying *amqp.Connection, so there is no way to get one channel
// doing both through it. Direct reply-to is worth that: no reply queue to
// declare, name or clean up per core instance, and replies cannot outlive the
// channel that is waiting for them. The cost is reconnect handling, which is
// kept minimal: the channel is (re)opened lazily on the next Call, and calls in
// flight on a channel that closes fail immediately rather than time out.

// directReplyTo is RabbitMQ's pseudo-queue for direct reply-to.
const directReplyTo = "amq.rabbitmq.reply-to"

// Call timeouts.
const (
	DefaultRPCTimeout = 10 * time.Second
	MaxRPCTimeout     = 2 * time.Minute
)

var (
	// ErrRPCTimeout means no reply arrived within the call's timeout.
	ErrRPCTimeout = errors.New("rpc timed out waiting for reply")
	// ErrRPCUnroutable means the broker returned the request: no queue is
	// bound to the routing key (the module's rpc_queue does not exist).
	ErrRPCUnroutable = errors.New("rpc request unroutable: module rpc queue does not exist")
	// ErrRPCChannelClosed means the reply channel closed while waiting.
	ErrRPCChannelClosed = errors.New("rpc channel closed before the reply arrived")
)

// RPCCaller sends a core → module RPC request and waits for its reply. A
// reply with status "error" is returned as a reply (err nil) — the caller
// decides what a module-reported error means. err is non-nil only when no
// reply was obtained (ErrRPCTimeout, ErrRPCUnroutable, transport failures).
type RPCCaller interface {
	Call(ctx context.Context, queue, opType string, payload any, timeout time.Duration) (ReplyEnvelope, error)
}

// rpcTransport is the seam between correlation (RPCClient) and the broker:
// publish one request on the channel whose direct reply-to consumer feeds
// RPCClient.deliver. Before publishing it reports, through bind, the
// generation of that channel, so the call fails promptly if the channel closes
// while it waits. Faked in tests.
type rpcTransport interface {
	publish(ctx context.Context, routingKey string, msg amqp.Publishing, bind func(gen uint64)) error
	close() error
}

type rpcResult struct {
	reply ReplyEnvelope
	err   error
}

type pendingCall struct {
	ch  chan rpcResult // buffered(1); receives at most one result
	gen uint64         // channel generation the request went out on
}

// RPCClient correlates replies to in-flight calls. Safe for concurrent use:
// any number of calls share one channel and one reply consumer, each waiting
// on its own entry in the correlation map.
type RPCClient struct {
	transport rpcTransport
	logger    *zap.Logger

	mu      sync.Mutex
	pending map[string]*pendingCall
}

// NewRPCClient builds a client that dials cfg's broker on first use.
func NewRPCClient(cfg Config, logger *zap.Logger) *RPCClient {
	c := newRPCClient(nil, logger)
	c.transport = &amqpRPCTransport{url: cfg.amqpURL(), client: c, logger: c.logger}
	return c
}

func newRPCClient(t rpcTransport, logger *zap.Logger) *RPCClient {
	return &RPCClient{
		transport: t,
		logger:    logger.With(zap.String("component", "rpc-client")),
		pending:   make(map[string]*pendingCall),
	}
}

// Close tears down the broker connection. Calls in flight fail.
func (c *RPCClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.close()
}

// Call implements RPCCaller. timeout <= 0 uses DefaultRPCTimeout; it is capped
// at MaxRPCTimeout. ctx cancellation also ends the wait.
func (c *RPCClient) Call(ctx context.Context, queue, opType string, payload any, timeout time.Duration) (ReplyEnvelope, error) {
	if queue == "" {
		return ReplyEnvelope{}, fmt.Errorf("rpc %s: empty routing key", opType)
	}
	if timeout <= 0 {
		timeout = DefaultRPCTimeout
	}
	if timeout > MaxRPCTimeout {
		timeout = MaxRPCTimeout
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return ReplyEnvelope{}, fmt.Errorf("rpc %s: marshal payload: %w", opType, err)
	}
	id := protocol.NewULID()
	env := Envelope{
		MessageID:     id,
		CorrelationID: id,
		Type:          opType,
		Version:       protocol.EnvelopeVersionV1,
		Timestamp:     protocol.NowTimestamp(),
		Source:        coreSource(),
		Payload:       raw,
	}
	body, err := json.Marshal(env)
	if err != nil {
		return ReplyEnvelope{}, fmt.Errorf("rpc %s: marshal envelope: %w", opType, err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Register before publishing: a fast module can answer before Publish
	// returns.
	call := &pendingCall{ch: make(chan rpcResult, 1)}
	c.mu.Lock()
	c.pending[id] = call
	c.mu.Unlock()
	defer c.forget(id)

	ms := timeout.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	bind := func(gen uint64) {
		c.mu.Lock()
		call.gen = gen
		c.mu.Unlock()
	}
	err = c.transport.publish(ctx, queue, amqp.Publishing{
		ContentType:   "application/json",
		CorrelationId: id,
		MessageId:     id,
		ReplyTo:       directReplyTo,
		Type:          opType,
		Expiration:    strconv.FormatInt(ms, 10),
		Timestamp:     time.Now().UTC(),
		Body:          body,
	}, bind)
	if err != nil {
		return ReplyEnvelope{}, fmt.Errorf("rpc %s to %s: publish: %w", opType, queue, err)
	}

	select {
	case r := <-call.ch:
		if r.err != nil {
			return ReplyEnvelope{}, fmt.Errorf("rpc %s to %s: %w", opType, queue, r.err)
		}
		return r.reply, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ReplyEnvelope{}, fmt.Errorf("rpc %s to %s after %s: %w", opType, queue, timeout, ErrRPCTimeout)
		}
		return ReplyEnvelope{}, ctx.Err()
	}
}

func (c *RPCClient) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// resolve hands r to the call waiting on id, if any. The channel is buffered
// and the entry removed under the lock, so a result is delivered at most once
// and never blocks the consumer loop.
func (c *RPCClient) resolve(id string, r rpcResult) bool {
	c.mu.Lock()
	call, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	call.ch <- r
	return true
}

// deliver routes one reply body. The AMQP correlation id is authoritative;
// the envelope's is the fallback for a module that forgot the property.
func (c *RPCClient) deliver(correlationID string, body []byte) {
	var reply ReplyEnvelope
	if err := json.Unmarshal(body, &reply); err != nil {
		c.logger.Warn("discarding unparseable rpc reply", zap.String("correlation_id", correlationID), zap.Error(err))
		return
	}
	if correlationID == "" {
		correlationID = reply.CorrelationID
	}
	if !c.resolve(correlationID, rpcResult{reply: reply}) {
		// The caller gave up (timeout) before the module answered.
		c.logger.Debug("rpc reply for no pending call", zap.String("correlation_id", correlationID), zap.String("type", reply.Type))
	}
}

// failGen fails every call that went out on channel generation gen.
func (c *RPCClient) failGen(gen uint64, err error) {
	c.mu.Lock()
	var hit []*pendingCall
	for id, call := range c.pending {
		if call.gen == gen {
			hit = append(hit, call)
			delete(c.pending, id)
		}
	}
	c.mu.Unlock()
	for _, call := range hit {
		call.ch <- rpcResult{err: err}
	}
}

// --- broker transport ---

// amqpRPCTransport owns one amqp091 connection and one channel that both
// publishes requests and consumes direct replies.
type amqpRPCTransport struct {
	url    string
	client *RPCClient
	logger *zap.Logger

	mu     sync.Mutex
	conn   *amqp.Connection
	ch     *amqp.Channel
	gen    uint64
	closed bool
}

// channel returns the live channel, dialing and/or reopening it as needed.
func (t *amqpRPCTransport) channel() (*amqp.Channel, uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, 0, errors.New("rpc client closed")
	}
	if t.ch != nil && !t.ch.IsClosed() {
		return t.ch, t.gen, nil
	}
	if t.conn == nil || t.conn.IsClosed() {
		conn, err := amqp.DialConfig(t.url, amqp.Config{
			Properties: amqp.Table{"connection_name": "logos-core rpc-client"},
		})
		if err != nil {
			return nil, 0, fmt.Errorf("dial: %w", err)
		}
		t.conn = conn
	}
	ch, err := t.conn.Channel()
	if err != nil {
		return nil, 0, fmt.Errorf("open channel: %w", err)
	}
	// Direct reply-to: consume the pseudo-queue in no-ack mode on the channel
	// the requests are published from.
	deliveries, err := ch.Consume(directReplyTo, "", true, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return nil, 0, fmt.Errorf("consume %s: %w", directReplyTo, err)
	}
	returns := ch.NotifyReturn(make(chan amqp.Return, 16))
	closes := ch.NotifyClose(make(chan *amqp.Error, 1))

	t.gen++
	gen := t.gen
	t.ch = ch

	go func() {
		for d := range deliveries {
			t.client.deliver(d.CorrelationId, d.Body)
		}
	}()
	go func() {
		for r := range returns {
			t.client.resolve(r.CorrelationId, rpcResult{err: fmt.Errorf("%w (%d %s, routing key %q)",
				ErrRPCUnroutable, r.ReplyCode, r.ReplyText, r.RoutingKey)})
		}
	}()
	go func() {
		amqpErr, ok := <-closes
		if ok && amqpErr != nil {
			t.logger.Warn("rpc client channel closed", zap.String("reason", amqpErr.Error()))
		}
		t.client.failGen(gen, ErrRPCChannelClosed)
	}()
	return ch, gen, nil
}

func (t *amqpRPCTransport) publish(ctx context.Context, routingKey string, msg amqp.Publishing, bind func(gen uint64)) error {
	ch, gen, err := t.channel()
	if err != nil {
		return err
	}
	bind(gen)
	// mandatory=true: an unroutable request comes back on NotifyReturn.
	return ch.PublishWithContext(ctx, "", routingKey, true, false, msg)
}

func (t *amqpRPCTransport) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}
