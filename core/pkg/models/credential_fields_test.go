package models

import (
	"strings"
	"testing"
	"time"
)

// Every field: set, read back, emptied. A field the accessors forget would
// pass the type checks while holding a value, which is what they exist for.
func TestCredentialFieldAccessors(t *testing.T) {
	at := time.Now()
	c := Credential{
		Username: "u", Realm: "r", Password: "p", Key: "k", Passphrase: "pp",
		Certificate: "cert", PrivateKey: "pk", Ticket: "t", Service: "s", ExpiresAt: &at,
		Hash: "h", Hex: "aa", Algorithm: "a", Keys: []CredentialKey{{Name: "n", Content: "c"}},
	}
	for _, f := range CredentialFields {
		if !c.HasField(f) {
			t.Errorf("%s: no value read back", f)
		}
		c.ClearField(f)
		if c.HasField(f) {
			t.Errorf("%s: still held after clearing", f)
		}
	}
}

// The table's own sanity: known fields and kinds, none twice in a type, and
// one required field for each type but OTHER (none: it is the old shape,
// where nothing was).
func TestCredentialTypeSpecs(t *testing.T) {
	known := map[CredentialField]bool{}
	for _, f := range CredentialFields {
		known[f] = true
	}
	kinds := map[CredentialFieldKind]bool{
		CredentialFieldKindText: true, CredentialFieldKindSecret: true, CredentialFieldKindBlob: true,
		CredentialFieldKindDateTime: true, CredentialFieldKindKeyList: true,
	}
	for _, spec := range CredentialTypeSpecs() {
		seen := map[CredentialField]bool{}
		required := 0
		for _, f := range spec.Fields {
			if !known[f.Field] || !kinds[f.Kind] || f.Label == "" {
				t.Errorf("%s: field %+v", spec.Type, f)
			}
			if seen[f.Field] {
				t.Errorf("%s: %s twice", spec.Type, f.Field)
			}
			seen[f.Field] = true
			if f.Required {
				required++
			}
		}
		want := 1
		if spec.Type == CredentialTypeOther {
			want = 0
		}
		if required != want {
			t.Errorf("%s: %d required fields, want %d", spec.Type, required, want)
		}
		if !spec.Type.IsValid() {
			t.Errorf("%s: not valid", spec.Type)
		}
	}
	for _, old := range []CredentialType{"PASSWORD", "SSH_KEY", "API_KEY", "TOKEN", ""} {
		if old.IsValid() {
			t.Errorf("old type %q still valid", old)
		}
	}
}

func TestCheckFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    Credential
		want string // "" for none
	}{
		{"a plaintext with its password", Credential{Type: CredentialTypePlaintext, Username: "u", Realm: "CORP", Password: "p"}, ""},
		{"a plaintext without", Credential{Type: CredentialTypePlaintext, Username: "u"}, "a PLAINTEXT credential needs a password"},
		{"a key with a password", Credential{Type: CredentialTypeKey, Key: "k", Password: "p"},
			"a KEY credential has no password (it takes username, key, passphrase)"},
		{"and with keys too, named in order", Credential{Type: CredentialTypeKey, Key: "k", Password: "p", Keys: []CredentialKey{{Name: "x"}}},
			"a KEY credential has no password, keys"},
		{"a ticket that expires", Credential{Type: CredentialTypeTicket, Ticket: "t", ExpiresAt: &time.Time{}}, ""},
		{"an expiry on a hash", Credential{Type: CredentialTypeHash, Hash: "h", ExpiresAt: &time.Time{}}, "has no expires_at"},
		{"hex that is not", Credential{Type: CredentialTypeHex, Hex: "zz"}, "is not hex digits"},
		{"other, empty", Credential{Type: CredentialTypeOther}, ""},
		{"other with the old shape", Credential{Type: CredentialTypeOther, Username: "u", Password: "p", Keys: []CredentialKey{{Name: "k"}}}, ""},
		{"an old type", Credential{Type: "SSH_KEY"}, `type "SSH_KEY" is not one of PLAINTEXT, KEY, CERTIFICATE, TICKET, HASH, HEX, OTHER`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.CheckFields()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// The read-time twin of the startup backfill: the two must agree, or a row
// reads as one thing before the backfill and another after.
func TestUpgradeLegacy(t *testing.T) {
	keys := []CredentialKey{{Name: "id_rsa", Content: "k"}}
	for _, tc := range []struct {
		name       string
		c          Credential
		wantType   CredentialType
		wantLegacy CredentialType
	}{
		{"a password, no keys: plaintext", Credential{Type: "PASSWORD", Username: "u", Password: "p"}, CredentialTypePlaintext, ""},
		{"a password with keys: other", Credential{Type: "PASSWORD", Password: "p", Keys: keys}, CredentialTypeOther, "PASSWORD"},
		{"a password type without one: other", Credential{Type: "PASSWORD", Username: "u"}, CredentialTypeOther, "PASSWORD"},
		{"an ssh key", Credential{Type: "SSH_KEY", Keys: keys}, CredentialTypeOther, "SSH_KEY"},
		{"an old hash, kept in the password", Credential{Type: "HASH", Password: "aad3b435"}, CredentialTypeOther, "HASH"},
		{"an old other stays plain", Credential{Type: "OTHER"}, CredentialTypeOther, ""},
		{"and so does a row with no type", Credential{}, CredentialTypeOther, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.c
			if !c.UpgradeLegacy() {
				t.Fatal("not upgraded")
			}
			if c.Type != tc.wantType || c.LegacyType != tc.wantLegacy || c.SchemaVersion != CredentialSchemaVersion {
				t.Fatalf("got %s (legacy %q, version %d), want %s (legacy %q)", c.Type, c.LegacyType, c.SchemaVersion, tc.wantType, tc.wantLegacy)
			}
			if err := c.CheckFields(); err != nil {
				t.Fatalf("upgraded to a credential that does not fit: %v", err)
			}
			if c.UpgradeLegacy() {
				t.Fatal("upgraded twice")
			}
		})
	}
	// A current HASH is a hash, not an old one.
	c := Credential{Type: CredentialTypeHash, Hash: "h", SchemaVersion: CredentialSchemaVersion}
	if c.UpgradeLegacy() || c.Type != CredentialTypeHash {
		t.Fatalf("a current row upgraded: %+v", c)
	}
}

func TestNormalizeCredentialHex(t *testing.T) {
	for in, want := range map[string]string{
		"":                   "",
		"aabbcc":             "aabbcc",
		"0xAA:bb cc\n":       "AAbbcc",
		" de ad\tbe:ef \r\n": "deadbeef",
	} {
		if got, err := NormalizeCredentialHex(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		"abc":  "odd number of digits",
		"xyzw": "not hex digits",
		"0x":   "", // only the prefix: nothing, which the required check catches
	} {
		got, err := NormalizeCredentialHex(in)
		if want == "" {
			if err != nil || got != "" {
				t.Errorf("%q: got %q, %v", in, got, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
}

// What the exports write out: the type's fields holding a value, in order,
// the expiry in RFC 3339, keys left to the caller.
func TestFilledFields(t *testing.T) {
	at := time.Date(2026, 11, 5, 18, 0, 0, 0, time.FixedZone("x", 2*3600))
	c := Credential{Type: CredentialTypeTicket, Username: "svc", Ticket: "doIF", ExpiresAt: &at, Password: "stray"}
	var got []string
	for _, f := range c.FilledFields() {
		got = append(got, string(f.Spec.Field)+"="+f.Value)
	}
	if strings.Join(got, " ") != "username=svc ticket=doIF expires_at=2026-11-05T16:00:00Z" {
		t.Fatalf("got %v", got)
	}
}
