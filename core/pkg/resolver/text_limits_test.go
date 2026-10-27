package resolver

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// Text caps count characters, as their messages say. "ж" is two bytes: a
// byte count refuses a text at the cap, which these tests check is taken.

func TestTaskTextCountsCharacters(t *testing.T) {
	if err := checkTaskName(strings.Repeat("ж", maxTaskNameLen)); err != nil {
		t.Errorf("a name of %d characters: %v", maxTaskNameLen, err)
	}
	if err := checkTaskName(strings.Repeat("ж", maxTaskNameLen+1)); err == nil {
		t.Errorf("a name of %d characters was taken", maxTaskNameLen+1)
	}
	if err := checkTaskDescription(strings.Repeat("ж", maxTaskDescriptionLen)); err != nil {
		t.Errorf("a description of %d characters: %v", maxTaskDescriptionLen, err)
	}
	if err := checkTaskDescription(strings.Repeat("ж", maxTaskDescriptionLen+1)); err == nil {
		t.Errorf("a description of %d characters was taken", maxTaskDescriptionLen+1)
	}
}

func TestSearchQueryCountsCharacters(t *testing.T) {
	q, err := searchQuery("  " + strings.Repeat("ж", maxSearchLength) + " ")
	if err != nil || utf8.RuneCountInString(q) != maxSearchLength {
		t.Errorf("a query of %d characters: %v", maxSearchLength, err)
	}
	if _, err := searchQuery(strings.Repeat("ж", maxSearchLength+1)); err == nil {
		t.Errorf("a query of %d characters was taken", maxSearchLength+1)
	}
}

func TestCopyTitleCutsBetweenCharacters(t *testing.T) {
	if got := copyTitle("Plan"); got != "Copy of Plan" {
		t.Errorf("copyTitle = %q", got)
	}
	got := copyTitle(strings.Repeat("ж", maxTitleLength))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != maxTitleLength || !strings.HasPrefix(got, "Copy of ж") {
		t.Errorf("%d characters, valid %v: %q", utf8.RuneCountInString(got), utf8.ValidString(got), got)
	}
}

func TestWikiTitleCountsCharacters(t *testing.T) {
	fits, over := strings.Repeat("ж", maxTitleLength), strings.Repeat("ж", maxTitleLength+1)

	repo := &stubDocRepo{byID: map[uuid.UUID]models.WikiDocument{}}
	r := newInstantiateResolver(repo)
	_, err := r.CreateWikiDocument(adminCtx(uuid.New()), uuid.New().String(), model.CreateWikiDocumentInput{Title: over})
	if err == nil || !strings.Contains(err.Error(), "title exceeds") {
		t.Errorf("a title of %d characters: %v", maxTitleLength+1, err)
	}

	templateID := uuid.New()
	repo = &stubDocRepo{byID: map[uuid.UUID]models.WikiDocument{templateID: {
		DocumentID: templateID, OperationID: uuid.New(), IsTemplate: true, Title: "Runbook",
	}}}
	r = newInstantiateResolver(repo)
	got, err := r.InstantiateTemplate(adminCtx(uuid.New()), templateID.String(), uuid.New().String(), nil, &fits, nil, nil, nil)
	if err != nil || got.Title != fits {
		t.Errorf("an instance titled with %d characters: %v", maxTitleLength, err)
	}
	if _, err := r.InstantiateTemplate(adminCtx(uuid.New()), templateID.String(), uuid.New().String(), nil, &over, nil, nil, nil); err == nil {
		t.Errorf("an instance titled with %d characters was made", maxTitleLength+1)
	}

	id := uuid.New()
	repo = &stubDocRepo{byID: map[uuid.UUID]models.WikiDocument{id: {DocumentID: id, OperationID: uuid.New(), Title: "Old"}}}
	r = newInstantiateResolver(repo)
	if _, err := r.UpdateWikiDocument(adminCtx(uuid.New()), id.String(), model.UpdateWikiDocumentInput{Title: &over}); err == nil ||
		!strings.Contains(err.Error(), "title exceeds") || repo.updated != nil {
		t.Errorf("a rename to %d characters: %v", maxTitleLength+1, err)
	}
}
