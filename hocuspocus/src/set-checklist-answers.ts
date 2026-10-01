// Set checklist answers by key on a live wiki document.
//
// This is the checklist-specific companion to apply-markdown.ts. Instead of
// matching text or replacing the whole body, it addresses checklist items by
// their stable UUID key and replaces their content region — one Y.js
// transaction, one persistence event, one broadcast.
//
// The motivating case is an agent filling a 20-item recon template: today
// that is 20 fragile exact-string-match edits; with this endpoint it is one
// call with an answers array.

import type { Express, Request, Response } from "express";
import type { Hocuspocus } from "@hocuspocus/server";
import * as Y from "yjs";
import { XmlElement, XmlFragment, XmlText } from "yjs";
import { prosemirrorJSONToYDoc } from "y-prosemirror";
import { wikiSchema } from "./wiki-schema.js";
import { parseOutlineMarkdown } from "./markdown-parser.js";
import { collectChecklistCoverage, type ChecklistCoverage } from "./references.js";
import { WIKI_SCHEMA_VERSION } from "./wiki-schema-version.js";
import { debounceMs } from "./persistence.js";
import { readRawBody, requireSignature } from "./internal-auth.js";

const MAX_BODY_BYTES = 1024 * 1024;

const Y_FRAGMENT_FIELD = "default";

const CHECKLIST_ITEM_NODE = "wikiChecklistItem";

interface AnswerInput {
  key: string;
  markdown: string;
}

interface SetChecklistAnswersRequest {
  documentId?: string;
  userId?: string;
  answers?: AnswerInput[];
}

interface AnswerResult {
  key: string;
  status: "ok" | "not_found" | "parse_error";
}

function roomName(documentId: string): string {
  return `wiki/${documentId}`;
}

function findChecklistItemByKey(
  node: XmlFragment | XmlElement,
  key: string,
): XmlElement | null {
  for (const child of node.toArray()) {
    if (!(child instanceof XmlElement)) continue;
    if (child.nodeName === CHECKLIST_ITEM_NODE) {
      if (child.getAttribute("key") === key) return child;
      continue;
    }
    const found = findChecklistItemByKey(child, key);
    if (found) return found;
  }
  return null;
}

function cloneNode(node: unknown): XmlElement | XmlText {
  if (node instanceof XmlText) {
    const copy = new XmlText();
    copy.applyDelta(node.toDelta());
    return copy;
  }
  if (node instanceof XmlElement) {
    const copy = new XmlElement(node.nodeName);
    for (const [k, v] of Object.entries(node.getAttributes())) {
      if (v !== undefined && v !== null) copy.setAttribute(k, v as string);
    }
    const children = node.toArray().map(cloneNode);
    if (children.length > 0) copy.insert(0, children);
    return copy;
  }
  if (node instanceof XmlFragment) {
    return new XmlElement("paragraph");
  }
  return new XmlText();
}

function markdownToDetachedNodes(markdown: string): (XmlElement | XmlText)[] {
  const pmDoc = parseOutlineMarkdown(markdown);
  const scratch = prosemirrorJSONToYDoc(wikiSchema, pmDoc.toJSON(), Y_FRAGMENT_FIELD);
  try {
    const fragment = scratch.getXmlFragment(Y_FRAGMENT_FIELD);
    return fragment.toArray().map(cloneNode);
  } finally {
    scratch.destroy();
  }
}

function replaceItemContent(item: XmlElement, markdown: string): boolean {
  const nodes = markdownToDetachedNodes(markdown);
  // Clear existing content
  if (item.length > 0) {
    item.delete(0, item.length);
  }
  // Insert new content; ensure at least one empty paragraph so the item
  // always has valid block+ content.
  if (nodes.length > 0) {
    item.insert(0, nodes);
  } else {
    const p = new XmlElement("paragraph");
    p.insert(0, [new XmlText()]);
    item.insert(0, [p]);
  }
  return true;
}

// Guard the in-memory Y.js doc against destructive counter-updates from
// connected browser clients. When the operator has the page open, the
// browser's y-prosemirror sync plugin may generate a counter-update that
// drops checklist items — this observer catches that and restores the doc
// from a snapshot taken immediately after the API write.
function guardAgainstCounterUpdates(
  doc: Y.Doc,
  postWriteTotal: number,
): void {
  if (postWriteTotal <= 0) return;

  const snapshot = Y.encodeStateAsUpdate(doc);
  let armed = true;

  const observer = (update: Uint8Array, origin: unknown) => {
    if (!armed) return;
    // Ignore updates from the same direct-connection origin (our own writes).
    if (origin === "hocuspocus-direct") return;

    const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);
    const current = collectChecklistCoverage(fragment);

    if (current.total < postWriteTotal) {
      console.warn(
        `set-checklist-answers: counter-update dropped checklist items ` +
          `(${postWriteTotal} → ${current.total}), restoring snapshot`,
      );
      armed = false;
      doc.off("update", observer);
      Y.applyUpdate(doc, snapshot);
    }
  };

  doc.on("update", observer);

  // Remove the guard after the debounce window + margin. By then persistence
  // has fired and the snapshot is stored; further updates are normal edits.
  const guardMs = debounceMs + 1000;
  setTimeout(() => {
    if (armed) {
      armed = false;
      doc.off("update", observer);
    }
  }, guardMs);
}

export function setupSetChecklistAnswersApi(app: Express, server: Hocuspocus): void {
  app.post(
    "/internal/set-checklist-answers",
    readRawBody(MAX_BODY_BYTES),
    async (req: Request, res: Response) => {
      const rawBody = requireSignature(req, res);
      if (!rawBody) return;

      let parsed: SetChecklistAnswersRequest;
      try {
        parsed = JSON.parse(rawBody.toString("utf8")) as SetChecklistAnswersRequest;
      } catch {
        res.status(400).json({ error: "malformed JSON" });
        return;
      }

      const { documentId, answers } = parsed;
      if (typeof documentId !== "string" || documentId === "") {
        res.status(400).json({ error: "documentId field required" });
        return;
      }
      if (!Array.isArray(answers) || answers.length === 0) {
        res.status(400).json({ error: "answers array required and must not be empty" });
        return;
      }
      for (const a of answers) {
        if (typeof a.key !== "string" || a.key === "") {
          res.status(400).json({ error: "each answer must have a non-empty key" });
          return;
        }
        if (typeof a.markdown !== "string") {
          res.status(400).json({ error: "each answer must have a markdown field" });
          return;
        }
      }

      let connection;
      try {
        connection = await server.openDirectConnection(roomName(documentId), {
          agent: true,
          userId: parsed.userId,
          schemaVersion: WIKI_SCHEMA_VERSION,
        });

        const results: AnswerResult[] = [];
        let coverage: ChecklistCoverage = { total: 0, required: 0, answered: 0 };
        let ydoc: Y.Doc | undefined;

        await connection.transact((document) => {
          ydoc = document;
          const fragment = document.getXmlFragment(Y_FRAGMENT_FIELD);

          // Wrap ALL mutations in a single Y.js transaction so connected
          // browsers receive one atomic update. Without this, each
          // item.delete() and item.insert() inside replaceItemContent emits
          // a separate update — the browser sees the delete first, leaving
          // a checklist item with zero children (violating block+), and
          // y-prosemirror may normalize it away before the insert arrives.
          document.transact(() => {
            for (const answer of answers) {
              const item = findChecklistItemByKey(fragment, answer.key);
              if (!item) {
                results.push({ key: answer.key, status: "not_found" });
                continue;
              }
              try {
                replaceItemContent(item, answer.markdown);
                results.push({ key: answer.key, status: "ok" });
              } catch (err) {
                console.error(`set-checklist-answers: parse error for key ${answer.key}:`, err);
                results.push({ key: answer.key, status: "parse_error" });
              }
            }
          });

          coverage = collectChecklistCoverage(fragment);
        });

        // Guard the doc against destructive counter-updates from a connected
        // browser. The operator's y-prosemirror sync can generate updates that
        // drop checklist items; this observer catches and reverts them.
        if (ydoc && coverage.total > 0) {
          guardAgainstCounterUpdates(ydoc, coverage.total);
        }

        const connections =
          server.documents.get(roomName(documentId))?.getConnections().length ?? 0;

        res.status(200).json({
          results,
          checklist: coverage,
          watchers: connections,
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : "set-checklist-answers failed";
        console.error("set-checklist-answers error:", err);
        res.status(500).json({ error: message });
      } finally {
        if (connection) {
          try {
            await connection.disconnect();
          } catch (err) {
            console.warn("set-checklist-answers: failed to close direct connection:", err);
          }
        }
      }
    },
  );
}
