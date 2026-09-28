package resolver

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A drawing renders a fixed glyph in the default colour, so the server stores
// no emoji, icon or colour for one. It used to be enforced only by the SPA's
// glyph lock, and a stored icon leaked into the browser tab, which read
// doc.icon without checking the kind.

func TestCreateWikiDocument_RefusesAnIconOnADrawing(t *testing.T) {
	repo := &stubDocRepo{byID: map[uuid.UUID]models.WikiDocument{}}
	r := newInstantiateResolver(repo)
	drawing := models.WikiDocumentKindDrawing
	icon := "Adaptive"

	_, err := r.CreateWikiDocument(adminCtx(uuid.New()), uuid.New().String(), model.CreateWikiDocumentInput{
		Title: "Network map",
		Kind:  &drawing,
		Icon:  &icon,
	})
	if !errors.Is(err, models.ErrFixedIcon) {
		t.Fatalf("err = %v, want ErrFixedIcon", err)
	}
	if repo.created != nil {
		t.Error("the drawing was created anyway")
	}
}

func TestUpdateWikiDocument_RefusesAnIconOnADrawing(t *testing.T) {
	id := uuid.New()
	repo := &stubDocRepo{byID: map[uuid.UUID]models.WikiDocument{
		id: {DocumentID: id, OperationID: uuid.New(), Title: "Map", Kind: models.WikiDocumentKindDrawing},
	}}
	r := newInstantiateResolver(repo)

	for name, input := range map[string]model.UpdateWikiDocumentInput{
		"icon":  {Icon: ptr("Network")},
		"emoji": {Emoji: ptr("🗺️")},
		"color": {Color: ptr("#F44336")},
	} {
		_, err := r.UpdateWikiDocument(adminCtx(uuid.New()), id.String(), input)
		if !errors.Is(err, models.ErrFixedIcon) {
			t.Errorf("%s: err = %v, want ErrFixedIcon", name, err)
		}
	}
	if repo.updated != nil {
		t.Errorf("wrote %v despite refusing", repo.updated)
	}
}

// A drawing template forks into a drawing. Whatever identity the template
// still carries from before the rule is not copied, and an explicit override
// is refused like on create.
func TestInstantiateTemplate_DrawingTakesNoIcon(t *testing.T) {
	templateID := uuid.New()
	repo := &stubDocRepo{byID: map[uuid.UUID]models.WikiDocument{templateID: {
		DocumentID:  templateID,
		OperationID: uuid.New(),
		IsTemplate:  true,
		Title:       "Topology",
		Kind:        models.WikiDocumentKindDrawing,
		Icon:        "Adaptive",
		Color:       "#E91E63",
	}}}
	r := newInstantiateResolver(repo)

	got, err := r.InstantiateTemplate(adminCtx(uuid.New()), templateID.String(), uuid.New().String(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Emoji != "" || got.Icon != "" || got.Color != "" {
		t.Errorf("instance identity = %q/%q/%q, want all empty", got.Emoji, got.Icon, got.Color)
	}

	_, err = r.InstantiateTemplate(adminCtx(uuid.New()), templateID.String(), uuid.New().String(), nil, nil, nil, ptr("Server"), nil)
	if !errors.Is(err, models.ErrFixedIcon) {
		t.Fatalf("override: err = %v, want ErrFixedIcon", err)
	}
}
