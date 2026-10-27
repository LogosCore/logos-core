package channelendpoints

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeRegistry maps a channel kind to its latest schema.
type fakeRegistry map[string]protocol.ConnectionSchema

func (f fakeRegistry) LatestConnectionSchemas(context.Context) (map[string]*models.ModuleConnectionSchema, error) {
	out := map[string]*models.ModuleConnectionSchema{}
	for k, s := range f {
		out[k] = &models.ModuleConnectionSchema{ConnectionSchema: s}
	}
	return out, nil
}

// LatestIcons gives every channel kind an icon naming it.
func (f fakeRegistry) LatestIcons(_ context.Context, moduleType string) (map[string]string, error) {
	out := map[string]string{}
	if moduleType == protocol.ServiceChannel {
		for k := range f {
			out[k] = "<svg " + k + "/>"
		}
	}
	return out, nil
}

func (f fakeRegistry) LatestConnectionSchema(_ context.Context, moduleName string) (*models.ModuleConnectionSchema, error) {
	s, ok := f[moduleName]
	if !ok {
		return nil, nil
	}
	return &models.ModuleConnectionSchema{ConnectionSchema: s}, nil
}

type fakeRepo struct {
	rows map[uuid.UUID]*models.ChannelEndpoint
}

func (f *fakeRepo) List(_ context.Context, operationID uuid.UUID, moduleName string) ([]models.ChannelEndpoint, error) {
	var out []models.ChannelEndpoint
	for _, ep := range f.rows {
		if ep.OperationID == operationID && (moduleName == "" || ep.ModuleName == moduleName) {
			out = append(out, *ep)
		}
	}
	return out, nil
}

func (f *fakeRepo) FindByEndpointID(_ context.Context, id uuid.UUID) (*models.ChannelEndpoint, error) {
	ep, ok := f.rows[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	cp := *ep
	return &cp, nil
}

func (f *fakeRepo) Insert(_ context.Context, ep *models.ChannelEndpoint) error {
	for _, r := range f.rows {
		if r.OperationID == ep.OperationID && r.Name == ep.Name {
			return repository.ErrChannelEndpointNameTaken
		}
	}
	if ep.EndpointID == uuid.Nil {
		ep.EndpointID = uuid.New()
	}
	ep.Id, ep.Revision = primitive.NewObjectID(), 1
	cp := *ep
	f.rows[ep.EndpointID] = &cp
	return nil
}

func (f *fakeRepo) Update(_ context.Context, ep *models.ChannelEndpoint) error {
	r, ok := f.rows[ep.EndpointID]
	if !ok || r.Revision != ep.Revision {
		return repository.ErrChannelEndpointConflict
	}
	ep.Revision++
	cp := *ep
	f.rows[ep.EndpointID] = &cp
	return nil
}

func (f *fakeRepo) DeleteByOperationID(_ context.Context, operationID uuid.UUID) error {
	for id, ep := range f.rows {
		if ep.OperationID == operationID {
			delete(f.rows, id)
		}
	}
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	_, ok := f.rows[id]
	delete(f.rows, id)
	return ok, nil
}

func (f *fakeRepo) BackfillEndpointIDs(context.Context) (int64, error) { return 0, nil }

var opA, opB = uuid.New(), uuid.New()

var schema = protocol.ConnectionSchema{
	SchemaVersion: protocol.ConnectionSchemaV1,
	Version:       1,
	Fields: []protocol.FieldSpec{
		{Key: "base_url", Type: protocol.FieldURL, Required: true, Pattern: `^https?://[^/?#\s]+$`},
		{Key: "tls_verify", Type: protocol.FieldBool, Default: false},
		{Key: "token", Type: protocol.FieldSecret},
		{Key: "user_agent", Type: protocol.FieldString},
	},
}

func newService() (*Service, *fakeRepo, fakeRegistry) {
	repo := &fakeRepo{rows: map[uuid.UUID]*models.ChannelEndpoint{}}
	reg := fakeRegistry{"http": schema}
	return NewService(repo, reg), repo, reg
}

func TestCreateValidatesAndNormalizes(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	v, err := svc.Create(ctx, opA, "http", Input{Name: "edge-eu", Values: map[string]any{
		"base_url": "https://edge.example.com", "user_agent": "", "token": "s3cret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Values["user_agent"]; ok {
		t.Error("empty string stored")
	}
	if v.SchemaVersion != 1 || v.ModuleName != "http" || len(v.Problems) != 0 {
		t.Errorf("view = %+v", v)
	}

	var ve *ValidationError
	_, err = svc.Create(ctx, opA, "http", Input{Name: "bad", Values: map[string]any{"base_url": "https://x.example.com/path"}})
	if !errors.As(err, &ve) || ve.Problems["base_url"] == "" {
		t.Errorf("path in base_url: %v", err)
	}
	if _, err = svc.Create(ctx, opA, "http", Input{Name: "edge-eu", Values: map[string]any{"base_url": "https://b.example.com"}}); !errors.As(err, &ve) {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err = svc.Create(ctx, opA, "http", Input{Name: "Edge EU", Values: map[string]any{"base_url": "https://b.example.com"}}); !errors.As(err, &ve) {
		t.Errorf("bad name: %v", err)
	}
	if _, err = svc.Create(ctx, opA, "telegram", Input{Name: "tg", Values: map[string]any{}}); !errors.As(err, &ve) {
		t.Errorf("kind without schema: %v", err)
	}
}

func TestDescriptionLengthCountsCharacters(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	// Two bytes each: a byte count would refuse the first.
	fits := strings.Repeat("é", MaxDescriptionLength)
	if _, err := svc.Create(ctx, opA, "http", Input{Name: "fits", Description: fits, Values: map[string]any{"base_url": "https://a.example.com"}}); err != nil {
		t.Errorf("a description of %d characters: %v", MaxDescriptionLength, err)
	}
	var ve *ValidationError
	if _, err := svc.Create(ctx, opA, "http", Input{Name: "over", Description: fits + "é", Values: map[string]any{"base_url": "https://a.example.com"}}); !errors.As(err, &ve) {
		t.Errorf("a description of %d characters: %v", MaxDescriptionLength+1, err)
	}
}

func TestUpdateReplacesValuesAndChecksRevision(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	v, err := svc.Create(ctx, opA, "http", Input{Name: "edge", Values: map[string]any{"base_url": "https://a.example.com", "token": "s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	id := v.EndpointID.String()
	v, err = svc.Update(ctx, id, 1, Input{Name: "edge", Values: map[string]any{"base_url": "https://b.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Values["token"]; ok || v.Values["base_url"] != "https://b.example.com" || v.Revision != 2 {
		t.Errorf("after update = %+v", v.ChannelEndpoint)
	}
	var conflict *ConflictError
	if _, err = svc.Update(ctx, id, 1, Input{Name: "edge", Values: map[string]any{"base_url": "https://c.example.com"}}); !errors.As(err, &conflict) {
		t.Errorf("stale revision: %v", err)
	}
}

func TestNamesAreUniquePerOperation(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	in := Input{Name: "edge", Values: map[string]any{"base_url": "https://a.example.com"}}
	if _, err := svc.Create(ctx, opA, "http", in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, opB, "http", in); err != nil {
		t.Errorf("same name in another operation: %v", err)
	}
	var ve *ValidationError
	if _, err := svc.Create(ctx, opA, "http", in); !errors.As(err, &ve) {
		t.Errorf("duplicate in one operation: %v", err)
	}
	if list, _ := svc.List(ctx, opB, ""); len(list) != 1 {
		t.Errorf("operation B lists %d endpoints", len(list))
	}
	kinds, err := svc.Kinds(ctx)
	if err != nil || len(kinds) != 1 || kinds[0].ModuleName != "http" || kinds[0].Icon != "<svg http/>" {
		t.Errorf("kinds = %+v (%v)", kinds, err)
	}
}

func TestExportAndProblems(t *testing.T) {
	svc, _, reg := newService()
	ctx := context.Background()
	v, err := svc.Create(ctx, opA, "http", Input{Name: "edge", Values: map[string]any{"base_url": "https://a.example.com", "token": "s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.Export(ctx, v.EndpointID.String())
	if err != nil {
		t.Fatal(err)
	}
	params := doc["params"].(map[string]any)
	if doc["format"] != ExportFormat || params["base_url"] != "https://a.example.com" || params["tls_verify"] != false || params["token"] != "s3cret" {
		t.Errorf("export = %v", doc)
	}

	// The channel declares a new schema version that requires another field.
	next := schema
	next.Version = 2
	next.Fields = append([]protocol.FieldSpec{{Key: "region", Type: protocol.FieldString, Required: true}}, schema.Fields...)
	reg["http"] = next

	got, err := svc.Get(ctx, v.EndpointID.String())
	if err != nil {
		t.Fatal(err)
	}
	if !got.SchemaChanged() || got.Problems["region"] == "" {
		t.Errorf("after schema change: changed=%v problems=%v", got.SchemaChanged(), got.Problems)
	}
	var ve *ValidationError
	if _, err := svc.Export(ctx, v.EndpointID.String()); !errors.As(err, &ve) {
		t.Errorf("export with problems: %v", err)
	}

	delete(reg, "http")
	list, err := svc.List(ctx, opA, "http")
	if err != nil || len(list) != 1 || list[0].Problems[""] == "" {
		t.Errorf("no schema left: %v %+v", err, list)
	}
	if err := svc.Delete(ctx, v.EndpointID.String()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, v.EndpointID.String()); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}
