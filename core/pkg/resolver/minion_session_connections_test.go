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
