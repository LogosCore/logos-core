package resolver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

type connSession struct {
	repository.IMinionSessionRepository
	s *models.MinionSession
}

func (o connSession) FindBySessionID(context.Context, uuid.UUID) (*models.MinionSession, error) {
	return o.s, nil
}

type buildSchemas map[string]*models.ModuleBuildSchema

func (b buildSchemas) LatestBuildSchemas(context.Context) (map[string]*models.ModuleBuildSchema, error) {
	return b, nil
}

// A session whose hello said its own connections: their factory values,
// which come by number, are named by the factory's build schema; a number
// it does not give stays.
func TestSessionOwnConnectionValuesNamed(t *testing.T) {
	sid := uuid.New()
	sess := &models.MinionSession{SessionID: sid, ModuleName: "bob", Connections: []models.MinionSessionConnSpec{{
		Channel: "http", Kind: protocol.KindReverseCall, Name: "front",
		Params: map[string]any{"url": "https://x"}, Values: map[string]any{"1": "POST", "9": "kept"},
	}}}
	schema := &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{Channels: []protocol.BuildChannel{{
		ModuleName: "http", Kinds: []protocol.BuildKind{{Kind: protocol.KindReverseCall,
			Fields: []protocol.FieldSpec{{Key: "method", Num: 1, Type: protocol.FieldString}}}},
	}}}}
	r := &minionSessionResolver{sessions: connSession{s: sess}, schemas: buildSchemas{"bob": schema}}
	out, err := r.Connections(context.Background(), &model.MinionSession{ID: sid.String()})
	if err != nil || len(out) != 1 {
		t.Fatalf("%v %d", err, len(out))
	}
	v, _ := out[0].Values.(map[string]any)
	p, _ := out[0].Params.(map[string]any)
	if v["method"] != "POST" || v["9"] != "kept" || v["1"] != nil || p["url"] != "https://x" {
		t.Fatalf("values %v params %v", v, out[0].Params)
	}
}

// A minion says its connections in their types' CBOR forms: core shows them,
// and an edit's form starts from them, as JSON values (a duration's text),
// which a connection param takes again.
func TestSessionOwnConnectionValuesAsJSON(t *testing.T) {
	sid := uuid.New()
	sess := &models.MinionSession{SessionID: sid, ModuleName: "bob", Connections: []models.MinionSessionConnSpec{{
		Channel: "http", Kind: protocol.KindReverseCall, Name: "front",
		Params: map[string]any{"base_url": "http://x", "checkin_interval": uint64(1000), "request_timeout": int64(40000)},
		Values: map[string]any{"1": "/new", "2": uint64(90000)},
	}}}
	schema := &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{Channels: []protocol.BuildChannel{{
		ModuleName: "http", Kinds: []protocol.BuildKind{{Kind: protocol.KindReverseCall, Fields: []protocol.FieldSpec{
			{Key: "path_prefix", Num: 1, Type: protocol.FieldString}, {Key: "pause", Num: 2, Type: protocol.FieldDuration}}}},
	}}}}
	channels := fakeChannelSchemas{"http": {Kinds: map[string]protocol.ConnectionKind{protocol.KindReverseCall: {Fields: []protocol.FieldSpec{
		{Key: "base_url", Num: 1, Type: protocol.FieldString}, {Key: "checkin_interval", Num: 5, Type: protocol.FieldDuration},
		{Key: "request_timeout", Num: 10, Type: protocol.FieldDuration}}}}}}
	r := &minionSessionResolver{sessions: connSession{s: sess}, schemas: buildSchemas{"bob": schema}, channels: channels}
	out, err := r.Connections(context.Background(), &model.MinionSession{ID: sid.String()})
	if err != nil || len(out) != 1 {
		t.Fatalf("%v %d", err, len(out))
	}
	p, _ := out[0].Params.(map[string]any)
	v, _ := out[0].Values.(map[string]any)
	if p["checkin_interval"] != "1s" || p["request_timeout"] != "40s" || p["base_url"] != "http://x" || v["path_prefix"] != "/new" || v["pause"] != "1m30s" {
		t.Fatalf("params %v values %v", p, v)
	}
	if _, problems := protocol.TaskArgs(channels["http"].Kinds[protocol.KindReverseCall].Fields, p); problems != nil {
		t.Fatalf("not taken again: %v", problems)
	}
}
