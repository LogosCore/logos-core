package minionsync

import (
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A tunnel's bytes packed where that pays (docs: contracts/minion-messages/
// wire-format.md, Compression). Most of what tunnels carry is encrypted
// already (a desktop's TLS, SSH) and never shrinks, and DEFLATE spends a
// millisecond on 64 KiB of it to learn that. So, a stream at a time: after
// tunnelPackMisses of its items in a row, of protocol.PackMinSize bytes or
// more, that packing did not shrink, its bytes go as they are for
// tunnelPackSkip bytes, and then packing is tried again. The minion does
// the same with what it sends.

const (
	tunnelPackMisses = 3
	tunnelPackSkip   = 1 << 20
	// tunnelPackIdle: what core keeps of a stream untouched that long (it
	// ended, or goes quiet) is let go, at most once a tunnelPackSweep.
	tunnelPackIdle  = 10 * time.Minute
	tunnelPackSweep = time.Minute
)

// tunnelPackKey is a stream of a session's tunnel.
type tunnelPackKey struct {
	session uuid.UUID
	task    [16]byte
	stream  uint64
}

// tunnelPackState is a stream whose bytes did not shrink lately: misses in
// a row, then skip bytes to send as they are.
type tunnelPackState struct {
	misses, skip int
	used         time.Time
}

// tunnelPacking is what core knows of its streams' packing.
type tunnelPacking struct {
	mu     sync.Mutex
	states map[tunnelPackKey]*tunnelPackState
	swept  time.Time
}

// packItem is item as core sends it to session's minion as the outbox adds
// it: a tunnel's bytes as their stream's packing goes, anything else as
// pack has it.
func (s *Service) packItem(session *models.MinionSession, item cbor.RawMessage) cbor.RawMessage {
	if session == nil || session.Compression == "" || protocol.ItemType(item) != protocol.ItemTunnelData {
		return pack(session, item)
	}
	if _, ok := sessionEnvelope(session); !ok {
		return item // a factory's envelope: it packs, core does not (packed)
	}
	key, ok := tunnelDataKey(session.SessionID, item)
	if !ok {
		return pack(session, item)
	}
	now := s.now()
	if s.tunnelPack.skipping(key, len(item), now) {
		return item
	}
	out := pack(session, item)
	if len(item) >= protocol.PackMinSize {
		s.tunnelPack.tried(key, protocol.ItemType(out) == protocol.ItemPacked, now)
	}
	return out
}

// tunnelDataKey is the stream a tunnel_data item is of.
func tunnelDataKey(session uuid.UUID, item cbor.RawMessage) (tunnelPackKey, bool) {
	var head struct {
		Task   []byte `cbor:"1,keyasint"`
		Stream uint64 `cbor:"2,keyasint"`
	}
	if protocol.UnmarshalCBOR(item, &head) != nil || len(head.Task) != len(tunnelPackKey{}.task) {
		return tunnelPackKey{}, false
	}
	key := tunnelPackKey{session: session, stream: head.Stream}
	copy(key.task[:], head.Task)
	return key, true
}

// skipping reports whether an item of size bytes of key's stream goes as it
// is, without trying: its skip lasts; size counts against it.
func (p *tunnelPacking) skipping(key tunnelPackKey, size int, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked(now)
	st := p.states[key]
	if st == nil || st.skip <= 0 {
		return false
	}
	st.skip -= size
	st.used = now
	return true
}

// tried notes how packing an item of key's stream went: one that shrank
// forgets the misses; tunnelPackMisses in a row start a skip.
func (p *tunnelPacking) tried(key tunnelPackKey, paid bool, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if paid {
		delete(p.states, key)
		return
	}
	if p.states == nil {
		p.states = map[tunnelPackKey]*tunnelPackState{}
	}
	st := p.states[key]
	if st == nil {
		st = &tunnelPackState{}
		p.states[key] = st
	}
	st.misses++
	st.used = now
	if st.misses >= tunnelPackMisses {
		st.misses, st.skip = 0, tunnelPackSkip
	}
}

// sweepLocked lets go of the streams untouched for tunnelPackIdle.
func (p *tunnelPacking) sweepLocked(now time.Time) {
	if now.Sub(p.swept) < tunnelPackSweep {
		return
	}
	p.swept = now
	for key, st := range p.states {
		if now.Sub(st.used) > tunnelPackIdle {
			delete(p.states, key)
		}
	}
}
