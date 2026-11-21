package minionsync

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A hello's roundtrip_time is kept on the session's hello: a rehello that
// says one replaces it, one without keeps the last, and a session whose
// hello said none has none.
func TestHelloRoundtripTime(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	say := func(id uuid.UUID, h protocol.HelloItem) []byte {
		t.Helper()
		reply, err := r.svc.Handle(ctx, id.String(), "", sealedMinionMessage(t, r.keys, id, nil, h))
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	ms := func(n uint64) *uint64 { return &n }
	roundtrip := func(sid uuid.UUID) any {
		if rt := r.sessionRow(t, sid).Hello.RoundtripTime; rt != nil {
			return *rt
		}
		return nil
	}

	h := hello("ws01")
	h.RoundtripTime = ms(142)
	sid := sessionID(t, r.open(t, say(r.build.BuildID, h)))
	if got := roundtrip(sid); got != 142 {
		t.Fatalf("after the hello: %v", got)
	}
	// A rehello without one keeps it.
	say(sid, hello("ws01"))
	if got := roundtrip(sid); got != 142 {
		t.Fatalf("after a rehello without: %v", got)
	}
	// One with one replaces it; 0 (under a millisecond) is a round trip.
	re := hello("ws01")
	re.RoundtripTime = ms(0)
	say(sid, re)
	if got := roundtrip(sid); got != 0 {
		t.Fatalf("after a rehello with 0: %v", got)
	}

	// A minion that never says one: none.
	other := sessionID(t, r.open(t, say(r.build.BuildID, hello("ws02"))))
	if got := roundtrip(other); got != nil {
		t.Fatalf("a hello without one: %v", got)
	}
}
