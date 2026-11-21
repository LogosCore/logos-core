package miniontunnels

import (
	"cmp"
	"context"
	"math/rand/v2"
	"slices"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// Port memory (docs: contracts/minion-messages/tunnels, Operational
// ports): what core took each port for. A tunnel that names no port gets
// one its session took before, the one it last took for the same use (the
// purpose and the target: models.OperationalPortTarget), then for the same
// purpose, then for any; a session with none free gets one of the ports
// least heated (taken the fewest times, every operation's tasks counted),
// at random among them, so that the operations' tunnels spread over the
// range rather than all coming back to its first ports.

// WithPortMemory keeps what ports were taken for, and offers them again;
// without it, a tunnel naming no port takes the first free one.
func (s *Service) WithPortMemory(m repository.IOperationalPortMemoryRepository) *Service {
	s.memory = m
	return s
}

// order is the ports an opening naming none tries, in turn: the session's,
// then the rest, the least heated first, at random within one heat. With
// no memory, or none read, the range in order.
func (s *Service) order(ctx context.Context, o Opening, purpose string) []int {
	if s.memory == nil || len(s.cfg.Ports) == 0 {
		return s.cfg.Ports
	}
	heat, err := s.memory.Heat(ctx)
	if err != nil {
		s.logger.Warn("tunnels: the ports' heat could not be read: the first free port", zap.Error(err))
		return s.cfg.Ports
	}
	uses, err := s.memory.BySession(ctx, o.Session)
	if err != nil {
		s.logger.Warn("tunnels: the session's ports could not be read", zap.String("session_id", o.Session.String()), zap.Error(err))
		uses = nil
	}
	target := models.OperationalPortTarget(&models.OperationalPort{Purpose: purpose, Host: o.Host, HostPort: o.HostPort, Path: o.Path, Program: o.Program})
	out := make([]int, 0, len(s.cfg.Ports))
	taken := make(map[int]bool, len(s.cfg.Ports))
	for _, p := range s.cfg.Ports {
		taken[p] = false
	}
	// The session's, the latest first: for the same use, the same purpose,
	// any. One out of the range now is not offered.
	for _, same := range []func(models.OperationalPortUse) bool{
		func(u models.OperationalPortUse) bool { return u.Purpose == purpose && u.Target == target },
		func(u models.OperationalPortUse) bool { return u.Purpose == purpose },
		func(models.OperationalPortUse) bool { return true },
	} {
		for _, u := range uses {
			if done, in := taken[u.Port]; in && !done && same(u) {
				taken[u.Port] = true
				out = append(out, u.Port)
			}
		}
	}
	rest := make([]int, 0, len(s.cfg.Ports)-len(out))
	for _, p := range s.cfg.Ports {
		if !taken[p] {
			rest = append(rest, p)
		}
	}
	shuffle := s.shuffle
	if shuffle == nil {
		shuffle = func(p []int) { rand.Shuffle(len(p), func(i, j int) { p[i], p[j] = p[j], p[i] }) }
	}
	shuffle(rest)
	slices.SortStableFunc(rest, func(a, b int) int { return cmp.Compare(heat[a].Uses, heat[b].Uses) })
	return append(out, rest...)
}

// remember counts the port taken for its session's use, and its heat. A
// failure costs the next choice alone: the tunnel is open.
func (s *Service) remember(ctx context.Context, rec *models.OperationalPort) {
	if s.memory == nil {
		return
	}
	u := models.OperationalPortUse{SessionID: rec.SessionID, OperationID: rec.OperationID, Port: rec.Port, Purpose: rec.Purpose,
		Target: models.OperationalPortTarget(rec)}
	if err := s.memory.Record(ctx, u, s.now()); err != nil {
		s.logger.Warn("tunnels: the port taken could not be remembered", zap.Int("port", rec.Port), zap.Error(err))
	}
}
