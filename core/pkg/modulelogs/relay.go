// Package modulelogs relays a module's live log tail to a viewer. For each
// viewer it opens a leased stream on the module (mgmt.logs.start, renewed
// while the viewer stays), consumes the batches the module publishes and
// hands them on. Core stores nothing: records exist only in transit.
//
// Every viewer gets a stream of its own, with its own level and backlog. A
// module caps concurrent streams (channel-core: 4, the oldest is replaced),
// which bounds the cost of many viewers.
package modulelogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.uber.org/zap"
)

// Defaults for a stream.
const (
	leaseSeconds   = 30
	renewEvery     = 10 * time.Second
	backlogRecords = 500
	rpcTimeout     = 5 * time.Second
	eventBuffer    = 64
)

// Errors returned by Stream before it starts.
var (
	ErrModuleNotFound = errors.New("module not found")
	ErrNotRegistered  = errors.New("module is not registered")
	ErrNotSupported   = errors.New("module does not support live logs")
)

// Registry finds a module instance.
type Registry interface {
	FindByInstance(ctx context.Context, instance string) (models.Module, error)
}

// Source opens the transport side of a stream (messaging.LogConsumer).
type Source interface {
	Open(ctx context.Context, streamID string) (<-chan []byte, error)
}

// State is a stream's condition as the viewer sees it.
type State string

const (
	StateStreaming    State = "STREAMING"
	StateDisconnected State = "DISCONNECTED" // the module is unreachable; still retrying
	StateEnded        State = "ENDED"        // the module ended the stream; reason says why
)

// Event is one update for the viewer: records and/or a state change.
type Event struct {
	Records []protocol.LogRecord
	Backlog bool
	// Dropped counts records lost before reaching the viewer: discarded by
	// the module (rate limit, slow publish) or by core (slow viewer).
	Dropped int64
	// Lost is set when batches went missing in transit (broker queue
	// overflow), so an unknown number of records is gone.
	Lost   bool
	State  State
	Reason string
}

// Request opens a stream.
type Request struct {
	Instance string
	MinLevel protocol.LogLevel
	Actor    models.ModuleActor
}

// Relay opens streams.
type Relay struct {
	registry Registry
	rpc      messaging.RPCCaller
	source   Source
	logger   *zap.Logger
	newID    func() string
}

// New returns a Relay.
func New(registry Registry, rpc messaging.RPCCaller, source Source, logger *zap.Logger) *Relay {
	return &Relay{
		registry: registry,
		rpc:      rpc,
		source:   source,
		logger:   logger.With(zap.String("component", "module-logs")),
		newID:    protocol.NewULID,
	}
}

// Stream starts a live tail of req.Instance. Events arrive on the returned
// channel until ctx ends; the module's stream is then stopped. The channel is
// never closed (gqlgen stops reading when ctx ends).
func (r *Relay) Stream(ctx context.Context, req Request) (<-chan *Event, error) {
	mod, err := r.registry.FindByInstance(ctx, req.Instance)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, ErrModuleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load module: %w", err)
	}
	if mod.Manifest == nil || !mod.Manifest.Has(protocol.CapabilityLogs) {
		return nil, ErrNotSupported
	}
	if mod.Status != models.ModuleStatusRegistered {
		return nil, ErrNotRegistered
	}
	if req.MinLevel == "" {
		req.MinLevel = protocol.LogInfo
	}

	s := &stream{
		relay: r,
		id:    r.newID(),
		queue: mod.RPCQueue,
		req:   req,
		out:   make(chan *Event, eventBuffer),
	}
	// Bind the queue before asking for records, or the backlog could be
	// published before anything listens.
	streamCtx, cancel := context.WithCancel(ctx)
	batches, err := r.source.Open(streamCtx, s.id)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open log queue: %w", err)
	}
	if err := s.start(ctx, backlogRecords); err != nil {
		cancel()
		return nil, err
	}
	r.logger.Info("module log stream opened",
		zap.String("instance", req.Instance), zap.String("stream_id", s.id),
		zap.String("min_level", string(req.MinLevel)), zap.String("actor_id", req.Actor.ID),
		zap.String("actor", req.Actor.Name))
	s.emit(&Event{State: StateStreaming})
	go s.run(streamCtx, cancel, batches)
	return s.out, nil
}

type stream struct {
	relay *Relay
	id    string
	queue string
	req   Request
	out   chan *Event

	lastSeq int64
	dropped int64 // events core could not hand on, carried into the next one
}

// start sends mgmt.logs.start; with backlog 0 it is a lease renewal.
func (s *stream) start(ctx context.Context, backlog int) error {
	reply, err := s.relay.rpc.Call(ctx, s.queue, protocol.TypeMgmtLogsStart, protocol.LogsStartRequest{
		StreamID:     s.id,
		MinLevel:     s.req.MinLevel,
		LeaseSeconds: leaseSeconds,
		Backlog:      backlog,
		Actor:        &protocol.Actor{Type: s.req.Actor.Type, ID: s.req.Actor.ID, Name: s.req.Actor.Name},
	}, rpcTimeout)
	if err != nil {
		return fmt.Errorf("start log stream: %w", err)
	}
	if reply.Status == protocol.StatusError {
		msg := "module refused the log stream"
		if reply.Error != nil {
			msg += ": " + reply.Error.Message
		}
		return errors.New(msg)
	}
	return nil
}

func (s *stream) run(ctx context.Context, cancel context.CancelFunc, batches <-chan []byte) {
	defer cancel()
	defer s.stop()
	renew := time.NewTicker(renewEvery)
	defer renew.Stop()
	connected := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-renew.C:
			// Renewal also recreates the stream after the module restarted
			// or reconnected (it then has no backlog to resend).
			err := s.start(ctx, 0)
			switch {
			case err != nil && connected:
				connected = false
				s.emit(&Event{State: StateDisconnected, Reason: err.Error()})
			case err == nil && !connected:
				connected = true
				s.emit(&Event{State: StateStreaming})
			}
		case body, ok := <-batches:
			if !ok {
				if ctx.Err() == nil {
					s.emit(&Event{State: StateEnded, Reason: "log queue closed"})
				}
				return
			}
			s.handle(body)
		}
	}
}

func (s *stream) handle(body []byte) {
	var b protocol.LogBatch
	if err := json.Unmarshal(body, &b); err != nil || b.StreamID != s.id {
		s.relay.logger.Warn("dropping malformed log batch", zap.String("stream_id", s.id), zap.Error(err))
		return
	}
	ev := &Event{Records: b.Records, Backlog: b.Backlog, Dropped: b.Dropped}
	// Seq restarts at 1 when the module recreated the stream.
	if b.Seq != 1 && s.lastSeq != 0 && b.Seq != s.lastSeq+1 {
		ev.Lost = true
	}
	s.lastSeq = b.Seq
	if b.End {
		ev.State, ev.Reason = StateEnded, b.EndReason
		// A module that shuts down or loses the slot comes back through the
		// next renewal, which recreates the stream.
	}
	s.emit(ev)
}

// emit hands ev on without blocking the consumer; when the viewer is behind
// the event is dropped and counted into the next one.
func (s *stream) emit(ev *Event) {
	ev.Dropped += s.dropped
	select {
	case s.out <- ev:
		s.dropped = 0
	default:
		s.dropped = ev.Dropped + int64(len(ev.Records))
	}
}

// stop tells the module to end the stream. Best effort: the lease ends it
// anyway.
func (s *stream) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	_, err := s.relay.rpc.Call(ctx, s.queue, protocol.TypeMgmtLogsStop, protocol.LogsStopRequest{StreamID: s.id}, rpcTimeout)
	s.relay.logger.Info("module log stream closed",
		zap.String("instance", s.req.Instance), zap.String("stream_id", s.id), zap.Error(err))
}
