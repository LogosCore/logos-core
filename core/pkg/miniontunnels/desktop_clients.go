package miniontunnels

import (
	"net"
	"net/netip"
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A desktop the web app opens is guacd's connection to the task's port
// (docs: contracts/minion-messages/tunnels.md, Desktops), whose address
// says nothing of who opened it. The desktop's handler names guacd's
// connections for it before guacd connects (DesktopClient): the operator,
// as a web terminal's stream says them ("web: alice").

// desktopClaim is a desktop the web app opened on a task's port: guacd's
// connections from from are client's; taken once one was.
type desktopClaim struct {
	from   netip.Addr
	client string
	taken  bool
}

// maxGuacdAddrs bounds the guacd addresses kept (Service.guacdAddrs).
const maxGuacdAddrs = 16

// DesktopClient names guacd's connections to a desktop task's port for a
// desktop the web app opens, until release: guacd's address as core
// reached it (from), and who opened it (client: "web: alice"). The first
// connection from there takes the name; a later one (an RDP reconnect)
// takes it too while it is the task's one desktop from there. A
// connection from a guacd address that no desktop names (two open on the
// task at once, or one ended) says it is guacd's.
func (s *Service) DesktopClient(task uuid.UUID, from netip.Addr, client string) (release func()) {
	c := &desktopClaim{from: from.Unmap(), client: client}
	s.mu.Lock()
	if s.desktopClaims == nil {
		s.desktopClaims = map[uuid.UUID][]*desktopClaim{}
	}
	s.desktopClaims[task] = append(s.desktopClaims[task], c)
	if !slices.Contains(s.guacdAddrs, c.from) {
		s.guacdAddrs = append(s.guacdAddrs, c.from)
		if len(s.guacdAddrs) > maxGuacdAddrs {
			s.guacdAddrs = s.guacdAddrs[1:]
		}
	}
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			claims := slices.DeleteFunc(s.desktopClaims[task], func(x *desktopClaim) bool { return x == c })
			if len(claims) == 0 {
				delete(s.desktopClaims, task)
			} else {
				s.desktopClaims[task] = claims
			}
		})
	}
}

// desktopClient is who a connection to a tunnel's port is, when the port
// is a desktop's and guacd made it: the desktop's name (DesktopClient), or
// guacd's own; "" for its address.
func (s *Service) desktopClient(t *tunnel, c net.Conn) string {
	ta, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok || !protocol.IsDesktopFront(t.purpose) {
		return ""
	}
	remote := ta.AddrPort()
	ip := remote.Addr().Unmap()
	s.mu.Lock()
	defer s.mu.Unlock()
	claims := s.desktopClaims[t.task]
	// A desktop opening: the oldest not yet taken from there.
	for _, d := range claims {
		if d.from == ip && !d.taken {
			d.taken = true
			return d.client
		}
	}
	// A desktop's later connection: its name, when it is the task's one
	// desktop from there.
	var only *desktopClaim
	n := 0
	for _, d := range claims {
		if d.from == ip {
			only, n = d, n+1
		}
	}
	if n == 1 {
		return only.client
	}
	if slices.Contains(s.guacdAddrs, ip) {
		return "web (guacd " + netip.AddrPortFrom(ip, remote.Port()).String() + ")"
	}
	return ""
}
