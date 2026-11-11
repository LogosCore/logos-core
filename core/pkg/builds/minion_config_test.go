package builds

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// configBob is BOB's schema for minions that read a minion config: its
// channel, connection parameters, fields and commands numbered.
func configBob() protocol.BuildSchema {
	sc := bobSchema()
	sc.MinionConfig = true
	sc.Channels[0].Num = 1
	k := &sc.Channels[0].Kinds[0]
	k.Fields = slices.Clone(k.Fields)
	k.Fields[0].Num, k.Fields[1].Num = 1, 2
	sc.Fields = slices.Clone(sc.Fields)
	sc.Fields[0].Num, sc.Fields[1].Num, sc.Fields[2].Num = 1, 2, 3 // the badge stays BOB's
	sc.Commands = slices.Clone(sc.Commands)
	for i := range sc.Commands {
		sc.Commands[i].Op = i + 1
	}
	return sc
}

// numberedHTTP is the HTTP channel's schema with its fields numbered.
func numberedHTTP() *protocol.ConnectionSchema {
	cs := httpSchema()
	k := cs.Kinds[protocol.KindReverseCall]
	k.Fields[0].Num, k.Fields[1].Num = 1, 2
	cs.Kinds[protocol.KindReverseCall] = k
	return cs
}

// configRig is a rig whose BOB reads minion configs, through an HTTP
// channel that numbers its fields.
func configRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	sc := configBob()
	r.reg.schemas["bob"] = &models.ModuleBuildSchema{BuildSchema: sc}
	inst := bobInstance("bob-1", 1)
	inst.Build = &models.ModuleBuildSchema{BuildSchema: sc}
	r.reg.setActive(inst)
	r.chs.schemas["http"] = numberedHTTP()
	return r
}

func TestOfferWithMinionConfig(t *testing.T) {
	r := configRig(t)
	b := r.queue(t, func(in *BuildInput) {
		in.Values["greeting"] = "hi"
		in.Values["kill_date"] = "2026-12-01T00:00:00Z"
	})
	r.svc.Tick(context.Background())
	got := r.get(t, b.BuildID.String(), models.BuildStateBuilding)
	calls := r.rpc.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
	req := calls[0].payload.(protocol.BuildRequest)
	c, err := protocol.ReadMinionConfig(req.MinionConfig)
	if err != nil {
		t.Fatalf("the request's minion config: %v", err)
	}
	if want := strings.ReplaceAll(b.BuildID.String(), "-", ""); hex.EncodeToString(c.Build) != want {
		t.Errorf("build %x", c.Build)
	}
	if !slices.Equal(c.Ops, []uint64{1, 2, 3, 4}) { // sleep, exit, hello, ls
		t.Errorf("ops %v", c.Ops)
	}
	if v, _ := protocol.ArgString(c.Values, 1); v != "hi" {
		t.Errorf("greeting %q", v)
	}
	if v, _ := protocol.ArgInt(c.Values, 2); v != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("kill date %d", v)
	}
	if v, _ := protocol.ArgStrings(c.Values, 3); !slices.Equal(v, []string{"jab"}) {
		t.Errorf("punches %v", v)
	}
	if len(c.Connections) != 1 {
		t.Fatalf("connections %+v", c.Connections)
	}
	h := c.Connections[0]
	if h.Channel != 1 || h.Version != 2 || h.Kind != protocol.KindNumReverseCall {
		t.Errorf("connection %+v", h)
	}
	if v, _ := protocol.ArgString(h.Params, 1); v != "https://edge.example.com" {
		t.Errorf("base url %q", v)
	}
	if v, _ := protocol.ArgDuration(h.Params, 2); v != 40*time.Second { // the channel's default
		t.Errorf("request timeout %s", v)
	}
	if v, _ := protocol.ArgString(h.Values, 1); v != "POST" {
		t.Errorf("method %q", v)
	}
	if v, _ := protocol.ArgString(h.Values, 2); v != "/cdn/" {
		t.Errorf("path prefix %q", v)
	}
	// The build keeps the request with it, as the factory got it.
	if got.Request != string(calls[0].raw) || !strings.Contains(got.Request, `"minion_config":"`) {
		t.Errorf("the build keeps %s", got.Request)
	}
}

func TestMinionConfigRefusedAsTheBuildIsMade(t *testing.T) {
	r := configRig(t)
	r.chs.schemas["http"] = httpSchema() // numbers none of its fields
	_, err := r.svc.CreateBuild(context.Background(), opID, r.input(nil))
	var v *ValidationError
	if !errors.As(err, &v) || !strings.Contains(v.Problems["minion_config"], `channel "http" numbers no fields of its reverse-call connections`) {
		t.Fatalf("got %v %v", err, problemsOf(err))
	}
	if len(r.builds.rows) != 0 {
		t.Error("a build was kept")
	}
}

func TestMinionConfigFailsTheOffer(t *testing.T) {
	r := configRig(t)
	b := r.queue(t, nil)
	// The channel follows another schema version before the build goes out.
	moved := numberedHTTP()
	moved.Version = 3
	r.chs.schemas["http"] = moved
	r.svc.Tick(context.Background())
	got := r.get(t, b.BuildID.String(), models.BuildStateFailed)
	if got.Error == nil || got.Error.Code != models.BuildErrorMinionConfig ||
		!strings.Contains(got.Error.Message, `channel "http"'s connection schema version 2 is not known`) {
		t.Fatalf("error %+v", got.Error)
	}
	if calls := r.rpc.recorded(); len(calls) != 0 {
		t.Errorf("offered anyway: %+v", calls)
	}
}

// A factory whose minions read none gets no config.
func TestNoMinionConfigForOthers(t *testing.T) {
	r := newRig(t)
	r.queue(t, nil)
	r.svc.Tick(context.Background())
	if req := r.rpc.recorded()[0].payload.(protocol.BuildRequest); req.MinionConfig != nil {
		t.Errorf("a config for minions that read none: %x", req.MinionConfig)
	}
}
