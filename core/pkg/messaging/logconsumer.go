package messaging

import (
	"context"
	"fmt"
	"sync"

	"github.com/logoscore/logos-golang-protocol/protocol"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Live module log tail: modules publish protocol.LogBatch messages to
// protocol.LogsExchange with routing key = stream id. For each stream core
// opens a temporary queue bound to that key and relays what arrives; nothing
// is stored.
//
// Like RPCClient this uses a plain amqp091 connection, dialled lazily and
// re-dialled after it closes. Each stream gets its own channel and queue:
//   - exclusive + auto-delete, so it vanishes with the channel (core crash,
//     stream end)
//   - x-max-length with drop-head, so a slow viewer can only ever cost the
//     broker logQueueMaxLength messages; the oldest batches are dropped

const logQueueMaxLength = 200 // batches

// LogConsumer opens per-stream log queues.
type LogConsumer struct {
	url    string
	logger *zap.Logger

	mu   sync.Mutex
	conn *amqp.Connection
}

// NewLogConsumer returns a consumer for the broker in cfg. It connects on
// first use.
func NewLogConsumer(cfg Config, logger *zap.Logger) *LogConsumer {
	return &LogConsumer{url: cfg.amqpURL(), logger: logger.With(zap.String("component", "log-consumer"))}
}

func (c *LogConsumer) connection() (*amqp.Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn, nil
	}
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	c.conn = conn
	return conn, nil
}

// Open binds a fresh queue to streamID and returns the message bodies that
// arrive on it. The channel closes when ctx ends or the broker channel goes
// away; the queue is deleted with it.
func (c *LogConsumer) Open(ctx context.Context, streamID string) (<-chan []byte, error) {
	conn, err := c.connection()
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}
	fail := func(what string, err error) (<-chan []byte, error) {
		_ = ch.Close()
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if err := ch.ExchangeDeclare(protocol.LogsExchange, "direct", true, false, false, false, nil); err != nil {
		return fail("declare exchange", err)
	}
	q, err := ch.QueueDeclare("logos.core.logs."+streamID, false, true, true, false, amqp.Table{
		"x-max-length": int32(logQueueMaxLength),
		"x-overflow":   "drop-head",
	})
	if err != nil {
		return fail("declare queue", err)
	}
	if err := ch.QueueBind(q.Name, streamID, protocol.LogsExchange, false, nil); err != nil {
		return fail("bind queue", err)
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		return fail("consume", err)
	}

	out := make(chan []byte, 16)
	go func() {
		defer close(out)
		defer func() { _ = ch.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					return
				}
				select {
				case out <- d.Body:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// Close closes the broker connection; open streams end.
func (c *LogConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.conn.IsClosed() {
		return nil
	}
	return c.conn.Close()
}
