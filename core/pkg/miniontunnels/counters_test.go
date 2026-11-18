package miniontunnels

import (
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A stream counts what its link cost: a send again from the ack, a probe of
// a shut window, never a first send; its rooms each way are the minion's
// past its next and core's window.
func TestStreamCountsResendsAndProbes(t *testing.T) {
	now := time.Now()
	st := upStream(100, 1000)
	d := st.dataItem(now, 1<<20, time.Second)
	st.sent(*d, now)
	if st.rec.Resends != 0 || st.rec.Probes != 0 {
		t.Fatalf("a first send counted: resends %d probes %d", st.rec.Resends, st.rec.Probes)
	}
	d = st.dataItem(now.Add(time.Second), 1<<20, time.Second)
	if d == nil || d.Off != 0 {
		t.Fatalf("no send again: %+v", d)
	}
	st.sent(*d, now.Add(time.Second))
	if st.rec.Resends != 1 || st.rec.Probes != 0 {
		t.Fatalf("a send again: resends %d probes %d", st.rec.Resends, st.rec.Probes)
	}

	shut := upStream(100, 0)
	shut.upSentAt = now
	p := shut.dataItem(now.Add(time.Second), 1<<20, time.Second)
	if p == nil || len(p.Data) != 1 {
		t.Fatalf("no probe: %+v", p)
	}
	shut.sent(*p, now.Add(time.Second))
	if shut.rec.Probes != 1 || shut.rec.Resends != 0 {
		t.Fatalf("a probe: resends %d probes %d", shut.rec.Resends, shut.rec.Probes)
	}

	st.upBase, st.upLimit, st.window = 200, 1000, 3<<20
	if up, down := st.windows(); up != 800 || down != 3<<20 {
		t.Fatalf("windows %d up, %d down", up, down)
	}
	st.upLimit = 100
	if up, _ := st.windows(); up != 0 {
		t.Fatalf("a room behind the minion's next: %d", up)
	}
}

// A ctl that goes again, its ack not come in the resend's time, counts as a
// send again; its first send does not.
func TestStreamCountsCtlResends(t *testing.T) {
	now := time.Now()
	st := upStream(0, 0)
	st.ctlOut = []protocol.TunnelCtlItem{{T: protocol.ItemTunnelCtl, Task: st.t.task[:], Stream: st.id, Seq: 0}}
	f := &filler{room: 1 << 20, most: 100, add: func(cbor.RawMessage) bool { return true }}
	if !st.putCtls(f, now, time.Second) || st.rec.Resends != 0 {
		t.Fatalf("its first send: resends %d", st.rec.Resends)
	}
	if st.putCtls(f, now.Add(time.Second/2), time.Second) {
		t.Fatal("sent again before its time")
	}
	if !st.putCtls(f, now.Add(time.Second), time.Second) || st.rec.Resends != 1 {
		t.Fatalf("sent again: resends %d", st.rec.Resends)
	}
}
