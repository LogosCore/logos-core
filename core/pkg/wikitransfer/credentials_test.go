package wikitransfer

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A bundle carries validity as of the three-state field, but bundles exported
// before it existed carry only the boolean it replaced. Both have to land on
// a sensible state, and the legacy false must not become INVALID: it meant
// "not marked working", which was overwhelmingly untested rather than tried
// and rejected. The startup backfill reads legacy rows the same way.
func TestBuildCredential_Validity(t *testing.T) {
	tests := []struct {
		name    string
		payload CredentialPayload
		want    models.CredentialValidity
	}{
		{"an explicit state is carried through", CredentialPayload{Validity: "INVALID"}, models.CredentialValidityInvalid},
		{"as is UNKNOWN", CredentialPayload{Validity: "UNKNOWN"}, models.CredentialValidityUnknown},
		{"a legacy true becomes VALID", CredentialPayload{LegacyIsValid: true}, models.CredentialValidityValid},
		{"a legacy false becomes UNKNOWN, not INVALID", CredentialPayload{LegacyIsValid: false}, models.CredentialValidityUnknown},
		{"and so does an unrecognised state", CredentialPayload{Validity: "MAYBE"}, models.CredentialValidityUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildCredential(tc.payload, uuid.New(), uuid.New())
			if err != nil {
				t.Fatal(err)
			}
			if got.Validity != tc.want {
				t.Fatalf("validity = %q, want %q", got.Validity, tc.want)
			}
		})
	}
}

// Export writes the new field and nothing else, so a bundle round-trips
// through the three-state form rather than collapsing back to a boolean.
func TestPayloadFromCredential_CarriesValidity(t *testing.T) {
	p := PayloadFromCredential(models.Credential{
		CredentialID: uuid.New(),
		Validity:     models.CredentialValidityInvalid,
	})
	if p.Validity != string(models.CredentialValidityInvalid) {
		t.Fatalf("validity = %q, want INVALID", p.Validity)
	}
	if p.LegacyIsValid {
		t.Fatal("export must not write the retired boolean")
	}
}

// A bundle exported before the types had fields (no version) carries the old
// types, and imports the way the startup backfill rewrites such a row.
func TestBuildCredential_LegacyTypes(t *testing.T) {
	keys := []CredentialKey{{Name: "id_rsa", Content: "KEY"}}
	for _, tc := range []struct {
		name       string
		payload    CredentialPayload
		wantType   models.CredentialType
		wantLegacy models.CredentialType
	}{
		{"a password alone is plaintext", CredentialPayload{Type: "PASSWORD", Username: "u", Password: "p"}, models.CredentialTypePlaintext, ""},
		{"a password with keys is other", CredentialPayload{Type: "PASSWORD", Password: "p", Keys: keys}, models.CredentialTypeOther, "PASSWORD"},
		{"an old hash is other", CredentialPayload{Type: "HASH", Password: "aad3b435"}, models.CredentialTypeOther, "HASH"},
		{"so is a type nobody knows", CredentialPayload{Type: "password"}, models.CredentialTypeOther, "password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildCredential(tc.payload, uuid.New(), uuid.New())
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != tc.wantType || got.LegacyType != tc.wantLegacy || got.SchemaVersion != models.CredentialSchemaVersion {
				t.Fatalf("got %s (legacy %q, version %d)", got.Type, got.LegacyType, got.SchemaVersion)
			}
		})
	}
}

// What this core exports comes back as it left: the typed fields, and the
// legacy type of an OTHER.
func TestCredentialPayload_RoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	for _, c := range []models.Credential{
		{Type: models.CredentialTypeTicket, Username: "svc", Realm: "CORP", Ticket: "doIF", Service: "cifs/dc01", ExpiresAt: &at},
		{Type: models.CredentialTypeCertificate, Certificate: "CERT", PrivateKey: "PK", Password: "pfx"},
		{Type: models.CredentialTypeHex, Hex: "deadbeef", Algorithm: "aes256"},
		{Type: models.CredentialTypeOther, LegacyType: "SSH_KEY", Keys: []models.CredentialKey{{Name: "k", Content: "c"}}},
	} {
		c.CredentialID = uuid.New()
		got, err := buildCredential(PayloadFromCredential(c), uuid.New(), uuid.New())
		if err != nil {
			t.Fatalf("%s: %v", c.Type, err)
		}
		if got.Type != c.Type || got.LegacyType != c.LegacyType ||
			!reflect.DeepEqual(got.FilledFields(), c.FilledFields()) || len(got.Keys) != len(c.Keys) {
			t.Errorf("%s: got %+v", c.Type, got)
		}
	}
}

// A payload that does not fit its type is refused, not stored broken.
func TestBuildCredential_RefusesAMisfit(t *testing.T) {
	_, err := buildCredential(CredentialPayload{Version: 2, Type: "KEY", Password: "p"}, uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "a KEY credential has no password") {
		t.Fatalf("got %v", err)
	}
}
