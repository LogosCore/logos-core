package wiki

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/logoscore/logos-core/core/pkg/models"
)

type documentRestoreRequest struct {
	DocumentID   string `json:"documentId"`
	ContentState string `json:"contentState"`
	Markdown     string `json:"markdown,omitempty"`
	UserID       string `json:"userId,omitempty"`
}

// RestoreDocumentResult reports what a prose restore did to the live page.
type RestoreDocumentResult struct {
	// Blocks is how many top-level blocks the restored page holds.
	Blocks int `json:"blocks"`
	// Source is where the body came from: "fragment" (the backup's editor
	// content), "legacy-text", "markdown" (a backup with no state) or "empty".
	Source   string `json:"source"`
	Watchers int    `json:"watchers"`
	// PreviousState and PreviousContent are the page as it stood just before
	// the restore — the pre-restore safety backup. See RestoreDrawingResult.
	PreviousState   []byte `json:"-"`
	PreviousContent string `json:"previousContent"`
	// PreviousImageReferences and PreviousFileReferences are the attachments
	// that pre-restore page used, for indexing the safety backup.
	PreviousImageReferences []string `json:"previousImageReferences"`
	PreviousFileReferences  []string `json:"previousFileReferences"`
}

type documentRestoreWireResponse struct {
	RestoreDocumentResult
	PreviousState string `json:"previousState"`
}

// RestoreDocument puts a backup's body back on a prose page, as an edit in
// the live room — for the same reasons as RestoreDrawing. markdown is the
// backup's stored content, used only when its state holds no body at all (a
// backup of a page nobody had opened yet).
func (c *HocuspocusClient) RestoreDocument(ctx context.Context, documentID string, contentState []byte, markdown, userID string) (RestoreDocumentResult, error) {
	var result RestoreDocumentResult

	body, err := json.Marshal(documentRestoreRequest{
		DocumentID:   documentID,
		ContentState: base64.StdEncoding.EncodeToString(contentState),
		Markdown:     markdown,
		UserID:       userID,
	})
	if err != nil {
		return result, fmt.Errorf("marshal document restore payload: %w", err)
	}

	resp, err := c.postInternal(ctx, "/internal/document/restore", body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("document restore returned %d: %s", resp.StatusCode, string(errBody))
	}

	var wire documentRestoreWireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&wire); err != nil {
		return result, fmt.Errorf("read document restore response: %w", err)
	}
	result = wire.RestoreDocumentResult
	if result.PreviousState, err = base64.StdEncoding.DecodeString(wire.PreviousState); err != nil {
		return result, fmt.Errorf("decode document restore previous state: %w", err)
	}
	return result, nil
}

// ErrStateUndecodable is the sidecar refusing stored bytes as not a Y.js
// update. Distinct from a transport failure: retrying will not change it.
var ErrStateUndecodable = errors.New("stored state is not a valid Y.js update")

type attachmentReferencesRequest struct {
	ContentState string `json:"contentState"`
	Kind         string `json:"kind"`
	Text         string `json:"text,omitempty"`
}

// AttachmentReferences are the images and files a stored body uses.
type AttachmentReferences struct {
	ImageReferences []string `json:"imageReferences"`
	FileReferences  []string `json:"fileReferences"`
}

// AttachmentReferences derives the attachments a stored body uses — by the
// rules the sidecar's store() applies to a live page, since only the sidecar
// can read the bytes. text is the plain body stored beside the state (a
// backup's Content), matched for attachment URLs when the state holds no
// editor content.
//
// An error means the answer is unknown, and must never be read as "none":
// that is the answer that lets the sweeper delete.
func (c *HocuspocusClient) AttachmentReferences(ctx context.Context, contentState []byte, kind models.WikiDocumentKind, text string) (AttachmentReferences, error) {
	var refs AttachmentReferences

	body, err := json.Marshal(attachmentReferencesRequest{
		ContentState: base64.StdEncoding.EncodeToString(contentState),
		Kind:         string(kind.Or()),
		Text:         text,
	})
	if err != nil {
		return refs, fmt.Errorf("marshal attachment references payload: %w", err)
	}

	resp, err := c.postInternal(ctx, "/internal/attachment-references", body)
	if err != nil {
		return refs, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return refs, ErrStateUndecodable
	}
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return refs, fmt.Errorf("attachment references returned %d: %s", resp.StatusCode, string(errBody))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&refs); err != nil {
		return refs, fmt.Errorf("read attachment references response: %w", err)
	}
	return refs, nil
}
