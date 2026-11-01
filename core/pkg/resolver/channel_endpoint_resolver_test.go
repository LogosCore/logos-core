package resolver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channelendpoints"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func TestToEndpointModel(t *testing.T) {
	schema := &protocol.ConnectionSchema{SchemaVersion: protocol.ConnectionSchemaV2, Version: 2, Kinds: map[string]protocol.ConnectionKind{
		protocol.KindReversePersistent: {Fields: []protocol.FieldSpec{{Key: "base_url", Type: protocol.FieldURL}}},
		protocol.KindReverseCall: {Fields: []protocol.FieldSpec{
			{Key: "base_url", Type: protocol.FieldURL},
			{Key: "token", Type: protocol.FieldSecret, Default: "dflt"},
		}},
	}}
	op := uuid.New()
	ep := models.ChannelEndpoint{OperationID: op, Name: "edge", Kind: protocol.KindReverseCall, SchemaVersion: 1, Values: models.JSONMap{"base_url": "https://a.example.com", "token": "s3cret"}}

	m := toEndpointModel(channelendpoints.View{ChannelEndpoint: ep, Schema: schema, Problems: map[string]string{"": "gone", "base_url": "bad"}})
	values := m.Values.(map[string]any)
	// Operation data: secret fields are shown like the rest.
	if values["token"] != "s3cret" || values["base_url"] != "https://a.example.com" {
		t.Errorf("values = %v", values)
	}
	if m.OperationID != op.String() || !m.SchemaChanged || m.Kind != protocol.KindReverseCall {
		t.Errorf("operationId = %s schemaChanged = %v", m.OperationID, m.SchemaChanged)
	}
	if len(m.Problems) != 2 || m.Problems[0].Key != nil || *m.Problems[1].Key != "base_url" {
		t.Errorf("problems = %+v", m.Problems)
	}
}

// Each kind keeps its own fields, in the protocol's kind order; a secret's
// default is shown, endpoint values being operation data.
func TestToConnectionModel(t *testing.T) {
	m := toConnectionModel(&protocol.ConnectionSchema{SchemaVersion: protocol.ConnectionSchemaV2, Version: 3, Kinds: map[string]protocol.ConnectionKind{
		protocol.KindReversePersistent: {Fields: []protocol.FieldSpec{{Key: "base_url", Type: protocol.FieldURL}, {Key: "backoff", Type: protocol.FieldDuration}}},
		protocol.KindReverseCall:       {Fields: []protocol.FieldSpec{{Key: "token", Type: protocol.FieldSecret, Default: "dflt"}}},
	}})
	if m.Version != 3 || len(m.Kinds) != 2 || m.Kinds[0].Kind != protocol.KindReverseCall || m.Kinds[1].Kind != protocol.KindReversePersistent {
		t.Fatalf("kinds = %+v", m.Kinds)
	}
	if len(m.Kinds[0].Fields) != 1 || m.Kinds[0].Fields[0].Default != "dflt" || len(m.Kinds[1].Fields) != 2 {
		t.Errorf("fields = %+v / %+v", m.Kinds[0].Fields, m.Kinds[1].Fields)
	}
}

// fakeEndpoints is a ChannelEndpointManager that records writes; every
// endpoint belongs to op.
type fakeEndpoints struct {
	op     uuid.UUID
	writes int
}

func (f *fakeEndpoints) Kinds(context.Context) ([]channelendpoints.Kind, error) { return nil, nil }
func (f *fakeEndpoints) List(context.Context, uuid.UUID, repository.ChannelEndpointFilter, *pagination.Cursor, int64, bool) ([]channelendpoints.View, error) {
	return nil, nil
}
func (f *fakeEndpoints) Count(context.Context, uuid.UUID, repository.ChannelEndpointFilter) (int64, error) {
	return 0, nil
}
func (f *fakeEndpoints) FreeSlug(_ context.Context, _ uuid.UUID, text, tag string) (string, error) {
	return text + tag, nil
}
func (f *fakeEndpoints) Get(context.Context, string) (channelendpoints.View, error) {
	return channelendpoints.View{ChannelEndpoint: models.ChannelEndpoint{OperationID: f.op}}, nil
}
func (f *fakeEndpoints) Create(context.Context, uuid.UUID, string, channelendpoints.Input) (channelendpoints.View, error) {
	f.writes++
	return channelendpoints.View{}, nil
}
func (f *fakeEndpoints) Update(context.Context, string, int64, channelendpoints.Input) (channelendpoints.View, error) {
	f.writes++
	return channelendpoints.View{}, nil
}
func (f *fakeEndpoints) Delete(context.Context, string) error { f.writes++; return nil }
func (f *fakeEndpoints) Export(context.Context, string) (protocol.ChannelEndpointExport, error) {
	return protocol.ChannelEndpointExport{}, nil
}

func TestChannelEndpointRoles(t *testing.T) {
	caller, opID := uuid.New(), uuid.New()
	newResolver := func(role models.OperationRole) (IChannelEndpointResolver, *fakeEndpoints) {
		eps := &fakeEndpoints{op: opID}
		opRepo := &mockOpRepo{findByIDFn: func(_ context.Context, id uuid.UUID) (models.Operation, error) {
			return memberOp(id, caller, role), nil
		}}
		return NewChannelEndpointResolver(eps, opRepo), eps
	}
	input := model.ChannelEndpointInput{Name: "edge", Values: map[string]any{}}
	ctx := newCallerCtx(caller)

	// A viewer reads and exports, but cannot change anything.
	r, eps := newResolver(models.OperationRoleViewer)
	if _, err := r.ChannelEndpoints(ctx, opID.String(), nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Errorf("viewer list: %v", err)
	}
	if _, err := r.ChannelEndpointExport(ctx, "x"); err != nil {
		t.Errorf("viewer export: %v", err)
	}
	if _, err := r.CreateChannelEndpoint(ctx, opID.String(), "http", input); err == nil {
		t.Error("viewer created an endpoint")
	}
	if _, err := r.DeleteChannelEndpoint(ctx, "x"); err == nil || eps.writes != 0 {
		t.Errorf("viewer deleted an endpoint: %v (writes %d)", err, eps.writes)
	}

	// An operator writes.
	r, eps = newResolver(models.OperationRoleOperator)
	if _, err := r.CreateChannelEndpoint(ctx, opID.String(), "http", input); err != nil {
		t.Errorf("operator create: %v", err)
	}
	if _, err := r.UpdateChannelEndpoint(ctx, "x", input, 1); err != nil || eps.writes != 2 {
		t.Errorf("operator update: %v (writes %d)", err, eps.writes)
	}

	// An agent key reads but never writes, whatever its role.
	agentCtx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{
		UserID: caller.String(),
		Agent:  &gqlctx.AgentInfo{AgentKeyID: "a-1", MaxRole: models.OperationRoleAdmin, AllowWrites: true, OperationScopes: []uuid.UUID{opID}},
	})
	r, eps = newResolver(models.OperationRoleAdmin)
	if _, err := r.CreateChannelEndpoint(agentCtx, opID.String(), "http", input); err == nil || eps.writes != 0 {
		t.Errorf("agent created an endpoint: %v", err)
	}
	if _, err := r.ChannelEndpointExport(agentCtx, "x"); err != nil {
		t.Errorf("agent export: %v", err)
	}
}
