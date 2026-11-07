// Package miniontunnels carries minions' tunnels (docs:
// contracts/minion-messages/tunnels.md): what core puts in front of one of
// its operational ports for a tunnel task (a SOCKS proxy, or a port forward
// to one destination), whose clients' connections become streams the
// minion opens from its host; and a reverse port forward's streams, which
// the minion opens for each connection to its listener and core connects
// to the task's destination. Their bytes go in the session's messages
// (items 21 to 27) with offsets and a window each way, a UDP stream's as
// datagrams.
//
// The service holds the listeners and the streams; minionsync puts what it
// has for a session in the session's messages (Fill) and hands it the
// minion's tunnel items (Take), and the task resolver opens the port (Open),
// or keeps a reverse forward's listener (OpenReverse), when it queues a
// tunnel task. One core instance holds them all: the ports, and what goes
// on them, live in its process.
package miniontunnels

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Config is how the service runs.
type Config struct {
	// Ports are the operational ports, sorted; none: no tunnel opens.
	Ports []int
	// Bind is the address the listeners bind; empty: every interface.
	Bind string
	// Address is the host operators reach the ports at, as shown; empty:
	// the host the web app is reached at.
	Address string
	// HandshakeWait bounds a SOCKS client's greeting and request.
	HandshakeWait time.Duration
	// ConnectSlack is how long past the command's timeout a SOCKS client
	// waits for its stream to open: the minion's way back.
	ConnectSlack time.Duration
	// ResendAfter: what was sent and not acked in that long is sent again,
	// from the ack.
	ResendAfter time.Duration
	// UDPIdle ends a UDP reverse port forward's stream with no datagram
	// either way for that long: core's backstop, past the minion's own
	// (protocol.TunnelUDPIdle).
	UDPIdle time.Duration
	// Sweep is how often the tunnels' tasks are read again, should an
	// event about one have been dropped.
	Sweep time.Duration
	// IdlePoll is how soon a minion that polls, with a stream open and
	// nothing under way, comes back; TaskPoll, with a tunnel task and no
	// stream.
	IdlePoll time.Duration
	TaskPoll time.Duration
	// Tell is how often a tunnel's change is said, at most, while its
	// streams come, go and carry bytes: its task's view and the Ports page
	// follow.
	Tell time.Duration
}

// DefaultConfig is the service's usual pace; the ports are the
// deployment's.
func DefaultConfig() Config {
	return Config{HandshakeWait: 10 * time.Second, ConnectSlack: 30 * time.Second, ResendAfter: 5 * time.Second,
		UDPIdle: protocol.TunnelUDPIdle + time.Minute,
		Sweep:   15 * time.Second, IdlePoll: 250 * time.Millisecond, TaskPoll: time.Second, Tell: time.Second}
}

// Tasks is what the service reads and keeps of tasks (the minion task
// repository).
type Tasks interface {
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
	// RequestCancelWhy asks the task to stop, as Stop does, keeping why.
	RequestCancelWhy(ctx context.Context, taskID uuid.UUID, at time.Time, why string) (*models.MinionTask, error)
	// SetTunnel keeps what the task's tunnel came to, as it closes;
	// TunnelOf reads it back (nil before).
	SetTunnel(ctx context.Context, taskID uuid.UUID, totals models.MinionTaskTunnel) error
	TunnelOf(ctx context.Context, taskID uuid.UUID) (*models.MinionTaskTunnel, error)
}

// Waker is told a session has tunnel items to go (minionsync's
// WakeTunnels): it pushes, calls or waits for the minion's next message.
type Waker interface {
	WakeTunnels(session uuid.UUID)
}

// ErrNoPort: no operational port is free, or the one asked for is not one
// or is in use.
var ErrNoPort = errors.New("no operational port")

// Service holds the tunnels.
type Service struct {
	cfg   Config
	ports repository.IOperationalPortRepository
	// listeners keeps reverse port forwards' (WithListeners): none, and
	// core opens none.
	listeners repository.IMinionListenerRepository
	tasks     Tasks
	bus       eventbus.IEventBus
	waker     Waker
	logger    *zap.Logger
	now       func() time.Time

	mu sync.Mutex
	// tunnels by task.
	tunnels map[uuid.UUID]*tunnel
	// rotation: where each session's next message starts sending data,
	// so every stream has its turn; rtt, each session's round trip, and
	// its wake for what goes again (rtt.go).
	rotation map[uuid.UUID]int
	rtt      map[uuid.UUID]*rttState

	unsubscribe func()
	stop        chan struct{}
	done        sync.WaitGroup
}

// New is the service; Start opens the ports of tunnels still running.
func New(cfg Config, ports repository.IOperationalPortRepository, tasks Tasks, bus eventbus.IEventBus, logger *zap.Logger) *Service {
	return &Service{cfg: cfg, ports: ports, tasks: tasks, bus: bus, logger: logger.Named("miniontunnels"),
		now: func() time.Time { return time.Now().UTC() }, tunnels: map[uuid.UUID]*tunnel{}, rotation: map[uuid.UUID]int{},
		rtt: map[uuid.UUID]*rttState{}, stop: make(chan struct{})}
}

// WithWaker gives the service the waker: without one, a session's items
// wait for its minion's next message.
func (s *Service) WithWaker(w Waker) *Service {
	s.waker = w
	return s
}

// Address is the host operators reach the ports at ("" for the web app's).
func (s *Service) Address() string { return s.cfg.Address }

// Opening is a tunnel task about to be queued.
type Opening struct {
	Task, Session, Operation, By uuid.UUID
	// Port is the one asked for; 0, any free one.
	Port int
	// MaxStreams is how many streams may be open at once.
	MaxStreams int
	// Timeout is the command's connect timeout, which the minion keeps: a
	// SOCKS client waits that long and the minion's way back.
	Timeout time.Duration
	// SessionNumber and ByName: what the Ports list shows of the session
	// and of who opened it.
	SessionNumber int
	ByName        string
	// Purpose is what core puts in front of the port, the task's front: a
	// SOCKS proxy (models.OperationalPurposeSOCKS, the default), an HTTP
	// proxy (OperationalPurposeHTTP) or a port forward to Host:HostPort
	// (OperationalPurposeForward). A reverse tunnel (OpenReverse) has no
	// port: the minion listens at Bind:Listen, and core does with its
	// streams what Front says (protocol.FrontForward: connect each to
	// Host:HostPort; protocol.FrontSOCKS: a SOCKS5 proxy behind a password
	// core makes).
	Purpose  string
	Front    string
	Host     string
	HostPort int
	Bind     string
	Listen   int
	// UDP: a SOCKS proxy takes UDP ASSOCIATE (its command opens udp); a
	// reverse tunnel's minion listens for datagrams.
	UDP bool
	// Cap is the tunnel's bandwidth cap, bytes a second each way; 0 for
	// none.
	Cap int64
	// StopIdle stops the task once no stream has been open that long (its
	// stop_idle); 0, never.
	StopIdle time.Duration
}

// Open takes an operational port for a tunnel task about to be queued, and
// listens on it: its SOCKS clients wait for the minion to have the task.
// ErrNoPort when none is free (or the one asked for is not free, or not
// one).
func (s *Service) Open(ctx context.Context, o Opening) (int, error) {
	candidates := s.cfg.Ports
	if o.Port != 0 {
		if !slices.Contains(s.cfg.Ports, o.Port) {
			return 0, fmt.Errorf("%w: %d is not one of core's operational ports", ErrNoPort, o.Port)
		}
		candidates = []int{o.Port}
	}
	if len(candidates) == 0 {
		return 0, fmt.Errorf("%w: core has none (OPERATIONAL_PORTS)", ErrNoPort)
	}
	for _, port := range candidates {
		if s.inUse(port) {
			continue
		}
		purpose := o.Purpose
		if purpose == "" {
			purpose = models.OperationalPurposeSOCKS
		}
		rec := &models.OperationalPort{Port: port, Purpose: purpose, Host: o.Host, HostPort: o.HostPort, OperationID: o.Operation, SessionID: o.Session,
			TaskID: o.Task, OpenedBy: o.By, OpenedAt: s.now(), MaxStreams: o.MaxStreams, Timeout: o.Timeout, StopIdle: o.StopIdle,
			SessionNumber: o.SessionNumber, OpenedByName: o.ByName, UDP: o.UDP && purpose == models.OperationalPurposeSOCKS, Cap: o.Cap}
		err := s.ports.Insert(ctx, rec)
		if errors.Is(err, repository.ErrPortTaken) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("take an operational port: %w", err)
		}
		if _, err := s.listen(rec, false); err != nil {
			s.logger.Warn("tunnels: an operational port does not open", zap.Int("port", port), zap.Error(err))
			if rErr := s.ports.Release(ctx, o.Task); rErr != nil {
				return 0, fmt.Errorf("give back an operational port: %w", rErr)
			}
			continue
		}
		s.portChanged(o.Task, o.Operation, true, false)
		return port, nil
	}
	if o.Port != 0 {
		return 0, fmt.Errorf("%w: %d is in use", ErrNoPort, o.Port)
	}
	return 0, fmt.Errorf("%w: all %d are in use", ErrNoPort, len(s.cfg.Ports))
}

// inUse: a tunnel of this core listens on the port.
func (s *Service) inUse(port int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tunnels {
		if t.port == port {
			return true
		}
	}
	return false
}

// listen opens the record's port for its task. acked: the minion has the
// task already (a restart's).
func (s *Service) listen(rec *models.OperationalPort, acked bool) (*tunnel, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(s.cfg.Bind, strconv.Itoa(rec.Port)))
	if err != nil {
		return nil, err
	}
	t := newTunnel(rec, ln, acked)
	s.mu.Lock()
	s.tunnels[rec.TaskID] = t
	s.mu.Unlock()
	s.done.Add(1)
	go s.accept(t)
	if t.purpose == models.OperationalPurposeSOCKS && t.udpRelay {
		// UDP ASSOCIATE's relay: SOCKS's alone, through a command that
		// opens udp.
		s.openRelay(t)
	}
	s.logger.Info("tunnels: port open", zap.Int("port", rec.Port), zap.String("front", rec.Purpose), zap.String("task_id", rec.TaskID.String()),
		zap.String("session_id", rec.SessionID.String()))
	return t, nil
}

// Close ends the task's tunnel: its port closes, its clients' connections
// with it, and the port goes back to the range. What it came to stays with
// its task (SetTunnel); its streams go. A task with none: nothing.
func (s *Service) Close(ctx context.Context, task uuid.UUID) {
	s.mu.Lock()
	t := s.tunnels[task]
	if t != nil && t.closing {
		// Another Close has it.
		s.mu.Unlock()
		return
	}
	var waiting []net.Conn
	var totals models.MinionTaskTunnel
	if t != nil {
		t.closing = true
		for _, st := range t.streams {
			s.drop(st, models.TunnelEndTaskEnded, false)
		}
		waiting = t.everyClient()
		totals = models.MinionTaskTunnel{Streams: t.opened, Up: t.up, Down: t.down}
	}
	s.mu.Unlock()
	if t != nil {
		// Its totals are kept before it goes: its task never shows it
		// without them.
		if err := s.tasks.SetTunnel(ctx, task, totals); err != nil {
			s.logger.Warn("tunnels: keep a tunnel's totals", zap.String("task_id", task.String()), zap.Error(err))
		}
		s.mu.Lock()
		delete(s.tunnels, task)
		if len(s.ofSession(t.session)) == 0 {
			delete(s.rotation, t.session)
			if r := s.rtt[t.session]; r != nil && r.wake != nil {
				r.wake.Stop()
			}
			delete(s.rtt, t.session)
		}
		s.mu.Unlock()
		if t.ln != nil {
			_ = t.ln.Close()
		}
		s.closeRelay(t)
		for _, c := range waiting {
			_ = c.Close()
		}
		if t.reverse {
			s.logger.Info("tunnels: reverse port forward ended", zap.String("task_id", task.String()))
		} else {
			s.logger.Info("tunnels: port closed", zap.Int("port", t.port), zap.String("task_id", task.String()))
		}
	}
	if t == nil || !t.reverse {
		if err := s.ports.Release(ctx, task); err != nil {
			s.logger.Warn("tunnels: give back a task's port", zap.String("task_id", task.String()), zap.Error(err))
		}
	}
	if s.listeners != nil && (t == nil || t.reverse) {
		if err := s.listeners.Release(ctx, task); err != nil {
			s.logger.Warn("tunnels: forget a task's reverse port forward", zap.String("task_id", task.String()), zap.Error(err))
		}
	}
	switch {
	case t != nil && t.reverse:
		s.listenerChanged(task, t.operation, false, true)
	case t != nil:
		s.portChanged(task, t.operation, false, true)
	}
}

// closeRelay closes the tunnel's UDP relay, if it has one.
func (s *Service) closeRelay(t *tunnel) {
	s.mu.Lock()
	pc := t.relay
	t.relay = nil
	s.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}

// portChanged says the task's port changed (the Ports page's): it opened,
// its streams' numbers changed, or it closed.
func (s *Service) portChanged(task, operation uuid.UUID, opened, closed bool) {
	if s.bus != nil {
		s.bus.Publish(eventbus.NewOperationalPortChangedEvent(eventbus.OperationalPortEventPayload{TaskID: task.String(),
			OperationID: operation.String(), Opened: opened, Closed: closed}))
	}
}

// Start opens again the ports of tunnels whose tasks still run, ends those
// whose port cannot open, and watches the tasks.
func (s *Service) Start(ctx context.Context) {
	recs, err := s.ports.All(ctx)
	if err != nil {
		s.logger.Error("tunnels: read the ports in use", zap.Error(err))
	}
	for i := range recs {
		s.reopen(ctx, &recs[i])
	}
	if s.listeners != nil {
		listeners, err := s.listeners.All(ctx)
		if err != nil {
			s.logger.Error("tunnels: read the reverse port forwards", zap.Error(err))
		}
		for i := range listeners {
			s.reopenReverse(ctx, &listeners[i])
		}
	}
	if s.bus != nil {
		s.unsubscribe = s.bus.Subscribe([]eventbus.Topic{eventbus.TopicMinionTaskChanged}, s.onTask)
	}
	s.done.Add(1)
	go s.sweep()
	if s.bus != nil {
		s.done.Add(1)
		go s.tell()
	}
}

// reopen opens a restart's port again for its task, still running; one
// that ended gives its port back, and one whose port does not open ends.
func (s *Service) reopen(ctx context.Context, rec *models.OperationalPort) {
	t, err := s.tasks.FindByTaskID(ctx, rec.TaskID)
	switch {
	case repository.IsNotFound(err) || (err == nil && (t.Finished() || t.CancelRequestedAt != nil)):
		if err == nil {
			// It ended while core was down: what it came to, as its port
			// last kept it.
			s.keepTotals(ctx, rec.TaskID, models.MinionTaskTunnel{Streams: rec.Opened, Up: rec.Up, Down: rec.Down})
		}
		if err := s.ports.Release(ctx, rec.TaskID); err != nil {
			s.logger.Warn("tunnels: give back an ended task's port", zap.Error(err))
		}
		return
	case err != nil:
		s.logger.Error("tunnels: read a tunnel's task", zap.String("task_id", rec.TaskID.String()), zap.Error(err))
		return
	}
	// Its numbers so far come with its port (newTunnel).
	if _, err := s.listen(rec, t.AckedAt != nil); err != nil {
		s.logger.Warn("tunnels: a running tunnel's port does not open again, its task ends", zap.Int("port", rec.Port),
			zap.String("task_id", rec.TaskID.String()), zap.Error(err))
		s.cancel(ctx, rec.TaskID, fmt.Sprintf("stopped by core: its port %d did not open again after core restarted", rec.Port))
		s.keepTotals(ctx, rec.TaskID, models.MinionTaskTunnel{Streams: rec.Opened, Up: rec.Up, Down: rec.Down})
		_ = s.ports.Release(ctx, rec.TaskID)
		s.portChanged(rec.TaskID, rec.OperationID, false, true)
		return
	}
	s.portChanged(rec.TaskID, rec.OperationID, false, false)
}

// keepTotals keeps what a task's tunnel came to with the task.
func (s *Service) keepTotals(ctx context.Context, task uuid.UUID, totals models.MinionTaskTunnel) {
	if err := s.tasks.SetTunnel(ctx, task, totals); err != nil {
		s.logger.Warn("tunnels: keep a tunnel's totals", zap.String("task_id", task.String()), zap.Error(err))
	}
}

// cancel asks the task to end, as Stop does, saying why.
func (s *Service) cancel(ctx context.Context, task uuid.UUID, why string) {
	t, err := s.tasks.RequestCancelWhy(ctx, task, s.now(), why)
	if err != nil {
		s.logger.Warn("tunnels: cancel a task", zap.String("task_id", task.String()), zap.Error(err))
		return
	}
	if s.bus != nil {
		s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{TaskID: t.TaskID.String(),
			SessionID: t.SessionID.String(), OperationID: t.OperationID.String()}))
	}
}

// Stop closes every port and connection: the tunnels' ports and listeners
// stay kept, their bytes so far with them, so a restart opens them again
// and counts on. Their streams go.
func (s *Service) Stop() {
	select {
	case <-s.stop:
		return
	default:
	}
	if s.unsubscribe != nil {
		s.unsubscribe()
	}
	s.mu.Lock()
	var all []*tunnel
	var waiting []net.Conn
	for id, t := range s.tunnels {
		all = append(all, t)
		t.closing = true
		for _, st := range t.streams {
			s.drop(st, models.TunnelEndCoreStopped, false)
		}
		waiting = append(waiting, t.everyClient()...)
		delete(s.tunnels, id)
	}
	s.mu.Unlock()
	for _, t := range all {
		if t.ln != nil {
			_ = t.ln.Close()
		}
		s.closeRelay(t)
	}
	for _, c := range waiting {
		_ = c.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s.keepBytes(ctx, all)
	cancel()
	close(s.stop)
	s.done.Wait()
}

// onTask hears a task changed: a tunnel's that the minion has, or that
// ended or is to.
func (s *Service) onTask(ctx context.Context, e eventbus.Event) {
	p, ok := e.Payload.(eventbus.MinionTaskEventPayload)
	if !ok {
		return
	}
	id, err := uuid.Parse(p.TaskID)
	if err != nil {
		return
	}
	s.mu.Lock()
	_, ours := s.tunnels[id]
	s.mu.Unlock()
	if ours {
		s.check(ctx, id)
	}
}

// check reads a tunnel's task again: once the minion has it, its streams
// open; once it ends, or is to (Stop), the tunnel closes.
func (s *Service) check(ctx context.Context, id uuid.UUID) {
	t, err := s.tasks.FindByTaskID(ctx, id)
	switch {
	case repository.IsNotFound(err):
		s.Close(ctx, id)
		return
	case err != nil:
		s.logger.Warn("tunnels: read a tunnel's task", zap.String("task_id", id.String()), zap.Error(err))
		return
	case t.Finished() || t.CancelRequestedAt != nil:
		s.Close(ctx, id)
		return
	}
	if t.AckedAt != nil && s.acked(id) {
		s.wake(t.SessionID)
	}
	if t.AckedAt != nil {
		s.idleCheck(ctx, id)
	}
}

// acked marks the tunnel's task as the minion's: true when that is news.
func (s *Service) acked(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnels[id]
	if t == nil || t.acked {
		return false
	}
	t.acked = true
	return true
}

// sweep reads the tunnels' tasks again now and then: an event about one
// may have been dropped.
func (s *Service) sweep() {
	defer s.done.Done()
	tick := time.NewTicker(s.cfg.Sweep)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
		}
		s.mu.Lock()
		ids := make([]uuid.UUID, 0, len(s.tunnels))
		for id := range s.tunnels {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		for _, id := range ids {
			s.check(context.Background(), id)
		}
		s.endIdleUDP()
		s.keepBytes(context.Background(), nil)
	}
}

// tell publishes the change of every tunnel whose view changed, every
// cfg.Tell: its task's (its tunnel's numbers, its streams), and its port's
// when its streams' numbers changed.
func (s *Service) tell() {
	defer s.done.Done()
	tick := time.NewTicker(s.cfg.Tell)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
		}
		s.mu.Lock()
		var changed []eventbus.MinionTaskEventPayload
		var counted []*tunnel
		for _, t := range s.tunnels {
			if t.changed {
				t.changed = false
				changed = append(changed, eventbus.MinionTaskEventPayload{TaskID: t.task.String(), SessionID: t.session.String(),
					OperationID: t.operation.String()})
			}
			if t.counted {
				t.counted = false
				counted = append(counted, t)
			}
		}
		s.mu.Unlock()
		for _, p := range changed {
			s.bus.Publish(eventbus.NewMinionTaskChangedEvent(p))
		}
		for _, t := range counted {
			if t.reverse {
				s.listenerChanged(t.task, t.operation, false, false)
			} else {
				s.portChanged(t.task, t.operation, false, false)
			}
		}
	}
}

// Summary is a tunnel's as its task shows it.
type Summary struct {
	Port int
	// Open: its streams now, opening or open; Streams: how many it has
	// had.
	Open    int
	Streams uint64
	// Cap is its bandwidth cap now, bytes a second each way (0: none).
	Cap int64
	// User and Password are what a reverse SOCKS proxy's clients give.
	User, Password string
	// Up and Down: the bytes its tools sent through it, and got back.
	Up, Down int64
	// Bound is where a reverse port forward's minion says it listens, ""
	// until it says.
	Bound string
	// StopsAt is when core stops the task, unless a stream opens first:
	// zero while a stream is open, before the minion has the task, or with
	// no stop_idle.
	StopsAt time.Time
}

// Summary is the task's tunnel while core listens for it; false when it
// does not (the task ended, or never had one here).
func (s *Service) Summary(task uuid.UUID) (Summary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnels[task]
	if t == nil {
		return Summary{}, false
	}
	return Summary{Port: t.port, Open: len(t.streams), Streams: t.opened, Up: t.up, Down: t.down, Bound: t.bound, StopsAt: t.stopsAt(),
		Cap: t.cap, User: t.user, Password: t.password}, true
}

// View is the task's tunnel as its task shows it: live while core listens
// for it (true); otherwise what it came to, as its task keeps it (nothing
// before it closed).
func (s *Service) View(ctx context.Context, task uuid.UUID) (Summary, bool, error) {
	if sum, ok := s.Summary(task); ok {
		return sum, true, nil
	}
	tot, err := s.tasks.TunnelOf(ctx, task)
	if err != nil && !repository.IsNotFound(err) {
		return Summary{}, false, err
	}
	if tot == nil {
		return Summary{}, false, nil
	}
	return Summary{Streams: tot.Streams, Up: tot.Up, Down: tot.Down}, false, nil
}

// Port is the task's port in use; IsNotFound for none.
func (s *Service) Port(ctx context.Context, task uuid.UUID) (*models.OperationalPort, error) {
	return s.ports.FindByTaskID(ctx, task)
}

// Ports is a page of the operation's ports in use, the latest opened
// first; CountPorts counts them.
func (s *Service) Ports(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.OperationalPort, error) {
	return s.ports.FindByOperationIDWithCursor(ctx, operationID, search, cursor, limit, forward)
}

func (s *Service) CountPorts(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	return s.ports.CountByOperationID(ctx, operationID, search)
}

// wake tells the waker the session has tunnel items, without waiting.
func (s *Service) wake(session uuid.UUID) {
	if s.waker != nil {
		go s.waker.WakeTunnels(session)
	}
}
