package resolver

import (
	"strings"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
)

func str(v string) *string { return &v }

// The update rules for typed fields (credentialFieldUpdates): a value only
// where the type has room, a required field never emptied, and a change of
// type that loses nothing silently.
func TestCredentialFieldUpdates(t *testing.T) {
	other := models.Credential{
		Type: models.CredentialTypeOther, LegacyType: "SSH_KEY", SchemaVersion: 2,
		Username: "root", Password: "pw", Keys: []models.CredentialKey{{Name: "id_rsa", Content: "KEY"}},
	}
	key := models.Credential{Type: models.CredentialTypeKey, SchemaVersion: 2, Key: "KEY"}

	for _, tc := range []struct {
		name    string
		cred    models.Credential
		input   model.UpdateCredentialInput
		wantErr string
		want    map[string]any // fields that must be in the $set, with their values
		absent  []string       // and fields that must not be
	}{
		{
			name: "a field the type has", cred: key,
			input: model.UpdateCredentialInput{Passphrase: str("open sesame")},
			want:  map[string]any{"passphrase": "open sesame"}, absent: []string{"type", "schema_version"},
		},
		{
			name: "one it has not", cred: key,
			input:   model.UpdateCredentialInput{Password: str("pw")},
			wantErr: "a KEY credential has no password (it takes username, key, passphrase)",
		},
		{
			name: "emptying one it has not is no value, so not refused", cred: key,
			input: model.UpdateCredentialInput{Password: str("")},
			want:  map[string]any{"password": ""},
		},
		{
			name: "a required field emptied", cred: key,
			input:   model.UpdateCredentialInput{Key: str("  ")},
			wantErr: "a KEY credential needs a key",
		},
		{
			name: "the same type again is no change", cred: key,
			input:  model.UpdateCredentialInput{Type: typePtr(models.CredentialTypeKey), Passphrase: str("x")},
			absent: []string{"type", "schema_version"},
		},
		{
			name: "a new type refuses what it would lose, by name", cred: other,
			input:   model.UpdateCredentialInput{Type: typePtr(models.CredentialTypeKey), Key: str("KEY")},
			wantErr: "a KEY credential has no password, keys (it takes username, key, passphrase): send them empty",
		},
		{
			name: "and takes them sent empty", cred: other,
			input: model.UpdateCredentialInput{
				Type: typePtr(models.CredentialTypeKey), Key: str("KEY"),
				Password: str(""), Keys: []*model.CredentialKeyInput{},
			},
			want: map[string]any{
				"type": models.CredentialTypeKey, "schema_version": models.CredentialSchemaVersion,
				"key": "KEY", "password": "", "legacy_type": "",
			},
		},
		{
			name: "a new type needs its required field", cred: other,
			input: model.UpdateCredentialInput{
				Type: typePtr(models.CredentialTypeKey), Password: str(""), Keys: []*model.CredentialKeyInput{},
			},
			wantErr: "a KEY credential needs a key",
		},
		{
			// A value in a field the type has not (two edits crossing a change
			// of type) is not the caller's to know about: emptied, not refused.
			name: "a value nobody could see is emptied", cred: models.Credential{
				Type: models.CredentialTypeOther, Username: "u", Password: "pw", Hash: "stray",
			},
			input:  model.UpdateCredentialInput{Type: typePtr(models.CredentialTypePlaintext)},
			want:   map[string]any{"hash": "", "type": models.CredentialTypePlaintext},
			absent: []string{"password", "username", "legacy_type"},
		},
		{
			name: "an expiry in any offset is stored in UTC", cred: models.Credential{Type: models.CredentialTypeTicket, Ticket: "t"},
			input: model.UpdateCredentialInput{ExpiresAt: str("2026-11-05T18:00:00+02:00")},
			want:  map[string]any{"expires_at": atPtr(time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC))},
		},
		{
			name: "and an empty one clears it", cred: models.Credential{Type: models.CredentialTypeTicket, Ticket: "t", ExpiresAt: atPtr(time.Now())},
			input: model.UpdateCredentialInput{ExpiresAt: str("")},
			want:  map[string]any{"expires_at": (*time.Time)(nil)},
		},
		{
			name: "an expiry that is not a time", cred: models.Credential{Type: models.CredentialTypeTicket, Ticket: "t"},
			input:   model.UpdateCredentialInput{ExpiresAt: str("tomorrow")},
			wantErr: "is not an RFC 3339 time",
		},
		{
			name: "hex is compacted", cred: models.Credential{Type: models.CredentialTypeHex, Hex: "aa"},
			input: model.UpdateCredentialInput{Hex: str("0xDE:AD be ef")},
			want:  map[string]any{"hex": "DEADbeef"},
		},
		{
			name: "one-line fields are trimmed, a password kept as typed", cred: models.Credential{Type: models.CredentialTypePlaintext, Password: "p"},
			input: model.UpdateCredentialInput{Username: str("  admin "), Realm: str(" CORP "), Password: str(" spaced ")},
			want:  map[string]any{"username": "admin", "realm": "CORP", "password": " spaced "},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := credentialFieldUpdates(tc.cred, tc.input)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v (%v), want %q", err, got, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for k, want := range tc.want {
				v, ok := got[k]
				if !ok {
					t.Errorf("%s not set: %v", k, got)
					continue
				}
				if !sameStored(v, want) {
					t.Errorf("%s = %#v, want %#v", k, v, want)
				}
			}
			for _, k := range tc.absent {
				if _, ok := got[k]; ok {
					t.Errorf("%s set: %v", k, got)
				}
			}
		})
	}
}

func typePtr(t models.CredentialType) *models.CredentialType { return &t }
func atPtr(t time.Time) *time.Time                           { return &t }

func sameStored(a, b any) bool {
	if at, ok := a.(*time.Time); ok {
		bt, _ := b.(*time.Time)
		if at == nil || bt == nil {
			return at == nil && bt == nil
		}
		return at.Equal(*bt)
	}
	return a == b
}

// The table the UI builds its forms from names fields and kinds the schema
// has: gqlgen's enums are generated from the schema, this from the models.
func TestCredentialTypesMatchTheSchema(t *testing.T) {
	specs, err := (&credentialResolver{}).CredentialTypes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != len(models.CredentialTypeSpecs()) {
		t.Fatalf("%d types", len(specs))
	}
	for _, s := range specs {
		for _, f := range s.Fields {
			if !f.Field.IsValid() || !f.Kind.IsValid() {
				t.Errorf("%s: field %s kind %s not in the schema", s.Type, f.Field, f.Kind)
			}
		}
	}
	for _, f := range models.CredentialFields {
		if !model.CredentialField(strings.ToUpper(string(f))).IsValid() {
			t.Errorf("%s not in the schema's CredentialField", f)
		}
	}
}
