package miniontunnels

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/auth"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// VPN tunnels (docs: contracts/vpn-front): a tunnel task whose front is
// protocol.FrontVPN has no operational port. logos-vpn, core's client on an
// operator's machine, connects over core's own address with a key (one
// operator's, for one task) and opens streams of its own over the link
// (vpnlink.go): a TCP connection its machine made to the tunnel's routes, a
// UDP source's association, its pings. For each, core opens a stream of its
// own toward the minion, as its SOCKS proxy does, and relays between the
// two legs, each with its own offsets, acks and window: the minion never
// learns there is a client. A client whose link dropped is kept
// protocol.VPNLinkGrace, its streams open, for it to connect again.
//
// The client's state is under s.mu, as the streams' are; the pipe between
// a client's stream and core's is read and written outside it.

var (
	// ErrNoVPN: this core keeps no VPN tunnels (WithVPN).
	ErrNoVPN = errors.New("core opens no VPN tunnels")
	// ErrVPNRefused: core does not take the client (protocol.VPNCloseRefused).
	ErrVPNRefused = errors.New("refused")
	// ErrVPNHello: the client's hello is of another link version, or does not
	// read (protocol.VPNCloseVersion).
	ErrVPNHello = errors.New("hello")
)

// VPNRoles says whether a user is (still) an operator of an operation:
// a client's key acts for its operator alone while they are.
type VPNRoles interface {
	IsOperator(ctx context.Context, operation, user uuid.UUID) bool
}

// WithVPN keeps VPN tunnels and their keys; without it core opens none.
func (s *Service) WithVPN(tunnels repository.IVPNTunnelRepository, keys repository.IVPNKeyRepository, roles VPNRoles) *Service {
	s.vpnTunnels, s.vpnKeys, s.roles = tunnels, keys, roles
	return s
}

const (
	// vpnWindow is the room core gives a client's stream: what it holds of
	// the client's bytes for the minion. The default room, so core's replies
	// leave it out.
	vpnWindow = protocol.DefaultTunnelRoom
	// vpnHeld is the most core holds of the minion's bytes for a client's
	// stream, whatever room the client gives.
	vpnHeld = 1 << 20
	// vpnRoleCheck is how often a connected client's operator's role is
	// read again.
	vpnRoleCheck = time.Minute
	// vpnAnswers is how many replies core keeps of a client's streams that
	// did not open, said again should the client ask again.
	vpnAnswers = 1024
	// vpnDatagrams is the most of a client's datagrams core holds for its
	// link; past it they are dropped, as UDP drops what does not fit.
	vpnDatagrams = 256 << 10
	// vpnFrameData is what a frame's data takes at most, below
	// protocol.MaxVPNFrame with the items' headers.
	vpnFrameData = protocol.MaxVPNFrame - 32<<10
)

// vpnTunnel is a VPN tunnel's own: its routes, what its command opens,
// and its clients by instance.
type vpnTunnel struct {
	routes        []netip.Prefix
	text          []string
	udp, icmp     bool
	sessionNumber int
	clients       map[string]*VPNClient
}

// OpenVPN keeps a VPN tunnel for a task about to be queued: its clients
// connect once the minion has the task. o.Routes are its networks (checked
// as protocol.TunnelRoutes does), o.UDP and o.ICMP what its command opens.
func (s *Service) OpenVPN(ctx context.Context, o Opening) error {
	if s.vpnTunnels == nil {
		return ErrNoVPN
	}
	routes, err := protocol.TunnelRoutes(o.Routes)
	if err != nil {
		return err
	}
	rec := &models.VPNTunnel{TaskID: o.Task, OperationID: o.Operation, SessionID: o.Session, OpenedBy: o.By, OpenedAt: s.now(),
		SessionNumber: o.SessionNumber, OpenedByName: o.ByName, Routes: prefixTexts(routes), UDP: o.UDP, ICMP: o.ICMP, Source: o.Source,
		MaxStreams: o.MaxStreams, Timeout: o.Timeout, StopIdle: o.StopIdle, Cap: o.Cap}
	if err := s.vpnTunnels.Insert(ctx, rec); err != nil {
		return fmt.Errorf("keep a VPN tunnel: %w", err)
	}
	s.takeUpVPN(rec, false)
	s.vpnChanged(o.Task, o.Operation, true, false)
	return nil
}

// vpnChanged says a VPN tunnel changed (the Ports page's): it opened, its
// clients or streams' numbers changed, or it closed.
func (s *Service) vpnChanged(task, operation uuid.UUID, opened, closed bool) {
	if s.bus != nil {
		s.bus.Publish(eventbus.NewVPNTunnelChangedEvent(eventbus.VPNTunnelEventPayload{TaskID: task.String(),
			OperationID: operation.String(), Opened: opened, Closed: closed}))
	}
}

// VPNTunnel is the task's VPN tunnel as kept; IsNotFound for none.
// VPNTunnels and CountVPNTunnels are a page of the operation's, the
// latest opened first, and how many.
func (s *Service) VPNTunnel(ctx context.Context, task uuid.UUID) (*models.VPNTunnel, error) {
	if s.vpnTunnels == nil {
		return nil, repository.ErrNotFound
	}
	return s.vpnTunnels.FindByTaskID(ctx, task)
}

func (s *Service) VPNTunnels(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.VPNTunnel, error) {
	if s.vpnTunnels == nil {
		return nil, nil
	}
	return s.vpnTunnels.FindByOperationIDWithCursor(ctx, operationID, search, cursor, limit, forward)
}

func (s *Service) CountVPNTunnels(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	if s.vpnTunnels == nil {
		return 0, nil
	}
	return s.vpnTunnels.CountByOperationID(ctx, operationID, search)
}

// VPNConnected is how many clients of the task's VPN tunnel are connected.
func (s *Service) VPNConnected(task uuid.UUID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnels[task]
	if t == nil || t.vpn == nil {
		return 0
	}
	n := 0
	for _, c := range t.vpn.clients {
		if c.link != nil {
			n++
		}
	}
	return n
}

// prefixTexts writes routes as their CIDRs.
func prefixTexts(routes []netip.Prefix) []string {
	out := make([]string, len(routes))
	for i, p := range routes {
		out[i] = p.String()
	}
	return out
}

// takeUpVPN holds a VPN tunnel, its numbers as its record kept them.
// acked: the minion has the task already (a restart's).
func (s *Service) takeUpVPN(rec *models.VPNTunnel, acked bool) *tunnel {
	routes, err := protocol.TunnelRoutes(rec.Routes)
	if err != nil {
		s.logger.Warn("tunnels: a VPN tunnel's routes do not read", zap.String("task_id", rec.TaskID.String()), zap.Error(err))
	}
	t := newTunnel(&models.OperationalPort{TaskID: rec.TaskID, SessionID: rec.SessionID, OperationID: rec.OperationID,
		Purpose: models.OperationalPurposeVPN, Source: rec.Source, Streams: rec.Streams, Opened: rec.Opened, Up: rec.Up, Down: rec.Down,
		MaxStreams: rec.MaxStreams, Timeout: rec.Timeout, StopIdle: rec.StopIdle, Cap: rec.Cap}, nil, acked)
	t.vpn = &vpnTunnel{routes: routes, text: rec.Routes, udp: rec.UDP, icmp: rec.ICMP, sessionNumber: rec.SessionNumber,
		clients: map[string]*VPNClient{}}
	s.mu.Lock()
	s.tunnels[rec.TaskID] = t
	s.mu.Unlock()
	s.logger.Info("tunnels: VPN tunnel", zap.String("task_id", rec.TaskID.String()), zap.Strings("routes", rec.Routes))
	return t
}

// reopenVPN takes up a restart's VPN tunnel whose task still runs; one
// whose task ended is forgotten, its keys with it, what it came to kept
// with its task. Its clients connect again.
func (s *Service) reopenVPN(ctx context.Context, rec *models.VPNTunnel) {
	task, err := s.tasks.FindByTaskID(ctx, rec.TaskID)
	switch {
	case repository.IsNotFound(err) || (err == nil && (task.Finished() || task.CancelRequestedAt != nil)):
		if err == nil {
			s.keepTotals(ctx, rec.TaskID, models.MinionTaskTunnel{Streams: rec.Opened, Up: rec.Up, Down: rec.Down})
		}
		s.releaseVPN(ctx, rec.TaskID)
		return
	case err != nil:
		s.logger.Error("tunnels: read a VPN tunnel's task", zap.String("task_id", rec.TaskID.String()), zap.Error(err))
		return
	}
	s.takeUpVPN(rec, task.AckedAt != nil)
}

// releaseVPN forgets a task's VPN tunnel and its keys.
func (s *Service) releaseVPN(ctx context.Context, task uuid.UUID) {
	if s.vpnTunnels == nil {
		return
	}
	if err := s.vpnTunnels.Release(ctx, task); err != nil {
		s.logger.Warn("tunnels: forget a task's VPN tunnel", zap.String("task_id", task.String()), zap.Error(err))
	}
	if err := s.vpnKeys.ReleaseTask(ctx, task); err != nil {
		s.logger.Warn("tunnels: forget a task's VPN keys", zap.String("task_id", task.String()), zap.Error(err))
	}
}

// NewVPNKey makes a key for a client of the task's VPN tunnel, for the
// user: the key (lgv_…), and when it lapses unused.
func (s *Service) NewVPNKey(ctx context.Context, task, user uuid.UUID, username string) (string, time.Time, error) {
	if s.vpnKeys == nil {
		return "", time.Time{}, ErrNoVPN
	}
	s.mu.Lock()
	t := s.tunnels[task]
	ok := t != nil && t.vpn != nil && !t.closing
	var operation uuid.UUID
	if ok {
		operation = t.operation
	}
	s.mu.Unlock()
	if !ok {
		return "", time.Time{}, fmt.Errorf("the task runs no VPN tunnel: %w", repository.ErrNotFound)
	}
	raw, keyID, hash, err := auth.GenerateKey(protocol.VPNKeyPrefix)
	if err != nil {
		return "", time.Time{}, err
	}
	lapses := s.now().Add(protocol.VPNKeyLapse)
	k := &models.VPNKey{KeyID: keyID, SecretHash: hash, TaskID: task, OperationID: operation, UserID: user, Username: username,
		LapsesAt: &lapses}
	if err := s.vpnKeys.Insert(ctx, k); err != nil {
		return "", time.Time{}, fmt.Errorf("keep a VPN key: %w", err)
	}
	return raw, lapses, nil
}

// SetRoutes changes a running VPN tunnel's routes: kept, and said to its
// clients at once. The routes as core keeps them (each a network's CIDR).
func (s *Service) SetRoutes(ctx context.Context, task uuid.UUID, routes []string) ([]string, error) {
	prefixes, err := protocol.TunnelRoutes(routes)
	if err != nil {
		return nil, err
	}
	text := prefixTexts(prefixes)
	s.mu.Lock()
	t := s.tunnels[task]
	if t == nil || t.vpn == nil || t.closing {
		s.mu.Unlock()
		return nil, fmt.Errorf("the task runs no VPN tunnel: %w", repository.ErrNotFound)
	}
	s.mu.Unlock()
	if err := s.vpnTunnels.SetRoutes(ctx, task, text); err != nil {
		return nil, fmt.Errorf("keep a VPN tunnel's routes: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.vpn != nil {
		t.vpn.routes, t.vpn.text, t.changed = prefixes, text, true
		for _, c := range t.vpn.clients {
			c.routesDue = true
			c.kick()
		}
	}
	return text, nil
}

// VPNClientInfo is a client of a VPN tunnel, as its task shows it.
type VPNClientInfo struct {
	// ID is its instance; Name what its streams say it is (vpn:
	// user@machine).
	ID, Name, User                   string
	Machine, OS, Arch, Version, From string
	Since                            time.Time
	// Connected: its link is up; else it is kept a while for another.
	Connected bool
	// Open: its streams now; Streams how many it opened; Up and Down its
	// bytes each way.
	Open     int
	Streams  uint64
	Up, Down int64
}

// VPN is the task's VPN tunnel's routes and clients, while core holds it.
func (s *Service) VPN(task uuid.UUID) (routes []string, clients []VPNClientInfo, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnels[task]
	if t == nil || t.vpn == nil {
		return nil, nil, false
	}
	for _, c := range t.vpn.clients {
		clients = append(clients, VPNClientInfo{ID: c.hello.Instance, Name: c.name, User: c.username, Machine: c.hello.Machine,
			OS: c.hello.OS, Arch: c.hello.Arch, Version: c.hello.Version, From: c.addr, Since: c.since, Connected: c.link != nil,
			Open: len(c.legs), Streams: c.opened, Up: c.up, Down: c.down})
	}
	slices.SortFunc(clients, func(a, b VPNClientInfo) int { return a.Since.Compare(b.Since) })
	return slices.Clone(t.vpn.text), clients, true
}

// DisconnectVPN ends one client of the task's VPN tunnel (its instance),
// saying who did; IsNotFound for none.
func (s *Service) DisconnectVPN(task uuid.UUID, instance, by string) error {
	s.mu.Lock()
	t := s.tunnels[task]
	var c *VPNClient
	if t != nil && t.vpn != nil {
		c = t.vpn.clients[instance]
	}
	if c == nil {
		s.mu.Unlock()
		return fmt.Errorf("no such client: %w", repository.ErrNotFound)
	}
	after := s.endClient(c, "disconnected by "+by)
	s.mu.Unlock()
	after()
	return nil
}

// ConnectVPN takes a client of a VPN tunnel by its key and its hello:
// again, when its instance is connected already or kept since its link
// dropped. ErrVPNRefused or ErrVPNHello, wrapped with why, when core does
// not take it. addr is where it came from, as core saw it.
func (s *Service) ConnectVPN(ctx context.Context, raw string, hello protocol.VPNHello, addr string) (*VPNClient, protocol.VPNWelcome, error) {
	refused := func(why string) error { return fmt.Errorf("%w: %s", ErrVPNRefused, why) }
	if s.vpnKeys == nil {
		return nil, protocol.VPNWelcome{}, refused("core opens no VPN tunnels")
	}
	if err := hello.Validate(); err != nil {
		return nil, protocol.VPNWelcome{}, fmt.Errorf("%w: %v", ErrVPNHello, err)
	}
	const unknown = "the key is unknown, lapsed or used: make a new one (Connect a machine, on the task's VPN tab)"
	keyID, hash, ok := auth.ParseKey(protocol.VPNKeyPrefix, raw)
	if !ok {
		return nil, protocol.VPNWelcome{}, refused("no VPN key (lgv_…)")
	}
	k, err := s.vpnKeys.FindByKeyID(ctx, keyID)
	switch {
	case repository.IsNotFound(err):
		return nil, protocol.VPNWelcome{}, refused(unknown)
	case err != nil:
		return nil, protocol.VPNWelcome{}, err
	case subtle.ConstantTimeCompare([]byte(k.SecretHash), []byte(hash)) != 1:
		return nil, protocol.VPNWelcome{}, refused(unknown)
	}
	if k, err = s.vpnKeys.Take(ctx, keyID, hello.Instance, s.now()); err != nil {
		if repository.IsNotFound(err) {
			return nil, protocol.VPNWelcome{}, refused(unknown)
		}
		return nil, protocol.VPNWelcome{}, err
	}
	if s.roles != nil && !s.roles.IsOperator(ctx, k.OperationID, k.UserID) {
		return nil, protocol.VPNWelcome{}, refused(k.Username + " is no longer an operator of the operation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnels[k.TaskID]
	if t == nil || t.vpn == nil || t.closing {
		return nil, protocol.VPNWelcome{}, refused("the task runs no VPN tunnel")
	}
	c := t.vpn.clients[hello.Instance]
	if c == nil {
		if len(t.vpn.clients) >= protocol.MaxVPNClients {
			return nil, protocol.VPNWelcome{}, refused(fmt.Sprintf("the tunnel's %d clients are connected already", protocol.MaxVPNClients))
		}
		c = &VPNClient{s: s, t: t, keyID: k.KeyID, user: k.UserID, username: k.Username, since: s.now(),
			name: fmt.Sprintf("vpn: %s@%s", k.Username, hello.Machine), legs: map[uint64]*vpnLeg{}, answers: map[uint64]protocol.TunnelReplyItem{}}
		t.vpn.clients[hello.Instance] = c
		t.streamsChanged(s.now())
		s.logger.Info("tunnels: a VPN client connected", zap.String("task_id", t.task.String()), zap.String("client", c.name),
			zap.String("from", addr))
	}
	// The welcome says the routes: a change from now on goes again.
	c.hello, c.addr, c.roleAt, c.routesDue, t.changed, t.counted = hello, addr, s.now(), false, true, true
	opens := []string{protocol.OpenTCP}
	if t.vpn.udp {
		opens = append(opens, protocol.OpenUDP)
	}
	if t.vpn.icmp {
		opens = append(opens, protocol.OpenICMP)
	}
	return c, protocol.VPNWelcome{Task: t.task.String(), Session: t.vpn.sessionNumber, Operation: t.operation.String(), User: c.username,
		Client: c.name, Routes: slices.Clone(t.vpn.text), MaxStreams: t.maxStreams, Opens: opens}, nil
}

// endClient ends a client (s.mu held): its streams reset on both legs, its
// key dropped, and its link told why (none: it said bye) and closed, by
// after, which runs once s.mu is let go.
func (s *Service) endClient(c *VPNClient, why string) (after func()) {
	if c.ended {
		return func() {}
	}
	c.ended = true
	if why == "" {
		why = "it disconnected"
	}
	c.endWhy = why
	for _, g := range c.sortedLegs() {
		if g.st != nil && !g.st.gone {
			s.abort(g.st, models.TunnelEndClientReset)
		}
		c.removeLeg(g)
	}
	t := c.t
	if t.vpn != nil && t.vpn.clients[c.hello.Instance] == c {
		delete(t.vpn.clients, c.hello.Instance)
	}
	t.streamsChanged(s.now())
	t.changed, t.counted = true, true
	ls := c.link
	c.link = nil
	key := c.keyID
	s.logger.Info("tunnels: a VPN client ended", zap.String("task_id", t.task.String()), zap.String("client", c.name), zap.String("why", why))
	return func() {
		if ls != nil {
			ls.end(why)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.vpnKeys.Release(ctx, key); err != nil {
			s.logger.Warn("tunnels: drop a VPN client's key", zap.Error(err))
		}
	}
}

// endVPN ends every client of a VPN tunnel that closes (s.mu held), saying
// why: after runs once s.mu is let go.
func (s *Service) endVPN(t *tunnel, why string) (after func()) {
	if t.vpn == nil {
		return func() {}
	}
	var afters []func()
	for _, c := range t.vpn.clients {
		afters = append(afters, s.endClient(c, why))
	}
	return func() {
		for _, a := range afters {
			a()
		}
	}
}

// vpnSweep ends the clients whose link stayed down past the grace, and
// those whose operator is no longer an operator of the operation.
func (s *Service) vpnSweep(ctx context.Context) {
	type due struct {
		c               *VPNClient
		operation, user uuid.UUID
	}
	s.mu.Lock()
	now := s.now()
	var afters []func()
	var roles []due
	for _, t := range s.tunnels {
		if t.vpn == nil {
			continue
		}
		for _, c := range t.vpn.clients {
			switch {
			case c.link == nil && !c.droppedAt.IsZero() && now.Sub(c.droppedAt) >= s.cfg.VPNGrace:
				afters = append(afters, s.endClient(c, fmt.Sprintf("its link stayed down past %s", shortDuration(s.cfg.VPNGrace))))
			case now.Sub(c.roleAt) >= vpnRoleCheck:
				c.roleAt = now
				roles = append(roles, due{c: c, operation: t.operation, user: c.user})
			}
		}
	}
	s.mu.Unlock()
	for _, a := range afters {
		a()
	}
	if s.roles == nil {
		return
	}
	for _, d := range roles {
		if s.roles.IsOperator(ctx, d.operation, d.user) {
			continue
		}
		s.mu.Lock()
		after := s.endClient(d.c, d.c.username+" is no longer an operator of the operation")
		s.mu.Unlock()
		after()
	}
}

// VPNClient is a client of a VPN tunnel: logos-vpn on one machine, for one
// operator, while it is connected and for protocol.VPNLinkGrace after its
// link dropped. Serve carries a link of it.
type VPNClient struct {
	s        *Service
	t        *tunnel
	keyID    string
	user     uuid.UUID
	username string
	// name is what its streams say they came from (vpn: user@machine).
	name  string
	since time.Time

	// Under s.mu: its hello (the latest), where it came from, its link now
	// (nil since droppedAt), when its operator's role was last read.
	hello     protocol.VPNHello
	addr      string
	link      *vpnLinkState
	droppedAt time.Time
	roleAt    time.Time
	ended     bool
	endWhy    string
	// legs are its streams by its numbers; last the highest it opened.
	// answers are the replies of those that did not open, said again
	// should it ask again (answered, in order, to drop the oldest).
	legs     map[uint64]*vpnLeg
	last     uint64
	answers  map[uint64]protocol.TunnelReplyItem
	answered []uint64
	// What goes to it with its link's next frame: replies, closes, its
	// datagrams (dgramBytes their payloads), the routes when changed.
	replies    []protocol.TunnelReplyItem
	closes     []protocol.TunnelCloseItem
	dgrams     []protocol.TunnelDatagramItem
	dgramBytes int
	routesDue  bool
	rotation   int
	// opened is how many streams it opened; up and down its bytes.
	opened   uint64
	up, down int64
}

// kick has its link's writer look for what to send (s.mu held, or not).
func (c *VPNClient) kick() {
	if ls := c.link; ls != nil {
		signal(ls.kick)
	}
}

func (c *VPNClient) sortedLegs() []*vpnLeg {
	out := make([]*vpnLeg, 0, len(c.legs))
	for _, g := range c.legs {
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b *vpnLeg) int { return compareUint(a.id, b.id) })
	return out
}

// ours: an item of the client's names its tunnel's task.
func (c *VPNClient) ours(task []byte) bool { return bytes.Equal(task, c.t.task[:]) }

// answer says a stream of the client's did not open, keeping the reply to
// say again.
func (c *VPNClient) answer(stream uint64, result int, msg string) {
	r := protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: c.t.task[:], Stream: stream, Result: result,
		Msg: clipText(msg, protocol.MaxTunnelMsg)}
	if _, ok := c.answers[stream]; !ok {
		c.answered = append(c.answered, stream)
		if len(c.answered) > vpnAnswers {
			delete(c.answers, c.answered[0])
			c.answered = c.answered[1:]
		}
	}
	c.answers[stream] = r
	c.replies = append(c.replies, r)
	c.kick()
}

// closeLeg tells the client its stream is over.
func (c *VPNClient) closeLeg(stream uint64, reason int) {
	c.closes = append(c.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: c.t.task[:], Stream: stream, Reason: reason})
	c.kick()
}

// vpnLeg is one stream of a client's: a TCP connection its machine made,
// a UDP source's association, or its pings, and core's stream toward the
// minion for it (st, once numbered), the two joined by a pipe (far: core's
// leg's end of it) for TCP.
type vpnLeg struct {
	c         *VPNClient
	id        uint64
	host      string
	port      int
	udp, icmp bool
	// outRoom is the room the client gave as it opened it.
	outRoom uint64
	st      *stream
	far     *pipeEnd
	// reply is the minion's, said to the client once (again should it ask
	// again); open: it said open.
	reply *protocol.TunnelReplyItem
	open  bool
	gone  bool
	done  chan struct{}
	// clientClosed: the client said the stream is over.
	clientClosed bool

	// From the client: taken up to inNext (its eof counted), what of it
	// is not yet written to the pipe (inBuf), its eof (inEOF) and whether
	// the pipe has it (inShut). An ack is due; ackedEnd the furthest the
	// acks sent let the client send.
	inNext   uint64
	inBuf    []byte
	inEOF    bool
	inShut   bool
	ackDue   bool
	ackedEnd uint64
	inSignal chan struct{}

	// To the client: the minion's bytes from outBase (the client's next)
	// not yet acked, sent up to outSent, nothing past outLimit (the
	// furthest the client's room reached); outEOF: the minion's eof came;
	// outDone: the client has it.
	outBuf    []byte
	outBase   uint64
	outSent   uint64
	outLimit  uint64
	outEOF    bool
	outDone   bool
	outSignal chan struct{}

	// A datagram stream's: its datagrams to the client, numbered; the
	// client's, taken once each; when one last went either way.
	seqOut     uint64
	in         replayWindow
	lastActive time.Time
}

// removeLeg forgets a stream of the client's (s.mu held): its pipe closes.
func (c *VPNClient) removeLeg(g *vpnLeg) {
	if g.gone {
		return
	}
	g.gone = true
	close(g.done)
	if c.legs[g.id] == g {
		delete(c.legs, g.id)
	}
	if g.far != nil {
		_ = g.far.Close()
	}
	c.t.changed = true
}

// takeOpen: the client opens a stream (s.mu held). One it opened already
// gets its reply again (none yet: it comes); one that went, a close.
func (s *Service) takeLegOpen(c *VPNClient, it protocol.TunnelItem) {
	if g := c.legs[it.Stream]; g != nil {
		if g.reply != nil {
			c.replies = append(c.replies, *g.reply)
			c.kick()
		}
		return
	}
	if r, ok := c.answers[it.Stream]; ok {
		c.replies = append(c.replies, r)
		c.kick()
		return
	}
	if it.Stream <= c.last {
		c.closeLeg(it.Stream, protocol.TunnelCloseUnknown)
		return
	}
	c.last = it.Stream
	v := c.t.vpn
	icmp := it.Kind == protocol.TunnelKindICMP
	g := &vpnLeg{c: c, id: it.Stream, udp: it.UDP, icmp: icmp, outRoom: protocol.TunnelRoom(it.Room), done: make(chan struct{}),
		inSignal: make(chan struct{}, 1), outSignal: make(chan struct{}, 1), lastActive: s.now()}
	switch {
	case it.Kind != 0 && !icmp, it.Path != "", it.TLS, it.Source != "", len(it.Args) > 0:
		c.answer(it.Stream, protocol.TunnelUnsupported, "a VPN's streams are TCP connections, UDP associations and pings")
		return
	case it.UDP && !v.udp:
		c.answer(it.Stream, protocol.TunnelUnsupported, "the tunnel's command opens no udp")
		return
	case icmp && !v.icmp:
		c.answer(it.Stream, protocol.TunnelUnsupported, "the tunnel's command opens no icmp")
		return
	case !it.UDP && !icmp:
		addr, err := netip.ParseAddr(it.Host)
		if err != nil {
			c.answer(it.Stream, protocol.TunnelUnsupported, "a VPN's stream names an address, not a name")
			return
		}
		if !protocol.TunnelRouted(v.routes, addr) {
			c.answer(it.Stream, protocol.TunnelDenied, it.Host+" is not in the tunnel's routes")
			return
		}
		g.host, g.port = addr.Unmap().String(), int(it.Port)
	}
	c.legs[it.Stream] = g
	c.opened++
	c.t.changed = true
	s.done.Add(1)
	go s.openLeg(g)
}

// openLeg opens core's stream toward the minion for a client's, answers
// the client by the minion's reply, and carries its bytes once it is open.
func (s *Service) openLeg(g *vpnLeg) {
	defer s.done.Done()
	c, t := g.c, g.c.t
	near, far := newPipe()
	req := request{host: g.host, port: g.port, udp: g.udp || g.icmp, client: c.name}
	if g.icmp {
		req.kind = protocol.TunnelKindICMP
	}
	st, code := s.newStream(t, near, req, false)
	s.mu.Lock()
	if st == nil {
		if !g.gone {
			result := protocol.TunnelFailed
			if code == socks5NotAllowed {
				result = protocol.TunnelNotAllowed
			}
			c.removeLeg(g)
			c.answer(g.id, result, "")
		}
		s.mu.Unlock()
		_ = near.Close()
		return
	}
	if g.gone {
		s.abort(st, models.TunnelEndClientReset)
		s.mu.Unlock()
		return
	}
	g.st, g.far, st.leg = st, far, g
	s.mu.Unlock()
	timer := time.NewTimer(t.connectWait + s.cfg.ConnectSlack)
	defer timer.Stop()
	select {
	case r := <-st.replied:
		s.mu.Lock()
		defer s.mu.Unlock()
		if g.gone {
			return
		}
		if r.Result != protocol.TunnelOpen {
			s.drop(st, models.TunnelEndNotOpened, false)
			c.removeLeg(g)
			c.answer(g.id, r.Result, r.Msg)
			return
		}
		reply := protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: t.task[:], Stream: g.id, Result: protocol.TunnelOpen,
			Bound: r.Bound}
		g.reply, g.open = &reply, true
		g.outLimit, g.ackedEnd = g.outRoom, vpnWindow
		c.replies = append(c.replies, reply)
		c.kick()
		if !g.udp && !g.icmp {
			// The engine's side of the pipe, as a front's, and the client's.
			s.done.Add(5)
			go func() {
				defer s.done.Done()
				s.readClient(st)
			}()
			go s.writeClient(st)
			go s.legIn(g)
			go s.legOut(g)
			go s.legWatch(g)
		}
	case <-timer.C:
		s.mu.Lock()
		defer s.mu.Unlock()
		if g.gone {
			return
		}
		s.abort(st, models.TunnelEndNoReply)
		c.removeLeg(g)
		c.answer(g.id, protocol.TunnelTimedOut, "the minion did not answer in time")
	case <-st.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		if g.gone {
			return
		}
		c.removeLeg(g)
		c.answer(g.id, protocol.TunnelFailed, "the tunnel's stream ended")
	}
}

// legIn writes the client's bytes to the pipe, then its eof, giving the
// client room back as they go.
func (s *Service) legIn(g *vpnLeg) {
	defer s.done.Done()
	for {
		s.mu.Lock()
		for !g.gone && len(g.inBuf) == 0 && (!g.inEOF || g.inShut) {
			s.mu.Unlock()
			select {
			case <-g.inSignal:
			case <-g.done:
			}
			s.mu.Lock()
		}
		if g.gone {
			s.mu.Unlock()
			return
		}
		far := g.far
		if len(g.inBuf) == 0 {
			// Its eof: the minion's end closes for writing.
			g.inShut = true
			s.mu.Unlock()
			_ = far.CloseWrite()
			s.mu.Lock()
			s.legOver(g)
			s.mu.Unlock()
			return
		}
		piece := g.inBuf[:min(len(g.inBuf), readPiece)]
		s.mu.Unlock()
		_, err := far.Write(piece)
		s.mu.Lock()
		if g.gone || err != nil {
			// The stream went: legWatch says so.
			s.mu.Unlock()
			return
		}
		g.inBuf = g.inBuf[len(piece):]
		if len(g.inBuf) == 0 {
			g.inBuf = nil
		}
		g.c.up += int64(len(piece))
		if g.roomGrew() {
			g.c.kick()
		}
		s.mu.Unlock()
	}
}

// roomGrew marks an ack due once the client may send a quarter of the
// window more than the acks sent let it.
func (g *vpnLeg) roomGrew() bool {
	if g.ackDue || g.inEOF {
		return false
	}
	room := uint64(max(vpnWindow-len(g.inBuf), 0))
	if g.inNext+room >= g.ackedEnd+uint64(vpnWindow/4) {
		g.ackDue = true
		return true
	}
	return false
}

// outFree is how much more of the minion's bytes core may hold for the
// client now.
func (g *vpnLeg) outFree() int {
	if g.outEOF {
		return 0
	}
	limit := min(g.outLimit-min(g.outLimit, g.outBase), vpnHeld)
	return max(int(limit)-len(g.outBuf), 0)
}

// legOut reads the minion's bytes from the pipe for the client, within the
// room it gives, then the minion's eof.
func (s *Service) legOut(g *vpnLeg) {
	defer s.done.Done()
	buf := make([]byte, readPiece)
	for {
		s.mu.Lock()
		for !g.gone && g.outFree() == 0 && !g.outEOF {
			s.mu.Unlock()
			select {
			case <-g.outSignal:
			case <-g.done:
			}
			s.mu.Lock()
		}
		if g.gone || g.outEOF {
			s.mu.Unlock()
			return
		}
		n, far := min(len(buf), g.outFree()), g.far
		s.mu.Unlock()
		k, err := far.Read(buf[:n])
		s.mu.Lock()
		if g.gone {
			s.mu.Unlock()
			return
		}
		if k > 0 {
			g.outBuf = append(g.outBuf, buf[:k]...)
			g.c.down += int64(k)
			g.c.kick()
		}
		if err != nil {
			// The minion's eof; or the stream went (legWatch resets it).
			if !g.st.gone || g.st.rec.End == models.TunnelEndDone {
				g.outEOF = true
				g.c.kick()
			}
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// legWatch ends a client's stream as core's toward the minion ends: done,
// once the client has every byte of it; else at once, saying why.
func (s *Service) legWatch(g *vpnLeg) {
	defer s.done.Done()
	select {
	case <-g.done:
		return
	case <-g.st.done:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if g.gone {
		return
	}
	switch g.st.rec.End {
	case models.TunnelEndDone:
		s.legOver(g)
	default:
		reason := protocol.TunnelCloseReset
		if g.st.rec.End == models.TunnelEndTaskEnded || g.st.rec.End == models.TunnelEndCoreStopped {
			reason = protocol.TunnelCloseTaskEnded
		}
		if !g.clientClosed {
			g.c.closeLeg(g.id, reason)
		}
		g.c.removeLeg(g)
	}
}

// legOver drops a client's stream once it is over (s.mu held): core's
// toward the minion ended done, the client has its every byte and eof, and
// the minion the client's; the client is told, unless it said so first.
func (s *Service) legOver(g *vpnLeg) {
	if g.gone || g.st == nil || !g.st.gone || g.st.rec.End != models.TunnelEndDone {
		return
	}
	if g.clientClosed {
		g.c.removeLeg(g)
		return
	}
	if !g.outDone || !g.inShut {
		return
	}
	g.c.closeLeg(g.id, protocol.TunnelCloseDone)
	g.c.removeLeg(g)
}

// takeLegData takes the client's bytes at core's next, within the room core
// gave (s.mu held); what core has already is acked again.
func (s *Service) takeLegData(g *vpnLeg, d protocol.TunnelDataItem) {
	if !g.open || g.udp || g.icmp {
		return
	}
	g.ackDue = true
	g.c.kick()
	if g.inEOF || d.End() <= g.inNext || d.Off > g.inNext {
		// Had, or ahead of next: the client sends again from the ack.
		return
	}
	data := d.Data[g.inNext-d.Off:]
	eof := d.EOF
	if free := max(vpnWindow-len(g.inBuf), 0); len(data) > free {
		data, eof = data[:free], false
	}
	g.inBuf = append(g.inBuf, data...)
	g.inNext += uint64(len(data))
	if eof {
		g.inEOF = true
		g.inNext++
	}
	signal(g.inSignal)
}

// takeLegAck drops what the client has of the minion's bytes and takes the
// room it gives (s.mu held).
func (s *Service) takeLegAck(g *vpnLeg, a protocol.TunnelAckItem) {
	if !g.open || g.udp || g.icmp {
		return
	}
	end := g.outBase + uint64(len(g.outBuf))
	if g.outDone || a.Next < g.outBase || a.Next > end+1 || (a.Next == end+1 && !g.outEOF) {
		return
	}
	room := g.outLimit - min(g.outLimit, g.outBase)
	if a.Room != nil {
		room = *a.Room
	}
	if a.Next == end+1 {
		g.outBuf, g.outBase, g.outDone = nil, end, true
	} else {
		g.outBuf = g.outBuf[a.Next-g.outBase:]
		g.outBase = a.Next
	}
	g.outSent = max(g.outSent, a.Next)
	g.outLimit = max(g.outLimit, a.Next+room)
	signal(g.outSignal)
	g.c.kick()
	if g.outDone {
		s.legOver(g)
	}
}

// takeLegClose: the client ended its stream (s.mu held). A datagram
// stream's goes, the minion told; a TCP one's that ended done goes once
// core's has; one reset resets core's.
func (s *Service) takeLegClose(g *vpnLeg, cl protocol.TunnelCloseItem) {
	g.clientClosed = true
	st := g.st
	switch {
	case g.udp || g.icmp:
		if st != nil && !st.gone {
			st.t.closes = append(st.t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: st.t.task[:], Stream: st.id,
				Reason: protocol.TunnelCloseDone})
			s.drop(st, models.TunnelEndDone, false)
			s.wake(st.t.session)
		}
		g.c.removeLeg(g)
	case cl.Reason == protocol.TunnelCloseDone && st != nil && !st.gone:
		// Core's ends as its bytes and eofs are through: legOver.
	default:
		if st != nil && !st.gone {
			s.abort(st, models.TunnelEndClientReset)
		}
		g.c.removeLeg(g)
	}
}

// takeLegDatagram queues a client's datagram for the minion (s.mu held):
// one to an address outside the routes, a ping that is no echo request,
// one taken already, or one past the queue, is dropped.
func (s *Service) takeLegDatagram(g *vpnLeg, d protocol.TunnelDatagramItem) {
	st := g.st
	if !g.open || st == nil || st.gone || st.udp == nil || !g.in.take(d.Seq) {
		return
	}
	addr, err := netip.ParseAddr(d.Host)
	if err != nil || !protocol.TunnelRouted(g.c.t.vpn.routes, addr) {
		return
	}
	addr = addr.Unmap()
	if g.icmp {
		req, _ := protocol.ICMPEchoTypes(addr.Is6())
		if m, err := protocol.ReadICMP(d.Data, addr.Is6()); err != nil || m.Type != req || d.Port != 0 {
			return
		}
	} else if d.Port == 0 {
		return
	}
	if st.queueDatagram(addr.String(), int(d.Port), d.Data) {
		g.c.up += int64(len(d.Data))
		g.lastActive, st.lastActive = s.now(), s.now()
		s.wake(st.t.session)
	}
}

// vpnDatagram queues a datagram the minion's socket got for the client's
// stream it answers (s.mu held); past the queue, it is dropped.
func (s *Service) vpnDatagram(st *stream, d protocol.TunnelDatagramItem) {
	g := st.leg
	c := g.c
	if g.gone || c.dgramBytes+len(d.Data) > vpnDatagrams {
		return
	}
	c.dgrams = append(c.dgrams, protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: c.t.task[:], Stream: g.id, Seq: g.seqOut,
		Host: d.Host, Port: d.Port, Data: bytes.Clone(d.Data)})
	g.seqOut++
	c.dgramBytes += len(d.Data)
	c.down += int64(len(d.Data))
	st.downBytes += int64(len(d.Data))
	st.t.down += int64(len(d.Data))
	st.t.changed = true
	g.lastActive, st.lastActive = s.now(), s.now()
	c.kick()
}

// endIdleVPN ends the clients' datagram streams that idled past
// cfg.UDPIdle (s.mu held): the client's own close went missing. Both sides
// are told.
func (s *Service) endIdleVPN(t *tunnel, now time.Time) {
	for _, c := range t.vpn.clients {
		for _, g := range c.sortedLegs() {
			if !(g.udp || g.icmp) || !g.open || now.Sub(g.lastActive) < s.cfg.UDPIdle {
				continue
			}
			if st := g.st; st != nil && !st.gone {
				t.closes = append(t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: t.task[:], Stream: st.id,
					Reason: protocol.TunnelCloseDone})
				s.drop(st, models.TunnelEndDone, false)
				s.wake(t.session)
			}
			c.closeLeg(g.id, protocol.TunnelCloseDone)
			c.removeLeg(g)
		}
	}
}

// stopVPN closes the links of every client (s.mu held) as core stops,
// keeping their keys: they connect again once core is back.
func (s *Service) stopVPN(t *tunnel) (links []*vpnLinkState) {
	if t.vpn == nil {
		return nil
	}
	for _, c := range t.vpn.clients {
		if c.link != nil {
			links = append(links, c.link)
			c.link = nil
		}
	}
	return links
}
