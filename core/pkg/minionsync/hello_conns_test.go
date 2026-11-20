package minionsync

import (
	"context"
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
// place of the build's (conn, its kind, name and size limit, the carrier
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
		{Channel: "http", Kind: protocol.KindReverseCall, Name: "front"},
		{Channel: "dns", Kind: protocol.KindReversePersistent, MaxMessage: 2048},
	}

	// No conn said: the carrier (a dns socket) fits the second of its own.
	h := hello("ws01")
	h.Connections = own
	_, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, h)
	s := r.sessionRow(t, sid)
	if s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent || s.ConnName != "dns-2" || s.ConnCount != 2 ||
		s.MessageLimit() != 2048 || len(s.Connections) != 2 {
		t.Fatalf("session conn %d %q %q, count %d, limit %d, own %d", s.Conn, s.ConnKind, s.ConnName, s.ConnCount,
			s.MessageLimit(), len(s.Connections))
	}

	// A rehello without connections keeps them; conn is a place in them.
	re := hello("ws01")
	re.Conn = 1
	r.say(t, ctx, c, sid, re)
	if s = r.sessionRow(t, sid); len(s.Connections) != 2 || s.Conn != 1 || s.ConnName != "front" {
		t.Fatalf("after a rehello without: own %d, conn %d %q", len(s.Connections), s.Conn, s.ConnName)
	}
	// A conn_update is a place in them too.
	r.say(t, ctx, c, sid, protocol.ConnUpdateItem{T: protocol.ItemConnUpdate, Conn: 2})
	if s = r.sessionRow(t, sid); s.Conn != 2 || s.ConnName != "dns-2" {
		t.Fatalf("after a conn_update: conn %d %q", s.Conn, s.ConnName)
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
	if s = r.sessionRow(t, sid); len(s.Connections) != 1 || s.ConnCount != 1 || s.Conn != 1 || s.ConnName != "dns-1" {
		t.Fatalf("after a rehello with: own %d, count %d, conn %d %q", len(s.Connections), s.ConnCount, s.Conn, s.ConnName)
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
	if s := r.sessionRow(t, sid); s.Connections != nil || s.Conn != 1 || s.ConnCount != 1 || s.ConnName != "http-reverse-call" {
		t.Fatalf("session own %v, conn %d %q, count %d", s.Connections, s.Conn, s.ConnName, s.ConnCount)
	}
}
