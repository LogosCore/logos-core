package builds

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeCreds holds the operation's credentials, by id.
type fakeCreds struct {
	rows map[string]protocol.Credential
	gone bool // Resolve fails: deleted since the build was queued
}

func (f *fakeCreds) Check(_ context.Context, _ uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]string, error) {
	for _, field := range fields {
		for _, id := range protocol.FieldCredentialIDs(field, values[field.Key]) {
			c, ok := f.rows[id]
			switch {
			case !ok:
				return map[string]string{field.Key: "not one of the operation's"}, nil
			case len(field.Accept) > 0 && !strings.Contains(strings.Join(field.Accept, ","), c.Type):
				return map[string]string{field.Key: "a " + c.Type + "; the field takes " + strings.Join(field.Accept, ", ")}, nil
			}
		}
	}
	return nil, nil
}

func (f *fakeCreds) Resolve(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error) {
	if f.gone {
		return nil, nil, &credentialrefs.Unusable{Why: "credential is gone, or not one of the operation's"}
	}
	out, revs := map[string]protocol.Credential{}, map[string]int64{}
	for _, id := range ids {
		out[id.String()], revs[id.String()] = f.rows[id.String()], 7
	}
	return out, revs, nil
}

// A build's credential fields: checked when queued, their ids kept, the
// credentials given to the factory with the request, and their revisions
// recorded once it is offered.
func TestBuildCredentials(t *testing.T) {
	r := newRig(t)
	cert, key := uuid.NewString(), uuid.NewString()
	creds := &fakeCreds{rows: map[string]protocol.Credential{
		cert: {ID: cert, Type: protocol.CredentialTypeCertificate, Certificate: "MII…"},
		key:  {ID: key, Type: protocol.CredentialTypeKey, Key: "KEY"},
	}}
	r.svc.WithCredentials(creds)
	sc := r.reg.schemas["bob"].BuildSchema
	sc.Fields = append(sc.Fields, protocol.FieldSpec{Key: "signing", Type: protocol.FieldCredential, Accept: []string{protocol.CredentialTypeCertificate}})
	r.reg.schemas["bob"].BuildSchema = sc

	if _, err := r.svc.CreateBuild(context.Background(), opID, r.input(func(in *BuildInput) { in.Values["signing"] = key })); err == nil ||
		!strings.Contains(problemsOf(err)["values.signing"], "takes CERTIFICATE") {
		t.Fatalf("a key where a certificate goes: %v %v", err, problemsOf(err))
	}
	b := r.queue(t, func(in *BuildInput) { in.Values["signing"] = cert })
	if len(b.CredentialIDs) != 1 || b.CredentialIDs[0].String() != cert {
		t.Fatalf("credential ids %v", b.CredentialIDs)
	}
	r.svc.Tick(context.Background())
	r.settle()
	calls := r.rpc.recorded()
	if len(calls) != 1 {
		t.Fatalf("%d offers", len(calls))
	}
	req := calls[0].payload.(protocol.BuildRequest)
	if req.Credentials[cert].Certificate != "MII…" || req.Values["signing"] != cert {
		t.Fatalf("the request's credentials: %+v, values %v", req.Credentials, req.Values)
	}
	if got := r.get(t, b.BuildID.String(), models.BuildStateBuilding); got.CredentialRevs[cert] != 7 {
		t.Fatalf("revisions %v", got.CredentialRevs)
	}

	// Deleted before a factory took the build: it fails, saying so.
	b = r.queue(t, func(in *BuildInput) { in.Values["signing"] = cert })
	creds.gone = true
	r.svc.Tick(context.Background())
	r.settle()
	if got := r.get(t, b.BuildID.String(), models.BuildStateFailed); got.Error == nil || got.Error.Code != models.BuildErrorCredentials {
		t.Fatalf("a build whose credential is gone: %+v", got)
	}
}
