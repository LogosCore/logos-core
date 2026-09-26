package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// markdownRenderer hands back the page's stored state as its markdown, standing
// in for the sidecar. The fixture's pages carry editor markdown verbatim.
type markdownRenderer struct{}

func (markdownRenderer) YjsToMarkdown(_ context.Context, state []byte) (string, error) {
	return string(state), nil
}

type markdownWorld struct {
	op       uuid.UUID
	viewer   uuid.UUID
	operator uuid.UUID
	cred     models.Credential
	host     models.Host
	linked   models.WikiDocument
	doc      models.WikiDocument
	resolver *wikiDocumentResolver
}

func newMarkdownWorld(t *testing.T) *markdownWorld {
	t.Helper()
	w := &markdownWorld{op: uuid.New(), viewer: uuid.New(), operator: uuid.New()}

	w.cred = models.Credential{
		CredentialID: uuid.New(), OperationID: w.op, Name: "svc-backup",
		Type: models.CredentialTypePassword, Username: "svc", Password: "s3cret",
		Validity: models.CredentialValidityValid,
	}
	w.host = models.Host{HostID: uuid.New(), OperationID: w.op, Hostname: "dc01.meridian.local"}
	w.linked = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Server VLAN"}
	w.doc = models.WikiDocument{
		DocumentID: uuid.New(), OperationID: w.op, Title: "dc01",
		ContentState: []byte(
			"Runs on [host](logos://host/" + w.host.HostID.String() + "), see " +
				"[page](logos://doc/" + w.linked.DocumentID.String() + ").\n\n" +
				"```logos-credential\n{\"id\": \"" + w.cred.CredentialID.String() + "\"}\n```\n\n" +
				":::checklist {\"prompt\":\"Shell accounts?\",\"state\":\"answered\",\"key\":\"shells\"}\nThree.\n:::\n"),
	}

	op := models.Operation{
		OperationID: w.op,
		Members: []models.OperationMember{
			{UserID: w.viewer, Role: models.OperationRoleViewer},
			{UserID: w.operator, Role: models.OperationRoleOperator},
		},
	}
	w.resolver = &wikiDocumentResolver{
		docRepo:  &markdownDocRepo{docs: []models.WikiDocument{w.doc, w.linked}},
		credRepo: &markdownCredRepo{cred: w.cred},
		hostRepo: &markdownHostRepo{host: w.host},
		operationRepo: &mockOpRepo{
			findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) { return op, nil },
		},
		renderer: markdownRenderer{},
	}
	return w
}

func (w *markdownWorld) render(t *testing.T, caller uuid.UUID) string {
	t.Helper()
	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: caller.String()})
	out, err := w.resolver.WikiDocumentMarkdown(ctx, w.doc.DocumentID.String())
	if err != nil {
		t.Fatalf("WikiDocumentMarkdown: %v", err)
	}
	return out
}

// The copy a viewer takes must not carry the secret. They may read the page
// — the chip on it already says a credential belongs here — but this is a
// surface whose whole purpose is to put text somewhere else.
func TestWikiDocumentMarkdown_ViewerGetsCredentialNameNotValues(t *testing.T) {
	w := newMarkdownWorld(t)
	out := w.render(t, w.viewer)

	if strings.Contains(out, "s3cret") || strings.Contains(out, "- Username:") {
		t.Errorf("a viewer's copy carries the secret:\n%s", out)
	}
	if !strings.Contains(out, "**Credential: svc-backup** (password, held in Logos)") {
		t.Errorf("a viewer should still be told which credential this is:\n%s", out)
	}
}

// An operator gets what an export would have given them.
func TestWikiDocumentMarkdown_OperatorGetsCredentialValues(t *testing.T) {
	w := newMarkdownWorld(t)
	out := w.render(t, w.operator)

	for _, want := range []string{
		"**Credential: svc-backup** (password, valid)",
		"- Username: `svc`",
		"- Password: `s3cret`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Everything else is lowered the same way for everyone.
func TestWikiDocumentMarkdown_LowersChipsAndChecklists(t *testing.T) {
	w := newMarkdownWorld(t)
	out := w.render(t, w.viewer)

	if strings.Contains(out, "logos://") || strings.Contains(out, ":::checklist") || strings.Contains(out, "logos-credential") {
		t.Errorf("Logos-only markup survived:\n%s", out)
	}
	if !strings.Contains(out, "Runs on dc01.meridian.local") {
		t.Errorf("host chip not lowered to its hostname:\n%s", out)
	}
	// A page chip points at the page in the app; the browser makes it
	// absolute on the way out.
	if !strings.Contains(out, "[Server VLAN](/wiki/"+w.linked.DocumentID.String()+")") {
		t.Errorf("page chip not lowered to an app link:\n%s", out)
	}
	if !strings.Contains(out, "- [x] **Shell accounts?**") {
		t.Errorf("checklist not lowered to a task list item:\n%s", out)
	}
}

// --- stubs -----------------------------------------------------------------

type markdownDocRepo struct {
	repository.IWikiDocumentRepository
	docs []models.WikiDocument
}

func (s *markdownDocRepo) FindByID(_ context.Context, id uuid.UUID) (models.WikiDocument, error) {
	for _, d := range s.docs {
		if d.DocumentID == id {
			return d, nil
		}
	}
	return models.WikiDocument{}, repository.ErrNotFound
}

type markdownCredRepo struct {
	repository.ICredentialRepository
	cred models.Credential
}

func (s *markdownCredRepo) FindByID(_ context.Context, id uuid.UUID) (models.Credential, error) {
	if id == s.cred.CredentialID {
		return s.cred, nil
	}
	return models.Credential{}, repository.ErrNotFound
}

type markdownHostRepo struct {
	repository.IHostRepository
	host models.Host
}

func (s *markdownHostRepo) FindByID(_ context.Context, id uuid.UUID) (models.Host, error) {
	if id == s.host.HostID {
		return s.host, nil
	}
	return models.Host{}, repository.ErrNotFound
}
