package minionsync

import (
	"context"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Tunnels (docs: contracts/minion-messages/tunnels.md): the streams of
// tunnel tasks, whose items go in the session's messages after its tasks
// (the outbox), and the minion's tunnel items, taken in its messages.

// Tunnels carries tunnel tasks' streams (miniontunnels.Service).
type Tunnels interface {
	// Fill adds the session's tunnel items through add, within room bytes
	// and most items: how many it added, and how soon a minion that polls
	// should come back (polls false: no tunnel of the session's runs).
	Fill(ctx context.Context, session uuid.UUID, room, most int, add func(cbor.RawMessage) bool) (added int, poll time.Duration, polls bool)
	// Take takes one of the minion's tunnel items: what goes back at once;
	// bad when it does not read.
	Take(ctx context.Context, session uuid.UUID, raw cbor.RawMessage) (answers []cbor.RawMessage, bad error)
}

// tunnelDelay is how long a push of tunnel items waits for more: a burst of
// a client's reads goes as one message.
const tunnelDelay = 5 * time.Millisecond

// WithTunnels gives the service the tunnels; without them, a minion's
// tunnel item is one core does not know.
func (s *Service) WithTunnels(t Tunnels) *Service {
	s.tunnels = t
	return s
}

// WakeTunnels: the session's tunnels have items to go (the tunnels'
// Waker). A persistent connection gets them at once, a bind-call page with
// a call; a minion that polls with its next message.
func (s *Service) WakeTunnels(session uuid.UUID) {
	if s.reachable(session) {
		s.pushAfter(session, false, tunnelDelay)
		return
	}
	s.Nudge(session, false)
}

// takeTunnel takes one of the minion's tunnel items.
func (s *Service) takeTunnel(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	if s.tunnels == nil {
		return errItem(protocol.ErrItemUnknownItem, "core carries no tunnels", i)
	}
	out, bad := s.tunnels.Take(ctx, session.SessionID, raw)
	if bad != nil {
		return errItem(protocol.ErrItemBadMessage, bad.Error(), i)
	}
	return out, nil
}
