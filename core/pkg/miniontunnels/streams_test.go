package miniontunnels

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func upStream(bytesRead int, limit uint64) *stream {
	return &stream{t: &tunnel{task: uuid.New()}, id: 1, opened: true, up: bytes.Repeat([]byte{'u'}, bytesRead), upLimit: limit}
}

// What went and was not acked goes again from the ack once the stream has
// been quiet for the resend's time; before that, what has not gone.
func TestDataResendsFromTheAck(t *testing.T) {
	now := time.Now()
	st := upStream(100, 1000)
	st.upSent, st.upSentAt = 50, now
	if d := st.dataItem(now, 1<<20, time.Second); d == nil || d.Off != 50 || len(d.Data) != 50 {
		t.Fatalf("not yet quiet: %+v", d)
	}
	if d := st.dataItem(now.Add(time.Second), 1<<20, time.Second); d == nil || d.Off != 0 || len(d.Data) != 100 {
		t.Fatalf("quiet: %+v", d)
	}
}

// Nothing goes past the minion's room but, once the window has been shut
// for the resend's time with bytes waiting, a probe: one byte past it, for
// the minion's answer to say its room again.
func TestDataProbesAShutWindow(t *testing.T) {
	now := time.Now()
	st := upStream(100, 0)
	st.upSentAt = now
	if d := st.dataItem(now, 1<<20, time.Second); d != nil {
		t.Fatalf("past a shut window: %+v", d)
	}
	d := st.dataItem(now.Add(time.Second), 1<<20, time.Second)
	if d == nil || d.Off != 0 || len(d.Data) != 1 || d.EOF {
		t.Fatalf("no probe: %+v", d)
	}
	st.sent(*d, now.Add(time.Second))
	if d := st.dataItem(now.Add(1500*time.Millisecond), 1<<20, time.Second); d != nil {
		t.Fatalf("a probe again at once: %+v", d)
	}
	// With nothing waiting, nothing probes.
	idle := upStream(0, 0)
	if d := idle.dataItem(now.Add(time.Hour), 1<<20, time.Second); d != nil {
		t.Fatalf("an idle stream probes: %+v", d)
	}
}

// The eof goes once every byte has, one place past them (with the last
// bytes when they fit, alone otherwise), needs no room, and goes again
// unacked; a piece smaller than minPiece waits when the stream has more.
func TestDataEOF(t *testing.T) {
	now := time.Now()
	st := upStream(10, 10)
	st.upEOF = true
	d := st.dataItem(now, 1<<20, time.Second)
	if d == nil || d.Off != 0 || len(d.Data) != 10 || !d.EOF || d.End() != 11 {
		t.Fatalf("bytes and eof: %+v", d)
	}
	st.sent(*d, now)
	if d := st.dataItem(now, 1<<20, time.Second); d != nil {
		t.Fatalf("the eof twice at once: %+v", d)
	}
	if d := st.dataItem(now.Add(time.Second), 1<<20, time.Second); d == nil || !d.EOF || d.Off != 0 {
		t.Fatalf("the eof unacked, not again: %+v", d)
	}
	alone := upStream(10, 10)
	alone.upSent, alone.upEOF, alone.upSentAt = 10, true, now
	if d := alone.dataItem(now, 1<<20, time.Second); d == nil || len(d.Data) != 0 || !d.EOF || d.Off != 10 {
		t.Fatalf("the eof alone: %+v", d)
	}
	big := upStream(5000, 5000)
	if d := big.dataItem(now, minPiece-1, time.Second); d != nil {
		t.Fatalf("crumbs: %+v", d)
	}
	if d := big.dataItem(now, 2000, time.Second); d == nil || len(d.Data) != 2000 || d.EOF {
		t.Fatalf("a piece in the room: %+v", d)
	}
	if d := big.dataItem(now, 1<<20, time.Second); d == nil || len(d.Data) != 5000 {
		t.Fatalf("all of it: %+v", d)
	}
	huge := upStream(protocol.MaxTunnelData+5, 1<<20)
	if d := huge.dataItem(now, 1<<20, time.Second); d == nil || len(d.Data) != protocol.MaxTunnelData {
		t.Fatalf("past 64 KiB: %+v", d)
	}
}
