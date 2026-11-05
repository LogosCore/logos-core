package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

type fakeTaskCreds struct {
	rows map[string]protocol.Credential
}

func (f fakeTaskCreds) ResolveFields(_ context.Context, _ uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]protocol.Credential, error) {
	out := map[string]protocol.Credential{}
	for _, id := range protocol.CredentialIDs(fields, values) {
		c, ok := f.rows[id]
		if !ok {
			return nil, errors.New("credential " + id + " is gone")
		}
		out[id] = c
	}
	return out, nil
}

// A task's credential params carry the credentials in place of their ids:
// a minion has no way to look one up. Their ids stay with the task.
func TestPutCredentials(t *testing.T) {
	as, k1, k2 := uuid.New(), uuid.New(), uuid.New()
	r := &minionTaskResolver{creds: fakeTaskCreds{rows: map[string]protocol.Credential{
		as.String(): {ID: as.String(), Type: protocol.CredentialTypePlaintext, Username: "admin", Password: "pw"},
		k1.String(): {ID: k1.String(), Type: protocol.CredentialTypeKey, Key: "K1"},
		k2.String(): {ID: k2.String(), Type: protocol.CredentialTypeKey, Key: "K2"},
	}}}
	params := []protocol.FieldSpec{
		{Key: "as", Num: 1, Type: protocol.FieldCredential},
		{Key: "keys", Num: 2, Type: protocol.FieldCredentialList},
		{Key: "path", Num: 3, Type: protocol.FieldString},
	}
	wire, problems := protocol.TaskArgs(params, map[string]any{"as": as.String(), "keys": []any{k2.String(), k1.String()}, "path": "/tmp"})
	if problems != nil {
		t.Fatal(problems)
	}
	ids, err := r.putCredentials(context.Background(), uuid.New(), params, wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("ids %v", ids)
	}
	if c, ok := wire["as"].(protocol.Credential); !ok || c.Password != "pw" {
		t.Fatalf("the credential arg: %#v", wire["as"])
	}
	if list, ok := wire["keys"].([]protocol.Credential); !ok || len(list) != 2 || list[0].Key != "K2" || list[1].Key != "K1" {
		t.Fatalf("the list arg, in its order: %#v", wire["keys"])
	}
	if wire["path"] != "/tmp" {
		t.Fatalf("a text arg beside them: %#v", wire["path"])
	}

	wire, _ = protocol.TaskArgs(params, map[string]any{"as": uuid.NewString()})
	if _, err := r.putCredentials(context.Background(), uuid.New(), params, wire); err == nil {
		t.Fatal("a credential gone was put")
	}
	none := &minionTaskResolver{}
	if ids, err := none.putCredentials(context.Background(), uuid.New(), params, map[string]any{"path": "/tmp"}); err != nil || ids != nil {
		t.Fatalf("no credential params: %v %v", ids, err)
	}
}
