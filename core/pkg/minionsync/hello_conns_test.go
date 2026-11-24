package minionsync

import (
	"context"
	"slices"
	"testing"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// errCodeIn is the code of the first err among a reply's items; 0 for none.
func errCodeIn(t *testing.T, msg protocol.CoreMessage) int {
	t.Helper()
	for _, raw := range msg.Items {
		if protocol.ItemType(raw) == protocol.ItemErr {
			var e protocol.ErrItem
			if err := protocol.UnmarshalCBOR(raw, &e); err != nil {
				t.Fatal(err)
			}
			return e.Code
		}
	}
	return 0
}

// A hello that says its own connections: core uses them for the session in
// place of the build's (conn, its kind and size limit, the carrier
// guess), a rehello without them keeps them, one with them replaces them;
// a channel core does not know, or a kind it does not carry, is refused.
func TestHelloOwnConnections(t *testing.T) {
	r := newRig(t)
	r.build.Connections.Items = []protocol.BuildConnection{endpoint(protocol.KindReverseCall, "http")}
	r.svc.WithChannelSchemas(fakeSchemas{
		"http": {Kinds: map[string]protocol.ConnectionKind{protocol.KindReverseCall: {}, protocol.KindReversePersistent: {}}},
		"dns":  {Kinds: map[string]protocol.ConnectionKind{protocol.KindReversePersistent: {}}},
	})
	ctx := ViaChannel(context.Background(), "dns")
	own := []protocol.HelloConnection{
		{Channel: "http", Kind: protocol.KindReverseCall},
		{Channel: "dns", Kind: protocol.KindReversePersistent, MaxMessage: 2048},
	}

	// No conn said: the carrier (a dns socket) fits the second of its own.
	h := hello("ws01")
	h.Connections = own
	_, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, h)
	s := r.sessionRow(t, sid)
	if s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent || s.ConnCount != 2 ||
		s.MessageLimit() != 2048 || len(s.Connections) != 2 {
		t.Fatalf("session conn %d %q, count %d, limit %d, own %d", s.Conn, s.ConnKind, s.ConnCount,
			s.MessageLimit(), len(s.Connections))
	}

	// A rehello without connections keeps them; conn is a place in them.
	re := hello("ws01")
	re.Conn = 1
	r.say(t, ctx, c, sid, re)
	if s = r.sessionRow(t, sid); len(s.Connections) != 2 || s.Conn != 1 || s.ConnKind != protocol.KindReverseCall {
		t.Fatalf("after a rehello without: own %d, conn %d %q", len(s.Connections), s.Conn, s.ConnKind)
	}
	// A conn_update is a place in them too.
	r.say(t, ctx, c, sid, protocol.ConnUpdateItem{T: protocol.ItemConnUpdate, Conn: 2})
	if s = r.sessionRow(t, sid); s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent {
		t.Fatalf("after a conn_update: conn %d %q", s.Conn, s.ConnKind)
	}
	// A conn_update past them is refused, and nothing is recorded.
	if code := errCodeIn(t, r.openUnder(t, sid, r.keys, r.say(t, ctx, c, sid, protocol.ConnUpdateItem{T: protocol.ItemConnUpdate, Conn: 3}))); code != protocol.ErrItemBadMessage {
		t.Fatalf("a conn_update past the list: err %d", code)
	}
	if s = r.sessionRow(t, sid); s.Conn != 2 || len(s.ConnChanges) != 1 {
		t.Fatalf("after a refused conn_update: conn %d, changes %d", s.Conn, len(s.ConnChanges))
	}
	// One with connections replaces them.
	re = hello("ws01")
	re.Conn = 1
	re.Connections = own[1:]
	r.say(t, ctx, c, sid, re)
	if s = r.sessionRow(t, sid); len(s.Connections) != 1 || s.ConnCount != 1 || s.Conn != 1 ||
		s.ConnKind != protocol.KindReversePersistent || s.Connections[0].Channel != "dns" {
		t.Fatalf("after a rehello with: own %+v, count %d, conn %d %q", s.Connections, s.ConnCount, s.Conn, s.ConnKind)
	}

	// Refused: a channel core does not know, a kind the channel does not
	// carry.
	for name, bad := range map[string]protocol.HelloConnection{
		"unknown channel":  {Channel: "smoke", Kind: protocol.KindReverseCall},
		"kind not carried": {Channel: "dns", Kind: protocol.KindReverseCall},
	} {
		h := hello("ws02")
		h.Connections = []protocol.HelloConnection{bad}
		_, c := newSock("ws")
		if code := errCodeIn(t, r.open(t, r.say(t, ctx, c, r.build.BuildID, h))); code != protocol.ErrItemBadMessage {
			t.Errorf("%s: err %d", name, code)
		}
	}
}

// Without connections of its own a hello is matched to the build's, as
// before.
func TestHelloWithoutOwnConnections(t *testing.T) {
	r := newRig(t)
	r.build.Connections.Items = []protocol.BuildConnection{endpoint(protocol.KindReverseCall, "http")}
	reply, err := r.svc.Handle(context.Background(), r.build.BuildID.String(), "", sealedMinionMessage(t, r.keys, r.build.BuildID, nil, hello("ws01")))
	if err != nil {
		t.Fatal(err)
	}
	sid := sessionID(t, r.open(t, reply))
	if s := r.sessionRow(t, sid); s.Connections != nil || s.Conn != 1 || s.ConnCount != 1 || s.ConnKind != protocol.KindReverseCall {
		t.Fatalf("session own %v, conn %d %q, count %d", s.Connections, s.Conn, s.ConnKind, s.ConnCount)
	}
}

// A connection_update replaces the session's connections, a build's ones
// too: the connection in use keeps its place and takes its new details,
// no move is recorded; a list too short for it, or one naming a channel
// core does not know, is refused and changes nothing; a conn_update before
// it in the same message moves first, so a shorter list passes.
func TestConnectionUpdate(t *testing.T) {
	r := newRig(t)
	r.build.Connections.Items = []protocol.BuildConnection{endpoint(protocol.KindReverseCall, "http"),
		endpoint(protocol.KindReversePersistent, "dns")}
	r.svc.WithChannelSchemas(fakeSchemas{
		"http": {Kinds: map[string]protocol.ConnectionKind{protocol.KindReverseCall: {}, protocol.KindReversePersistent: {}}},
		"dns":  {Kinds: map[string]protocol.ConnectionKind{protocol.KindReversePersistent: {}}},
	})
	ctx := ViaChannel(context.Background(), "dns")
	h := hello("ws01")
	h.Conn = 2
	_, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, h)
	if s := r.sessionRow(t, sid); s.Connections != nil || s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent {
		t.Fatalf("build's: own %v, conn %d %q", s.Connections, s.Conn, s.ConnKind)
	}
	update := func(conns ...protocol.HelloConnection) protocol.ConnectionUpdateItem {
		return protocol.ConnectionUpdateItem{T: protocol.ItemConnectionUpdate, Connections: conns}
	}
	say := func(items ...any) int {
		return errCodeIn(t, r.openUnder(t, sid, r.keys, r.say(t, ctx, c, sid, items...)))
	}

	// Replaced: the one in use (2) keeps its place, with its new details.
	if code := say(update(protocol.HelloConnection{Channel: "http", Kind: protocol.KindReverseCall},
		protocol.HelloConnection{Channel: "dns", Kind: protocol.KindReversePersistent, MaxMessage: 1024})); code != 0 {
		t.Fatalf("err %d", code)
	}
	s := r.sessionRow(t, sid)
	if len(s.Connections) != 2 || s.ConnCount != 2 || s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent ||
		s.MessageLimit() != 1024 || len(s.ConnChanges) != 0 {
		t.Fatalf("replaced: own %d, count %d, conn %d %q, limit %d, changes %d", len(s.Connections), s.ConnCount, s.Conn,
			s.ConnKind, s.MessageLimit(), len(s.ConnChanges))
	}

	// Refused, nothing changed: too short for the one in use, an unknown
	// channel.
	for name, u := range map[string]protocol.ConnectionUpdateItem{
		"too short":       update(protocol.HelloConnection{Channel: "http", Kind: protocol.KindReverseCall}),
		"unknown channel": update(protocol.HelloConnection{Channel: "smoke", Kind: protocol.KindReverseCall}),
	} {
		if code := say(u); code != protocol.ErrItemBadMessage {
			t.Errorf("%s: err %d", name, code)
		}
	}
	if s = r.sessionRow(t, sid); len(s.Connections) != 2 || s.MessageLimit() != 1024 {
		t.Fatalf("after refusals: own %d, limit %d", len(s.Connections), s.MessageLimit())
	}

	// The move first, then the shorter list, in one message: items are
	// taken in their order.
	if code := say(protocol.ConnUpdateItem{T: protocol.ItemConnUpdate, Conn: 1},
		update(protocol.HelloConnection{Channel: "http", Kind: protocol.KindReverseCall})); code != 0 {
		t.Fatalf("move then shorten: err %d", code)
	}
	if s = r.sessionRow(t, sid); len(s.Connections) != 1 || s.Conn != 1 || s.ConnKind != protocol.KindReverseCall {
		t.Fatalf("after move then shorten: own %d, conn %d %q", len(s.Connections), s.Conn, s.ConnKind)
	}
}

// A connection taken out (remove_connection): the update says its place
// before (removed), and core moves the place in use with the list,
// recording no move; an update that removed the one in use
// is refused.
func TestConnectionUpdateRemoved(t *testing.T) {
	r := newRig(t)
	r.build.Connections.Items = []protocol.BuildConnection{endpoint(protocol.KindReverseCall, "http"),
		endpoint(protocol.KindReversePersistent, "dns"), endpoint(protocol.KindReversePersistent, "http")}
	r.svc.WithChannelSchemas(fakeSchemas{
		"http": {Kinds: map[string]protocol.ConnectionKind{protocol.KindReverseCall: {}, protocol.KindReversePersistent: {}}},
		"dns":  {Kinds: map[string]protocol.ConnectionKind{protocol.KindReversePersistent: {}}},
	})
	ctx := ViaChannel(context.Background(), "http")
	h := hello("ws01")
	h.Conn = 3
	_, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, h)
	update := func(removed uint64, conns ...protocol.HelloConnection) int {
		t.Helper()
		u := protocol.ConnectionUpdateItem{T: protocol.ItemConnectionUpdate, Connections: conns, Removed: removed}
		return errCodeIn(t, r.openUnder(t, sid, r.keys, r.say(t, ctx, c, sid, u)))
	}
	dns := protocol.HelloConnection{Channel: "dns", Kind: protocol.KindReversePersistent}
	ws := protocol.HelloConnection{Channel: "http", Kind: protocol.KindReversePersistent}

	// The one in use cannot have gone.
	if code := update(3, dns, ws); code != protocol.ErrItemBadMessage {
		t.Fatalf("the one in use removed: err %d", code)
	}
	// The first taken out: the one in use moves up.
	if code := update(1, dns, ws); code != 0 {
		t.Fatalf("err %d", code)
	}
	s := r.sessionRow(t, sid)
	var channels []string
	for _, sc := range s.Connections {
		channels = append(channels, sc.Channel)
	}
	if !slices.Equal(channels, []string{"dns", "http"}) || s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent ||
		len(s.ConnChanges) != 0 {
		t.Fatalf("channels %v, conn %d %q, changes %d", channels, s.Conn, s.ConnKind, len(s.ConnChanges))
	}
}
