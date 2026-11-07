package miniontunnels

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// A running tunnel lists its open streams and its latest KeptStreams ended
// ones, the latest first, narrowed and searched in the lists' language; the
// sweep keeps its bytes so far with its port. Once it closed it lists none,
// and its task keeps what it came to.
func TestTunnelKeeps(t *testing.T) {
	r := newRig(t, 0, func(c *Config) { c.Sweep = 20 * time.Millisecond })
	r.tasks.ack(r.task)
	startMinion(t, r)
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	defer c.Close()
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	// A thousand bytes there and back, the stream left open.
	if _, err := c.Write(make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(c, make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	// Ended streams past the open one, KeptStreams of them and more: the
	// oldest give way.
	const extra = 205
	last := uint64(1 + KeptStreams + extra)
	r.svc.mu.Lock()
	tun := r.svc.tunnels[r.task]
	for i := uint64(2); i <= last; i++ {
		rec := models.MinionTunnelStream{TaskID: r.task, Stream: i, Client: "127.0.0.1:5000", Host: fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			Port: 445, End: models.TunnelEndDone}
		rec.SearchText = models.TunnelStreamSearchText(&rec)
		tun.ended.add(rec)
	}
	r.svc.mu.Unlock()

	ctx := context.Background()
	all, _ := r.svc.Streams(ctx, r.task, StreamFilter{}, 0, -1)
	if n := len(all); n != KeptStreams+1 || all[0].Stream != last || all[n-2].Stream != last-KeptStreams+1 || all[n-1].Stream != 1 {
		t.Fatalf("listed %d: %d first, then to %d and %d", n, all[0].Stream, all[n-2].Stream, all[n-1].Stream)
	}
	open, _ := r.svc.Streams(ctx, r.task, StreamFilter{State: StreamsOpen}, 0, 50)
	if len(open) != 1 || open[0].Stream != 1 || open[0].Up != 1000 || open[0].Down != 1000 || open[0].EndedAt != nil {
		t.Errorf("open: %+v", open)
	}
	page, _ := r.svc.Streams(ctx, r.task, StreamFilter{State: StreamsEnded}, 500, 50)
	if len(page) != 50 || page[0].Stream != 499 || page[49].Stream != 450 {
		t.Errorf("ended, before 500: %d", len(page))
	}
	// 10.0.3.7 is stream 775's; 838 to 847's, 10.0.3.70 to 79, have it too.
	if got, _ := r.svc.Streams(ctx, r.task, StreamFilter{Search: " 10.0.3.7 "}, 0, 50); len(got) != 11 {
		t.Errorf("a part of the destination: %d", len(got))
	}
	if got, _ := r.svc.Streams(ctx, r.task, StreamFilter{Search: `"10.0.3.7"`}, 0, 50); len(got) != 1 || got[0].Stream != 775 {
		t.Errorf("a whole one: %+v", got)
	}
	eventually(t, "the sweep keeps its bytes", func() bool {
		p, err := r.ports.FindByTaskID(ctx, r.task)
		return err == nil && p.Up == 1000 && p.Down == 1000
	})

	r.svc.Close(ctx, r.task)
	if got, _ := r.svc.Streams(ctx, r.task, StreamFilter{}, 0, -1); len(got) != 0 {
		t.Errorf("listed once it closed: %d", len(got))
	}
	if tot := r.tasks.tunnel(r.task); tot == nil || *tot != (models.MinionTaskTunnel{Streams: 1, Up: 1000, Down: 1000}) {
		t.Errorf("its totals: %+v", tot)
	}
}
