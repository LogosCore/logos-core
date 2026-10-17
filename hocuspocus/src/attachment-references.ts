// Which images and files a stored page body uses.
//
// The attachment sweeper deletes a blob once nothing references it, and for a
// live page "references" is the image_references / file_references arrays
// store() writes on every save. A backup is a body too — restoring one puts
// its pictures back on the page — so backups need the same two arrays, or the
// sweeper reclaims an image the moment the live page stops using it and every
// later restore brings back an empty frame.
//
// This derives them from a backup's stored bytes, by the same rules store()
// applies to a live page, so a backup and the page it was taken from agree
// about what they reference.

import type { Express, Request, Response } from "express";
import * as Y from "yjs";

import { deriveDrawingProjection } from "./drawing-projection.js";
import { collectFileReferenceIds, collectImageReferenceIds } from "./references.js";
import { readRawBody, requireSignature } from "./internal-auth.js";
import { applyStoredState } from "./stored-state.js";

const MAX_BODY_BYTES = 16 * 1024 * 1024;

const IMAGE_URL_RE = /\/api\/v1\/wiki\/images\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/gi;
const FILE_URL_RE = /\/api\/v1\/wiki\/files\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/gi;

export interface AttachmentReferences {
  imageReferences: string[];
  fileReferences: string[];
}

function urlIDs(text: string, re: RegExp, into: Set<string>): void {
  for (const match of text.matchAll(re)) into.add(match[1].toLowerCase());
}

/**
 * The attachments a body references.
 *
 * `text` is whatever plain body travels beside the state: the textarea-era
 * text, or the Markdown of a page saved before it was ever opened. Neither has
 * nodes to walk, so their attachment URLs are matched as text — which can only
 * err towards keeping a blob, never towards deleting one in use.
 */
export function attachmentReferencesOf(
  doc: Y.Doc,
  kind: "document" | "drawing",
  text = "",
): AttachmentReferences {
  const images = new Set<string>();
  const files = new Set<string>();

  if (kind === "drawing") {
    for (const id of deriveDrawingProjection(doc).imageReferences) images.add(id);
  } else {
    const fragment = doc.getXmlFragment("default");
    for (const id of collectImageReferenceIds(fragment)) images.add(id);
    for (const id of collectFileReferenceIds(fragment)) files.add(id);
    for (const plain of [doc.getText("content").toString(), text]) {
      urlIDs(plain, IMAGE_URL_RE, images);
      urlIDs(plain, FILE_URL_RE, files);
    }
  }

  return { imageReferences: [...images], fileReferences: [...files] };
}

export function setupAttachmentReferencesApi(app: Express): void {
  // POST /internal/attachment-references
  //
  //   Request  { contentState: <base64>, kind: "document"|"drawing", text? }
  //   Response { imageReferences: [uuid], fileReferences: [uuid] }
  //
  // A body that cannot be decoded is a 422, and the caller must treat it as
  // unknown rather than as referencing nothing: an empty answer is what lets
  // the sweeper delete.
  app.post(
    "/internal/attachment-references",
    readRawBody(MAX_BODY_BYTES),
    (req: Request, res: Response) => {
      const rawBody = requireSignature(req, res);
      if (!rawBody) return;

      let body: { contentState?: unknown; kind?: unknown; text?: unknown };
      try {
        body = JSON.parse(rawBody.toString("utf8"));
      } catch {
        res.status(400).json({ error: "malformed JSON" });
        return;
      }
      if (typeof body.contentState !== "string") {
        res.status(400).json({ error: "contentState field required" });
        return;
      }
      const kind = body.kind === "drawing" ? "drawing" : "document";
      const text = typeof body.text === "string" ? body.text : "";

      const doc = new Y.Doc();
      try {
        applyStoredState(doc, new Uint8Array(Buffer.from(body.contentState, "base64")));
      } catch {
        res.status(422).json({ error: "contentState is not a valid Y.js update" });
        doc.destroy();
        return;
      }
      try {
        res.status(200).json(attachmentReferencesOf(doc, kind, text));
      } finally {
        doc.destroy();
      }
    },
  );
}
