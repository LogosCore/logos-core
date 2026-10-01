// Set the content of keyed block nodes on a live wiki document.
//
// Generalises set-checklist-answers to any block node with a `key` attribute
// and a block+ content region. Currently that means wikiNotice; wikiChecklistItem
// has its own endpoint because it also carries checklist coverage.

import type { Express, Request, Response } from "express";
import type { Hocuspocus } from "@hocuspocus/server";
import { XmlElement, XmlFragment, XmlText } from "yjs";
import { prosemirrorJSONToYDoc } from "y-prosemirror";
import { wikiSchema } from "./wiki-schema.js";
import { parseOutlineMarkdown } from "./markdown-parser.js";
import { WIKI_SCHEMA_VERSION } from "./wiki-schema-version.js";
import { readRawBody, requireSignature } from "./internal-auth.js";

const MAX_BODY_BYTES = 1024 * 1024;

const Y_FRAGMENT_FIELD = "default";

const KEYED_BLOCK_NODES = new Set(["wikiNotice"]);

interface BlockContentInput {
  key: string;
  markdown: string;
}

interface SetBlockContentRequest {
  documentId?: string;
  userId?: string;
  blocks?: BlockContentInput[];
}

interface BlockResult {
  key: string;
  nodeType: string;
  status: "ok" | "not_found" | "parse_error";
}

function roomName(documentId: string): string {
  return `wiki/${documentId}`;
}

function findKeyedBlock(
  node: XmlFragment | XmlElement,
  key: string,
): XmlElement | null {
  for (const child of node.toArray()) {
    if (!(child instanceof XmlElement)) continue;
    if (KEYED_BLOCK_NODES.has(child.nodeName)) {
      if (child.getAttribute("key") === key) return child;
    }
    const found = findKeyedBlock(child, key);
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

function replaceBlockContent(block: XmlElement, markdown: string): boolean {
  const nodes = markdownToDetachedNodes(markdown);
  if (block.length > 0) {
    block.delete(0, block.length);
  }
  if (nodes.length > 0) {
    block.insert(0, nodes);
  } else {
    const p = new XmlElement("paragraph");
    p.insert(0, [new XmlText()]);
    block.insert(0, [p]);
  }
  return true;
}

export function setupSetBlockContentApi(app: Express, server: Hocuspocus): void {
  app.post(
    "/internal/set-block-content",
    readRawBody(MAX_BODY_BYTES),
    async (req: Request, res: Response) => {
      const rawBody = requireSignature(req, res);
      if (!rawBody) return;

      let parsed: SetBlockContentRequest;
      try {
        parsed = JSON.parse(rawBody.toString("utf8")) as SetBlockContentRequest;
      } catch {
        res.status(400).json({ error: "malformed JSON" });
        return;
      }

      const { documentId, blocks } = parsed;
      if (typeof documentId !== "string" || documentId === "") {
        res.status(400).json({ error: "documentId field required" });
        return;
      }
      if (!Array.isArray(blocks) || blocks.length === 0) {
        res.status(400).json({ error: "blocks array required and must not be empty" });
        return;
      }
      for (const b of blocks) {
        if (typeof b.key !== "string" || b.key === "") {
          res.status(400).json({ error: "each block must have a non-empty key" });
          return;
        }
        if (typeof b.markdown !== "string") {
          res.status(400).json({ error: "each block must have a markdown field" });
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

        const results: BlockResult[] = [];

        await connection.transact((document) => {
          const fragment = document.getXmlFragment(Y_FRAGMENT_FIELD);

          // Wrap in a Y.js transaction so connected browsers receive one
          // atomic update (same rationale as set-checklist-answers).
          document.transact(() => {
            for (const block of blocks) {
              const node = findKeyedBlock(fragment, block.key);
              if (!node) {
                results.push({ key: block.key, nodeType: "", status: "not_found" });
                continue;
              }
              try {
                replaceBlockContent(node, block.markdown);
                results.push({ key: block.key, nodeType: node.nodeName, status: "ok" });
              } catch (err) {
                console.error(`set-block-content: parse error for key ${block.key}:`, err);
                results.push({ key: block.key, nodeType: node.nodeName, status: "parse_error" });
              }
            }
          });
        });

        const connections =
          server.documents.get(roomName(documentId))?.getConnections().length ?? 0;

        res.status(200).json({ results, watchers: connections });
      } catch (err) {
        const message = err instanceof Error ? err.message : "set-block-content failed";
        console.error("set-block-content error:", err);
        res.status(500).json({ error: message });
      } finally {
        if (connection) {
          try {
            await connection.disconnect();
          } catch (err) {
            console.warn("set-block-content: failed to close direct connection:", err);
          }
        }
      }
    },
  );
}
