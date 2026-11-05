package minionsync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeCredentials resolves the credentials it holds and keeps the logins
// that worked.
type fakeCredentials struct {
	mu     sync.Mutex
	given  map[string]protocol.Credential
	fail   error
	worked []uuid.UUID
}

func (f *fakeCredentials) ResolveFields(_ context.Context, _ uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]protocol.Credential, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	out := map[string]protocol.Credential{}
	for _, id := range protocol.CredentialIDs(fields, values) {
		c, ok := f.given[id]
		if !ok {
			return nil, &credentialrefs.Unusable{Why: "credential " + id + " is gone"}
		}
		out[id] = c
	}
	return out, nil
}

func (f *fakeCredentials) LoginWorked(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.worked = append(f.worked, id)
	return nil
}

// credentialRig is the agentless rig with a target whose credentials field
// names a key and a password.
func credentialRig(t *testing.T) (*bindRig, *fakeAgentless, *fakeCredentials, uuid.UUID, uuid.UUID) {
	t.Helper()
	b, ch := newAgentlessRig(t, protocol.KindBindCall)
	key, pw := uuid.New(), uuid.New()
	field := protocol.FieldSpec{Key: "credentials", Type: protocol.FieldCredentialList,
		Accept: []string{protocol.CredentialTypeKey, protocol.CredentialTypePlaintext}}
	b.svc.WithChannelSchemas(fakeSchemas{"ssh": {SchemaVersion: protocol.ConnectionSchemaV2, Version: 1,
		Kinds: map[string]protocol.ConnectionKind{protocol.KindBindCall: {Agentless: true,
			Target: []protocol.FieldSpec{{Key: "host", Type: protocol.FieldString}, field}}}}})
	row := b.targets.rows[b.target]
	row.Values["credentials"] = []any{key.String(), pw.String()}
	row.CredentialIDs = []uuid.UUID{key, pw}
	b.targets.rows[b.target] = row
	creds := &fakeCredentials{given: map[string]protocol.Credential{
		key.String(): {ID: key.String(), Type: protocol.CredentialTypeKey, Username: "root", Key: "KEY"},
		pw.String():  {ID: pw.String(), Type: protocol.CredentialTypePlaintext, Username: "root", Password: "pw"},
	}}
	b.svc.WithCredentials(creds)
	return b, ch, creds, key, pw
}

// A call carries the credentials its values name; the channel's logins are
// noted on the target, the one that worked marked valid, and what the
// channel says of a credential it was not given is no news.
func TestCallCarriesCredentialsAndNotesLogins(t *testing.T) {
	b, ch, creds, key, pw := credentialRig(t)
	stranger := uuid.New()
	ch.logins = []protocol.LinkLogin{{Credential: key.String()}, {Credential: pw.String(), Accepted: true},
		{Credential: stranger.String(), Accepted: true}}
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.creds) != 1 || ch.creds[0][key.String()].Key != "KEY" || ch.creds[0][pw.String()].Password != "pw" {
		t.Fatalf("the call's credentials: %+v", ch.creds)
	}
	if tg.State != models.TargetConnected {
		t.Fatalf("target %+v", tg)
	}
	row := b.targets.rows[b.target]
	if l := row.Logins[key.String()]; l.Accepted || l.At.IsZero() {
		t.Errorf("the refused key: %+v", l)
	}
	if l := row.Logins[pw.String()]; !l.Accepted {
		t.Errorf("the password that worked: %+v", l)
	}
	if _, ok := row.Logins[stranger.String()]; ok {
		t.Error("a login with a credential the call did not carry noted")
	}
	if len(creds.worked) != 1 || creds.worked[0] != pw {
		t.Errorf("marked valid: %v", creds.worked)
	}
}

// A credential that cannot be given (gone, converted) refuses the target
// without a call: retrying changes nothing until it is edited.
func TestCallWithoutItsCredentials(t *testing.T) {
	b, ch, creds, _, _ := credentialRig(t)
	creds.fail = &credentialrefs.Unusable{Why: "credential x is gone, or not one of the operation's"}
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetRefused || len(ch.calls) != 0 || tg.StateDetail != "credentials: credential x is gone, or not one of the operation's" {
		t.Fatalf("target %s %q after %d calls", tg.State, tg.StateDetail, len(ch.calls))
	}
}

// A store failing to resolve them is core's own trouble: the target is not
// refused for it, and no call goes without them.
func TestCallWhileCredentialsDoNotLoad(t *testing.T) {
	b, ch, creds, _, _ := credentialRig(t)
	creds.fail = errors.New("load credentials: connection reset")
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	// Called again soon, as any call core could not make.
	if tg.State == models.TargetRefused || len(ch.calls) != 0 || tg.NextCallAt == nil ||
		!strings.Contains(tg.StateDetail, "core could not make the call") {
		t.Fatalf("target %s %q, next call %v, after %d calls", tg.State, tg.StateDetail, tg.NextCallAt, len(ch.calls))
	}
}

// A refused login is noted too: the target says why, the credential which.
func TestRefusedLoginNoted(t *testing.T) {
	b, ch, creds, key, pw := credentialRig(t)
	ch.fail = &protocol.LinkFailure{Kind: protocol.LinkFailLogin, Msg: "root (key): refused; root (password): refused"}
	ch.logins = []protocol.LinkLogin{{Credential: key.String()}, {Credential: pw.String()}}
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	row := b.targets.rows[b.target]
	if tg.State != models.TargetRefused || row.Logins[key.String()].Accepted || row.Logins[pw.String()].Accepted ||
		len(row.Logins) != 2 || len(creds.worked) != 0 {
		t.Fatalf("target %s, logins %+v, worked %v", tg.State, row.Logins, creds.worked)
	}
}
