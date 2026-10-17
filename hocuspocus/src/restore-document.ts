// Restore a prose page to a backup, inside its live room.
//
// The prose counterpart of restore-drawing.ts, for the same reason: writing a
// backup's bytes over content_state does not stick. A loaded room never
// re-reads Mongo and writes its own state back on the next store, and a
// browser that reconnects holding the newer state merges it into the restored
// bytes — a CRDT merge is a union, so every paragraph typed since the backup
// comes back alongside the restored ones.
//
// So the backup is applied as an edit. Its top-level blocks are copied into
// the live fragment with the same minimal splice apply-markdown's replace
// uses: blocks the backup shares with the page are left where they are, which
// keeps the carets of anyone typing in them, and only the span that differs
// is rewritten. Deleting and inserting Y.js items is something every peer
// takes as it is, so — unlike a drawing — there are no versions to outrank.
//
// The splice compares blocks exactly. apply-markdown's comparison ignores
// attributes the server schema does not declare, because Markdown could never
// carry them; a backup does carry them, and a restore that kept the current
// copy of a block differing only in one of those would not be a restore.

import type { Express, Request, Response } from "express";
import type { Hocuspocus } from "@hocuspocus/server";
import * as Y from "yjs";

import {
  cloneNode,
  markdownToDetachedBlocks,
  spliceFragment,
  type DetachedBlock,
} from "./apply-markdown.js";
import { extractTextFromFragment } from "./projection.js";
import { attachmentReferencesOf, type AttachmentReferences } from "./attachment-references.js";
import { Y_FRAGMENT_FIELD } from "./markdown-to-yjs.js";
import { WIKI_SCHEMA_VERSION } from "./wiki-schema-version.js";
import { readRawBody, requireSignature } from "./internal-auth.js";
import { applyStoredState } from "./stored-state.js";

/** The textarea-era body: plain text under its own root key, from before the
 * rich editor. The editor migrates it into the fragment when it opens a page
 * whose fragment is empty. */
const LEGACY_TEXT_FIELD = "content";

const MAX_BODY_BYTES = 16 * 1024 * 1024;

/**
 * A block's exact identity: its name, every attribute, and every inline mark.
 * Two blocks with the same key are interchangeable for a restore.
 */
export function exactKey(node: unknown): string {
  if (node instanceof Y.XmlText) {
    return "#text:" + JSON.stringify(node.toDelta());
  }
  if (node instanceof Y.XmlElement) {
    const attrs = Object.entries(node.getAttributes())
      .filter(([, value]) => value !== undefined && value !== null)
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([key, value]) => ` ${key}=${JSON.stringify(value)}`)
      .join("");
    const children = node.toArray().map(exactKey).join("");
    return `<${node.nodeName}${attrs}>${children}</${node.nodeName}>`;
  }
  return String(node);
}

export interface RestoreDocumentResult {
  /** Top-level blocks the restored page holds. */
  blocks: number;
  /** Where the body came from: the backup's editor content, its
   * textarea-era text, or — for a backup taken before the page had ever been
   * opened — the Markdown it was created with. */
  source: "fragment" | "legacy-text" | "markdown" | "empty";
}

/**
 * The backup's body as detached blocks, ready to splice into a live fragment.
 *
 * Keys are taken while the nodes are still attached to the backup document:
 * a detached Y.js type refuses to be read.
 */
function backupBlocks(backup: Y.Doc, fallbackMarkdown: string): {
  blocks: DetachedBlock[];
  source: RestoreDocumentResult["source"];
} {
  const fragment = backup.getXmlFragment(Y_FRAGMENT_FIELD);
  if (fragment.length > 0) {
    return {
      blocks: fragment.toArray().map((node) => ({ node: cloneNode(node), key: exactKey(node) })),
      source: "fragment",
    };
  }

  // No editor content. A textarea-era backup holds its body as plain text,
  // and one taken of a page nobody had opened holds only the Markdown the
  // page was created with. Either is read the way the rest of the wiki reads
  // Markdown, which is what the editor would have done with it on open.
  const legacy = backup.getText(LEGACY_TEXT_FIELD).toString();
  const markdown = legacy !== "" ? legacy : fallbackMarkdown;
  if (markdown.trim() === "") return { blocks: [], source: "empty" };
  return {
    blocks: markdownToDetachedBlocks(markdown, exactKey),
    source: legacy !== "" ? "legacy-text" : "markdown",
  };
}

/**
 * Make `target`'s body the body held in `backupState`.
 *
 * Must be called inside a transaction on the target, so collaborators see one
 * change rather than the page emptying and refilling.
 */
export function restoreDocumentInto(
  target: Y.Doc,
  backupState: Uint8Array,
  fallbackMarkdown = "",
): RestoreDocumentResult {
  const backup = new Y.Doc();
  try {
    applyStoredState(backup, backupState);
    const { blocks, source } = backupBlocks(backup, fallbackMarkdown);

    const fragment = target.getXmlFragment(Y_FRAGMENT_FIELD);
    spliceFragment(fragment, blocks, exactKey);

    // A page that was never opened in the rich editor still has its body in
    // the legacy text, and the editor migrates that only into an *empty*
    // fragment. Left behind, it would come back the moment a restore emptied
    // the page. The restored fragment is the whole body now.
    const legacy = target.getText(LEGACY_TEXT_FIELD);
    if (legacy.length > 0) legacy.delete(0, legacy.length);

    return { blocks: fragment.length, source };
  } finally {
    backup.destroy();
  }
}

/** A prose page's search text, as persistence.ts derives it. */
function contentOf(doc: Y.Doc): string {
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);
  if (fragment.length > 0) return extractTextFromFragment(fragment);
  return doc.getText(LEGACY_TEXT_FIELD).toString();
}

export function setupRestoreDocumentApi(app: Express, server: Hocuspocus): void {
  //   Request  { documentId, contentState: <base64 backup state>,
  //              markdown?: <backup's stored content>, userId? }
  //   Response { ok, blocks, source, watchers,
  //              previousState: <base64>, previousContent,
  //              previousImageReferences, previousFileReferences }
  //
  // previousState is the room as it stood just before the restore, for the
  // caller's pre-restore safety backup — see the drawing route.
  app.post(
    "/internal/document/restore",
    readRawBody(MAX_BODY_BYTES),
    async (req: Request, res: Response) => {
      const rawBody = requireSignature(req, res);
      if (!rawBody) return;

      let body: { documentId?: unknown; contentState?: unknown; markdown?: unknown; userId?: unknown };
      try {
        body = JSON.parse(rawBody.toString("utf8"));
      } catch {
        res.status(400).json({ error: "malformed JSON" });
        return;
      }

      const documentId = body.documentId;
      if (typeof documentId !== "string" || documentId === "") {
        res.status(400).json({ error: "documentId field required" });
        return;
      }
      if (typeof body.contentState !== "string") {
        res.status(400).json({ error: "contentState field required" });
        return;
      }
      const markdown = typeof body.markdown === "string" ? body.markdown : "";
      const backupState = new Uint8Array(Buffer.from(body.contentState, "base64"));

      // Decoded before a seat is taken, so a corrupt backup is refused
      // without touching the room.
      try {
        applyStoredState(new Y.Doc(), backupState);
      } catch {
        res.status(422).json({ error: "contentState is not a valid Y.js update" });
        return;
      }

      let connection;
      try {
        connection = await server.openDirectConnection(`wiki/${documentId}`, {
          agent: true,
          userId: body.userId,
          // Without it the write counts as schema 0, and persistence.ts
          // takes any restore that shortens the page for a stale browser tab
          // pruning nodes it does not know — and discards it, while this
          // route reports success. A restore is a deliberate shrink.
          schemaVersion: WIKI_SCHEMA_VERSION,
        });

        let previousState: Uint8Array = new Uint8Array();
        let previousContent = "";
        let previousRefs: AttachmentReferences = { imageReferences: [], fileReferences: [] };
        let result: RestoreDocumentResult = { blocks: 0, source: "empty" };
        await connection.transact((document) => {
          previousState = Y.encodeStateAsUpdate(document);
          previousContent = contentOf(document);
          previousRefs = attachmentReferencesOf(document, "document");
          // One Y.js transaction: collaborators see the restore land whole.
          document.transact(() => {
            result = restoreDocumentInto(document, backupState, markdown);
          });
        });

        res.status(200).json({
          ok: true,
          ...result,
          watchers: server.documents.get(`wiki/${documentId}`)?.getConnections().length ?? 0,
          previousState: Buffer.from(previousState).toString("base64"),
          previousContent,
          previousImageReferences: previousRefs.imageReferences,
          previousFileReferences: previousRefs.fileReferences,
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : "restore failed";
        console.error("document restore error:", err);
        res.status(500).json({ error: message });
      } finally {
        if (connection) {
          try {
            await connection.disconnect();
          } catch (err) {
            console.warn("document restore: failed to close direct connection:", err);
          }
        }
      }
    },
  );
}
