// Package factorylink keeps the factory links (docs:
// contracts/minion-messages/codecs/, The factory link): one WebSocket per
// instance of a factory that translates its minions' codec, which the
// instance opens, over which core asks it to read a minion's message, write
// one, or make its suite's keys, and it answers each request by its id.
// Core runs as one instance, so the links are this process's; callers go
// through Call.
package factorylink

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

const (
	pingEvery = 30 * time.Second
	// silentFor drops a link nothing came over, not even a pong.
	silentFor = 90 * time.Second
	writeWait = 10 * time.Second
)

var (
	// ErrNoLink means no instance of the factory has its link open.
	ErrNoLink = errors.New("no instance of the factory has its link open")
	// ErrLinkDropped means the link a call went over dropped before its
	// answer came.
	ErrLinkDropped = errors.New("the factory link dropped before the answer came")
	// ErrNoAnswer means the factory did not answer within the call's
	// timeout.
	ErrNoAnswer = errors.New("the factory did not answer in time")
)

// Down reports whether err says the factory did not answer: no link, the
// link dropped, or no answer in time. Core treats each as the factory not
// answering (a refusal inbound, a wait outbound).
func Down(err error) bool {
	return errors.Is(err, ErrNoLink) || errors.Is(err, ErrLinkDropped) || errors.Is(err, ErrNoAnswer)
}

// Failed is an answer that is a failure: the factory answered, and could
// not do what it was asked.
type Failed struct{ protocol.CodecFailure }

func (f *Failed) Error() string {
	if f.Msg == "" {
		return "the factory: " + protocol.CodecFailName(f.Kind)
	}
	return fmt.Sprintf("the factory: %s: %s", protocol.CodecFailName(f.Kind), f.Msg)
}

// Hub holds the open links.
type Hub struct {
	log  *zap.Logger
	next atomic.Uint64

	mu    sync.Mutex
	links map[string]*link
	onUp  func(module string)
}

// New is an empty hub.
func New(log *zap.Logger) *Hub {
	return &Hub{log: log.Named("factorylink"), links: map[string]*link{}}
}

// OnUp registers f to be told when an instance of a factory opens its link
// (what waited for the factory can go then).
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
	waiting map[uint64]chan protocol.FactoryAnswerFrame
	closed  bool
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
// waiting, its request id and deadline (timeout) set here, and waits for
// its answer, checked against c. A failure the factory answered is a
// *Failed; ErrNoLink, ErrLinkDropped and ErrNoAnswer when it did not
// answer (Down).
func (h *Hub) Call(ctx context.Context, module string, c protocol.FactoryCall, timeout time.Duration) (protocol.FactoryAnswerFrame, error) {
	l := h.pick(module)
	if l == nil {
		return protocol.FactoryAnswerFrame{}, ErrNoLink
	}
	if timeout <= 0 {
		timeout = protocol.DefaultCodecTimeout
	}
	c.Request = h.next.Add(1)
	c.Deadline = uint64(max(timeout/time.Millisecond, 1))
	b, err := protocol.EncodeFactoryCall(c)
	if err != nil {
		return protocol.FactoryAnswerFrame{}, err
	}
	ch := make(chan protocol.FactoryAnswerFrame, 1)
	if !l.wait(c.Request, ch) {
		return protocol.FactoryAnswerFrame{}, ErrLinkDropped
	}
	defer l.forget(c.Request)
	if err := l.write(b); err != nil {
		_ = l.ws.Close()
		return protocol.FactoryAnswerFrame{}, fmt.Errorf("%w: %v", ErrLinkDropped, err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case a, ok := <-ch:
		if !ok {
			return protocol.FactoryAnswerFrame{}, ErrLinkDropped
		}
		if err := a.ValidateFor(c); err != nil {
			return protocol.FactoryAnswerFrame{}, err
		}
		if a.Failure != nil {
			return a, &Failed{*a.Failure}
		}
		return a, nil
	case <-timer.C:
		return protocol.FactoryAnswerFrame{}, ErrNoAnswer
	case <-ctx.Done():
		return protocol.FactoryAnswerFrame{}, ctx.Err()
	}
}

// BuildKeys asks module to make its suite's keys for build: the material.
func (h *Hub) BuildKeys(ctx context.Context, module string, build uuid.UUID, timeout time.Duration) ([]byte, error) {
	a, err := h.Call(ctx, module, protocol.FactoryCall{Type: protocol.FactoryKeys, Build: build[:], For: protocol.FactoryKeysForBuild}, timeout)
	if err != nil {
		return nil, err
	}
	return a.Material, nil
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
		if load := l.load(); best == nil || load < bestLoad {
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

func (l *link) wait(req uint64, ch chan protocol.FactoryAnswerFrame) bool {
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
func (l *link) answered(a protocol.FactoryAnswerFrame) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch, ok := l.waiting[a.Request]
	if ok {
		delete(l.waiting, a.Request)
		ch <- a
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

func (l *link) write(b []byte) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if err := l.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return l.ws.WriteMessage(websocket.BinaryMessage, b)
}

// Serve holds ws as instance's link until it closes. A newer link of the
// same instance replaces an older one, which is closed.
func (h *Hub) Serve(ws *websocket.Conn, instance, module string) {
	l := &link{instance: instance, module: module, ws: ws, waiting: map[uint64]chan protocol.FactoryAnswerFrame{}}
	log := h.log.With(zap.String("instance", instance), zap.String("module", module))
	h.mu.Lock()
	old := h.links[instance]
	h.links[instance] = l
	onUp := h.onUp
	h.mu.Unlock()
	if old != nil {
		log.Info("factory link replaced by a newer one")
		_ = old.ws.Close()
	}
	log.Info("factory link opened")
	defer func() {
		h.mu.Lock()
		if h.links[instance] == l {
			delete(h.links, instance)
		}
		h.mu.Unlock()
		l.close()
		_ = ws.Close()
		log.Info("factory link closed")
	}()
	if onUp != nil {
		go onUp(module)
	}

	ws.SetReadLimit(protocol.MaxFactoryFrameSize + 64)
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
			log.Info("factory link: a text frame, closing")
			return
		}
		a, err := protocol.DecodeFactoryAnswer(data)
		if err != nil {
			log.Info("factory link: a frame that is not an answer, closing", zap.Error(err))
			return
		}
		if !l.answered(a) {
			log.Debug("factory link: an answer nothing waits for", zap.Uint64("request", a.Request))
		}
	}
}
