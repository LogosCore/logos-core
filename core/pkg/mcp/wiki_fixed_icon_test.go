package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/resolver"
	"go.uber.org/zap"
)

// A drawing renders a fixed glyph in the default colour. Agents used to give
// every drawing an icon — explicitly, or through the Adaptive default that
// create_wiki_page adds for prose pages — and it leaked into the browser tab.

type fixedIconWikiDocs struct {
	resolver.IWikiDocumentResolver
	created *model.CreateWikiDocumentInput
	updated *model.UpdateWikiDocumentInput
	doc     models.WikiDocument
}

func (f *fixedIconWikiDocs) CreateWikiDocument(_ context.Context, opID string, input model.CreateWikiDocumentInput) (*models.WikiDocument, error) {
	f.created = &input
	return &models.WikiDocument{
		DocumentID: uuid.New(), OperationID: uuid.MustParse(opID), Title: input.Title, Kind: input.Kind.Or(),
	}, nil
}

func (f *fixedIconWikiDocs) WikiDocument(context.Context, string) (*models.WikiDocument, error) {
	return &f.doc, nil
}

func (f *fixedIconWikiDocs) UpdateWikiDocument(_ context.Context, _ string, input model.UpdateWikiDocumentInput) (*models.WikiDocument, error) {
	f.updated = &input
	return &f.doc, nil
}

type publicOnlyOps struct {
	repository.IOperationRepository
}

func (publicOnlyOps) FindByID(context.Context, uuid.UUID) (models.Operation, error) {
	return models.SynthesizePublicOperation(), nil
}

func fixedIconServer() (*Server, *fixedIconWikiDocs, context.Context) {
	docs := &fixedIconWikiDocs{}
	s := &Server{deps: Deps{WikiDocs: docs, OperationRepo: publicOnlyOps{}, Logger: zap.NewNop()}}
	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{
		UserID: uuid.New().String(),
		Agent: &gqlctx.AgentInfo{
			MaxRole:         models.OperationRoleOperator,
			AllowWrites:     true,
			OperationScopes: []uuid.UUID{models.PublicOperationID},
		},
	})
	return s, docs, ctx
}

func TestCreateWikiDocument_DrawingGetsNoIcon(t *testing.T) {
	s, docs, ctx := fixedIconServer()
	_, err := handleCreateWikiDocument(ctx, s, createWikiDocumentArgs{
		OperationID: models.PublicOperationID.String(), Title: "Network map", Kind: "drawing",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := docs.created
	if in.Emoji != nil || in.Icon != nil || in.Color != nil {
		t.Errorf("drawing created with identity %v/%v/%v, want none — not even Adaptive", in.Emoji, in.Icon, in.Color)
	}

	// A prose page still gets the Adaptive default, as from the operator's dialog.
	if _, err := handleCreateWikiDocument(ctx, s, createWikiDocumentArgs{
		OperationID: models.PublicOperationID.String(), Title: "Notes",
	}); err != nil {
		t.Fatal(err)
	}
	if docs.created.Icon == nil || *docs.created.Icon != AdaptiveIconName {
		t.Errorf("prose page icon = %v, want %s", docs.created.Icon, AdaptiveIconName)
	}
}

func TestCreateWikiDocument_RefusesAnIconForADrawing(t *testing.T) {
	s, docs, ctx := fixedIconServer()
	_, err := handleCreateWikiDocument(ctx, s, createWikiDocumentArgs{
		OperationID: models.PublicOperationID.String(), Title: "Network map", Kind: "drawing",
		visualIdentity: visualIdentity{Color: "#E91E63"},
	})
	if err == nil || !strings.Contains(err.Error(), "fixed icon") {
		t.Fatalf("err = %v, want a refusal naming the fixed icon", err)
	}
	if docs.created != nil {
		t.Error("the drawing was created anyway")
	}
}

func TestUpdateWikiDocument_RefusesAnIconForADrawing(t *testing.T) {
	s, docs, ctx := fixedIconServer()
	docs.doc = models.WikiDocument{
		DocumentID: uuid.New(), OperationID: models.PublicOperationID, Title: "Map", Kind: models.WikiDocumentKindDrawing,
	}
	_, err := handleUpdateWikiDocument(ctx, s, updateWikiDocumentArgs{
		DocumentID: docs.doc.DocumentID.String(), visualIdentity: visualIdentity{Icon: "Network"},
	})
	if err == nil || !strings.Contains(err.Error(), "fixed icon") {
		t.Fatalf("err = %v, want a refusal naming the fixed icon", err)
	}
	if docs.updated != nil {
		t.Error("the icon was written anyway")
	}
}
