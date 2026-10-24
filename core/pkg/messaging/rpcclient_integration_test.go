package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/logoscore/logos-golang-protocol/protocol"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// TestIntegrationRPCClient runs the core → module RPC client against a real
// broker: a stand-in module consumes its own queue and answers through the
// default exchange to reply_to, which is what direct reply-to needs and what
// the fake transport cannot check. Opt-in through INTEGRATION_AMQP_URL, e.g.
//
//	INTEGRATION_AMQP_URL=amqp://logos:pass@localhost:5672/ go test ./pkg/messaging -run Integration
func TestIntegrationRPCClient(t *testing.T) {
	url := os.Getenv("INTEGRATION_AMQP_URL")
	if url == "" {
		t.Skip("INTEGRATION_AMQP_URL not set")
	}

	// The stand-in module.
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil) // server-named, exclusive
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan amqp.Delivery, 1)
	go func() {
		for d := range deliveries {
			seen <- d
			var req protocol.Envelope
			_ = json.Unmarshal(d.Body, &req)
			reply, _ := protocol.NewReply(req, protocol.Source{Service: "channel", Instance: "it"},
				protocol.ConfigApplyReply{AppliedRevision: 5})
			body, _ := json.Marshal(reply)
			_ = ch.PublishWithContext(context.Background(), "", d.ReplyTo, false, false, amqp.Publishing{
				CorrelationId: d.CorrelationId, ContentType: "application/json", Body: body,
			})
		}
	}()

	c := newRPCClient(nil, zap.NewNop())
	c.transport = &amqpRPCTransport{url: url, client: c, logger: c.logger}
	defer func() { _ = c.Close() }()

	reply, err := c.Call(context.Background(), q.Name, protocol.TypeMgmtConfigApply,
		protocol.ConfigApplyRequest{ManagementConfig: protocol.ManagementConfig{Revision: 5, Values: map[string]any{}}}, 5*time.Second)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var res protocol.ConfigApplyReply
	if err := json.Unmarshal(reply.Payload, &res); err != nil || res.AppliedRevision != 5 {
		t.Fatalf("reply = %+v (%v)", reply, err)
	}
	d := <-seen
	if d.Type != protocol.TypeMgmtConfigApply || d.Expiration != "5000" || d.CorrelationId != d.MessageId {
		t.Errorf("delivery props: type %q expiration %q corr %q msg %q", d.Type, d.Expiration, d.CorrelationId, d.MessageId)
	}

	// No such queue: returned by the broker as unroutable, not a timeout.
	start := time.Now()
	_, err = c.Call(context.Background(), "logos.it.no-such-queue", "x", nil, 5*time.Second)
	if !errors.Is(err, ErrRPCUnroutable) {
		t.Errorf("err = %v, want ErrRPCUnroutable", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("unroutable call waited for the timeout")
	}
}
