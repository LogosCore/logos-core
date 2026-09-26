package markdown

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// fakeCredLookup is a map-backed CredentialLookup. Returning the zero
// Credential with no error signals "missing"; setting a credential with a
// non-target operation id exercises the cross-op tombstone path.
type fakeCredLookup struct {
	by map[uuid.UUID]models.Credential
}

func (f *fakeCredLookup) FindByID(_ context.Context, id uuid.UUID) (models.Credential, error) {
	if f == nil || f.by == nil {
		return models.Credential{}, nil
	}
	if c, ok := f.by[id]; ok {
		return c, nil
	}
	return models.Credential{}, nil
}

func fence(id string) string {
	return "```logos-credential\n{\n  \"id\": \"" + id + "\"\n}\n```"
}

// The whole point of the pass: what reaches the zip is markdown, not a
// fence under an info-string only Logos knows.
func TestLowerCredentialFences_WritesReadableMarkdown(t *testing.T) {
	opID := uuid.New()
	credID := uuid.New()
	lookup := &fakeCredLookup{by: map[uuid.UUID]models.Credential{
		credID: {
			CredentialID: credID,
			OperationID:  opID,
			Name:         "prod-db",
			Type:         models.CredentialTypePassword,
			Username:     "admin",
			Password:     "hunter2",
			Validity:     models.CredentialValidityValid,
			Tags:         []string{"db", "prod"},
			Properties:   []models.CredentialProperty{{Name: "Host", Value: "sql01"}},
			Keys:         []models.CredentialKey{{Name: "id_ed25519", Content: "-----BEGIN KEY-----\nabc\n-----END KEY-----"}},
		},
	}}

	out, written, tomb := LowerCredentialFences(context.Background(), fence(credID.String()), opID, lookup, CredentialValues)
	if written != 1 || tomb != 0 {
		t.Fatalf("written=%d tombstoned=%d", written, tomb)
	}
	if strings.Contains(out, "logos-credential") {
		t.Errorf("the Logos-only fence survived:\n%s", out)
	}
	if strings.Contains(out, credID.String()) {
		t.Errorf("a uuid no foreign reader can resolve survived:\n%s", out)
	}
	// Everything a reader can act on is still there.
	for _, want := range []string{
		"**Credential: prod-db** (password, valid)",
		"- Username: `admin`",
		"- Password: `hunter2`",
		"- Host: `sql01`",
		"- Tags: db, prod",
		"id\\_ed25519:",
		"-----BEGIN KEY-----",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A credential with nothing but a name still reads as a sentence rather
// than as an empty list.
func TestLowerCredentialFences_SparseCredential(t *testing.T) {
	opID := uuid.New()
	credID := uuid.New()
	lookup := &fakeCredLookup{by: map[uuid.UUID]models.Credential{
		credID: {CredentialID: credID, OperationID: opID, Name: "token only", Type: models.CredentialTypeToken},
	}}

	out, written, _ := LowerCredentialFences(context.Background(), fence(credID.String()), opID, lookup, CredentialValues)
	if written != 1 {
		t.Fatalf("written=%d", written)
	}
	if strings.TrimSpace(out) != "**Credential: token only** (token)" {
		t.Errorf("sparse credential = %q", out)
	}
}

func TestLowerCredentialFences_NoLookupTombstones(t *testing.T) {
	out, written, tomb := LowerCredentialFences(context.Background(), fence(uuid.NewString()), uuid.New(), nil, CredentialValues)
	if written != 0 || tomb != 1 {
		t.Fatalf("written=%d tombstoned=%d", written, tomb)
	}
	if strings.Contains(out, "logos-credential") {
		t.Errorf("fence survived without a lookup:\n%s", out)
	}
}

func TestLowerCredentialFences_TombstonesCrossOpReference(t *testing.T) {
	docOp := uuid.New()
	otherOp := uuid.New()
	credID := uuid.New()
	lookup := &fakeCredLookup{by: map[uuid.UUID]models.Credential{
		credID: {
			CredentialID: credID,
			OperationID:  otherOp, // different from docOp
			Name:         "leaky",
			Password:     "should-not-leak",
		},
	}}

	out, written, tomb := LowerCredentialFences(context.Background(), fence(credID.String()), docOp, lookup, CredentialValues)
	if written != 0 {
		t.Fatalf("cross-op credential should NOT be written out; got written=%d", written)
	}
	if tomb != 1 {
		t.Fatalf("expected 1 tombstoned, got %d", tomb)
	}
	if strings.Contains(out, "should-not-leak") || strings.Contains(out, "leaky") {
		t.Fatalf("cross-op credential metadata leaked into export: %q", out)
	}
	if !strings.Contains(out, "no longer available") {
		t.Fatalf("expected a tombstone the reader can understand; got %q", out)
	}
}

func TestLowerCredentialFences_TombstonesMissingCredential(t *testing.T) {
	opID := uuid.New()
	missingID := uuid.New()
	lookup := &fakeCredLookup{by: map[uuid.UUID]models.Credential{}}

	out, written, tomb := LowerCredentialFences(context.Background(), fence(missingID.String()), opID, lookup, CredentialValues)
	if written != 0 || tomb != 1 {
		t.Fatalf("expected written=0 tomb=1, got written=%d tomb=%d", written, tomb)
	}
	if !strings.Contains(out, "no longer available") {
		t.Fatalf("expected tombstone, got %q", out)
	}
	// The id went with the fence: it names a row in a database the reader
	// of this zip has no access to.
	if strings.Contains(out, missingID.String()) {
		t.Fatalf("tombstone still carries the uuid: %q", out)
	}
}

func TestLowerCredentialFences_RepeatedIDsResolveOnce(t *testing.T) {
	opID := uuid.New()
	credID := uuid.New()
	calls := 0
	lookup := &countingLookup{
		inner: &fakeCredLookup{by: map[uuid.UUID]models.Credential{
			credID: {CredentialID: credID, OperationID: opID, Name: "n"},
		}},
		calls: &calls,
	}
	body := fence(credID.String()) + "\n\npara\n\n" + fence(credID.String())

	_, written, tomb := LowerCredentialFences(context.Background(), body, opID, lookup, CredentialValues)
	if written != 1 {
		t.Fatalf("counter should increment once for unique ids, got %d", written)
	}
	if tomb != 0 {
		t.Fatalf("expected no tombstones, got %d", tomb)
	}
	if calls != 1 {
		t.Fatalf("repository should be hit once per unique id, got %d", calls)
	}
}

// A zip exported before this change carries JSON tombstones. Re-exporting
// such a page lowers them like any other fence rather than passing the JSON
// through.
func TestLowerCredentialFences_LowersExistingTombstone(t *testing.T) {
	opID := uuid.New()
	credID := uuid.New()
	lookup := &fakeCredLookup{by: map[uuid.UUID]models.Credential{}}
	body := "```logos-credential\n{\n  \"id\": \"" + credID.String() + "\",\n  \"deleted\": true\n}\n```"

	out, written, tomb := LowerCredentialFences(context.Background(), body, opID, lookup, CredentialValues)
	if written != 0 || tomb != 1 {
		t.Fatalf("got written=%d tomb=%d", written, tomb)
	}
	if strings.Contains(out, "logos-credential") || strings.Contains(out, "deleted") {
		t.Fatalf("tombstone fence survived: %q", out)
	}
}

// A fence whose body is not the JSON we write is left exactly as it is: it
// is somebody's code block, and mangling it would lose page content.
func TestLowerCredentialFences_LeavesUnreadableFenceAlone(t *testing.T) {
	body := "```logos-credential\nnot json at all\n```"
	out, written, tomb := LowerCredentialFences(context.Background(), body, uuid.New(), &fakeCredLookup{}, CredentialValues)
	if out != body || written != 0 || tomb != 0 {
		t.Fatalf("out=%q written=%d tomb=%d", out, written, tomb)
	}
}

// Backticks in a secret must not break out of the code span, and a key
// containing a fence must not close the block early.
func TestLowerCredentialFences_EscapesAwkwardValues(t *testing.T) {
	opID := uuid.New()
	credID := uuid.New()
	lookup := &fakeCredLookup{by: map[uuid.UUID]models.Credential{
		credID: {
			CredentialID: credID, OperationID: opID, Name: "awkward",
			Password: "a`b``c",
			Keys:     []models.CredentialKey{{Name: "k", Content: "```\nnested\n```"}},
		},
	}}

	out, _, _ := LowerCredentialFences(context.Background(), fence(credID.String()), opID, lookup, CredentialValues)
	if !strings.Contains(out, "``` a`b``c ```") {
		t.Errorf("password code span not widened for its backticks:\n%s", out)
	}
	if !strings.Contains(out, "````\n```\nnested\n```\n````") {
		t.Errorf("key fence not widened for the fence inside it:\n%s", out)
	}
}

// countingLookup counts FindByID invocations to prove memoisation works.
type countingLookup struct {
	inner CredentialLookup
	calls *int
}

func (c *countingLookup) FindByID(ctx context.Context, id uuid.UUID) (models.Credential, error) {
	*c.calls++
	return c.inner.FindByID(ctx, id)
}
