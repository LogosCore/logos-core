// Package channellink keeps the channels' control links (docs:
// contracts/channel-core-connections/, The control link): one WebSocket per
// channel instance, which the instance opens, over which core asks it to
// reach bind minions and it answers each request by its id. Core runs as
// one instance (docs: architecture, One core instance), so the links are
// this process's; callers go through Call, never a link directly.
package channellink

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

const (
	pingEvery = 30 * time.Second
	// silentFor drops a link nothing came over, not even a pong.
	silentFor = 90 * time.Second
	writeWait = 10 * time.Second
	// answerSlack is how much longer than a call's deadline core waits for
	// its answer: the channel's own way there and back.
	answerSlack = 5 * time.Second
)

// ErrNoLink means no instance of the channel has its control link open.
var ErrNoLink = errors.New("no instance of the channel has its control link open")

// ErrLinkDropped means the link a call went over dropped before its answer
// came: whether the channel made the call is not known.
var ErrLinkDropped = errors.New("the control link dropped before the answer came")

// Hub holds the open links.
type Hub struct {
	log  *zap.Logger
	next atomic.Uint64

	mu    sync.Mutex
	links map[string]*link
	// onUp is told the channel module whose instance opened a link.
	onUp func(module string)
}

// New is an empty hub.
func New(log *zap.Logger) *Hub {
	return &Hub{log: log.Named("channellink"), links: map[string]*link{}}
}

// OnUp registers f to be told when an instance of a channel opens its link
// (calls waiting for a channel can go then).
func (h *Hub) OnUp(f func(module string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUp = f
}

type link struct {
	instance, module string
	ws               *websocket.Conn
	writeMu          sync.Mutex

	mu      sync.Mutex
	waiting map[uint64]chan protocol.LinkFrame
	closed  bool
}

// Call is one request to a channel: deliver Data, sent under ID, to the
// target Values say. Keys are ID's, for a channel that acts as the minion
// (an agentless kind); nil for any other.
type Call struct {
	Values map[string]any
	// Credentials are the credentials Values name (credential fields), by
	// id: the channel gets them beside the values.
	Credentials map[string]protocol.Credential
	ID          [16]byte
	Data        []byte
	Deadline    time.Duration
	Keys        *protocol.LinkKeys
}

// Up reports whether an instance of module has its link open.
func (h *Hub) Up(module string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.links {
		if l.module == module {
			return true
		}
	}
	return false
}

// Instances are the instances with a link open, by module, for status.
func (h *Hub) Instances() map[string][]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string][]string{}
	for _, l := range h.links {
		out[l.module] = append(out[l.module], l.instance)
	}
	return out
}

// Call sends c to an instance of module, the one with the fewest calls
// waiting, and waits for its answer: until the deadline and a little more,
// or ctx. ErrNoLink when no instance has its link open; ErrLinkDropped when
// the link dropped meanwhile.
func (h *Hub) Call(ctx context.Context, module string, c Call) (protocol.LinkFrame, error) {
	l := h.pick(module)
	if l == nil {
		return protocol.LinkFrame{}, ErrNoLink
	}
	req := h.next.Add(1)
	f := protocol.LinkFrame{Type: protocol.LinkCall, Request: req, Values: c.Values, ID: c.ID[:], Data: c.Data,
		Deadline: uint64(c.Deadline / time.Millisecond), Keys: c.Keys, Credentials: c.Credentials}
	b, err := protocol.EncodeLinkFrame(f)
	if err != nil {
		return protocol.LinkFrame{}, err
	}
	ch := make(chan protocol.LinkFrame, 1)
	if !l.wait(req, ch) {
		return protocol.LinkFrame{}, ErrLinkDropped
	}
	defer l.forget(req)
	if err := l.write(websocket.BinaryMessage, b); err != nil {
		_ = l.ws.Close()
		return protocol.LinkFrame{}, fmt.Errorf("%w: %v", ErrLinkDropped, err)
	}
	wait := c.Deadline + answerSlack
	if c.Deadline <= 0 {
		wait = 2 * time.Minute
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a, ok := <-ch:
		if !ok {
			return protocol.LinkFrame{}, ErrLinkDropped
		}
		return a, nil
	case <-timer.C:
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: req,
			Failure: &protocol.LinkFailure{Kind: protocol.LinkFailTimeout, Msg: "the channel did not answer in time"}}, nil
	case <-ctx.Done():
		return protocol.LinkFrame{}, ctx.Err()
	}
}

// Open asks an instance of module to open a bind-persistent connection to
// the target values say (creds the credentials they name, by id), the
// minion to speak under under on it (keys are under's, for a channel that
// acts as the minion; nil for any other), and
// waits for its answer: as Call, deadline 30 seconds when it says none.
// sent is told the instance and the request before the open goes out: the
// connection's socket names them, and may come before the answer.
func (h *Hub) Open(ctx context.Context, module string, values map[string]any, creds map[string]protocol.Credential, under [16]byte,
	keys *protocol.LinkKeys, deadline time.Duration, sent func(instance string, request uint64)) (protocol.LinkFrame, error) {
	l := h.pick(module)
	if l == nil {
		return protocol.LinkFrame{}, ErrNoLink
	}
	if deadline <= 0 {
		deadline = 30 * time.Second
	}
	req := h.next.Add(1)
	f := protocol.LinkFrame{Type: protocol.LinkOpen, Request: req, Values: values, Credentials: creds,
		Deadline: uint64(deadline / time.Millisecond)}
	if under != [16]byte{} {
		f.Under, f.Keys = under[:], keys
	}
	b, err := protocol.EncodeLinkFrame(f)
	if err != nil {
		return protocol.LinkFrame{}, err
	}
	return h.ask(ctx, l, req, b, deadline, func() {
		if sent != nil {
			sent(l.instance, req)
		}
	})
}

// ask writes b, request req, on l and waits for its answer (the deadline
// and answerSlack), first telling before.
func (h *Hub) ask(ctx context.Context, l *link, req uint64, b []byte, deadline time.Duration, before func()) (protocol.LinkFrame, error) {
	ch := make(chan protocol.LinkFrame, 1)
	if !l.wait(req, ch) {
		return protocol.LinkFrame{}, ErrLinkDropped
	}
	defer l.forget(req)
	before()
	if err := l.write(websocket.BinaryMessage, b); err != nil {
		_ = l.ws.Close()
		return protocol.LinkFrame{}, fmt.Errorf("%w: %v", ErrLinkDropped, err)
	}
	timer := time.NewTimer(deadline + answerSlack)
	defer timer.Stop()
	select {
	case a, ok := <-ch:
		if !ok {
			return protocol.LinkFrame{}, ErrLinkDropped
		}
		return a, nil
	case <-timer.C:
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: req,
			Failure: &protocol.LinkFailure{Kind: protocol.LinkFailTimeout, Msg: "the channel did not answer in time"}}, nil
	case <-ctx.Done():
		return protocol.LinkFrame{}, ctx.Err()
	}
}

// pick is the link of module with the fewest calls waiting.
func (h *Hub) pick(module string) *link {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *link
	bestLoad := 0
	for _, l := range h.links {
		if l.module != module {
			continue
		}
		load := l.load()
		if best == nil || load < bestLoad {
			best, bestLoad = l, load
		}
	}
	return best
}

func (l *link) load() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.waiting)
}

func (l *link) wait(req uint64, ch chan protocol.LinkFrame) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.waiting[req] = ch
	return true
}

func (l *link) forget(req uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.waiting, req)
}

// answered hands an answer to the call waiting for it; one nothing waits
// for (it came too late) is dropped.
func (l *link) answered(f protocol.LinkFrame) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch, ok := l.waiting[f.Request]
	if ok {
		delete(l.waiting, f.Request)
		ch <- f
	}
	return ok
}

// close fails the calls waiting.
func (l *link) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	for req, ch := range l.waiting {
		close(ch)
		delete(l.waiting, req)
	}
}

func (l *link) write(mt int, b []byte) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if err := l.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return l.ws.WriteMessage(mt, b)
}

// Serve holds ws as instance's link until it closes. A newer link of the
// same instance replaces an older one, which is closed.
func (h *Hub) Serve(ws *websocket.Conn, instance, module string) {
	l := &link{instance: instance, module: module, ws: ws, waiting: map[uint64]chan protocol.LinkFrame{}}
	log := h.log.With(zap.String("instance", instance), zap.String("module", module))
	h.mu.Lock()
	old := h.links[instance]
	h.links[instance] = l
	onUp := h.onUp
	h.mu.Unlock()
	if old != nil {
		log.Info("control link replaced by a newer one")
		_ = old.ws.Close()
	}
	log.Info("control link opened")
	defer func() {
		h.mu.Lock()
		if h.links[instance] == l {
			delete(h.links, instance)
		}
		h.mu.Unlock()
		l.close()
		_ = ws.Close()
		log.Info("control link closed")
	}()
	if onUp != nil {
		go onUp(module)
	}

	ws.SetReadLimit(protocol.MaxLinkFrameSize + 64)
	alive := func() error { return ws.SetReadDeadline(time.Now().Add(silentFor)) }
	_ = alive()
	ws.SetPongHandler(func(string) error { return alive() })
	ws.SetPingHandler(func(data string) error {
		_ = alive()
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(writeWait))
	})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				l.writeMu.Lock()
				err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
				l.writeMu.Unlock()
				if err != nil {
					_ = ws.Close()
					return
				}
			}
		}
	}()

	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = alive()
		if mt != websocket.BinaryMessage {
			log.Info("control link: a text frame, closing")
			return
		}
		f, err := protocol.DecodeLinkFrame(data)
		if err != nil || f.Type != protocol.LinkAnswer {
			log.Info("control link: a frame that is not an answer, closing", zap.Error(err), zap.Int("type", f.Type))
			return
		}
		if !l.answered(f) {
			log.Debug("control link: an answer nothing waits for", zap.Uint64("request", f.Request))
		}
	}
}
