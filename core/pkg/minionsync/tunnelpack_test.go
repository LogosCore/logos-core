package minionsync

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func tunnelData(t *testing.T, task uuid.UUID, stream uint64, data []byte) cbor.RawMessage {
	t.Helper()
	item, err := protocol.MarshalCBOR(protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: task[:], Stream: stream, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// A stream whose bytes do not shrink (encrypted already) goes unpacked
// without trying, three misses in a row on, for a MiB; then packing is
// tried again. A stream's bytes that shrink go packed, and forget its
// misses; another stream's go their own way.
func TestTunnelPackSkipsWhatDoesNotShrink(t *testing.T) {
	r := newRig(t)
	now := time.Unix(1000, 0)
	r.svc.now = func() time.Time { return now }
	session := &models.MinionSession{SessionID: uuid.New(), Compression: protocol.CompressionDeflate}
	task := uuid.New()
	key, _ := tunnelDataKey(session.SessionID, tunnelData(t, task, 1, nil))
	state := func() (misses, skip int, ok bool) {
		r.svc.tunnelPack.mu.Lock()
		defer r.svc.tunnelPack.mu.Unlock()
		st := r.svc.tunnelPack.states[key]
		if st == nil {
			return 0, 0, false
		}
		return st.misses, st.skip, true
	}
	tls := tunnelData(t, task, 1, randomBytes(64<<10))
	for i := range tunnelPackMisses {
		if out := r.svc.packItem(session, tls); !bytes.Equal(out, tls) {
			t.Fatalf("random bytes packed (%d)", i)
		}
	}
	if misses, skip, _ := state(); misses != 0 || skip != tunnelPackSkip {
		t.Fatalf("after %d misses: misses %d, skip %d", tunnelPackMisses, misses, skip)
	}
	// Another stream's items change nothing of it; its own small one counts
	// its bytes, as every item it sends meanwhile.
	r.svc.packItem(session, tunnelData(t, task, 2, randomBytes(64<<10)))
	small := tunnelData(t, task, 1, []byte("hi"))
	r.svc.packItem(session, small)
	if misses, skip, _ := state(); misses != 0 || skip != tunnelPackSkip-len(small) {
		t.Fatalf("misses %d, skip %d", misses, skip)
	}
	// The skip lasts a MiB of items, which go as they are.
	sent := len(small)
	for sent < tunnelPackSkip {
		if out := r.svc.packItem(session, tls); !bytes.Equal(out, tls) {
			t.Fatal("packed while skipping")
		}
		sent += len(tls)
	}
	if _, skip, _ := state(); skip > 0 {
		t.Fatalf("skip %d left after %d bytes", skip, sent)
	}
	// Then packing is tried again: a miss counts.
	r.svc.packItem(session, tls)
	if misses, skip, _ := state(); misses != 1 || skip > 0 {
		t.Fatalf("tried again: misses %d, skip %d", misses, skip)
	}
	// Bytes that shrink go packed, and the stream's misses are forgotten.
	text := tunnelData(t, task, 1, bytes.Repeat([]byte("GET / HTTP/1.1\r\n"), 64))
	if out := r.svc.packItem(session, text); protocol.ItemType(out) != protocol.ItemPacked {
		t.Fatal("text not packed")
	}
	if _, _, ok := state(); ok {
		t.Fatal("misses kept after a packing that paid")
	}
	// What core keeps of a stream untouched for tunnelPackIdle goes.
	for range tunnelPackMisses {
		r.svc.packItem(session, tls)
	}
	now = now.Add(tunnelPackIdle + tunnelPackSweep + time.Second)
	r.svc.packItem(session, tunnelData(t, task, 3, []byte("hi")))
	if _, _, ok := state(); ok {
		t.Fatal("an idle stream's state kept")
	}
}

// Sealed, a tunnel's bytes stay as the outbox left them: not packed (or
// tried) a second time; other items go packed where that pays.
func TestPackMessageLeavesTunnelData(t *testing.T) {
	on := &models.MinionSession{Compression: protocol.CompressionDeflate}
	text := tunnelData(t, uuid.New(), 1, bytes.Repeat([]byte("hello "), 200))
	out := packMessage(on, []cbor.RawMessage{text})
	if !bytes.Equal(out[0], text) {
		t.Fatal("a tunnel's bytes packed again as the message was sealed")
	}
}
